package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
)

func newCacheTestService(t *testing.T) (*Service, *gorm.DB, string) {
	t.Helper()
	dataDir := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(dataDir, "jianvideo.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("读取底层数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(
		&models.Space{},
		&models.LibraryPath{},
		&models.MediaFile{},
		&models.CacheAsset{},
		&models.AuditEvent{},
	); err != nil {
		t.Fatalf("迁移测试表失败: %v", err)
	}
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	if err := db.Create(&models.Space{ID: models.DefaultSpaceID, Name: "默认 Space", OwnerUserID: 1, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("创建默认 Space 失败: %v", err)
	}
	return NewService(db, dataDir).WithAudit(audit.NewRecorder(db)), db, dataDir
}

func TestCacheRegisterRejectsNonWhitelistPaths(t *testing.T) {
	svc, _, dataDir := newCacheTestService(t)
	outside := filepath.Join(t.TempDir(), "x.jpg")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatalf("写外部文件失败: %v", err)
	}
	if _, err := svc.RegisterFile(context.Background(), RegisterInput{
		SpaceID: models.DefaultSpaceID,
		Kind:    CacheKindThumbnail,
		Path:    outside,
	}); err == nil {
		t.Fatal("数据目录外路径不应允许登记")
	}

	dbFile := filepath.Join(dataDir, "jianvideo.db")
	if err := os.WriteFile(dbFile, []byte("db"), 0o600); err != nil {
		t.Fatalf("写数据库占位失败: %v", err)
	}
	if _, err := svc.RegisterFile(context.Background(), RegisterInput{
		SpaceID: models.DefaultSpaceID,
		Kind:    CacheKindThumbnail,
		Path:    dbFile,
	}); err == nil {
		t.Fatal("数据库文件不应允许登记为缓存")
	}
}

func TestCacheRegisterHLSDirectoryAggregatesFiles(t *testing.T) {
	svc, db, dataDir := newCacheTestService(t)
	dir := filepath.Join(dataDir, "hls", "42")
	mustWriteFile(t, filepath.Join(dir, "master.m3u8"), "m")
	mustWriteFile(t, filepath.Join(dir, "480p_segment_000.ts"), "aaaa")
	mustWriteFile(t, filepath.Join(dir, "480p_segment_001.ts"), "bbbb")

	asset, err := svc.RegisterDirectory(context.Background(), RegisterInput{
		SpaceID:   models.DefaultSpaceID,
		MediaID:   42,
		Kind:      CacheKindHLS,
		ProfileID: "h264",
		Path:      dir,
	})
	if err != nil {
		t.Fatalf("登记 HLS 目录失败: %v", err)
	}
	if asset.AssetLevel != CacheAssetLevelDirectory {
		t.Fatalf("HLS 应按目录登记，实际 level=%s", asset.AssetLevel)
	}
	if asset.FileCount != 3 || asset.SizeBytes != 9 {
		t.Fatalf("HLS 聚合统计不符: %+v", asset)
	}
	var count int64
	if err := db.Model(&models.CacheAsset{}).Where("kind = ?", CacheKindHLS).Count(&count).Error; err != nil {
		t.Fatalf("统计缓存资产失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("HLS segment 不应逐个登记，实际行数 %d", count)
	}
}

func TestCacheSummaryAndDryRunClean(t *testing.T) {
	svc, _, dataDir := newCacheTestService(t)
	thumb := filepath.Join(dataDir, "thumbnails", "a.jpg")
	proxy := filepath.Join(dataDir, "image_cache", "b.jpg")
	source := filepath.Join(t.TempDir(), "source.mp4")
	mustWriteFile(t, thumb, "12345")
	mustWriteFile(t, proxy, "123")
	mustWriteFile(t, source, "source")

	if _, err := svc.RegisterFile(context.Background(), RegisterInput{SpaceID: models.DefaultSpaceID, Kind: CacheKindThumbnail, Path: thumb}); err != nil {
		t.Fatalf("登记缩略图失败: %v", err)
	}
	if _, err := svc.RegisterFile(context.Background(), RegisterInput{SpaceID: models.DefaultSpaceID, Kind: CacheKindImageProxy, Path: proxy}); err != nil {
		t.Fatalf("登记图片代理失败: %v", err)
	}

	summary, err := svc.Summary(context.Background(), SummaryQuery{SpaceID: models.DefaultSpaceID})
	if err != nil {
		t.Fatalf("查询缓存统计失败: %v", err)
	}
	if summary.TotalSizeBytes != 8 || summary.ByKind[CacheKindThumbnail].SizeBytes != 5 || summary.ByKind[CacheKindImageProxy].SizeBytes != 3 {
		t.Fatalf("缓存统计聚合不符: %+v", summary)
	}

	preview, err := svc.Clean(context.Background(), CleanInput{SpaceID: models.DefaultSpaceID, Kinds: []string{CacheKindThumbnail}, DryRun: true})
	if err != nil {
		t.Fatalf("dry-run 清理失败: %v", err)
	}
	if !preview.DryRun || preview.CandidateCount != 1 || preview.TotalSizeBytes != 5 {
		t.Fatalf("dry-run 影响范围不符: %+v", preview)
	}
	if _, err := os.Stat(thumb); err != nil {
		t.Fatalf("dry-run 不应删除缩略图: %v", err)
	}

	result, err := svc.Clean(context.Background(), CleanInput{SpaceID: models.DefaultSpaceID, Kinds: []string{CacheKindThumbnail}})
	if err != nil {
		t.Fatalf("执行清理失败: %v", err)
	}
	if result.DeletedCount != 1 || result.DeletedSizeBytes != 5 {
		t.Fatalf("清理结果不符: %+v", result)
	}
	if _, err := os.Stat(thumb); !os.IsNotExist(err) {
		t.Fatalf("缩略图应被删除，stat err=%v", err)
	}
	if _, err := os.Stat(proxy); err != nil {
		t.Fatalf("非目标 kind 不应被删除: %v", err)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("原媒体不应被删除: %v", err)
	}
	mustWriteFile(t, thumb, "123456")
	rebuilt, err := svc.RegisterFile(context.Background(), RegisterInput{SpaceID: models.DefaultSpaceID, Kind: CacheKindThumbnail, Path: thumb})
	if err != nil {
		t.Fatalf("清理后应允许重建登记: %v", err)
	}
	if rebuilt.SizeBytes != 6 {
		t.Fatalf("重建登记尺寸不符: %+v", rebuilt)
	}
}

func TestCacheInventoryMarksMissingAndDiscoversWhitelistFiles(t *testing.T) {
	svc, db, dataDir := newCacheTestService(t)
	missing := filepath.Join(dataDir, "thumbnails", "missing.jpg")
	if _, err := svc.RegisterFile(context.Background(), RegisterInput{SpaceID: models.DefaultSpaceID, Kind: CacheKindThumbnail, Path: missing}); err == nil {
		t.Fatal("不存在文件不能登记")
	}
	existing := filepath.Join(dataDir, "thumbnails", "existing.jpg")
	mustWriteFile(t, existing, "abc")
	if _, err := svc.Inventory(context.Background(), InventoryInput{SpaceID: models.DefaultSpaceID}); err != nil {
		t.Fatalf("盘点失败: %v", err)
	}
	var asset models.CacheAsset
	if err := db.Where("kind = ? AND relative_path = ?", CacheKindThumbnail, filepath.ToSlash(filepath.Join("thumbnails", "existing.jpg"))).First(&asset).Error; err != nil {
		t.Fatalf("盘点应发现白名单文件: %v", err)
	}
	if asset.SizeBytes != 3 || asset.MissingAt != nil {
		t.Fatalf("盘点登记字段异常: %+v", asset)
	}

	if err := os.Remove(existing); err != nil {
		t.Fatalf("删除缓存文件失败: %v", err)
	}
	if _, err := svc.Inventory(context.Background(), InventoryInput{SpaceID: models.DefaultSpaceID}); err != nil {
		t.Fatalf("二次盘点失败: %v", err)
	}
	if err := db.First(&asset, asset.ID).Error; err != nil {
		t.Fatalf("读取盘点资产失败: %v", err)
	}
	if asset.MissingAt == nil {
		t.Fatalf("磁盘缺失资产应标记 missing_at: %+v", asset)
	}
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
}
