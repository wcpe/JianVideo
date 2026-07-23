package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
)

func newLibraryAuditTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}, &models.MediaExtension{}, &models.AuditEvent{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	return NewService(db).WithAudit(audit.NewRecorder(db)), db
}

func TestLibraryCreateUpdateDeleteRecordsAuditEvents(t *testing.T) {
	svc, db := newLibraryAuditTestService(t)
	dir := t.TempDir()

	lp, err := svc.CreateLibraryPathInSpace(models.DefaultSpaceID, dir, "local", "审计库")
	if err != nil {
		t.Fatalf("创建媒体库失败: %v", err)
	}
	label := "审计库更新"
	enabled := false
	if _, err := svc.UpdateLibraryPathInSpace(models.DefaultSpaceID, lp.ID, &label, &enabled); err != nil {
		t.Fatalf("更新媒体库失败: %v", err)
	}
	if err := svc.DeleteLibraryPathInSpace(models.DefaultSpaceID, lp.ID); err != nil {
		t.Fatalf("删除媒体库失败: %v", err)
	}

	assertAuditCount(t, db, "library.created", 1)
	assertAuditCount(t, db, "library.updated", 1)
	assertAuditCount(t, db, "library.deleted", 1)
}

func TestMediaDeleteRestoreRenameRecordsAuditEvents(t *testing.T) {
	svc, db := newLibraryAuditTestService(t)
	dir := t.TempDir()
	lp, _ := svc.CreateLibraryPathInSpace(models.DefaultSpaceID, dir, "local", "媒体库")
	file := filepath.Join(dir, "old.mp4")
	if err := os.WriteFile(file, []byte("video"), 0o600); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}
	mf, _ := svc.CreateMediaFileInSpace(models.DefaultSpaceID, lp.ID, file, 5)

	renamed, err := svc.RenameMediaFileInSpace(models.DefaultSpaceID, mf.ID, "new.mp4")
	if err != nil {
		t.Fatalf("重命名失败: %v", err)
	}
	if err := svc.DeleteMediaFileInSpace(models.DefaultSpaceID, renamed.ID); err != nil {
		t.Fatalf("软删失败: %v", err)
	}
	if err := svc.RestoreMediaFileInSpace(models.DefaultSpaceID, renamed.ID); err != nil {
		t.Fatalf("还原失败: %v", err)
	}

	assertAuditCount(t, db, "media.renamed", 1)
	assertAuditCount(t, db, "media.deleted", 1)
	assertAuditCount(t, db, "media.restored", 1)
}

func TestMediaMoveRecordsAuditEvent(t *testing.T) {
	svc, db := newLibraryAuditTestService(t)
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	dstDir := filepath.Join(root, "dst")
	if err := os.MkdirAll(srcDir, 0o700); err != nil {
		t.Fatalf("创建源目录失败: %v", err)
	}
	if err := os.MkdirAll(dstDir, 0o700); err != nil {
		t.Fatalf("创建目标目录失败: %v", err)
	}
	lp, _ := svc.CreateLibraryPathInSpace(models.DefaultSpaceID, root, "local", "媒体库")
	file := filepath.Join(srcDir, "clip.mp4")
	if err := os.WriteFile(file, []byte("video"), 0o600); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}
	mf, _ := svc.CreateMediaFileInSpace(models.DefaultSpaceID, lp.ID, file, 5)

	moved, err := svc.MoveMediaFileInSpace(models.DefaultSpaceID, mf.ID, dstDir)
	if err != nil {
		t.Fatalf("移动媒体失败: %v", err)
	}
	if moved.FilePath != filepath.ToSlash(filepath.Join(dstDir, "clip.mp4")) {
		t.Fatalf("媒体路径未更新: %+v", moved)
	}

	assertAuditCount(t, db, "media.moved", 1)
}

