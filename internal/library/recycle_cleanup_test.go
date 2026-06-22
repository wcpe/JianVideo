package library

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jianvideo/internal/db/models"
)

// createSoftDeletedMedia 在指定库内创建一条媒体记录并立即软删（deleted_at=指定时刻）。
// 返回入库后的记录，filePath 直接落库（已正斜杠化）。
func createSoftDeletedMedia(t *testing.T, svc *Service, libraryID int64, filePath string, deletedAt time.Time) *models.MediaFile {
	t.Helper()
	mf, err := svc.CreateMediaFile(libraryID, filePath, 100)
	if err != nil {
		t.Fatalf("创建媒体失败: %v", err)
	}
	if err := svc.db.Model(&models.MediaFile{}).Where("id = ?", mf.ID).Update("deleted_at", deletedAt).Error; err != nil {
		t.Fatalf("软删媒体失败: %v", err)
	}
	mf.DeletedAt = &deletedAt
	return mf
}

// TestCleanupRecycle_UnsetPathRejected 软删项所在盘符未配置回收站路径时，整体拒绝、不移动不删除。
func TestCleanupRecycle_UnsetPathRejected(t *testing.T) {
	svc, _ := newTestService(t)

	// 在临时目录里造一个真实源文件，软删它
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "误删.mp4")
	if err := os.WriteFile(srcFile, []byte("data"), 0o644); err != nil {
		t.Fatalf("写源文件失败: %v", err)
	}
	srcSlash := filepath.ToSlash(srcFile)
	mf := createSoftDeletedMedia(t, svc, 1, srcSlash, time.Now())

	// 传入一个不含该盘符（或为空）的配置映射 → 期望拒绝
	_, err := svc.CleanupRecycle(map[string]string{})
	if !errors.Is(err, ErrRecycleBinPathUnset) {
		t.Fatalf("期望 ErrRecycleBinPathUnset, 实际 %v", err)
	}

	// 源文件仍在原位
	if _, statErr := os.Stat(srcFile); statErr != nil {
		t.Fatalf("拒绝清理后源文件应仍在原位, 实际: %v", statErr)
	}
	// 记录仍在（仍可在回收站查到）
	deleted, _ := svc.ListDeletedMediaFiles()
	if len(deleted) != 1 || deleted[0].ID != mf.ID {
		t.Fatalf("拒绝清理后软删记录应仍在, 实际: %+v", deleted)
	}
}

// TestCleanupRecycle_MovesFileAndDeletesRecord 配置盘符路径后，源文件移动到回收站按删除日期分目录、记录被删。
func TestCleanupRecycle_MovesFileAndDeletesRecord(t *testing.T) {
	svc, _ := newTestService(t)

	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "片子.mkv")
	if err := os.WriteFile(srcFile, []byte("hello"), 0o644); err != nil {
		t.Fatalf("写源文件失败: %v", err)
	}
	srcSlash := filepath.ToSlash(srcFile)
	// 解析出盘符（Windows 为盘符字母，类 Unix 临时目录无盘符则跳过此用例）
	drive := driveOfPath(srcSlash)
	if drive == "" {
		t.Skip("当前平台临时目录无盘符，跳过真实移动用例")
	}

	deletedAt := time.Date(2026, 6, 22, 10, 0, 0, 0, time.Local)
	mf := createSoftDeletedMedia(t, svc, 1, srcSlash, deletedAt)

	// 回收站目录配在同盘符下的临时目录（保证同卷可 Rename）
	recycleDir := filepath.Join(srcDir, ".recycle")
	result, err := svc.CleanupRecycle(map[string]string{drive: filepath.ToSlash(recycleDir)})
	if err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if result.Moved != 1 || result.Failed != 0 {
		t.Fatalf("期望 moved=1 failed=0, 实际 %+v", result)
	}

	// 源文件已移走
	if _, statErr := os.Stat(srcFile); !os.IsNotExist(statErr) {
		t.Fatalf("清理后源文件应已移走, 实际 stat err=%v", statErr)
	}
	// 目标按删除日期分目录存在
	dest := filepath.Join(recycleDir, "2026-06-22", "片子.mkv")
	if _, statErr := os.Stat(dest); statErr != nil {
		t.Fatalf("目标文件应存在于 %s, 实际: %v", dest, statErr)
	}
	// 记录被删（回收站清空）
	deleted, _ := svc.ListDeletedMediaFiles()
	if len(deleted) != 0 {
		t.Fatalf("清理后回收站应为空, 实际还剩 %d 条", len(deleted))
	}
	_ = mf
}

// TestCleanupRecycle_EmptyRecycle 回收站为空时返回零结果且不报错。
func TestCleanupRecycle_EmptyRecycle(t *testing.T) {
	svc, _ := newTestService(t)
	result, err := svc.CleanupRecycle(map[string]string{"D": "D:/.recycle"})
	if err != nil {
		t.Fatalf("空回收站不应报错, 实际: %v", err)
	}
	if result.Moved != 0 || result.Failed != 0 {
		t.Fatalf("空回收站期望零结果, 实际 %+v", result)
	}
}

// TestCleanupRecycle_SMBRejected SMB 软删项无盘符，整体拒绝清理。
func TestCleanupRecycle_SMBRejected(t *testing.T) {
	svc, _ := newTestService(t)
	createSoftDeletedMedia(t, svc, 1, "smb://host/share/远程.mp4", time.Now())

	_, err := svc.CleanupRecycle(map[string]string{"D": "D:/.recycle"})
	if !errors.Is(err, ErrRecycleBinPathUnset) {
		t.Fatalf("SMB 项无盘符应拒绝清理, 实际 %v", err)
	}
}

// TestDriveOfPath 盘符解析纯函数：Windows 盘符路径取盘符字母，SMB/无盘符返回空。
func TestDriveOfPath(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"D:/a/b.mp4", "D"},
		{"c:/x.png", "C"},
		{"smb://host/share/x.mp4", ""},
		{"/home/user/x.mp4", ""},
		{"relative/x.mp4", ""},
	}
	for _, c := range cases {
		if got := driveOfPath(c.path); got != c.want {
			t.Errorf("driveOfPath(%q)=%q, 期望 %q", c.path, got, c.want)
		}
	}
}

// TestUniqueDestPath 目标已存在同名文件时加序号避免覆盖。
func TestUniqueDestPath(t *testing.T) {
	dir := t.TempDir()
	name := "a.mp4"
	first := uniqueDestPath(dir, name)
	if filepath.Base(first) != name {
		t.Fatalf("首个目标应为原名, 实际 %s", filepath.Base(first))
	}
	// 占位首个目标后，应让出带序号的名字
	if err := os.WriteFile(first, []byte("x"), 0o644); err != nil {
		t.Fatalf("占位失败: %v", err)
	}
	second := uniqueDestPath(dir, name)
	if second == first || !strings.Contains(filepath.Base(second), "a") {
		t.Fatalf("第二个目标应换名避免覆盖, 实际 %s", filepath.Base(second))
	}
}
