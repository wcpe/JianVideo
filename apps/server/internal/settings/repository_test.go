package settings

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

func setupRepoDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.Setting{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	return db
}

func TestRepositoryUpsertAndGet(t *testing.T) {
	repo := NewRepository(setupRepoDB(t))
	ctx := context.Background()
	if err := repo.Upsert(ctx, KeyScanInterval, "60"); err != nil {
		t.Fatalf("upsert 失败: %v", err)
	}
	v, err := repo.Get(ctx, KeyScanInterval)
	if err != nil || v != "60" {
		t.Fatalf("Get 期望 60, 实际 %q err=%v", v, err)
	}
	if err := repo.Upsert(ctx, KeyScanInterval, "120"); err != nil {
		t.Fatalf("覆盖 upsert 失败: %v", err)
	}
	v, err = repo.Get(ctx, KeyScanInterval)
	if err != nil || v != "120" {
		t.Fatalf("覆盖后期望 120, 实际 %q err=%v", v, err)
	}
}

func TestRepositoryGetMissingEmpty(t *testing.T) {
	repo := NewRepository(setupRepoDB(t))
	v, err := repo.Get(context.Background(), "nope")
	if err != nil || v != "" {
		t.Fatalf("缺失键应空串, 实际 %q err=%v", v, err)
	}
}

func TestRepositoryTransaction(t *testing.T) {
	repo := NewRepository(setupRepoDB(t))
	ctx := context.Background()
	err := repo.Transaction(ctx, func(tx TxRepository) error {
		if err := tx.Upsert(ctx, KeyScanInterval, "1"); err != nil {
			return err
		}
		m, err := tx.GetMany(ctx, []string{KeyScanInterval})
		if err != nil {
			return err
		}
		if m[KeyScanInterval] != "1" {
			t.Fatalf("事务内读期望 1, 实际 %q", m[KeyScanInterval])
		}
		return nil
	})
	if err != nil {
		t.Fatalf("事务失败: %v", err)
	}
	v, _ := repo.Get(ctx, KeyScanInterval)
	if v != "1" {
		t.Fatalf("事务提交后期望 1, 实际 %q", v)
	}
}
