package tools

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/wcpe/JianVideo/internal/netproxy"
)

const maxDownloadBytes int64 = 2 << 30

var errArchiveFormat = errors.New("压缩包格式不匹配")

// Installer 下载、校验并安装外部工具到受控目录。
type Installer struct {
	baseDir string
	client  *http.Client
}

// NewInstaller 创建工具安装器。
func NewInstaller(baseDir string, client *http.Client) *Installer {
	if client == nil {
		client = &http.Client{Transport: &http.Transport{Proxy: netproxy.ProxyFunc}}
	}
	return &Installer{baseDir: baseDir, client: client}
}

// Install 安装指定下载源，并返回可执行文件路径。
func (i *Installer) Install(ctx context.Context, source Source, progressFns ...ProgressFunc) (InstallResult, error) {
	progress := firstProgress(progressFns)
	if err := validateSourceURL(source.URL, source.AllowHTTP); err != nil {
		return InstallResult{}, err
	}
	if !isSHA256(strings.ToLower(source.SHA256)) {
		return InstallResult{}, fmt.Errorf("下载源缺少合法 sha256")
	}
	if err := os.MkdirAll(i.baseDir, 0o750); err != nil {
		return InstallResult{}, fmt.Errorf("创建工具目录失败: %w", err)
	}
	tmpDir, err := os.MkdirTemp(i.baseDir, ".download-*")
	if err != nil {
		return InstallResult{}, fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	archivePath := filepath.Join(tmpDir, "archive")
	if err := i.download(ctx, source, archivePath, progress); err != nil {
		return InstallResult{}, err
	}
	stageDir := filepath.Join(tmpDir, "stage")
	if err := reportStep(progress, "解压中"); err != nil {
		return InstallResult{}, err
	}
	if err := extractArchive(archivePath, stageDir); err != nil {
		return InstallResult{}, err
	}
	if err := reportStep(progress, "探测中"); err != nil {
		return InstallResult{}, err
	}
	exe, err := findExecutable(stageDir, source.Tool)
	if err != nil {
		return InstallResult{}, err
	}
	versionText, err := detectVersion(ctx, exe)
	if err != nil {
		return InstallResult{}, err
	}
	rel, err := filepath.Rel(stageDir, exe)
	if err != nil {
		return InstallResult{}, fmt.Errorf("解析工具路径失败: %w", err)
	}
	if err := reportStep(progress, "安装中"); err != nil {
		return InstallResult{}, err
	}
	version := safePathPart(source.Version)
	finalDir := filepath.Join(i.baseDir, safePathPart(source.Tool), version)
	if err := os.MkdirAll(filepath.Dir(finalDir), 0o750); err != nil {
		return InstallResult{}, fmt.Errorf("创建工具父目录失败: %w", err)
	}
	if err := installStaged(stageDir, finalDir, os.Rename); err != nil {
		return InstallResult{}, fmt.Errorf("安装工具失败: %w", err)
	}
	path := filepath.Join(finalDir, rel)
	return InstallResult{Tool: source.Tool, Path: path, Version: source.Version, VersionText: versionText}, nil
}

func (i *Installer) download(ctx context.Context, source Source, dst string, progress ProgressFunc) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
	if err != nil {
		return fmt.Errorf("构造下载请求失败: %w", err)
	}
	resp, err := i.client.Do(req)
	if err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("下载源返回 HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > maxDownloadBytes {
		return fmt.Errorf("下载文件超过大小上限")
	}
	// #nosec G304 -- dst 位于 Install 创建的受控临时目录内。
	file, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("创建下载文件失败: %w", err)
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	writer := io.MultiWriter(file, hash)
	if err := copyWithProgress(resp.Body, writer, resp.ContentLength, progress); err != nil {
		return err
	}
	got := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(got, source.SHA256) {
		return fmt.Errorf("sha256 校验失败")
	}
	return nil
}

func copyWithProgress(r io.Reader, w io.Writer, total int64, progress ProgressFunc) error {
	buf := make([]byte, 64*1024)
	var downloaded int64
	for {
		n, readErr := r.Read(buf)
		if n > 0 {
			downloaded += int64(n)
			if downloaded > maxDownloadBytes {
				return fmt.Errorf("下载文件超过大小上限")
			}
			if _, err := w.Write(buf[:n]); err != nil {
				return fmt.Errorf("写入下载文件失败: %w", err)
			}
			if progress != nil {
				if err := progress(downloaded, total, "下载中"); err != nil {
					return err
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return fmt.Errorf("读取下载响应失败: %w", readErr)
		}
	}
	return nil
}

func extractArchive(archivePath, dst string) error {
	if err := os.MkdirAll(dst, 0o750); err != nil {
		return fmt.Errorf("创建解压目录失败: %w", err)
	}
	if err := extractZip(archivePath, dst); err == nil {
		return nil
	} else if !errors.Is(err, errArchiveFormat) {
		return err
	}
	if err := extractTarGz(archivePath, dst); err == nil {
		return nil
	} else if !errors.Is(err, errArchiveFormat) {
		return err
	}
	return fmt.Errorf("不支持的工具压缩包格式")
}

func extractZip(archivePath, dst string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return errArchiveFormat
	}
	defer func() { _ = zr.Close() }()
	for _, file := range zr.File {
		mode := file.FileInfo().Mode()
		if mode&os.ModeSymlink != 0 {
			return fmt.Errorf("拒绝解压链接条目")
		}
		target, err := safeJoin(dst, file.Name)
		if err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o750); err != nil {
				return fmt.Errorf("创建目录失败: %w", err)
			}
			continue
		}
		if !mode.IsRegular() {
			return fmt.Errorf("拒绝解压非普通文件")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return fmt.Errorf("创建目录失败: %w", err)
		}
		src, err := file.Open()
		if err != nil {
			return fmt.Errorf("打开 zip 条目失败: %w", err)
		}
		if err := writeFile(target, src, mode.Perm()); err != nil {
			_ = src.Close()
			return err
		}
		_ = src.Close()
	}
	return nil
}

