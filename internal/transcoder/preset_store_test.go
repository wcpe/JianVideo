package transcoder

import (
	"errors"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// newPresetTestDB 创建带 TranscodePreset 表的单连接内存库。
func newPresetTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.TranscodePreset{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	return db
}

// TestPresetStore_CreateListGet 建预设后可列出与按 ID 取回，编码被归一化。
func TestPresetStore_CreateListGet(t *testing.T) {
	store := NewPresetStore(newPresetTestDB(t))

	p, err := store.Create("1080p HEVC", "hevc", 1920, 1080)
	if err != nil {
		t.Fatalf("创建预设失败: %v", err)
	}
	if p.ID == 0 {
		t.Fatal("创建应返回非零 ID")
	}
	// hevc 归一化为 h265
	if p.Codec != "h265" {
		t.Fatalf("编码应归一化为 h265, 实际 %q", p.Codec)
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("列出预设失败: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("应有 1 条预设, 实际 %d", len(list))
	}

	got, err := store.Get(p.ID)
	if err != nil {
		t.Fatalf("取预设失败: %v", err)
	}
	if got.Name != "1080p HEVC" || got.Width != 1920 || got.Height != 1080 {
		t.Fatalf("取回预设字段不符: %+v", got)
	}
}

// TestPresetStore_CreateRejectInvalid 非法字段被拒（空名 / 不支持编码 / 负分辨率）。
func TestPresetStore_CreateRejectInvalid(t *testing.T) {
	store := NewPresetStore(newPresetTestDB(t))

	if _, err := store.Create("  ", "h264", 0, 0); !errors.Is(err, ErrPresetNameEmpty) {
		t.Fatalf("空名应被拒, 实际 err=%v", err)
	}
	if _, err := store.Create("x", "mpeg2", 0, 0); !errors.Is(err, ErrPresetCodecInvalid) {
		t.Fatalf("不支持编码应被拒, 实际 err=%v", err)
	}
	if _, err := store.Create("x", "h264", -1, 0); !errors.Is(err, ErrPresetDimensionNegative) {
		t.Fatalf("负分辨率应被拒, 实际 err=%v", err)
	}
}

// TestPresetStore_UpdateDelete 更新与删除预设，含不存在的边界。
func TestPresetStore_UpdateDelete(t *testing.T) {
	store := NewPresetStore(newPresetTestDB(t))

	p, _ := store.Create("orig", "h264", 0, 0)

	updated, err := store.Update(p.ID, "new", "av1", 1280, 720)
	if err != nil {
		t.Fatalf("更新预设失败: %v", err)
	}
	if updated.Name != "new" || updated.Codec != "av1" || updated.Width != 1280 {
		t.Fatalf("更新后字段不符: %+v", updated)
	}

	if _, err := store.Update(99999, "x", "h264", 0, 0); !errors.Is(err, ErrPresetNotFound) {
		t.Fatalf("更新不存在预设应返回 ErrPresetNotFound, 实际 %v", err)
	}

	if err := store.Delete(p.ID); err != nil {
		t.Fatalf("删除预设失败: %v", err)
	}
	if err := store.Delete(p.ID); !errors.Is(err, ErrPresetNotFound) {
		t.Fatalf("删除不存在预设应返回 ErrPresetNotFound, 实际 %v", err)
	}
}
