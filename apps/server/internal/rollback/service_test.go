package rollback

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/settings"
)

func setupRollbackTest(t *testing.T) (*Service, *settings.Service, *library.Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开库: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(
		&models.Setting{}, &models.AuditEvent{},
		&models.LibraryPath{}, &models.MediaFile{},
	); err != nil {
		t.Fatalf("迁移: %v", err)
	}
	rec := audit.NewRecorder(db)
	set := settings.NewService(db).WithAudit(rec)
	lib := library.NewService(db).WithAudit(rec)
	svc := NewService(rec, set, lib)
	return svc, set, lib, db
}

func TestSettingsUpdated_RollbackRestoresBefore(t *testing.T) {
	svc, set, _, db := setupRollbackTest(t)
	// 初始值
	if err := set.SetMany(map[string]string{settings.KeyScanInterval: "300"}); err != nil {
		t.Fatal(err)
	}
	// 变更
	if err := set.SetMany(map[string]string{settings.KeyScanInterval: "600"}); err != nil {
		t.Fatal(err)
	}
	var event models.AuditEvent
	// 取最新 settings.updated
	if err := db.Where("action = ?", "settings.updated").Order("id DESC").First(&event).Error; err != nil {
		t.Fatalf("找审计: %v", err)
	}
	if err := svc.Apply(context.Background(), ApplyInput{EventID: event.ID, Confirm: true, ActorID: "test"}); err != nil {
		t.Fatalf("回滚失败: %v", err)
	}
	got, err := set.Get(settings.KeyScanInterval)
	if err != nil {
		t.Fatal(err)
	}
	// before 在第二次 SetMany 时是 "300"
	if got != "300" {
		t.Fatalf("回滚后期望 300, 得到 %q", got)
	}
	var applied int64
	if err := db.Model(&models.AuditEvent{}).Where("action = ?", "rollback.applied").Count(&applied).Error; err != nil || applied < 1 {
		t.Fatalf("应写 rollback.applied: n=%d err=%v", applied, err)
	}
}

func TestSettingsUpdated_SensitiveNotRollbackable(t *testing.T) {
	svc, set, _, db := setupRollbackTest(t)
	if err := set.SetMany(map[string]string{settings.KeyNetworkProxy: "http://user:secret@example.com:8080"}); err != nil {
		t.Fatal(err)
	}
	var event models.AuditEvent
	if err := db.Where("action = ?", "settings.updated").Order("id DESC").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	view := svc.annotate(&event)
	if view.Rollbackable {
		t.Fatal("敏感设置应不可回滚")
	}
	if view.ReasonKey != ReasonSensitiveKeys {
		t.Fatalf("reason=%q", view.ReasonKey)
	}
	err := svc.Apply(context.Background(), ApplyInput{EventID: event.ID, Confirm: true})
	if !errors.Is(err, ErrNotRollbackable) {
		t.Fatalf("期望不可回滚: %v", err)
	}
}

func TestSettingsUpdated_MissingBeforeNotRollbackable(t *testing.T) {
	svc, _, _, db := setupRollbackTest(t)
	rec := audit.NewRecorder(db)
	if err := rec.Record(context.Background(), audit.EventInput{
		Scope: audit.ScopeSystem, ActorType: audit.ActorSystem,
		Action: "settings.updated", ResourceType: "settings",
		// 无 Before
		After: map[string]string{settings.KeyScanInterval: "1"},
	}); err != nil {
		t.Fatal(err)
	}
	var event models.AuditEvent
	if err := db.Where("action = ?", "settings.updated").Order("id DESC").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	view := svc.annotate(&event)
	if view.Rollbackable {
		t.Fatal("无 before 应不可回滚")
	}
	if view.ReasonKey != ReasonMissingBefore {
		t.Fatalf("reason=%q", view.ReasonKey)
	}
}