func extractTarGz(archivePath, dst string) error {
	// #nosec G304 -- archivePath 位于 Install 创建的受控临时目录内。
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return errArchiveFormat
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if header.Typeflag == tar.TypeSymlink || header.Typeflag == tar.TypeLink {
			return fmt.Errorf("拒绝解压链接条目")
		}
		target, err := safeJoin(dst, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o750); err != nil {
				return fmt.Errorf("创建目录失败: %w", err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
				return fmt.Errorf("创建目录失败: %w", err)
			}
			if err := writeTarFile(target, tr, header.Mode); err != nil {
				return err
			}
		default:
			return fmt.Errorf("拒绝解压非普通文件")
		}
	}
}

func safeJoin(root, entry string) (string, error) {
	entry = filepath.Clean(filepath.FromSlash(entry))
	if entry == "." || entry == "" || filepath.IsAbs(entry) || strings.HasPrefix(entry, ".."+string(filepath.Separator)) || entry == ".." {
		return "", fmt.Errorf("压缩包条目路径不安全")
	}
	target := filepath.Join(root, entry)
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("压缩包条目路径不安全")
	}
	return target, nil
}

func writeFile(path string, src io.Reader, mode os.FileMode) error {
	perm := mode.Perm()
	if perm == 0 {
		perm = 0o640
	}
	if perm&0o111 != 0 {
		perm = 0o750
	} else {
		perm = 0o640
	}
	// #nosec G304 -- path 已由 safeJoin 校验在受控解压目录内。
	dst, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("创建文件失败: %w", err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return fmt.Errorf("写入文件失败: %w", err)
	}
	if err := dst.Close(); err != nil {
		return fmt.Errorf("关闭文件失败: %w", err)
	}
	return os.Chmod(path, perm)
}

func writeTarFile(path string, src io.Reader, mode int64) error {
	perm := os.FileMode(0o640)
	if mode&0o111 != 0 {
		perm = 0o750
	}
	return writeFile(path, src, perm)
}

func findExecutable(root, tool string) (string, error) {
	names := executableNames(tool)
	var found string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name := strings.ToLower(entry.Name())
		for _, candidate := range names {
			if name == candidate {
				found = path
				return filepath.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("查找工具失败: %w", err)
	}
	if found == "" {
		return "", fmt.Errorf("压缩包内未找到工具可执行文件")
	}
	return found, nil
}

func executableNames(tool string) []string {
	return []string{tool, tool + ".exe", tool + ".cmd", tool + ".bat"}
}

func detectVersion(ctx context.Context, path string) (string, error) {
	detectCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	cmd := exec.CommandContext(detectCtx, path, "-version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("工具版本探测失败: %w", err)
	}
	line := strings.TrimSpace(strings.SplitN(string(output), "\n", 2)[0])
	line = strings.TrimRight(line, "\r")
	if line == "" {
		return "", fmt.Errorf("工具版本探测结果为空")
	}
	return line, nil
}

func safePathPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "unknown"
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "..", "_")
	return replacer.Replace(value)
}

func firstProgress(fns []ProgressFunc) ProgressFunc {
	if len(fns) == 0 {
		return nil
	}
	return fns[0]
}

func reportStep(progress ProgressFunc, step string) error {
	if progress == nil {
		return nil
	}
	return progress(0, 0, step)
}

func installStaged(stageDir, finalDir string, rename func(string, string) error) error {
	if _, err := os.Stat(finalDir); errors.Is(err, os.ErrNotExist) {
		return rename(stageDir, finalDir)
	} else if err != nil {
		return err
	}

	suffix := fmt.Sprintf(".%d", time.Now().UnixNano())
	replacement := finalDir + ".new" + suffix
	backup := finalDir + ".old" + suffix
	if err := rename(stageDir, replacement); err != nil {
		return err
	}
	if err := rename(finalDir, backup); err != nil {
		_ = os.RemoveAll(replacement)
		return err
	}
	if err := rename(replacement, finalDir); err != nil {
		_ = rename(backup, finalDir)
		_ = os.RemoveAll(replacement)
		return err
	}
	_ = os.RemoveAll(backup)
	return nil
}

func defaultExecutableName(tool string) string {
	if runtime.GOOS == "windows" {
		return tool + ".exe"
	}
	return tool
}
