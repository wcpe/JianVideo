package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

func timelineGenerationPath(dataDir, spaceID string, mediaID int64, profile, fingerprint, generation string) string {
	return filepath.Join(dataDir, "timeline_previews", spaceID, strconv.FormatInt(mediaID, 10), profile, fingerprint, generation)
}

func TestTimelinePreviewInventory严格按Generation目录登记(t *testing.T) {
	svc, db, dataDir := newCacheTestService(t)
	valid := timelineGenerationPath(dataDir, models.DefaultSpaceID, 42, "desktop", "source-a", "gen-a")
	other := timelineGenerationPath(dataDir, "space-other", 42, "desktop", "source-b", "gen-b")
	mustWriteFile(t, filepath.Join(valid, "summary.json"), "123")
	mustWriteFile(t, filepath.Join(valid, "tiles", "000.webp"), "12345")
	mustWriteFile(t, filepath.Join(other, "summary.json"), "other")
	mustWriteFile(t, filepath.Join(dataDir, "timeline_previews", models.DefaultSpaceID, "42", "desktop", "unknown.txt"), "bad")

	discovered, err := svc.inventoryTimelinePreviews(context.Background(), models.DefaultSpaceID, filepath.Join(dataDir, "timeline_previews"))
	if err != nil {
		t.Fatalf("盘点时间线预览失败: %v", err)
	}
	if discovered != 1 {
		t.Fatalf("应只登记一个合法 generation，实际 %d", discovered)
	}
	var asset models.CacheAsset
	if err := db.Where("kind = ?", CacheKindTimelinePreview).First(&asset).Error; err != nil {
		t.Fatalf("读取时间线预览资产失败: %v", err)
	}
	if asset.SpaceID != models.DefaultSpaceID || asset.MediaID != 42 || asset.ProfileID != "desktop" {
		t.Fatalf("资产身份不符: %+v", asset)
	}
	if asset.AssetLevel != CacheAssetLevelDirectory || asset.SizeBytes != 8 || asset.FileCount != 2 {
		t.Fatalf("generation 应按目录聚合: %+v", asset)
	}
	if asset.Variant != "source-a:gen-a" || asset.CacheKey != "timeline_preview:space-default:42:desktop:source-a:gen-a" {
		t.Fatalf("缓存键应隔离 fingerprint 与 generation: %+v", asset)
	}
	assertTimelinePreviewQueries(t, svc)
}

func assertTimelinePreviewQueries(t *testing.T, svc *Service) {
	t.Helper()
	summary, err := svc.Summary(context.Background(), SummaryQuery{SpaceID: models.DefaultSpaceID})
	if err != nil || summary.ByKind[CacheKindTimelinePreview].AssetCount != 1 {
		t.Fatalf("时间线预览汇总异常: summary=%+v err=%v", summary, err)
	}
	page, err := svc.ListAssets(context.Background(), AssetQuery{
		SpaceID: models.DefaultSpaceID, Kind: CacheKindTimelinePreview,
	})
	if err != nil || page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("时间线预览资产列表异常: page=%+v err=%v", page, err)
	}
}

func TestRegisterDirectoryTx拒绝其他服务准备的资产(t *testing.T) {
	svc, db, dataDir := newCacheTestService(t)
	path := timelineGenerationPath(dataDir, models.DefaultSpaceID, 42, "desktop", "source-a", "gen-a")
	mustWriteFile(t, filepath.Join(path, "index.vtt"), "preview")
	input, err := parseTimelinePreviewPath(filepath.Join(dataDir, "timeline_previews"), path, models.DefaultSpaceID)
	if err != nil {
		t.Fatalf("解析时间线目录失败: %v", err)
	}
	prepared, err := svc.PrepareDirectoryAsset(input)
	if err != nil {
		t.Fatalf("准备目录资产失败: %v", err)
	}
	other := NewService(db, t.TempDir())
	err = db.Transaction(func(tx *gorm.DB) error {
		_, registerErr := other.RegisterDirectoryTx(context.Background(), tx, prepared)
		return registerErr
	})
	if !errors.Is(err, ErrUnsafeCachePath) {
		t.Fatalf("其他缓存根不得登记已准备资产: %v", err)
	}
}

