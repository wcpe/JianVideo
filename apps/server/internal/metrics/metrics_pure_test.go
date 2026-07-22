package metrics

import (
	"testing"
	"time"
)

// TestResolveRange 穷举 range 映射：各取值的归一化名、窗口、桶大小，以及缺省/未知回退 24h。
func TestResolveRange(t *testing.T) {
	cases := []struct {
		in         string
		normalized string
		window     time.Duration
		bucket     time.Duration
	}{
		{"1h", "1h", time.Hour, 60 * time.Second},
		{"24h", "24h", 24 * time.Hour, 300 * time.Second},
		{"7d", "7d", 7 * 24 * time.Hour, 1800 * time.Second},
		// 缺省与未知值均回退 24h
		{"", "24h", 24 * time.Hour, 300 * time.Second},
		{"foo", "24h", 24 * time.Hour, 300 * time.Second},
		{"30d", "24h", 24 * time.Hour, 300 * time.Second},
	}
	for _, tc := range cases {
		got := resolveRange(tc.in)
		if got.normalized != tc.normalized {
			t.Errorf("range=%q 归一化名期望 %q，实际 %q", tc.in, tc.normalized, got.normalized)
		}
		if got.window != tc.window {
			t.Errorf("range=%q 窗口期望 %v，实际 %v", tc.in, tc.window, got.window)
		}
		if got.bucket != tc.bucket {
			t.Errorf("range=%q 桶大小期望 %v，实际 %v", tc.in, tc.bucket, got.bucket)
		}
	}
}

// TestResolveRange_PointsBounded 校验各 range 窗口/桶组合下，理论最大点数有界（≤ maxPoints）。
// 点数上界 = 窗口 / 桶；这是下采样「点数有界」契约的数学保证。
func TestResolveRange_PointsBounded(t *testing.T) {
	for _, r := range []string{"1h", "24h", "7d"} {
		rw := resolveRange(r)
		maxBuckets := int(rw.window / rw.bucket)
		if maxBuckets > maxPoints {
			t.Errorf("range=%q 理论最大点数 %d 超过上界 %d", r, maxBuckets, maxPoints)
		}
	}
}
