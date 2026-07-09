package tools

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallerRejectsChecksumMismatch(t *testing.T) {
	archive := MakeTestToolZip(t, ToolFFmpeg, "ffmpeg version test")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer server.Close()

	installer := NewInstaller(t.TempDir(), nil)
	_, err := installer.Install(context.Background(), Source{
		Tool:      ToolFFmpeg,
		Version:   "test",
		URL:       server.URL,
		SHA256:    strings.Repeat("0", 64),
		AllowHTTP: true,
	})
	if err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("checksum 错误应拒绝，实际 %v", err)
	}
}

func TestInstallerRejectsPathTraversalAndLinks(t *testing.T) {
	cases := []struct {
		name    string
		archive []byte
		want    string
	}{
		{name: "zip 路径穿越", archive: makeZip(t, zipEntry{name: "../evil.txt", body: "bad"}), want: "路径"},
		{name: "zip 符号链接", archive: makeZip(t, zipEntry{name: "bin/ffmpeg", body: "../evil", mode: os.ModeSymlink | 0o777}), want: "链接"},
		{name: "tar hardlink", archive: makeTarGz(t, tarEntry{name: "bin/ffmpeg", link: "target", typ: tar.TypeLink}), want: "链接"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(tc.archive)
			}))
			defer server.Close()

			sum := sha256.Sum256(tc.archive)
			installer := NewInstaller(t.TempDir(), nil)
			_, err := installer.Install(context.Background(), Source{
				Tool:      ToolFFmpeg,
				Version:   "test",
				URL:       server.URL,
				SHA256:    hex.EncodeToString(sum[:]),
				AllowHTTP: true,
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("期望拒绝并包含 %q，实际 %v", tc.want, err)
			}
		})
	}
}

func TestInstallerInstallsToolAndDetectsVersion(t *testing.T) {
	archive := MakeTestToolZip(t, ToolFFmpeg, "ffmpeg version test")
	sum := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer server.Close()

	baseDir := t.TempDir()
	installer := NewInstaller(baseDir, nil)
	result, err := installer.Install(context.Background(), Source{
		Tool:      ToolFFmpeg,
		Version:   "test",
		URL:       server.URL,
		SHA256:    hex.EncodeToString(sum[:]),
		AllowHTTP: true,
	})
	if err != nil {
		t.Fatalf("安装应成功: %v", err)
	}
	if result.VersionText != "ffmpeg version test" {
		t.Fatalf("版本探测不正确: %+v", result)
	}
	if !strings.HasPrefix(result.Path, filepath.Join(baseDir, "ffmpeg", "test")) {
		t.Fatalf("工具路径必须位于受控目录内，实际 %s", result.Path)
	}
	if _, err := os.Stat(result.Path); err != nil {
		t.Fatalf("安装后的工具不存在: %v", err)
	}
}

func TestInstallStagedRestoresOldDirWhenFinalRenameFails(t *testing.T) {
	root := t.TempDir()
	stage := filepath.Join(root, "stage")
	final := filepath.Join(root, "tool", "test")
	if err := os.MkdirAll(stage, 0o750); err != nil {
		t.Fatalf("创建 stage 失败: %v", err)
	}
	if err := os.MkdirAll(final, 0o750); err != nil {
		t.Fatalf("创建 final 失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stage, "new.txt"), []byte("new"), 0o640); err != nil {
		t.Fatalf("写入新文件失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(final, "old.txt"), []byte("old"), 0o640); err != nil {
		t.Fatalf("写入旧文件失败: %v", err)
	}

	calls := 0
	errRenameFailed := errors.New("模拟最终切换失败")
	rename := func(oldPath, newPath string) error {
		calls++
		if calls == 3 {
			return errRenameFailed
		}
		return os.Rename(oldPath, newPath)
	}

	err := installStaged(stage, final, rename)
	if !errors.Is(err, errRenameFailed) {
		t.Fatalf("应返回最终切换错误，实际 %v", err)
	}
	if _, err := os.Stat(filepath.Join(final, "old.txt")); err != nil {
		t.Fatalf("安装失败时旧目录必须恢复: %v", err)
	}
	if _, err := os.Stat(filepath.Join(final, "new.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("安装失败时不应留下新目录内容，实际 %v", err)
	}
}

type zipEntry struct {
	name string
	body string
	mode os.FileMode
}

type tarEntry struct {
	name string
	body string
	link string
	typ  byte
}

func MakeTestToolZip(t *testing.T, tool, versionLine string) []byte {
	t.Helper()
	name := tool
	body := "#!/bin/sh\necho '" + versionLine + "'\n"
	mode := os.FileMode(0o755)
	if runtime.GOOS == "windows" {
		name += ".cmd"
		body = "@echo " + versionLine + "\r\n"
		mode = 0o755
	}
	return makeZip(t, zipEntry{name: "bin/" + name, body: body, mode: mode})
}

func makeZip(t *testing.T, entries ...zipEntry) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		if entry.mode != 0 {
			header.SetMode(entry.mode)
		}
		w, err := zw.CreateHeader(header)
		if err != nil {
			t.Fatalf("创建 zip 条目失败: %v", err)
		}
		if _, err := w.Write([]byte(entry.body)); err != nil {
			t.Fatalf("写入 zip 条目失败: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("关闭 zip 失败: %v", err)
	}
	return b.Bytes()
}

func makeTarGz(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var b bytes.Buffer
	gw := gzip.NewWriter(&b)
	tw := tar.NewWriter(gw)
	for _, entry := range entries {
		body := []byte(entry.body)
		header := &tar.Header{Name: entry.name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}
		if entry.typ != 0 {
			header.Typeflag = entry.typ
			header.Linkname = entry.link
			header.Size = 0
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatalf("写入 tar 头失败: %v", err)
		}
		if len(body) > 0 {
			if _, err := tw.Write(body); err != nil {
				t.Fatalf("写入 tar 条目失败: %v", err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("关闭 tar 失败: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("关闭 gzip 失败: %v", err)
	}
	return b.Bytes()
}