func TestRegisterDirectoryTx随调用方事务回滚(t *testing.T) {
	svc, db, dataDir := newCacheTestService(t)
	path := timelineGenerationPath(dataDir, models.DefaultSpaceID, 42, "desktop", "source-a", "gen-a")
	mustWriteFile(t, filepath.Join(path, "index.vtt"), "preview")
	input, err := parseTimelinePreviewPath(filepath.Join(dataDir, "timeline_previews"), path, models.DefaultSpaceID)
	if err != nil {
		t.Fatalf("解析时间线目录失败: %v", err)
	}
	prepared, err := svc.PrepareDirectoryAsset(input)
	if err != nil {
		t.Fatalf("准备目录资产失败: %v", err)
	}
	rollbackErr := errors.New("模拟调用方回滚")
	err = db.Transaction(func(tx *gorm.DB) error {
		if _, registerErr := svc.RegisterDirectoryTx(context.Background(), tx, prepared); registerErr != nil {
			return registerErr
		}
		return rollbackErr
	})
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("事务应返回模拟错误: %v", err)
	}
	var count int64
	if err := db.Model(&models.CacheAsset{}).Where("cache_key = ?", input.CacheKey).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("调用方回滚后不得保留缓存登记: count=%d err=%v", count, err)
	}
}

func TestTimelinePreviewKindRoot唯一且参与默认Kinds(t *testing.T) {
	if kindDirs()[CacheKindTimelinePreview] != "timeline_previews" {
		t.Fatalf("时间线预览 root 映射错误: %v", kindDirs())
	}
	for kind, root := range kindDirs() {
		if kind != CacheKindTimelinePreview && root == "timeline_previews" {
			t.Fatalf("timeline_previews root 不得映射到其他 kind: %s", kind)
		}
	}
	kinds, err := normalizeKinds(nil)
	if err != nil || !containsKind(kinds, CacheKindTimelinePreview) {
		t.Fatalf("默认 kind 应包含时间线预览: kinds=%v err=%v", kinds, err)
	}
}

func containsKind(kinds []string, target string) bool {
	for _, kind := range kinds {
		if kind == target {
			return true
		}
	}
	return false
}

func TestTimelinePreviewStrictPathRejectsInvalidIdentityAndLevels(t *testing.T) {
	svc, _, dataDir := newCacheTestService(t)
	cases := []string{
		filepath.Join(dataDir, "timeline_previews", models.DefaultSpaceID, "0", "desktop", "source", "gen"),
		filepath.Join(dataDir, "timeline_previews", models.DefaultSpaceID, "42x", "desktop", "source", "gen"),
		filepath.Join(dataDir, "timeline_previews", models.DefaultSpaceID, "42", "bad token", "source", "gen"),
		filepath.Join(dataDir, "timeline_previews", models.DefaultSpaceID, "42", "desktop", "source"),
		filepath.Join(dataDir, "timeline_previews", models.DefaultSpaceID, "42", "desktop", "source", "gen", "file.webp"),
		filepath.Join(dataDir, "hls", models.DefaultSpaceID, "42", "desktop", "source", "gen"),
	}
	for _, path := range cases {
		if _, _, err := svc.safePath(path, CacheKindTimelinePreview); !errors.Is(err, ErrUnsafeCachePath) {
			t.Errorf("非法时间线预览路径应被拒绝: path=%s err=%v", path, err)
		}
	}
	valid := timelineGenerationPath(dataDir, models.DefaultSpaceID, 42, "desktop", "source", "gen")
	if _, err := parseTimelinePreviewPath(filepath.Join(dataDir, "timeline_previews"), valid, "space-other"); !errors.Is(err, ErrUnsafeCachePath) {
		t.Fatalf("请求 Space 与路径身份不一致应被拒绝: %v", err)
	}
}

