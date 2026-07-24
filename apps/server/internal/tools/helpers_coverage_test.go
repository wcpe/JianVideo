package tools

import (
	"runtime"
	"strings"
	"testing"
)

// TestNormalizeToolAndKeys 覆盖工具名规范化与设置键映射（抬高 tools 覆盖率裕量）。
func TestNormalizeToolAndKeys(t *testing.T) {
	t.Parallel()
	if normalizeTool(" FFMPEG ") != ToolFFmpeg || normalizeTool("ffprobe") != ToolFFprobe {
		t.Fatal("normalizeTool ffmpeg/ffprobe")
	}
	if normalizeTool("ImageMagick") != ToolMagick || normalizeTool("magick") != ToolMagick {
		t.Fatal("normalizeTool magick")
	}
	if normalizeTool("unknown") != "" || normalizeTool("") != "" {
		t.Fatal("normalizeTool 非法应空")
	}
	if settingKey(ToolFFmpeg) == "" || settingKey(ToolFFprobe) == "" || settingKey(ToolMagick) == "" {
		t.Fatal("settingKey 已知工具")
	}
	if settingKey("nope") != "" {
		t.Fatal("settingKey 未知应空")
	}
	if TaskIDString(42) != "42" || TaskIDString(0) != "0" {
		t.Fatal("TaskIDString")
	}
	name := defaultExecutableName("ffmpeg")
	if runtime.GOOS == "windows" {
		if name != "ffmpeg.exe" {
			t.Fatalf("windows 可执行名: %s", name)
		}
	} else if name != "ffmpeg" {
		t.Fatalf("unix 可执行名: %s", name)
	}
}

// TestIsSHA256AndLocalHost 覆盖校验助手。
func TestIsSHA256AndLocalHost(t *testing.T) {
	t.Parallel()
	if isSHA256("short") || isSHA256(strings.Repeat("g", 64)) {
		t.Fatal("isSHA256 非法")
	}
	if !isSHA256(strings.Repeat("ab", 32)) {
		t.Fatal("isSHA256 合法 hex")
	}
	if !isLocalHost("localhost") || !isLocalHost("127.0.0.1") || !isLocalHost("::1") {
		t.Fatal("isLocalHost 本地")
	}
	if isLocalHost("example.com") || isLocalHost("") {
		t.Fatal("isLocalHost 非本地")
	}
}