func TestMediaDeleted_RestoreThenSoftDeleteSymmetric(t *testing.T) {
	svc, _, lib, db := setupRollbackTest(t)
	dir := t.TempDir()
	lp, err := lib.CreateLibraryPathInSpace(models.DefaultSpaceID, dir, "local", "lib")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "a.mp4")
	if err := os.WriteFile(file, []byte("vid"), 0o600); err != nil {
		t.Fatal(err)
	}
	mf, err := lib.CreateMediaFileInSpace(models.DefaultSpaceID, lp.ID, file, 3)
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.DeleteMediaFileInSpace(models.DefaultSpaceID, mf.ID); err != nil {
		t.Fatal(err)
	}
	var delEvent models.AuditEvent
	if err := db.Where("action = ? AND resource_id = ?", "media.deleted", strconv.FormatInt(mf.ID, 10)).
		Order("id DESC").First(&delEvent).Error; err != nil {
		t.Fatalf("media.deleted 事件: %v", err)
	}
	// 回滚删除 = 还原
	if err := svc.Apply(context.Background(), ApplyInput{
		EventID: delEvent.ID, SpaceID: models.DefaultSpaceID, Confirm: true, ActorID: "t",
	}); err != nil {
		t.Fatalf("回滚删除: %v", err)
	}
	active, err := lib.GetMediaFileByIDInSpace(models.DefaultSpaceID, mf.ID)
	if err != nil || active.DeletedAt != nil {
		t.Fatalf("应已还原: err=%v deleted=%v", err, active != nil && active.DeletedAt != nil)
	}
	// 找 media.restored 事件
	var restEvent models.AuditEvent
	if err := db.Where("action = ? AND resource_id = ?", "media.restored", strconv.FormatInt(mf.ID, 10)).
		Order("id DESC").First(&restEvent).Error; err != nil {
		t.Fatalf("media.restored: %v", err)
	}
	// 回滚还原 = 再软删
	if err := svc.Apply(context.Background(), ApplyInput{
		EventID: restEvent.ID, SpaceID: models.DefaultSpaceID, Confirm: true, ActorID: "t",
	}); err != nil {
		t.Fatalf("回滚还原: %v", err)
	}
	var again models.MediaFile
	if err := db.First(&again, mf.ID).Error; err != nil {
		t.Fatal(err)
	}
	if again.DeletedAt == nil {
		t.Fatal("再软删后 deleted_at 应非空")
	}
}

func TestApply_RequiresConfirm(t *testing.T) {
	svc, set, _, db := setupRollbackTest(t)
	_ = set.SetMany(map[string]string{settings.KeyScanInterval: "10"})
	var event models.AuditEvent
	_ = db.Where("action = ?", "settings.updated").Order("id DESC").First(&event)
	err := svc.Apply(context.Background(), ApplyInput{EventID: event.ID, Confirm: false})
	if !errors.Is(err, ErrConfirmRequired) {
		t.Fatalf("期望 confirm: %v", err)
	}
}

func TestNotRegisteredAction(t *testing.T) {
	svc, _, _, db := setupRollbackTest(t)
	rec := audit.NewRecorder(db)
	if err := rec.Record(context.Background(), audit.EventInput{
		Scope: audit.ScopeSpace, SpaceID: models.DefaultSpaceID,
		ActorType: audit.ActorSystem, Action: "cache.cleaned",
		ResourceType: "cache", ResourceID: "x",
	}); err != nil {
		t.Fatal(err)
	}
	var event models.AuditEvent
	if err := db.Where("action = ?", "cache.cleaned").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	view := svc.annotate(&event)
	if view.Rollbackable || view.ReasonKey != ReasonNotRegistered {
		t.Fatalf("不可回滚: %+v", view)
	}
}

