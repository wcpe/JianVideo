package update

import "testing"

// TestIsNewer 覆盖版本比较各分支：数值比较、正式 vs 预发布、滚动预发布、解析失败兜底。
func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v0.6.3", "0.6.2", true},                 // 修订更高
		{"v0.6.2", "0.6.2", false},                // 相同
		{"v0.6.1", "0.6.2", false},                // 更低
		{"v1.0.0", "0.9.9", true},                 // 主版本更高
		{"0.6.2", "0.6.2-dev.abc", true},          // 正式版优于同基线预发布
		{"0.6.2-dev.abc", "0.6.2", false},         // 预发布不覆盖同基线正式版
		{"0.6.2-dev.def", "0.6.2-dev.abc", true},  // 两预发布标识不同→有更新
		{"0.6.2-dev.abc", "0.6.2-dev.abc", false}, // 两预发布完全相同
		{"v0.7.0-dev.x", "0.6.2", true},           // 更高基线的预发布
		{"garbage", "0.6.2", true},                // 无法解析且不等→保守有更新
		{"", "0.6.2", false},                      // 空 latest→无更新
	}
	for _, c := range cases {
		if got := isNewer(c.latest, c.current); got != c.want {
			t.Errorf("isNewer(%q, %q)=%v, 期望 %v", c.latest, c.current, got, c.want)
		}
	}
}

// TestParseVersion 校验解析的成功与失败用例。
func TestParseVersion(t *testing.T) {
	if v := parseVersion("v1.2.3"); !v.ok || v.major != 1 || v.minor != 2 || v.patch != 3 || v.pre != "" {
		t.Errorf("解析 v1.2.3 异常: %+v", v)
	}
	if v := parseVersion("1.2.3-dev.abc"); !v.ok || v.pre != "dev.abc" {
		t.Errorf("解析预发布异常: %+v", v)
	}
	for _, bad := range []string{"", "1.2", "1.2.x", "v1"} {
		if parseVersion(bad).ok {
			t.Errorf("%q 不应解析成功", bad)
		}
	}
}