func TestMetadataWritebackRecordsAuditEvents(t *testing.T) {
	svc, db := newLibraryAuditTestService(t)
	orig := enrichMediaMetadataFn
	t.Cleanup(func() { enrichMediaMetadataFn = orig })
	enrichMediaMetadataFn = func(mf *models.MediaFile) {
		mf.Duration = 12.5
		mf.VideoCodec = "h264"
		mf.Width = 1920
		mf.Height = 1080
	}
	dir := t.TempDir()
	lp, _ := svc.CreateLibraryPathInSpace(models.DefaultSpaceID, dir, "local", "媒体库")
	file := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(file, []byte("video"), 0o600); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}
	mf, _ := svc.CreateMediaFileInSpace(models.DefaultSpaceID, lp.ID, file, 5)

	got, err := svc.WritebackMediaMetadataInSpace(models.DefaultSpaceID, mf.ID)
	if err != nil {
		t.Fatalf("回写元数据失败: %v", err)
	}
	if got.Duration != 12.5 || got.VideoCodec != "h264" || got.Width != 1920 || got.Height != 1080 {
		t.Fatalf("元数据未回写: %+v", got)
	}

	assertAuditCount(t, db, "metadata.writeback.started", 1)
	assertAuditCount(t, db, "metadata.writeback.succeeded", 1)
}

func TestMetadataWritebackFailureRecordsAuditEvent(t *testing.T) {
	svc, db := newLibraryAuditTestService(t)
	mf := models.MediaFile{
		SpaceID:   models.DefaultSpaceID,
		LibraryID: 1,
		FilePath:  filepath.ToSlash(filepath.Join(t.TempDir(), "missing.mp4")),
		FileName:  "missing.mp4",
		AddedAt:   time.Now(),
	}
	if err := db.Create(&mf).Error; err != nil {
		t.Fatalf("预置媒体失败: %v", err)
	}

	if _, err := svc.WritebackMediaMetadataInSpace(models.DefaultSpaceID, mf.ID); err == nil {
		t.Fatal("源文件缺失时应回写失败")
	}

	assertAuditCount(t, db, "metadata.writeback.started", 1)
	assertAuditCount(t, db, "metadata.writeback.failed", 1)
}

func TestMediaDeleteRollsBackWhenAuditFails(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.MediaFile{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	svc := NewService(db).WithAudit(failingLibraryAudit{})
	mf := models.MediaFile{SpaceID: models.DefaultSpaceID, LibraryID: 1, FilePath: "D:/a.mp4", FileName: "a.mp4", AddedAt: time.Now()}
	if err := db.Create(&mf).Error; err != nil {
		t.Fatalf("预置媒体失败: %v", err)
	}

	if err := svc.DeleteMediaFileInSpace(models.DefaultSpaceID, mf.ID); err == nil {
		t.Fatal("审计失败时软删应失败")
	}
	var got models.MediaFile
	if err := db.First(&got, mf.ID).Error; err != nil {
		t.Fatalf("读取媒体失败: %v", err)
	}
	if got.DeletedAt != nil {
		t.Fatal("审计失败时 deleted_at 应回滚")
	}
}

type failingLibraryAudit struct{}

func (f failingLibraryAudit) Record(context.Context, audit.EventInput) error {
	return errors.New("审计写入失败")
}

func (f failingLibraryAudit) RecordTx(context.Context, *gorm.DB, audit.EventInput) error {
	return errors.New("审计写入失败")
}

func (f failingLibraryAudit) List(context.Context, audit.Query) (audit.Page, error) {
	return audit.Page{}, nil
}

func (f failingLibraryAudit) GetByID(context.Context, int64) (*models.AuditEvent, error) {
	return nil, errors.New("审计写入失败")
}

func assertAuditCount(t *testing.T, db *gorm.DB, action string, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(&models.AuditEvent{}).Where("action = ?", action).Count(&count).Error; err != nil {
		t.Fatalf("统计审计事件失败: %v", err)
	}
	if count != want {
		t.Fatalf("%s 审计事件数量 got=%d want=%d", action, count, want)
	}
}
