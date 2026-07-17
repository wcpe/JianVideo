//go:build windows

package library

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/wcpe/JianVideo/internal/db/models"
	"gorm.io/gorm"
)

func createWindowsJunction(t *testing.T, junction, target string) {
	t.Helper()
	cmd := exec.Command("cmd.exe", "/c", "mklink", "/J", junction, target)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("创建 Windows junction 失败: %v, output=%s", err, output)
	}
}

func assertRecycleBoundaryRejected(
	t *testing.T,
	svc *Service,
	db *gorm.DB,
	drive, recycleRoot, source string,
	media *models.MediaFile,
) {
	t.Helper()
	result, err := svc.CleanupRecycle(map[string]string{drive: filepath.ToSlash(recycleRoot)})
	if err == nil || result.Moved != 0 || result.Failed != 1 {
		t.Fatalf("不安全回收站边界必须拒绝并报告残余: result=%+v err=%v", result, err)
	}
	if got, readErr := os.ReadFile(source); readErr != nil || string(got) != "原始媒体" {
		t.Fatalf("拒绝不安全边界后源文件必须保留: content=%q err=%v", got, readErr)
	}
	var count int64
	if err := db.Model(&models.MediaFile{}).Where("space_id = ? AND id = ?", media.SpaceID, media.ID).Count(&count).Error; err != nil {
		t.Fatalf("统计数据库记录失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("拒绝不安全边界后数据库记录必须保留, 实际 %d", count)
	}
}

func TestCleanupRecycle_RejectsWindowsRootJunction(t *testing.T) {
	svc, db := newTestService(t)
	srcDir := t.TempDir()
	source := filepath.Join(srcDir, "root-junction.mkv")
	if err := os.WriteFile(source, []byte("原始媒体"), 0o644); err != nil {
		t.Fatalf("写源文件失败: %v", err)
	}
	drive := driveOfPath(filepath.ToSlash(source))
	if drive == "" {
		t.Fatal("Windows 临时目录必须具有盘符")
	}
	deletedAt := time.Date(2026, 7, 16, 10, 0, 0, 0, time.Local)
	media := createSoftDeletedMedia(t, svc, 1, filepath.ToSlash(source), deletedAt)
	outside := t.TempDir()
	recycleRoot := filepath.Join(srcDir, "recycle-root-junction")
	createWindowsJunction(t, recycleRoot, outside)

	assertRecycleBoundaryRejected(t, svc, db, drive, recycleRoot, source, media)
	outsideTarget := filepath.Join(outside, deletedAt.Format("2006-01-02"), filepath.Base(expectedRecycleTarget(recycleRoot, media)))
	if _, err := os.Lstat(outsideTarget); !os.IsNotExist(err) {
		t.Fatalf("拒绝 root junction 后外部目录不得产生目标: %v", err)
	}
}

func TestCleanupRecycle_RejectsWindowsDateDirectoryJunction(t *testing.T) {
	svc, db := newTestService(t)
	srcDir := t.TempDir()
	source := filepath.Join(srcDir, "date-junction.mkv")
	if err := os.WriteFile(source, []byte("原始媒体"), 0o644); err != nil {
		t.Fatalf("写源文件失败: %v", err)
	}
	drive := driveOfPath(filepath.ToSlash(source))
	if drive == "" {
		t.Fatal("Windows 临时目录必须具有盘符")
	}
	deletedAt := time.Date(2026, 7, 16, 11, 0, 0, 0, time.Local)
	media := createSoftDeletedMedia(t, svc, 1, filepath.ToSlash(source), deletedAt)
	recycleRoot := filepath.Join(srcDir, "recycle-root")
	if err := os.Mkdir(recycleRoot, 0o750); err != nil {
		t.Fatalf("创建回收站根目录失败: %v", err)
	}
	outside := t.TempDir()
	dateDir := filepath.Join(recycleRoot, deletedAt.Format("2006-01-02"))
	createWindowsJunction(t, dateDir, outside)

	assertRecycleBoundaryRejected(t, svc, db, drive, recycleRoot, source, media)
	outsideTarget := filepath.Join(outside, filepath.Base(expectedRecycleTarget(recycleRoot, media)))
	if _, err := os.Lstat(outsideTarget); !os.IsNotExist(err) {
		t.Fatalf("拒绝日期目录 junction 后外部目录不得产生目标: %v", err)
	}
}