func TestListRollbackEvents_Annotates(t *testing.T) {
	svc, set, lib, _ := setupRollbackTest(t)
	_ = set.SetMany(map[string]string{settings.KeyScanInterval: "100"})
	dir := t.TempDir()
	lp, _ := lib.CreateLibraryPathInSpace(models.DefaultSpaceID, dir, "local", "l")
	f := filepath.Join(dir, "b.mp4")
	_ = os.WriteFile(f, []byte("x"), 0o600)
	mf, _ := lib.CreateMediaFileInSpace(models.DefaultSpaceID, lp.ID, f, 1)
	_ = lib.DeleteMediaFileInSpace(models.DefaultSpaceID, mf.ID)

	list, err := svc.ListRollbackEvents(context.Background(), models.DefaultSpaceID, false, 30, 50, "")
	if err != nil {
		t.Fatal(err)
	}
	var sawDeleted, sawSettings bool
	for _, it := range list.Items {
		if it.Event.Action == "media.deleted" && it.Rollbackable {
			sawDeleted = true
		}
		if it.Event.Action == "settings.updated" && it.Rollbackable {
			sawSettings = true
		}
	}
	if !sawDeleted {
		t.Fatal("列表应含可回滚 media.deleted")
	}
	if !sawSettings {
		t.Fatal("列表应合并可回滚 settings.updated")
	}
}

func TestMediaRenamed_RollbackRestoresOldName(t *testing.T) {
	svc, _, lib, db := setupRollbackTest(t)
	dir := t.TempDir()
	lp, err := lib.CreateLibraryPathInSpace(models.DefaultSpaceID, dir, "local", "lib")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "old.mp4")
	if err := os.WriteFile(file, []byte("vid"), 0o600); err != nil {
		t.Fatal(err)
	}
	mf, err := lib.CreateMediaFileInSpace(models.DefaultSpaceID, lp.ID, file, 3)
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := lib.RenameMediaFileInSpace(models.DefaultSpaceID, mf.ID, "new.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.FileName != "new.mp4" {
		t.Fatalf("重命名后文件名: %s", renamed.FileName)
	}
	var event models.AuditEvent
	if err := db.Where("action = ? AND resource_id = ?", "media.renamed", strconv.FormatInt(mf.ID, 10)).
		Order("id DESC").First(&event).Error; err != nil {
		t.Fatalf("media.renamed: %v", err)
	}
	view := svc.annotate(&event)
	if !view.Rollbackable {
		t.Fatalf("应可回滚: reason=%q", view.ReasonKey)
	}
	if err := svc.Apply(context.Background(), ApplyInput{
		EventID: event.ID, SpaceID: models.DefaultSpaceID, Confirm: true, ActorID: "t",
	}); err != nil {
		t.Fatalf("回滚重命名: %v", err)
	}
	got, err := lib.GetMediaFileByIDInSpace(models.DefaultSpaceID, mf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.FileName != "old.mp4" {
		t.Fatalf("回滚后文件名期望 old.mp4, 得到 %q", got.FileName)
	}
	if _, err := os.Stat(filepath.Join(dir, "old.mp4")); err != nil {
		t.Fatalf("磁盘应恢复 old.mp4: %v", err)
	}
}