func TestTimelinePreviewSafePathRejectsSymlinkEscape(t *testing.T) {
	svc, _, dataDir := newCacheTestService(t)
	outside := t.TempDir()
	mustWriteFile(t, filepath.Join(outside, "summary.json"), "outside")
	generation := timelineGenerationPath(dataDir, models.DefaultSpaceID, 42, "desktop", "source", "gen")
	if err := os.MkdirAll(filepath.Dir(generation), 0o755); err != nil {
		t.Fatalf("创建父目录失败: %v", err)
	}
	if err := os.Symlink(outside, generation); err != nil {
		t.Skipf("当前环境不支持符号链接: %v", err)
	}
	if _, _, err := svc.safePath(generation, CacheKindTimelinePreview); !errors.Is(err, ErrUnsafeCachePath) {
		t.Fatalf("越界符号链接应被拒绝: %v", err)
	}
}

func TestCleanUnregisteredTimelinePreviewGeneration拒绝路径冒充与跨Space(t *testing.T) {
	svc, _, dataDir := newCacheTestService(t)
	valid := timelineGenerationPath(dataDir, models.DefaultSpaceID, 42, "desktop", "source-a", "gen-a")
	otherSpace := timelineGenerationPath(dataDir, "space-other", 42, "desktop", "source-a", "gen-a")
	mustWriteFile(t, filepath.Join(valid, "index.vtt"), "preview")
	mustWriteFile(t, filepath.Join(otherSpace, "index.vtt"), "other")

	cases := []string{valid, otherSpace}
	generations := []string{"gen-b", "gen-a"}
	for index, path := range cases {
		err := svc.CleanUnregisteredTimelinePreviewGeneration(
			context.Background(), models.DefaultSpaceID, 42, "desktop", "source-a", generations[index], path,
		)
		if !errors.Is(err, ErrUnsafeCachePath) {
			t.Errorf("冒充身份路径应被拒绝: path=%s err=%v", path, err)
		}
	}
}

func TestCleanUnregisteredTimelinePreviewGeneration拒绝当前指针(t *testing.T) {
	svc, db, dataDir := newCacheTestService(t)
	path := timelineGenerationPath(dataDir, models.DefaultSpaceID, 42, "desktop", "source-a", "gen-a")
	mustWriteFile(t, filepath.Join(path, "index.vtt"), "preview")
	pointer := models.MediaTimelinePreview{
		SpaceID: models.DefaultSpaceID, MediaID: 42, ProfileID: "desktop",
		SourceFingerprint: "source-a", GenerationID: "gen-a", AssetID: 99,
	}
	if err := db.Create(&pointer).Error; err != nil {
		t.Fatalf("创建当前指针失败: %v", err)
	}

	err := svc.CleanUnregisteredTimelinePreviewGeneration(
		context.Background(), models.DefaultSpaceID, 42, "desktop", "source-a", "gen-a", path,
	)
	if err == nil {
		t.Fatal("当前指针指向 generation 时应拒绝补偿清理")
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("拒绝后目录应保留: %v", statErr)
	}
}

