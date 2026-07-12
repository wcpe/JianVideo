package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/wcpe/JianVideo/internal/db/models"
)

func TestPrepareHLSRebuildDeletesOnlyRequestedProfile(t *testing.T) {
	svc, db, dataDir := newCacheTestService(t)
	target := filepath.Join(dataDir, "hls", models.DefaultSpaceID, "42", "h264")
	other := filepath.Join(dataDir, "hls", models.DefaultSpaceID, "42", "h265")
	mustWriteFile(t, filepath.Join(target, "master.m3u8"), "h264")
	mustWriteFile(t, filepath.Join(other, "index.m3u8"), "h265")
	if _, err := svc.RegisterDirectory(context.Background(), RegisterInput{SpaceID: models.DefaultSpaceID, MediaID: 42, Kind: CacheKindHLS, ProfileID: "h264", Path: target}); err != nil {
		t.Fatalf("登记目标 profile 失败: %v", err)
	}
	if _, err := svc.RegisterDirectory(context.Background(), RegisterInput{SpaceID: models.DefaultSpaceID, MediaID: 42, Kind: CacheKindHLS, ProfileID: "h265", Path: other}); err != nil {
		t.Fatalf("登记其他 profile 失败: %v", err)
	}

	if err := svc.PrepareHLSRebuild(context.Background(), models.DefaultSpaceID, 42, "h264", target); err != nil {
		t.Fatalf("准备 HLS 安全重建失败: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("目标 profile 应被清理: %v", err)
	}
	if _, err := os.Stat(filepath.Join(other, "index.m3u8")); err != nil {
		t.Fatalf("其他 profile 不应被删除: %v", err)
	}
	var targetAssets int64
	if err := db.Model(&models.CacheAsset{}).Where("media_id = ? AND profile_id = ?", 42, "h264").Count(&targetAssets).Error; err != nil {
		t.Fatalf("统计目标资产失败: %v", err)
	}
	if targetAssets != 0 {
		t.Fatalf("目标 profile 资产登记应删除，实际 %d", targetAssets)
	}
}
