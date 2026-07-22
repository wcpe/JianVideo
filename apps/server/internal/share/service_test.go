package share

import (
	"errors"
	"sync"
	"sync/atomic"
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
	// :memory: 每个连接是独立库；并发消费测试需共享同一库，限制单连接。
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.Share{}); err != nil {
		t.Fatalf("迁移 shares 表失败: %v", err)
	}
	return db
}

// TestCreateGeneratesUnguessableToken 创建分享生成不可猜 token，且每次不同。
func TestCreateGeneratesUnguessableToken(t *testing.T) {
	svc := NewService(setupTestDB(t))

	s1, err := svc.Create(models.ShareResourceMedia, 1, nil, "", 0)
	if err != nil {
		t.Fatalf("创建分享失败: %v", err)
	}
	if len(s1.Token) < 32 {
		t.Fatalf("token 应足够长且不可猜, 实际长度 %d", len(s1.Token))
	}
	s2, _ := svc.Create(models.ShareResourceMedia, 1, nil, "", 0)
	if s1.Token == s2.Token {
		t.Fatal("两次创建的 token 不应相同")
	}
}

// TestCreateRejectsInvalidType 非法资源类型被拒。
func TestCreateRejectsInvalidType(t *testing.T) {
	svc := NewService(setupTestDB(t))
	if _, err := svc.Create("bogus", 1, nil, "", 0); err == nil {
		t.Fatal("非法资源类型应报错")
	}
}

// TestGetValidShare 取有效分享返回记录。
func TestGetValidShare(t *testing.T) {
	svc := NewService(setupTestDB(t))
	created, _ := svc.Create(models.ShareResourceAlbum, 9, nil, "", 0)

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
	created, _ := svc.Create(models.ShareResourceMedia, 5, &past, "", 0)

	if _, err := svc.Get(created.Token); !errors.Is(err, ErrShareExpired) {
		t.Fatalf("过期 token 应返回 ErrShareExpired, 实际 %v", err)
	}
}

// TestGetFutureExpiryValid 未来过期时间内有效。
func TestGetFutureExpiryValid(t *testing.T) {
	svc := NewService(setupTestDB(t))
	future := time.Now().Add(time.Hour)
	created, _ := svc.Create(models.ShareResourceMedia, 5, &future, "", 0)

	if _, err := svc.Get(created.Token); err != nil {
		t.Fatalf("未过期 token 应有效, 实际 %v", err)
	}
}

// TestRevoke 撤销后不可再取。
func TestRevoke(t *testing.T) {
	svc := NewService(setupTestDB(t))
	created, _ := svc.Create(models.ShareResourceMedia, 1, nil, "", 0)

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
	_, _ = svc.Create(models.ShareResourceMedia, 1, nil, "", 0)
	past := time.Now().Add(-time.Hour)
	_, _ = svc.Create(models.ShareResourceAlbum, 2, &past, "", 0)

	all, err := svc.List()
	if err != nil {
		t.Fatalf("列出分享失败: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("应有 2 条分享（含过期）, 实际 %d", len(all))
	}
}

// TestCreateWithPasswordHashesNotPlaintext 带密码创建：PasswordHash 为 bcrypt 哈希、非明文（FR-78）。
func TestCreateWithPasswordHashesNotPlaintext(t *testing.T) {
	svc := NewService(setupTestDB(t))
	sh, err := svc.Create(models.ShareResourceMedia, 1, nil, "s3cret", 0)
	if err != nil {
		t.Fatalf("带密码创建失败: %v", err)
	}
	if sh.PasswordHash == "" {
		t.Fatal("设密码后 PasswordHash 不应为空")
	}
	if sh.PasswordHash == "s3cret" {
		t.Fatal("PasswordHash 绝不能是明文密码")
	}
}

// TestVerifyPassword 校验访问密码：无密码放行、正确放行、错误拒绝（FR-78）。
func TestVerifyPassword(t *testing.T) {
	svc := NewService(setupTestDB(t))

	noPwd, _ := svc.Create(models.ShareResourceMedia, 1, nil, "", 0)
	if err := svc.VerifyPassword(noPwd, "any"); err != nil {
		t.Fatalf("无密码分享应放行, 实际 %v", err)
	}

	withPwd, _ := svc.Create(models.ShareResourceMedia, 1, nil, "open-sesame", 0)
	if err := svc.VerifyPassword(withPwd, "open-sesame"); err != nil {
		t.Fatalf("正确密码应放行, 实际 %v", err)
	}
	if err := svc.VerifyPassword(withPwd, "wrong"); !errors.Is(err, ErrShareForbidden) {
		t.Fatalf("错误密码应返回 ErrShareForbidden, 实际 %v", err)
	}
}

// TestConsumeUseAtomicAndExhausted 限次：自增到上限后耗尽，无限次不计数（FR-78）。
func TestConsumeUseAtomicAndExhausted(t *testing.T) {
	svc := NewService(setupTestDB(t))

	// 限 2 次：前两次成功、第三次耗尽
	limited, _ := svc.Create(models.ShareResourceMedia, 1, nil, "", 2)
	if err := svc.ConsumeUse(limited.Token); err != nil {
		t.Fatalf("第 1 次消费应成功, 实际 %v", err)
	}
	if err := svc.ConsumeUse(limited.Token); err != nil {
		t.Fatalf("第 2 次消费应成功, 实际 %v", err)
	}
	if err := svc.ConsumeUse(limited.Token); !errors.Is(err, ErrShareExhausted) {
		t.Fatalf("第 3 次应耗尽返回 ErrShareExhausted, 实际 %v", err)
	}
	// UsedCount 应停在上限 2（自增不越界）
	got, _ := svc.Get(limited.Token)
	if got.UsedCount != 2 {
		t.Fatalf("UsedCount 应停在 2, 实际 %d", got.UsedCount)
	}

	// 无限次：多次消费均成功、UsedCount 不增
	unlimited, _ := svc.Create(models.ShareResourceMedia, 1, nil, "", 0)
	for i := 0; i < 5; i++ {
		if err := svc.ConsumeUse(unlimited.Token); err != nil {
			t.Fatalf("无限次第 %d 次消费应成功, 实际 %v", i+1, err)
		}
	}
	gotUnlimited, _ := svc.Get(unlimited.Token)
	if gotUnlimited.UsedCount != 0 {
		t.Fatalf("无限次分享 UsedCount 不应自增, 实际 %d", gotUnlimited.UsedCount)
	}
}

// TestConsumeUseConcurrentNoOversend 并发消费不超发（FR-78 高风险：限次并发原子自增）。
func TestConsumeUseConcurrentNoOversend(t *testing.T) {
	svc := NewService(setupTestDB(t))
	const limit = 5
	const goroutines = 20
	limited, _ := svc.Create(models.ShareResourceMedia, 1, nil, "", limit)

	var success, exhausted int32
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := svc.ConsumeUse(limited.Token)
			switch {
			case err == nil:
				atomic.AddInt32(&success, 1)
			case errors.Is(err, ErrShareExhausted):
				atomic.AddInt32(&exhausted, 1)
			default:
				t.Errorf("意外错误: %v", err)
			}
		}()
	}
	wg.Wait()

	if int(success) != limit {
		t.Fatalf("并发下成功消费应恰为上限 %d（不超发）, 实际 %d", limit, success)
	}
	got, _ := svc.Get(limited.Token)
	if got.UsedCount != limit {
		t.Fatalf("UsedCount 应恰为上限 %d, 实际 %d", limit, got.UsedCount)
	}
}
