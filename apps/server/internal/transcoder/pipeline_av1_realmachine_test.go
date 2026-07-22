package transcoder

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRealMachine_AV1Pipeline 真机验证（FR-50）：设首选 av1 → 选码走 libsvtav1，
// 经真实 ffmpeg 用生产路径 NewPipelineForCodec("av1") + Run 转一小段，
// 再用 ffprobe 断言输出视频编码=av1。
//
// 仅在显式开启 JIANVIDEO_REALMACHINE=1 且本机 ffmpeg/ffprobe 可用、编入 libsvtav1 时运行；
// 否则跳过（CI / 无 ffmpeg 环境不阻塞）。硬件 AV1（av1_amf 等）本机无法验，标注「待硬件」。
func TestRealMachine_AV1Pipeline(t *testing.T) {
	if os.Getenv("JIANVIDEO_REALMACHINE") != "1" {
		t.Skip("跳过真机测试（设 JIANVIDEO_REALMACHINE=1 启用）")
	}
	if !IsFFmpegAvailable() {
		t.Skip("ffmpeg 不可用，跳过真机测试")
	}

	ctx := context.Background()

	// 设实测快照：仅软件 av1（libsvtav1）可用，模拟「首选 av1、本机无硬件 av1」。
	setProbeSnapshot([]EncoderProbeResult{
		{Encoder: "libsvtav1", Family: "software", Codec: "av1", Compiled: true, TestedOK: true},
	})
	defer setProbeSnapshot(nil)

	// 选码断言：经生产路径（selectEncoder：硬件无 av1 → 软件兜底）应选出 libsvtav1。
	// 注：SelectEncoderForCodec 仅在硬件家族中选，软件兜底由 selectEncoder 负责。
	enc, _ := selectEncoder("av1")
	if enc != "libsvtav1" {
		t.Fatalf("av1 选码期望 libsvtav1，实际 enc=%q", enc)
	}

	// 经生产路径构建 av1 管道。
	p := NewPipelineForCodec("av1")
	if p.encoderName != "libsvtav1" {
		t.Fatalf("管道编码器期望 libsvtav1，实际 %q", p.encoderName)
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "src.mp4")
	if err := exec.CommandContext(ctx, ffmpegPath, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=10:duration=2",
		"-pix_fmt", "yuv420p", src).Run(); err != nil {
		t.Fatalf("生成测试源失败: %v", err)
	}

	// 用真实 ffmpeg 经生产路径（buildArgs + Run）转码到 TS 文件（Run 写 stdout，这里重定向到文件）。
	out := filepath.Join(dir, "out.ts")
	f, err := os.Create(out)
	if err != nil {
		t.Fatalf("创建输出文件失败: %v", err)
	}
	defer func() { _ = f.Close() }()

	runCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := p.Run(runCtx, src, f); err != nil {
		t.Fatalf("av1 转码失败: %v", err)
	}
	_ = f.Close()

	// 生产路径确实产出非空 TS（管道按 av1 参数化执行成功）。
	if fi, statErr := os.Stat(out); statErr != nil || fi.Size() == 0 {
		t.Fatalf("生产路径 TS 输出为空或不存在: err=%v", statErr)
	}

	// ffprobe 断言编码=av1：MPEG-TS 无 AV1 标准流类型，TS 内 AV1 会被探测为 bin_data
	// （这正是 FR-51 fMP4 输出要解决的容器问题，不在 FR-50 范围）。故用与管道相同的
	// 选码编码器 + 相同输出参数（CodecOutputParams）封装进可被 ffprobe 识别的 Matroska，
	// 对「该编码器确实产出 av1 流」做确定性断言。
	params, _ := CodecOutputParams("av1")
	mkv := filepath.Join(dir, "out.mkv")
	mkvArgs := []string{"-hide_banner", "-loglevel", "error", "-y",
		"-i", src, "-c:v", p.encoderName, "-pix_fmt", params.PixFmt}
	mkvArgs = append(mkvArgs, params.ExtraArgs...)
	mkvArgs = append(mkvArgs, "-an", "-f", "matroska", mkv)
	if err := exec.CommandContext(ctx, ffmpegPath, mkvArgs...).Run(); err != nil {
		t.Fatalf("av1 转 Matroska 失败: %v", err)
	}
	probe := exec.CommandContext(ctx, ffprobePath, "-hide_banner", "-v", "error",
		"-select_streams", "v:0", "-show_entries", "stream=codec_name",
		"-of", "csv=p=0", mkv)
	codecOut, err := probe.Output()
	if err != nil {
		t.Fatalf("ffprobe 失败: %v", err)
	}
	got := strings.TrimSpace(string(codecOut))
	if got != "av1" {
		t.Fatalf("输出视频编码期望 av1，实际 %q", got)
	}
	t.Logf("真机 AV1 验证通过：选码=%s，生产路径产出非空 TS，ffprobe 断言编码=%s", p.encoderName, got)
}