func TestCleanUnregisteredTimelinePreviewGeneration删除合法未登记目录(t *testing.T) {
	svc, db, dataDir := newCacheTestService(t)
	path := timelineGenerationPath(dataDir, models.DefaultSpaceID, 42, "desktop", "source-a", "gen-a")
	mustWriteFile(t, filepath.Join(path, "index.vtt"), "preview")

	err := svc.CleanUnregisteredTimelinePreviewGeneration(
		context.Background(), models.DefaultSpaceID, 42, "desktop", "source-a", "gen-a", path,
	)
	if err != nil {
		t.Fatalf("清理合法未登记 generation 失败: %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("合法未登记 generation 应删除: %v", statErr)
	}
	var event models.AuditEvent
	if err := db.Where("action = ?", "cache.timeline_preview.compensation_cleanup").First(&event).Error; err != nil {
		t.Fatalf("应记录补偿清理审计: %v", err)
	}
}

func TestCleanUnregisteredTimelinePreviewGeneration已登记时走资产删除协议(t *testing.T) {
	svc, db, dataDir := newCacheTestService(t)
	path := timelineGenerationPath(dataDir, models.DefaultSpaceID, 42, "desktop", "source-a", "gen-a")
	mustWriteFile(t, filepath.Join(path, "index.vtt"), "preview")
	asset := mustRegisterTimelineGeneration(t, svc, path)

	err := svc.CleanUnregisteredTimelinePreviewGeneration(
		context.Background(), models.DefaultSpaceID, 42, "desktop", "source-a", "gen-a", path,
	)
	if err != nil {
		t.Fatalf("清理已登记 generation 失败: %v", err)
	}
	var count int64
	if err := db.Model(&models.CacheAsset{}).Where("id = ?", asset.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("已登记 generation 必须通过资产协议删除: count=%d err=%v", count, err)
	}
}

func TestCleanUnregisteredTimelinePreviewGeneration返回删除失败(t *testing.T) {
	svc, _, dataDir := newCacheTestService(t)
	path := timelineGenerationPath(dataDir, models.DefaultSpaceID, 42, "desktop", "source-a", "gen-a")
	mustWriteFile(t, filepath.Join(path, "index.vtt"), "preview")
	deleteErr := errors.New("模拟补偿删除失败")
	svc.removeAll = func(string) error { return deleteErr }

	err := svc.CleanUnregisteredTimelinePreviewGeneration(
		context.Background(), models.DefaultSpaceID, 42, "desktop", "source-a", "gen-a", path,
	)
	if !errors.Is(err, deleteErr) {
		t.Fatalf("应返回目录删除错误: %v", err)
	}
}

func TestCleanUnregisteredTimelinePreviewGeneration拒绝符号链接(t *testing.T) {
	svc, _, dataDir := newCacheTestService(t)
	outside := t.TempDir()
	mustWriteFile(t, filepath.Join(outside, "index.vtt"), "outside")
	path := timelineGenerationPath(dataDir, models.DefaultSpaceID, 42, "desktop", "source-a", "gen-a")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("创建 generation 父目录失败: %v", err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Skipf("当前环境不支持符号链接: %v", err)
	}

	err := svc.CleanUnregisteredTimelinePreviewGeneration(
		context.Background(), models.DefaultSpaceID, 42, "desktop", "source-a", "gen-a", path,
	)
	if !errors.Is(err, ErrUnsafeCachePath) {
		t.Fatalf("符号链接 generation 应被拒绝: %v", err)
	}
}

func TestDeleteTimelinePreviewGeneration拒绝跨Space资产ID(t *testing.T) {
	svc, db, dataDir := newCacheTestService(t)
	const otherSpace = "space-other"
	path := timelineGenerationPath(dataDir, otherSpace, 42, "desktop", "source-a", "gen-a")
	mustWriteFile(t, filepath.Join(path, "summary.json"), "preview")
	asset := mustRegisterTimelineGenerationInSpace(t, svc, path, otherSpace)
	pointer := createTimelinePointerInSpace(t, db, asset.ID, otherSpace)

	err := svc.DeleteTimelinePreviewGeneration(context.Background(), models.DefaultSpaceID, asset.ID, "gen-a")
	if err == nil {
		t.Fatal("跨 Space 资产 ID 应被拒绝")
	}
	assertTimelineDeletionUntouched(t, db, pointer.ID, asset.ID, path)
}

func TestDeleteTimelinePreviewGeneration提交后才删除目录(t *testing.T) {
	svc, db, dataDir := newCacheTestService(t)
	path := timelineGenerationPath(dataDir, models.DefaultSpaceID, 42, "desktop", "source-a", "gen-a")
	mustWriteFile(t, filepath.Join(path, "summary.json"), "preview")
	asset := mustRegisterTimelineGeneration(t, svc, path)
	pointer := createTimelinePointer(t, db, asset.ID)
	svc.removeAll = func(target string) error {
		assertTimelineDatabaseDeleted(t, db, pointer.ID, asset.ID)
		if target != path {
			t.Fatalf("应直接删除 generation 目录: got=%s want=%s", target, path)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("数据库提交前后、文件删除前 generation 必须仍存在: %v", err)
		}
		return os.RemoveAll(target)
	}

	if err := svc.DeleteTimelinePreviewGeneration(context.Background(), models.DefaultSpaceID, asset.ID, "gen-a"); err != nil {
		t.Fatalf("删除 generation 失败: %v", err)
	}
}

func TestDeleteTimelinePreviewGeneration目录删除失败不回滚数据库且可盘点恢复(t *testing.T) {
	svc, db, dataDir := newCacheTestService(t)
	path := timelineGenerationPath(dataDir, models.DefaultSpaceID, 42, "desktop", "source-a", "gen-a")
	mustWriteFile(t, filepath.Join(path, "summary.json"), "preview")
	asset := mustRegisterTimelineGeneration(t, svc, path)
	pointer := createTimelinePointer(t, db, asset.ID)
	deleteErr := errors.New("模拟目录删除失败")
	svc.removeAll = func(string) error { return deleteErr }

	err := svc.DeleteTimelinePreviewGeneration(context.Background(), models.DefaultSpaceID, asset.ID, "gen-a")
	if !errors.Is(err, deleteErr) {
		t.Fatalf("应返回目录删除错误: %v", err)
	}
	assertTimelineDatabaseDeleted(t, db, pointer.ID, asset.ID)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("删除失败后应残留未登记目录: %v", err)
	}
	if discovered, err := svc.inventoryTimelinePreviews(context.Background(), models.DefaultSpaceID, filepath.Join(dataDir, "timeline_previews")); err != nil || discovered != 1 {
		t.Fatalf("后续盘点应恢复目录登记: discovered=%d err=%v", discovered, err)
	}
	var recovered int64
	if err := db.Model(&models.CacheAsset{}).
		Where("space_id = ? AND kind = ? AND variant = ?", models.DefaultSpaceID, CacheKindTimelinePreview, "source-a:gen-a").
		Count(&recovered).Error; err != nil || recovered != 1 {
		t.Fatalf("后续盘点应恢复资产登记: count=%d err=%v", recovered, err)
	}
}

