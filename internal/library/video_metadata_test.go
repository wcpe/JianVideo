package library

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestParseVideoProbe 纯函数解析 ffprobe JSON，确定性回归（不依赖真实 ffprobe）。
func TestParseVideoProbe(t *testing.T) {
	t.Run("完整容器与流", func(t *testing.T) {
		raw := []byte(`{
			"format": {
				"duration": "12.500000",
				"bit_rate": "1048576",
				"tags": {"creation_time": "2023-05-20T18:30:00.000000Z"}
			},
			"streams": [
				{"codec_type": "video", "codec_name": "h264", "width": 1920, "height": 1080},
				{"codec_type": "audio", "codec_name": "aac"}
			]
		}`)
		meta := parseVideoProbe(raw)
		if meta.Duration != 12.5 {
			t.Fatalf("时长期望 12.5, 实际 %v", meta.Duration)
		}
		if meta.Bitrate != 1048576 {
			t.Fatalf("码率期望 1048576, 实际 %d", meta.Bitrate)
		}
		if meta.VideoCodec != "h264" {
			t.Fatalf("视频编码期望 h264, 实际 %q", meta.VideoCodec)
		}
		if meta.AudioCodec != "aac" {
			t.Fatalf("音频编码期望 aac, 实际 %q", meta.AudioCodec)
		}
		if meta.Width != 1920 || meta.Height != 1080 {
			t.Fatalf("分辨率期望 1920x1080, 实际 %dx%d", meta.Width, meta.Height)
		}
		want := time.Date(2023, 5, 20, 18, 30, 0, 0, time.UTC)
		if !meta.CreationTime.Equal(want) {
			t.Fatalf("creation_time 期望 %v, 实际 %v", want, meta.CreationTime)
		}
	})

	t.Run("多视频流取首个", func(t *testing.T) {
		raw := []byte(`{"streams": [
			{"codec_type": "video", "codec_name": "hevc", "width": 3840, "height": 2160},
			{"codec_type": "video", "codec_name": "h264", "width": 640, "height": 480}
		]}`)
		meta := parseVideoProbe(raw)
		if meta.VideoCodec != "hevc" || meta.Width != 3840 || meta.Height != 2160 {
			t.Fatalf("应取首个视频流 hevc/3840x2160, 实际 %q/%dx%d", meta.VideoCodec, meta.Width, meta.Height)
		}
	})

	t.Run("无效时长码率不报错", func(t *testing.T) {
		raw := []byte(`{"format": {"duration": "N/A", "bit_rate": ""}, "streams": []}`)
		meta := parseVideoProbe(raw)
		if meta.Duration != 0 || meta.Bitrate != 0 {
			t.Fatalf("无效时长/码率应为零值, 实际 %v / %d", meta.Duration, meta.Bitrate)
		}
	})

	t.Run("非法 JSON 返回零值", func(t *testing.T) {
		meta := parseVideoProbe([]byte("not-json"))
		if meta != (videoMetadata{}) {
			t.Fatalf("非法 JSON 应返回零值结构, 实际 %+v", meta)
		}
	})
}

// ffmpegAvailableForTest 检查测试机是否可用 ffmpeg/ffprobe，缺失则跳过依赖真实转码的用例。
func ffmpegAvailableForTest(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg 不可用，跳过视频元数据集成测试")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe 不可用，跳过视频元数据集成测试")
	}
}

// generateTestVideoForLibrary 用 ffmpeg 生成 1 秒 H.264 + AAC 测试视频（320x240）。
func generateTestVideoForLibrary(t *testing.T, outputPath string) {
	t.Helper()
	args := []string{
		"-y",
		"-f", "lavfi", "-i", "testsrc=duration=1:size=320x240:rate=24",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-c:v", "libx264",
		"-c:a", "aac",
		"-t", "1",
		outputPath,
	}
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("生成测试视频失败: %v, 输出: %s", err, string(out))
	}
}

// TestCreateMediaFile_VideoMetadata 复现并回归：扫描入库视频时应探测并写入
// 时长 / 视频编码 / 音频编码 / 分辨率。修复前这些字段恒为零值（本测试先红）。
func TestCreateMediaFile_VideoMetadata(t *testing.T) {
	ffmpegAvailableForTest(t)
	svc, _ := newTestService(t)

	dir := t.TempDir()
	videoPath := filepath.Join(dir, "clip.mp4")
	generateTestVideoForLibrary(t, videoPath)

	info, err := os.Stat(videoPath)
	if err != nil {
		t.Fatalf("读取测试视频失败: %v", err)
	}

	mf, err := svc.CreateMediaFile(1, videoPath, info.Size())
	if err != nil {
		t.Fatalf("入库失败: %v", err)
	}

	if mf.Duration <= 0 {
		t.Fatalf("时长应被探测写入（>0），实际 %v", mf.Duration)
	}
	if mf.VideoCodec != "h264" {
		t.Fatalf("视频编码期望 h264, 实际 %q", mf.VideoCodec)
	}
	if mf.AudioCodec != "aac" {
		t.Fatalf("音频编码期望 aac, 实际 %q", mf.AudioCodec)
	}
	if mf.Width != 320 || mf.Height != 240 {
		t.Fatalf("分辨率期望 320x240, 实际 %dx%d", mf.Width, mf.Height)
	}
}
