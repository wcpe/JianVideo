package storage

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

func BenchmarkCacheSummary10000Assets(b *testing.B) {
	dataDir := b.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(dataDir, "jianvideo.db")), &gorm.Config{})
	if err != nil {
		b.Fatalf("打开测试库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		b.Fatalf("读取底层数据库失败: %v", err)
	}
	b.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.CacheAsset{}); err != nil {
		b.Fatalf("迁移缓存资产失败: %v", err)
	}
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	kinds := orderedKinds()
	assets := make([]models.CacheAsset, 0, 10000)
	for i := range 10000 {
		kind := kinds[i%len(kinds)]
		assets = append(assets, models.CacheAsset{
			SpaceID:      models.DefaultSpaceID,
			Kind:         kind,
			AssetLevel:   CacheAssetLevelFile,
			RelativePath: filepath.ToSlash(filepath.Join(kindDirs()[kind], "bench", fmt.Sprintf("%05d.cache", i))),
			SizeBytes:    int64(100 + i%97),
			FileCount:    1,
			Rebuildable:  true,
			CreatedAt:    now,
			UpdatedAt:    now,
		})
	}
	if err := db.CreateInBatches(assets, 500).Error; err != nil {
		b.Fatalf("写入缓存资产失败: %v", err)
	}
	svc := NewService(db, dataDir)
	b.ResetTimer()
	for b.Loop() {
		if _, err := svc.Summary(context.Background(), SummaryQuery{SpaceID: models.DefaultSpaceID}); err != nil {
			b.Fatalf("统计缓存失败: %v", err)
		}
	}
}
