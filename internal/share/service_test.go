package share

import (
	"errors"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.Share{}); err != nil {
		t.Fatalf("迁移 shares 表失败: %v", err)
	}
	return db
}

// TestCreateGeneratesUnguessableToken 创建分享生成不可猜 token，且每次不同。
func TestCreateGeneratesUnguessableToken(t *testing.T) {
	svc := NewService(setupTestDB(t))

	s1, err := svc.Create(models.ShareResourceMedia, 1, nil)
	if err != nil {
		t.Fatalf("创建分享失败: %v", err)
	}
	if len(s1.Token) < 32 {
		t.Fatalf("token 应足够长且不可猜, 实际长度 %d", len(s1.Token))
	}
	s2, _ := svc.Create(models.ShareResourceMedia, 1, nil)
	if s1.Token == s2.Token {
		t.Fatal("两次创建的 token 不应相同")
	}
}

// TestCreateRejectsInvalidType 非法资源类型被拒。
func TestCreateRejectsInvalidType(t *testing.T) {
	svc := NewService(setupTestDB(t))
	if _, err := svc.Create("bogus", 1, nil); err == nil {
		t.Fatal("非法资源类型应报错")
	}
}

// TestGetValidShare 取有效分享返回记录。
func TestGetValidShare(t *testing.T) {
	svc := NewService(setupTestDB(t))
	created, _ := svc.Create(models.ShareResourceAlbum, 9, nil)

	got, err := svc.Get(created.Token)
	if err != nil {
		t.Fatalf("取有效分享失败: %v", err)
	}
	if got.ResourceType != models.ShareResourceAlbum || got.ResourceID != 9 {
		t.Fatalf("分享内容不符: %+v", got)
	}
}

// TestGetMissingReturnsNotFound 不存在 token 返回 ErrShareNotFound。
func TestGetMissingReturnsNotFound(t *testing.T) {
	svc := NewService(setupTestDB(t))
	if _, err := svc.Get("nonexistent-token"); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("不存在 token 应返回 ErrShareNotFound, 实际 %v", err)
	}
}

// TestGetExpiredReturnsExpired 过期 token 返回 ErrShareExpired。
func TestGetExpiredReturnsExpired(t *testing.T) {
	svc := NewService(setupTestDB(t))
	past := time.Now().Add(-time.Hour)
	created, _ := svc.Create(models.ShareResourceMedia, 5, &past)

	if _, err := svc.Get(created.Token); !errors.Is(err, ErrShareExpired) {
		t.Fatalf("过期 token 应返回 ErrShareExpired, 实际 %v", err)
	}
}

// TestGetFutureExpiryValid 未来过期时间内有效。
func TestGetFutureExpiryValid(t *testing.T) {
	svc := NewService(setupTestDB(t))
	future := time.Now().Add(time.Hour)
	created, _ := svc.Create(models.ShareResourceMedia, 5, &future)

	if _, err := svc.Get(created.Token); err != nil {
		t.Fatalf("未过期 token 应有效, 实际 %v", err)
	}
}

// TestRevoke 撤销后不可再取。
func TestRevoke(t *testing.T) {
	svc := NewService(setupTestDB(t))
	created, _ := svc.Create(models.ShareResourceMedia, 1, nil)

	if err := svc.Revoke(created.Token); err != nil {
		t.Fatalf("撤销失败: %v", err)
	}
	if _, err := svc.Get(created.Token); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("撤销后应不存在, 实际 %v", err)
	}
}

// TestList 列出全部分享（含过期，供管理展示）。
func TestList(t *testing.T) {
	svc := NewService(setupTestDB(t))
	_, _ = svc.Create(models.ShareResourceMedia, 1, nil)
	past := time.Now().Add(-time.Hour)
	_, _ = svc.Create(models.ShareResourceAlbum, 2, &past)

	all, err := svc.List()
	if err != nil {
		t.Fatalf("列出分享失败: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("应有 2 条分享（含过期）, 实际 %d", len(all))
	}
}