func TestMediaMoved_RollbackRestoresOldDir(t *testing.T) {
	svc, _, lib, db := setupRollbackTest(t)
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	dstDir := filepath.Join(root, "dst")
	if err := os.MkdirAll(srcDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dstDir, 0o700); err != nil {
		t.Fatal(err)
	}
	lp, err := lib.CreateLibraryPathInSpace(models.DefaultSpaceID, root, "local", "lib")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(srcDir, "clip.mp4")
	if err := os.WriteFile(file, []byte("vid"), 0o600); err != nil {
		t.Fatal(err)
	}
	mf, err := lib.CreateMediaFileInSpace(models.DefaultSpaceID, lp.ID, file, 3)
	if err != nil {
		t.Fatal(err)
	}
	oldPathSlash := filepath.ToSlash(file)
	moved, err := lib.MoveMediaFileInSpace(models.DefaultSpaceID, mf.ID, dstDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(filepath.ToSlash(moved.FilePath), "/dst/") {
		t.Fatalf("应在 dst: %s", moved.FilePath)
	}
	var event models.AuditEvent
	if err := db.Where("action = ? AND resource_id = ?", "media.moved", strconv.FormatInt(mf.ID, 10)).
		Order("id DESC").First(&event).Error; err != nil {
		t.Fatalf("media.moved: %v", err)
	}
	// FR2-040 会脱敏 C:/Users/<name>/；本测写入未脱敏 before 以验证 reverter 逻辑本身。
	// 真实部署若 before 已被脱敏 → path_redacted（见 TestMediaMoved_PathRedacted）。
	event.BeforeJSON = `{"id":` + strconv.FormatInt(mf.ID, 10) +
		`,"file_path":` + strconv.Quote(oldPathSlash) +
		`,"file_name":"clip.mp4"}`
	if err := db.Save(&event).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.Apply(context.Background(), ApplyInput{
		EventID: event.ID, SpaceID: models.DefaultSpaceID, Confirm: true, ActorID: "t",
	}); err != nil {
		t.Fatalf("回滚移动: %v", err)
	}
	got, err := lib.GetMediaFileByIDInSpace(models.DefaultSpaceID, mf.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.ToSlash(filepath.Join(srcDir, "clip.mp4"))
	if got.FilePath != want {
		t.Fatalf("回滚后路径期望 %q, 得到 %q", want, got.FilePath)
	}
	if _, err := os.Stat(filepath.Join(srcDir, "clip.mp4")); err != nil {
		t.Fatalf("磁盘应回到 src: %v", err)
	}
}

func TestMediaMoved_PathRedactedNotRollbackable(t *testing.T) {
	svc, _, _, db := setupRollbackTest(t)
	rec := audit.NewRecorder(db)
	// 模拟审计脱敏后的 Users 路径
	if err := rec.Record(context.Background(), audit.EventInput{
		Scope: audit.ScopeSpace, SpaceID: models.DefaultSpaceID,
		ActorType: audit.ActorSystem, Action: "media.moved",
		ResourceType: "media", ResourceID: "77",
		Before: map[string]any{"file_path": "C:/Users/****/AppData/Local/Temp/src/a.mp4", "file_name": "a.mp4"},
		After:  map[string]any{"file_path": "C:/Users/****/AppData/Local/Temp/dst/a.mp4"},
	}); err != nil {
		t.Fatal(err)
	}
	var event models.AuditEvent
	if err := db.Where("resource_id = ?", "77").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	// Record 可能再次脱敏；直接写死含 **** 的 before
	event.BeforeJSON = `{"file_path":"C:/Users/****/AppData/Local/Temp/src/a.mp4","file_name":"a.mp4"}`
	_ = db.Save(&event)
	view := svc.annotate(&event)
	if view.Rollbackable || view.ReasonKey != ReasonPathRedacted {
		t.Fatalf("脱敏路径应 path_redacted: %+v", view)
	}
}

func TestMediaRenamed_MissingBeforeNotRollbackable(t *testing.T) {
	svc, _, _, db := setupRollbackTest(t)
	rec := audit.NewRecorder(db)
	if err := rec.Record(context.Background(), audit.EventInput{
		Scope: audit.ScopeSpace, SpaceID: models.DefaultSpaceID,
		ActorType: audit.ActorSystem, Action: "media.renamed",
		ResourceType: "media", ResourceID: "9",
		// 无 Before
		After: map[string]any{"file_name": "x.mp4"},
	}); err != nil {
		t.Fatal(err)
	}
	var event models.AuditEvent
	if err := db.Where("action = ?", "media.renamed").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	view := svc.annotate(&event)
	if view.Rollbackable || view.ReasonKey != ReasonMissingBefore {
		t.Fatalf("无 before 应不可回滚: %+v", view)
	}
}

func TestWritebackSucceeded_RestoreFromSnapshot(t *testing.T) {
	svc, _, _, db := setupRollbackTest(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "photo.jpg")
	snap := filepath.Join(dir, "snap-photo.jpg")
	if err := os.WriteFile(src, []byte("after-writeback"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snap, []byte("before-writeback"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := audit.NewRecorder(db)
	if err := rec.Record(context.Background(), audit.EventInput{
		Scope: audit.ScopeSpace, SpaceID: models.DefaultSpaceID,
		ActorType: audit.ActorSystem, Action: "metadata.writeback.succeeded",
		ResourceType: "media", ResourceID: "1",
		After: map[string]any{"fields": map[string]string{"camera": "Canon"}},
		Metadata: map[string]any{
			"summary": "元数据写回原文件成功", "snapshot_path": snap, "file_path": src,
		},
	}); err != nil {
		t.Fatal(err)
	}
	var event models.AuditEvent
	if err := db.Where("action = ?", "metadata.writeback.succeeded").Order("id DESC").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	view := svc.annotate(&event)
	if !view.Rollbackable {
		t.Fatalf("写回成功且快照存在应可回滚: reason=%q", view.ReasonKey)
	}
	if err := svc.Apply(context.Background(), ApplyInput{
		EventID: event.ID, SpaceID: models.DefaultSpaceID, Confirm: true, ActorID: "t",
	}); err != nil {
		t.Fatalf("回滚写回: %v", err)
	}
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "before-writeback" {
		t.Fatalf("原文件应被快照覆盖, 得到 %q", string(raw))
	}
	// 快照保留
	if _, err := os.Stat(snap); err != nil {
		t.Fatalf("回滚后快照应保留: %v", err)
	}
	var applied int64
	if err := db.Model(&models.AuditEvent{}).Where("action = ?", "rollback.applied").Count(&applied).Error; err != nil || applied < 1 {
		t.Fatalf("应写 rollback.applied: n=%d err=%v", applied, err)
	}
}

func TestWritebackSucceeded_MissingSnapshotNotRollbackable(t *testing.T) {
	svc, _, _, db := setupRollbackTest(t)
	rec := audit.NewRecorder(db)
	if err := rec.Record(context.Background(), audit.EventInput{
		Scope: audit.ScopeSpace, SpaceID: models.DefaultSpaceID,
		ActorType: audit.ActorSystem, Action: "metadata.writeback.succeeded",
		ResourceType: "media", ResourceID: "2",
		Metadata: map[string]any{"summary": "ok"},
	}); err != nil {
		t.Fatal(err)
	}
	var event models.AuditEvent
	if err := db.Where("action = ?", "metadata.writeback.succeeded").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	view := svc.annotate(&event)
	if view.Rollbackable {
		t.Fatal("无 snapshot_path 应不可回滚")
	}
	if view.ReasonKey != ReasonMissingSnapshot {
		t.Fatalf("reason=%q", view.ReasonKey)
	}
}

func TestWritebackSucceeded_SnapshotGoneNotRollbackable(t *testing.T) {
	svc, _, _, db := setupRollbackTest(t)
	dir := t.TempDir()
	gone := filepath.Join(dir, "gone.jpg")
	src := filepath.Join(dir, "target.jpg")
	_ = os.WriteFile(src, []byte("x"), 0o600)
	// 故意不写 gone
	rec := audit.NewRecorder(db)
	if err := rec.Record(context.Background(), audit.EventInput{
		Scope: audit.ScopeSpace, SpaceID: models.DefaultSpaceID,
		ActorType: audit.ActorSystem, Action: "metadata.writeback.succeeded",
		ResourceType: "media", ResourceID: "3",
		Metadata: map[string]any{"snapshot_path": gone, "file_path": src},
	}); err != nil {
		t.Fatal(err)
	}
	var event models.AuditEvent
	if err := db.Where("resource_id = ?", "3").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	view := svc.annotate(&event)
	if view.Rollbackable || view.ReasonKey != ReasonSnapshotGone {
		t.Fatalf("快照缺失应 snapshot_gone: %+v", view)
	}
}
