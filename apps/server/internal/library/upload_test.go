package library

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// TestNormalizeUploadNamingRule 仅 date 视为整齐归档，其余回退保留原样。
func TestNormalizeUploadNamingRule(t *testing.T) {
	cases := map[string]string{
		UploadNamingDate:     UploadNamingDate,
		UploadNamingOriginal: UploadNamingOriginal,
		"":                   UploadNamingOriginal,
		"unknown":            UploadNamingOriginal,
	}
	for in, want := range cases {
		if got := NormalizeUploadNamingRule(in); got != want {
			t.Errorf("NormalizeUploadNamingRule(%q)=%q, 期望 %q", in, got, want)
		}
	}
}

// TestResolveUploadLibrary_WithinLibrary 目标目录在启用本地库内时命中归属库。
func TestResolveUploadLibrary_WithinLibrary(t *testing.T) {
	paths := []models.LibraryPath{
		{ID: 1, Path: "D:/media/photos", Type: "local", Enabled: 1},
		{ID: 2, Path: "E:/videos", Type: "local", Enabled: 1},
	}

	// 子目录命中
	lp, dir, err := ResolveUploadLibrary(paths, "D:/media/photos/2024/06")
	if err != nil {
		t.Fatalf("期望命中库，得到错误: %v", err)
	}
	if lp.ID != 1 {
		t.Errorf("期望命中库 1，实际 %d", lp.ID)
	}
	if dir != "D:/media/photos/2024/06" {
		t.Errorf("规范化目录不符: %q", dir)
	}

	// 库根目录自身命中
	if _, _, err := ResolveUploadLibrary(paths, "E:/videos"); err != nil {
		t.Errorf("库根目录应命中，得到错误: %v", err)
	}
}

// TestResolveUploadLibrary_Rejections 目录在库外、越权逃逸、禁用库、SMB 库均拒绝。
func TestResolveUploadLibrary_Rejections(t *testing.T) {
	paths := []models.LibraryPath{
		{ID: 1, Path: "D:/media/photos", Type: "local", Enabled: 1},
		{ID: 2, Path: "F:/disabled", Type: "local", Enabled: 0},
		{ID: 3, Path: "host/share", Type: "smb", Enabled: 1},
	}

	rejects := []string{
		"D:/other",                     // 库外
		"D:/media/photos/../../escape", // .. 逃逸到库外
		"F:/disabled/sub",              // 禁用库
		"host/share/sub",               // SMB 库不接受本地落盘
		"D:/media/photosX",             // 前缀相同但非子目录
		"",                             // 空
	}
	for _, target := range rejects {
		if _, _, err := ResolveUploadLibrary(paths, target); !errors.Is(err, ErrUploadTargetNotInLibrary) {
			t.Errorf("目标 %q 应被拒绝，实际 err=%v", target, err)
		}
	}
}

// TestBuildUploadPath_Original 保留原样：直接落目标目录。
func TestBuildUploadPath_Original(t *testing.T) {
	now := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)
	got, err := BuildUploadPath("D:/media/photos", "IMG_001.jpg", UploadNamingOriginal, now)
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if got != "D:/media/photos/IMG_001.jpg" {
		t.Errorf("保留原样路径不符: %q", got)
	}
}

// TestBuildUploadPath_Date 按规则整齐归档：分 YYYY/MM 子目录。
func TestBuildUploadPath_Date(t *testing.T) {
	now := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)
	got, err := BuildUploadPath("D:/media/photos", "IMG_001.jpg", UploadNamingDate, now)
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if got != "D:/media/photos/2024/06/IMG_001.jpg" {
		t.Errorf("整齐归档路径不符: %q", got)
	}
}

// TestBuildUploadPath_RejectsTraversalName 文件名中的路径片段被剥离，防逃逸。
func TestBuildUploadPath_RejectsTraversalName(t *testing.T) {
	now := time.Now()
	// 含路径前缀的名仅取最后一段，落在 baseDir 内
	got, err := BuildUploadPath("D:/media/photos", "../../etc/evil.png", UploadNamingOriginal, now)
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if got != "D:/media/photos/evil.png" {
		t.Errorf("路径片段未被剥离: %q", got)
	}

	// 纯非法名拒绝
	if _, err := BuildUploadPath("D:/media/photos", "..", UploadNamingOriginal, now); !errors.Is(err, ErrInvalidUploadName) {
		t.Errorf("纯非法名应拒绝，实际 err=%v", err)
	}
}

// TestResolveUploadConflict 重名时按 名(N).ext 递增避让。
func TestResolveUploadConflict(t *testing.T) {
	// 无冲突直接返回原路径
	got, err := ResolveUploadConflict("D:/m/a.jpg", func(string) bool { return false })
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if got != "D:/m/a.jpg" {
		t.Errorf("无冲突应返回原路径，实际 %q", got)
	}

	// 原名与 (1) 均占用，落到 (2)
	occupied := map[string]bool{
		"D:/m/a.jpg":    true,
		"D:/m/a(1).jpg": true,
	}
	got, err = ResolveUploadConflict("D:/m/a.jpg", func(p string) bool { return occupied[p] })
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if got != "D:/m/a(2).jpg" {
		t.Errorf("冲突避让路径不符: %q", got)
	}
}

// TestSaveUploadFile 真实写盘：内容写入目标，自动创建子目录，临时文件清理。
func TestSaveUploadFile(t *testing.T) {
	root := t.TempDir()
	dest := filepath.ToSlash(filepath.Join(root, "2024", "06", "x.jpg"))

	content := "hello-upload"
	if err := SaveUploadFile(dest, strings.NewReader(content)); err != nil {
		t.Fatalf("写盘失败: %v", err)
	}

	got, err := os.ReadFile(filepath.FromSlash(dest))
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if string(got) != content {
		t.Errorf("内容不符: %q", string(got))
	}
	// 临时文件不应残留
	if _, err := os.Stat(filepath.FromSlash(dest) + ".part"); !os.IsNotExist(err) {
		t.Errorf("临时文件未清理")
	}
}
