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

func TestPrepareHLSRebuildDeletesOnlyRequestedAudioTask(t *testing.T) {
	svc, db, dataDir := newCacheTestService(t)
	profileID := "audio-h264-aac-0123456789abcdef01234567"
	first := filepath.Join(dataDir, "hls", models.DefaultSpaceID, "42", profileID, "tasks", "101")
	second := filepath.Join(dataDir, "hls", models.DefaultSpaceID, "42", profileID, "tasks", "102")
	mustWriteFile(t, filepath.Join(first, "master.m3u8"), "first")
	mustWriteFile(t, filepath.Join(second, "master.m3u8"), "second")
	for taskID, path := range map[string]string{"101": first, "102": second} {
		if _, err := svc.RegisterDirectory(context.Background(), RegisterInput{
			SpaceID: models.DefaultSpaceID, MediaID: 42, Kind: CacheKindHLS,
			ProfileID: profileID, Variant: taskID, Path: path,
		}); err != nil {
			t.Fatalf("登记音轨 task 缓存失败: task=%s err=%v", taskID, err)
		}
	}
	if err := svc.PrepareHLSRebuild(context.Background(), models.DefaultSpaceID, 42, profileID, first); err != nil {
		t.Fatalf("准备音轨 task 安全重建失败: %v", err)
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("目标音轨 task 应被清理: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(second, "master.m3u8")); err != nil || string(data) != "second" {
		t.Fatalf("同 profile 的其他 task 不应被删除: data=%q err=%v", data, err)
	}
	var assets []models.CacheAsset
	if err := db.Where("media_id = ? AND profile_id = ?", 42, profileID).Find(&assets).Error; err != nil {
		t.Fatalf("读取剩余音轨缓存资产失败: %v", err)
	}
	if len(assets) != 1 || assets[0].Variant != "102" {
		t.Fatalf("重建清理必须保持其他 task 资产: %+v", assets)
	}
}

func TestPrepareHLSRebuildDeletesAllVariantAssetsBelowProfile(t *testing.T) {
	svc, db, dataDir := newCacheTestService(t)
	profileDir := filepath.Join(dataDir, "hls", models.DefaultSpaceID, "42", "profiles", "abr-h264")
	master := filepath.Join(profileDir, "master.m3u8")
	variant720 := filepath.Join(profileDir, "720p")
	variant480 := filepath.Join(profileDir, "480p")
	mustWriteFile(t, master, "master")
	mustWriteFile(t, filepath.Join(variant720, "index.m3u8"), "720p")
	mustWriteFile(t, filepath.Join(variant480, "index.m3u8"), "480p")
	for _, input := range []RegisterInput{
		{SpaceID: models.DefaultSpaceID, MediaID: 42, Kind: CacheKindHLS, ProfileID: "abr-h264", Variant: "master", Path: master},
		{SpaceID: models.DefaultSpaceID, MediaID: 42, Kind: CacheKindHLS, ProfileID: "abr-h264", Variant: "720p", Path: variant720},
		{SpaceID: models.DefaultSpaceID, MediaID: 42, Kind: CacheKindHLS, ProfileID: "abr-h264", Variant: "480p", Path: variant480},
	} {
		var err error
		if input.Variant == "master" {
			_, err = svc.RegisterFile(context.Background(), input)
		} else {
			_, err = svc.RegisterDirectory(context.Background(), input)
		}
		if err != nil {
			t.Fatalf("登记 ABR 缓存资产失败: %v", err)
		}
	}

	if err := svc.PrepareHLSRebuild(context.Background(), models.DefaultSpaceID, 42, "abr-h264", profileDir); err != nil {
		t.Fatalf("准备 ABR profile 重建失败: %v", err)
	}
	if _, err := os.Stat(profileDir); !os.IsNotExist(err) {
		t.Fatalf("ABR profile 目录应被完整删除: %v", err)
	}
	var count int64
	if err := db.Model(&models.CacheAsset{}).Where("media_id = ? AND profile_id = ?", 42, "abr-h264").Count(&count).Error; err != nil {
		t.Fatalf("统计 ABR 缓存资产失败: %v", err)
	}
	if count != 0 {
		t.Fatalf("ABR profile 下逐档资产应全部清理，实际 %d", count)
	}
}