func TestDeleteTimelinePreviewGeneration条件清理不误伤新指针(t *testing.T) {
	svc, db, dataDir := newCacheTestService(t)
	oldPath := timelineGenerationPath(dataDir, models.DefaultSpaceID, 42, "desktop", "source-a", "gen-old")
	newPath := timelineGenerationPath(dataDir, models.DefaultSpaceID, 42, "desktop", "source-b", "gen-new")
	oldAsset, newAsset, pointer := prepareTimelineRace(t, svc, db, oldPath, newPath)

	if err := svc.DeleteTimelinePreviewGeneration(context.Background(), models.DefaultSpaceID, oldAsset.ID, "gen-old"); err != nil {
		t.Fatalf("清理旧 generation 失败: %v", err)
	}
	assertTimelineRaceResult(t, db, pointer, newAsset.ID, oldPath, newPath)
}

func prepareTimelineRace(t *testing.T, svc *Service, db *gorm.DB, oldPath, newPath string) (*models.CacheAsset, *models.CacheAsset, models.MediaTimelinePreview) {
	t.Helper()
	mustWriteFile(t, filepath.Join(oldPath, "summary.json"), "old")
	mustWriteFile(t, filepath.Join(newPath, "summary.json"), "new")
	oldAsset := mustRegisterTimelineGeneration(t, svc, oldPath)
	newAsset := mustRegisterTimelineGeneration(t, svc, newPath)
	pointer := models.MediaTimelinePreview{
		SpaceID: models.DefaultSpaceID, MediaID: 42, ProfileID: "desktop",
		SourceFingerprint: "source-b", GenerationID: "gen-new", AssetID: newAsset.ID,
	}
	if err := db.Create(&pointer).Error; err != nil {
		t.Fatalf("创建当前指针失败: %v", err)
	}
	return oldAsset, newAsset, pointer
}

