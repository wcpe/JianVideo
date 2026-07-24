package smb

import (
	"errors"
	"io/fs"
	"os"
	"testing"
)

// TestNormalizePath 覆盖 SMB 路径规范化与路径遍历清洗。
func TestNormalizePath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want string
	}{
		{".", ""},
		{"/a/b", "a/b"},
		{`\a\b`, "a/b"},
		{"a/../b", "a//b"}, // ".." 被清空，不引入父目录
		{"", ""},
		{"share/file.mp4", "share/file.mp4"},
	}
	for _, tc := range cases {
		if got := normalize(tc.in); got != tc.want {
			t.Fatalf("normalize(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

// TestMapSMBError 覆盖错误映射到 fs 标准错误。
func TestMapSMBError(t *testing.T) {
	t.Parallel()
	if mapSMBError(nil) != nil {
		t.Fatal("nil 应返回 nil")
	}
	if !errors.Is(mapSMBError(os.ErrNotExist), fs.ErrNotExist) {
		t.Fatal("NotExist 映射")
	}
	if !errors.Is(mapSMBError(os.ErrPermission), fs.ErrPermission) {
		t.Fatal("Permission 映射")
	}
	other := errors.New("other smb")
	if mapSMBError(other) != other {
		t.Fatal("其它错误应原样返回")
	}
}
