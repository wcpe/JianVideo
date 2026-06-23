package transcoder

import (
	"context"
	"testing"
)

// TestCheckFFmpegPath_CurrentAvailable 当前环境 ffmpeg 可用时，CheckFFmpegPath("") 应返回 available=true 且版本非空。
// 不改全局状态，仅探测；ffmpeg 不可用的开发环境跳过。
func TestCheckFFmpegPath_CurrentAvailable(t *testing.T) {
	if !IsFFmpegAvailable() {
		t.Skip("ffmpeg 不可用，跳过当前路径检测")
	}
	saved := GetFFmpegPath()
	available, version := CheckFFmpegPath(context.Background(), "")
	if !available {
		t.Fatalf("当前 ffmpeg 应可用，实际 available=false")
	}
	if version == "" {
		t.Fatalf("可用时应返回非空版本串")
	}
	// 探测不得改写全局路径
	if GetFFmpegPath() != saved {
		t.Fatalf("CheckFFmpegPath 不应改写全局路径，前 %q 后 %q", saved, GetFFmpegPath())
	}
}

// TestCheckFFmpegPath_NonexistentUnavailable 指定不存在的可执行文件路径应返回 available=false、版本空。
func TestCheckFFmpegPath_NonexistentUnavailable(t *testing.T) {
	saved := GetFFmpegPath()
	available, version := CheckFFmpegPath(context.Background(), "jianvideo-nonexistent-ffmpeg-xyz")
	if available {
		t.Fatalf("不存在的路径应返回 available=false")
	}
	if version != "" {
		t.Fatalf("不可用时版本应为空串，实际 %q", version)
	}
	if GetFFmpegPath() != saved {
		t.Fatalf("CheckFFmpegPath 不应改写全局路径，前 %q 后 %q", saved, GetFFmpegPath())
	}
}