func assertTimelineRaceResult(t *testing.T, db *gorm.DB, pointer models.MediaTimelinePreview, newAssetID int64, oldPath, newPath string) {
	t.Helper()
	var got models.MediaTimelinePreview
	if err := db.First(&got, pointer.ID).Error; err != nil {
		t.Fatalf("新指针不应被误清: %v", err)
	}
	if got.AssetID != newAssetID || got.GenerationID != "gen-new" {
		t.Fatalf("新指针被误改: %+v", got)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("旧 generation 应删除: %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("新 generation 不应删除: %v", err)
	}
}

func TestDeleteTimelinePreviewGeneration事务内指针切换不清新Generation(t *testing.T) {
	svc, db, dataDir := newCacheTestService(t)
	oldPath := timelineGenerationPath(dataDir, models.DefaultSpaceID, 42, "desktop", "source-a", "gen-old")
	newPath := timelineGenerationPath(dataDir, models.DefaultSpaceID, 42, "desktop", "source-b", "gen-new")
	mustWriteFile(t, filepath.Join(oldPath, "summary.json"), "old")
	mustWriteFile(t, filepath.Join(newPath, "summary.json"), "new")
	oldAsset := mustRegisterTimelineGeneration(t, svc, oldPath)
	newAsset := mustRegisterTimelineGeneration(t, svc, newPath)
	pointer := models.MediaTimelinePreview{
		SpaceID: models.DefaultSpaceID, MediaID: 42, ProfileID: "desktop",
		SourceFingerprint: "source-a", GenerationID: "gen-old", AssetID: oldAsset.ID,
	}
	if err := db.Create(&pointer).Error; err != nil {
		t.Fatalf("创建旧 generation 指针失败: %v", err)
	}
	callbackName := "测试删除前切换时间线指针"
	if err := db.Callback().Delete().Before("gorm:delete").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != "cache_assets" {
			return
		}
		err := tx.Session(&gorm.Session{NewDB: true, SkipHooks: true}).Model(&models.MediaTimelinePreview{}).
			Where("id = ?", pointer.ID).
			Updates(map[string]any{
				"asset_id": newAsset.ID, "source_fingerprint": "source-b", "generation_id": "gen-new",
			}).Error
		if err != nil {
			t.Errorf("事务内切换指针失败: %v", err)
		}
	}); err != nil {
		t.Fatalf("注册竞态回调失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Delete().Remove(callbackName) })

	if err := svc.DeleteTimelinePreviewGeneration(context.Background(), models.DefaultSpaceID, oldAsset.ID, "gen-old"); err != nil {
		t.Fatalf("清理旧 generation 失败: %v", err)
	}
	assertTimelineRaceResult(t, db, pointer, newAsset.ID, oldPath, newPath)
}

func TestTimelinePreviewClean只清目标Generation且不伤其他资产(t *testing.T) {
	svc, db, dataDir := newCacheTestService(t)
	target := timelineGenerationPath(dataDir, models.DefaultSpaceID, 42, "desktop", "source-a", "gen-a")
	other := timelineGenerationPath(dataDir, models.DefaultSpaceID, 42, "desktop", "source-a", "gen-b")
	hls, thumb, source := prepareProtectedAssets(t, svc, dataDir, target, other)
	targetAsset := mustRegisterTimelineGeneration(t, svc, target)
	mustRegisterTimelineGeneration(t, svc, other)
	preview, err := svc.Clean(context.Background(), CleanInput{
		SpaceID: models.DefaultSpaceID, Kinds: []string{CacheKindTimelinePreview}, DryRun: true,
	})
	if err != nil || preview.CandidateCount != 2 {
		t.Fatalf("dry-run 范围异常: result=%+v err=%v", preview, err)
	}
	if err := svc.DeleteTimelinePreviewGeneration(context.Background(), models.DefaultSpaceID, targetAsset.ID, "gen-a"); err != nil {
		t.Fatalf("清理目标 generation 失败: %v", err)
	}
	assertProtectedAssets(t, db, []string{other, hls, thumb, source})
}

func prepareProtectedAssets(t *testing.T, svc *Service, dataDir, target, other string) (string, string, string) {
	t.Helper()
	hls := filepath.Join(dataDir, "hls", "42")
	thumb := filepath.Join(dataDir, "thumbnails", "42.jpg")
	source := filepath.Join(t.TempDir(), "source.mp4")
	for path, content := range map[string]string{
		filepath.Join(target, "summary.json"): "target", filepath.Join(other, "summary.json"): "other",
		filepath.Join(hls, "master.m3u8"): "hls", thumb: "thumb", source: "source",
	} {
		mustWriteFile(t, path, content)
	}
	if _, err := svc.RegisterDirectory(context.Background(), RegisterInput{SpaceID: models.DefaultSpaceID, MediaID: 42, Kind: CacheKindHLS, Path: hls}); err != nil {
		t.Fatalf("登记 HLS 失败: %v", err)
	}
	if _, err := svc.RegisterFile(context.Background(), RegisterInput{SpaceID: models.DefaultSpaceID, MediaID: 42, Kind: CacheKindThumbnail, Path: thumb}); err != nil {
		t.Fatalf("登记缩略图失败: %v", err)
	}
	return hls, thumb, source
}

func assertProtectedAssets(t *testing.T, db *gorm.DB, paths []string) {
	t.Helper()
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("非目标资产不应受损: path=%s err=%v", path, err)
		}
	}
	var remaining int64
	if err := db.Model(&models.CacheAsset{}).Where("kind = ?", CacheKindTimelinePreview).Count(&remaining).Error; err != nil || remaining != 1 {
		t.Fatalf("应保留另一 generation 登记: count=%d err=%v", remaining, err)
	}
}

