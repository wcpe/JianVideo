package update

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRollbackAvailableAt 校验「是否可回滚」判定：仅当 .old 备份存在时为真（FIX-2）。
func TestRollbackAvailableAt(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "app.exe")
	if err := os.WriteFile(exe, []byte("cur"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rollbackAvailableAt(exe) {
		t.Error("无 .old 备份时应不可回滚")
	}
	if err := os.WriteFile(exe+oldSuffix, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !rollbackAvailableAt(exe) {
		t.Error("有 .old 备份时应可回滚")
	}
}

// TestLandBinary_PersistsNewContent 复现「重启后失效」根因：
// 落地后 exePath 的磁盘内容必须等于新二进制内容（持久化正确），
// 且旧内容应备份到 .old。这是自更新「重启仍为新版」的前提。
func TestLandBinary_PersistsNewContent(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "jianvideo.exe")
	if err := os.WriteFile(exe, []byte("OLD-BINARY"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 新二进制放在另一临时目录，模拟 os.MkdirTemp 下载落点
	srcDir := t.TempDir()
	newPath := filepath.Join(srcDir, "jianvideo-windows-amd64")
	if err := os.WriteFile(newPath, []byte("NEW-BINARY"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := landBinary(exe, newPath); err != nil {
		t.Fatalf("landBinary 失败: %v", err)
	}

	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatalf("读取落地后 exe 失败: %v", err)
	}
	if string(got) != "NEW-BINARY" {
		t.Fatalf("落地后 exe 内容应为新二进制，得到 %q", string(got))
	}
	// 旧版应备份到 .old，供回滚
	oldBytes, err := os.ReadFile(exe + oldSuffix)
	if err != nil {
		t.Fatalf("应存在 .old 备份: %v", err)
	}
	if string(oldBytes) != "OLD-BINARY" {
		t.Fatalf(".old 应保存旧二进制，得到 %q", string(oldBytes))
	}
}

// TestCopyAndRemove_FlushesAndRemovesSource 复现根因：跨卷退化复制路径必须把内容完整
// 落地到目标、且成功删除源。Windows 下若删除源前未关闭源句柄，os.Remove 会因「文件被占用」
// 失败，导致整体落地失败、上层回退到旧二进制（在线更新重启后失效）。
// 直测 copyAndRemove（不依赖具体卷）以确定性地走复制 + 删除分支。
func TestCopyAndRemove_FlushesAndRemovesSource(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src.bin")
	dst := filepath.Join(t.TempDir(), "dst.bin")
	content := "HELLO-NEW-BINARY-CONTENT"
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyAndRemove(src, dst); err != nil {
		t.Fatalf("copyAndRemove 失败: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("读取目标失败: %v", err)
	}
	if string(got) != content {
		t.Fatalf("目标内容不匹配，得到 %q", string(got))
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("移动后源文件应被删除，得到 stat err=%v", err)
	}
}
