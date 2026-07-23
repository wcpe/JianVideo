package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"
	"unicode"
)

// 登录防爆破默认策略（FR2-062）。
const (
	DefaultLoginMaxFailures   = 10
	DefaultLoginWindow        = 10 * time.Minute
	DefaultLoginLockDuration  = 15 * time.Minute
)

// LoginLimiter 按「规范化用户名 + 客户端 IP」滑动窗口限流（进程内）。
// 单二进制默认足够；多实例部署可二期换持久化表。
type LoginLimiter struct {
	mu           sync.Mutex
	attempts     map[string]*loginAttempt
	MaxFailures  int
	Window       time.Duration
	LockDuration time.Duration
	now          func() time.Time
}

type loginAttempt struct {
	failures    int
	windowStart time.Time
	lockedUntil time.Time
}

// NewLoginLimiter 创建限流器；零值字段用默认策略。
func NewLoginLimiter() *LoginLimiter {
	return &LoginLimiter{
		attempts:     make(map[string]*loginAttempt),
		MaxFailures:  DefaultLoginMaxFailures,
		Window:       DefaultLoginWindow,
		LockDuration: DefaultLoginLockDuration,
		now:          time.Now,
	}
}

// AttemptKey 生成限流键（小写用户名 + IP）。
func AttemptKey(username, clientIP string) string {
	u := strings.ToLower(strings.TrimSpace(username))
	// 去掉控制字符，避免异常键。
	u = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, u)
	ip := strings.TrimSpace(clientIP)
	if ip == "" {
		ip = "unknown"
	}
	return u + "|" + ip
}

// HashIP 对 IP 做短哈希，供审计脱敏。
func HashIP(clientIP string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(clientIP)))
	return hex.EncodeToString(sum[:8])
}

// Check 是否允许尝试登录。不允许时返回剩余锁定时间。
func (l *LoginLimiter) Check(key string) (allowed bool, retryAfter time.Duration) {
	if l == nil {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.nowFn()
	st := l.attempts[key]
	if st == nil {
		return true, 0
	}
	if !st.lockedUntil.IsZero() && now.Before(st.lockedUntil) {
		return false, st.lockedUntil.Sub(now)
	}
	// 锁已过期：清零以便新窗口。
	if !st.lockedUntil.IsZero() && !now.Before(st.lockedUntil) {
		delete(l.attempts, key)
	}
	return true, 0
}

// Fail 记录一次失败；若达到阈值则锁定并返回 locked=true。
func (l *LoginLimiter) Fail(key string) (locked bool) {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.nowFn()
	st := l.attempts[key]
	if st == nil {
		st = &loginAttempt{windowStart: now}
		l.attempts[key] = st
	}
	// 窗口外：重置计数。
	if now.Sub(st.windowStart) > l.window() {
		st.failures = 0
		st.windowStart = now
		st.lockedUntil = time.Time{}
	}
	st.failures++
	if st.failures >= l.maxFailures() {
		st.lockedUntil = now.Add(l.lockDuration())
		return true
	}
	return false
}

// Success 登录成功，清除该键状态。
func (l *LoginLimiter) Success(key string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

func (l *LoginLimiter) nowFn() time.Time {
	if l.now != nil {
		return l.now()
	}
	return time.Now()
}

func (l *LoginLimiter) maxFailures() int {
	if l.MaxFailures <= 0 {
		return DefaultLoginMaxFailures
	}
	return l.MaxFailures
}

func (l *LoginLimiter) window() time.Duration {
	if l.Window <= 0 {
		return DefaultLoginWindow
	}
	return l.Window
}

func (l *LoginLimiter) lockDuration() time.Duration {
	if l.LockDuration <= 0 {
		return DefaultLoginLockDuration
	}
	return l.LockDuration
}