func TestTimelinePreviewInventory隔离MediaProfileFingerprint和Generation(t *testing.T) {
	svc, db, dataDir := newCacheTestService(t)
	paths := []string{
		timelineGenerationPath(dataDir, models.DefaultSpaceID, 42, "desktop", "source-a", "gen-a"),
		timelineGenerationPath(dataDir, models.DefaultSpaceID, 43, "desktop", "source-a", "gen-a"),
		timelineGenerationPath(dataDir, models.DefaultSpaceID, 42, "mobile", "source-a", "gen-a"),
		timelineGenerationPath(dataDir, models.DefaultSpaceID, 42, "desktop", "source-b", "gen-a"),
		timelineGenerationPath(dataDir, models.DefaultSpaceID, 42, "desktop", "source-a", "gen-b"),
	}
	for _, path := range paths {
		mustWriteFile(t, filepath.Join(path, "summary.json"), "preview")
	}
	discovered, err := svc.inventoryTimelinePreviews(context.Background(), models.DefaultSpaceID, filepath.Join(dataDir, "timeline_previews"))
	if err != nil || discovered != int64(len(paths)) {
		t.Fatalf("身份隔离盘点异常: discovered=%d err=%v", discovered, err)
	}
	var assets []models.CacheAsset
	if err := db.Where("kind = ?", CacheKindTimelinePreview).Find(&assets).Error; err != nil {
		t.Fatalf("查询时间线预览资产失败: %v", err)
	}
	if len(assets) != len(paths) {
		t.Fatalf("各身份应独立登记: got=%d want=%d", len(assets), len(paths))
	}
}

func TestTimelinePreviewClean清空当前指针并删除Generation(t *testing.T) {
	svc, db, dataDir := newCacheTestService(t)
	path := timelineGenerationPath(dataDir, models.DefaultSpaceID, 42, "desktop", "source-a", "gen-a")
	mustWriteFile(t, filepath.Join(path, "summary.json"), "preview")
	asset := mustRegisterTimelineGeneration(t, svc, path)
	pointer := createTimelinePointer(t, db, asset.ID)
	result, err := svc.Clean(context.Background(), CleanInput{
		SpaceID: models.DefaultSpaceID, Kinds: []string{CacheKindTimelinePreview},
	})
	if err != nil {
		t.Fatalf("时间线预览清理入队失败: %v", err)
	}
	runCacheWorkers(t, svc)
	assertCacheTaskSucceeded(t, svc.tasks, result.TaskID)
	assertTimelinePointerCleared(t, db, pointer.ID, asset.ID, path)
}

