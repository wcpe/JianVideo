package auth

import (
	"testing"
	"time"
)

func TestLoginLimiter_LocksAfterMaxFailures(t *testing.T) {
	l := NewLoginLimiter()
	l.MaxFailures = 3
	l.Window = time.Minute
	l.LockDuration = 5 * time.Minute
	base := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	now := base
	l.now = func() time.Time { return now }

	key := AttemptKey("Alice", "1.2.3.4")
	for i := 0; i < 2; i++ {
		if locked := l.Fail(key); locked {
			t.Fatalf("第 %d 次失败不应锁定", i+1)
		}
		if ok, _ := l.Check(key); !ok {
			t.Fatal("未达阈值应允许")
		}
	}
	if locked := l.Fail(key); !locked {
		t.Fatal("第 3 次失败应锁定")
	}
	ok, retry := l.Check(key)
	if ok {
		t.Fatal("锁定后应拒绝")
	}
	if retry <= 0 || retry > 5*time.Minute {
		t.Fatalf("retryAfter 异常: %v", retry)
	}
}

func TestLoginLimiter_SuccessClears(t *testing.T) {
	l := NewLoginLimiter()
	l.MaxFailures = 2
	now := time.Now()
	l.now = func() time.Time { return now }
	key := AttemptKey("bob", "10.0.0.1")
	_ = l.Fail(key)
	l.Success(key)
	if locked := l.Fail(key); locked {
		t.Fatal("成功清零后单次失败不应锁定")
	}
}

func TestLoginLimiter_WindowResets(t *testing.T) {
	l := NewLoginLimiter()
	l.MaxFailures = 2
	l.Window = time.Minute
	base := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	now := base
	l.now = func() time.Time { return now }
	key := AttemptKey("c", "9.9.9.9")
	_ = l.Fail(key)
	now = base.Add(2 * time.Minute)
	if locked := l.Fail(key); locked {
		t.Fatal("窗口外应重置，单次失败不锁")
	}
}

func TestLoginLimiter_LockExpires(t *testing.T) {
	l := NewLoginLimiter()
	l.MaxFailures = 1
	l.LockDuration = time.Minute
	base := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	now := base
	l.now = func() time.Time { return now }
	key := AttemptKey("d", "::1")
	_ = l.Fail(key)
	if ok, _ := l.Check(key); ok {
		t.Fatal("应锁定")
	}
	now = base.Add(2 * time.Minute)
	if ok, _ := l.Check(key); !ok {
		t.Fatal("锁过期后应允许")
	}
}

func TestAttemptKeyNormalizesUsername(t *testing.T) {
	if AttemptKey("Admin", "1.1.1.1") != AttemptKey("admin", "1.1.1.1") {
		t.Fatal("用户名应大小写不敏感")
	}
}

func TestHashIPStable(t *testing.T) {
	a := HashIP("192.168.1.1")
	b := HashIP("192.168.1.1")
	if a == "" || a != b || len(a) != 16 {
		t.Fatalf("HashIP 异常: %q %q", a, b)
	}
}
