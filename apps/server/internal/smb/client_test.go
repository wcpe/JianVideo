package smb

import "testing"

// TestNormalizeDialAddr 校验拨号地址补默认端口逻辑：
// 不含端口的主机补 445；已含端口的原样保留。回归 SMB「missing port in address」连接失败。
func TestNormalizeDialAddr(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"localhost", "localhost:445"},
		{"127.0.0.1", "127.0.0.1:445"},
		{"nas.local", "nas.local:445"},
		{"192.168.1.10:1445", "192.168.1.10:1445"},
		{"localhost:445", "localhost:445"},
	}
	for _, c := range cases {
		if got := normalizeDialAddr(c.in); got != c.want {
			t.Errorf("normalizeDialAddr(%q)=%q, 期望 %q", c.in, got, c.want)
		}
	}
}