func createTimelinePointer(t *testing.T, db *gorm.DB, assetID int64) models.MediaTimelinePreview {
	t.Helper()
	return createTimelinePointerInSpace(t, db, assetID, models.DefaultSpaceID)
}

func createTimelinePointerInSpace(t *testing.T, db *gorm.DB, assetID int64, spaceID string) models.MediaTimelinePreview {
	t.Helper()
	pointer := models.MediaTimelinePreview{
		SpaceID: spaceID, MediaID: 42, ProfileID: "desktop",
		SourceFingerprint: "source-a", GenerationID: "gen-a", AssetID: assetID,
	}
	if err := db.Create(&pointer).Error; err != nil {
		t.Fatalf("创建当前指针失败: %v", err)
	}
	return pointer
}

func assertTimelineDeletionUntouched(t *testing.T, db *gorm.DB, pointerID, assetID int64, path string) {
	t.Helper()
	var pointer models.MediaTimelinePreview
	if err := db.First(&pointer, pointerID).Error; err != nil || pointer.AssetID != assetID || pointer.GenerationID != "gen-a" {
		t.Fatalf("拒绝删除后指针不应变化: pointer=%+v err=%v", pointer, err)
	}
	var count int64
	if err := db.Model(&models.CacheAsset{}).Where("id = ?", assetID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("拒绝删除后资产登记不应变化: count=%d err=%v", count, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("拒绝删除后目录不应变化: %v", err)
	}
}

func assertTimelineDatabaseDeleted(t *testing.T, db *gorm.DB, pointerID, assetID int64) {
	t.Helper()
	var pointer models.MediaTimelinePreview
	if err := db.First(&pointer, pointerID).Error; err != nil {
		t.Fatalf("读取清理后指针失败: %v", err)
	}
	if pointer.AssetID != 0 || pointer.GenerationID != "" || pointer.SourceFingerprint != "" {
		t.Fatalf("数据库提交后当前指针应已清空: %+v", pointer)
	}
	var count int64
	if err := db.Model(&models.CacheAsset{}).Where("id = ?", assetID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("数据库提交后资产登记应已删除: count=%d err=%v", count, err)
	}
}

func assertTimelinePointerCleared(t *testing.T, db *gorm.DB, pointerID, assetID int64, path string) {
	t.Helper()
	var got models.MediaTimelinePreview
	if err := db.First(&got, pointerID).Error; err != nil {
		t.Fatalf("清理后读取指针失败: %v", err)
	}
	if got.AssetID != 0 || got.SourceFingerprint != "" || got.GenerationID != "" {
		t.Fatalf("当前 generation 指针应被条件清空: %+v", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("目标 generation 目录应删除: %v", err)
	}
	var count int64
	if err := db.Model(&models.CacheAsset{}).Where("id = ?", assetID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("目标资产登记应删除: count=%d err=%v", count, err)
	}
}

func mustRegisterTimelineGeneration(t *testing.T, svc *Service, path string) *models.CacheAsset {
	t.Helper()
	return mustRegisterTimelineGenerationInSpace(t, svc, path, models.DefaultSpaceID)
}

func mustRegisterTimelineGenerationInSpace(t *testing.T, svc *Service, path, spaceID string) *models.CacheAsset {
	t.Helper()
	identity, err := parseTimelinePreviewPath(filepath.Join(svc.data, "timeline_previews"), path, spaceID)
	if err != nil {
		t.Fatalf("解析 generation 路径失败: %v", err)
	}
	asset, err := svc.RegisterDirectory(context.Background(), identity)
	if err != nil {
		t.Fatalf("登记 generation 失败: %v", err)
	}
	return asset
}
