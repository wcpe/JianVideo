package transcoder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultTimelinePreviewProfile版本化且ID稳定(t *testing.T) {
	profile := DefaultTimelinePreviewProfile()
	if profile.Version != 1 || profile.Interval != 5*time.Second || profile.CellWidth != 160 {
		t.Fatalf("默认 profile 参数错误: %+v", profile)
	}
	if profile.Columns != 5 || profile.Rows != 5 || profile.Format != TimelinePreviewFormatJPEG {
		t.Fatalf("默认 profile 网格或格式错误: %+v", profile)
	}
	if profile.ID != "timeline-v1-5c91f0de846e09c8" || profile.ID != TimelinePreviewProfileID(profile) {
		t.Fatalf("profile ID 必须稳定且由产物参数决定: %+v", profile)
	}
}

func TestTimelineSourceFingerprint优先有效SHA256否则使用Stat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.mp4")
	if err := os.WriteFile(path, []byte("source"), 0o600); err != nil {
		t.Fatalf("写入源文件失败: %v", err)
	}
	hash := strings.Repeat("a", 64)
	fingerprint, err := TimelineSourceFingerprint(path, hash, "sha256", false)
	if err != nil || fingerprint != "sha256-"+hash {
		t.Fatalf("有效 SHA-256 指纹错误: fingerprint=%s err=%v", fingerprint, err)
	}
	first, err := TimelineSourceFingerprint(path, hash, "sha256", true)
	if err != nil || !strings.HasPrefix(first, "stat-v1-") {
		t.Fatalf("过期哈希应回退 stat-v1: fingerprint=%s err=%v", first, err)
	}
	if err := os.WriteFile(path, []byte("source-changed"), 0o600); err != nil {
		t.Fatalf("修改源文件失败: %v", err)
	}
	changed, err := TimelineSourceFingerprint(path, "", "", true)
	if err != nil || first == changed {
		t.Fatalf("size 或 mtime 变化必须改变 stat-v1: first=%s changed=%s err=%v", first, changed, err)
	}
}

func TestTimelinePreviewPayloadKeyCacheKey和Path包含完整身份(t *testing.T) {
	payload := TimelinePreviewPayload{
		SpaceID: "space-a", MediaID: 42, ProfileID: "profile-a",
		SourceFingerprint: "source-a", GenerationID: "generation-a",
	}
	if got := TimelinePreviewTaskKey(payload); got != "preview.timeline.generate:space-a:42:profile-a:source-a:generation-a" {
		t.Fatalf("任务键错误: %s", got)
	}
	if got := TimelinePreviewCacheKey(payload); got != "timeline_preview:space-a:42:profile-a:source-a:generation-a" {
		t.Fatalf("缓存键错误: %s", got)
	}
	want := filepath.Join("root", "timeline_previews", "space-a", "42", "profile-a", "source-a", "generation-a")
	if got := TimelinePreviewGenerationPath("root", payload); got != want {
		t.Fatalf("generation 路径错误: got=%s want=%s", got, want)
	}
}

func TestDecodeTimelinePreviewPayload严格拒绝未知字段和尾随JSON(t *testing.T) {
	valid := `{"space_id":"space-a","media_id":42,"profile_id":"profile-a","source_fingerprint":"source-a","generation_id":"generation-a","force_rebuild":false}`
	if _, err := DecodeTimelinePreviewPayload(valid); err != nil {
		t.Fatalf("合法 payload 应通过: %v", err)
	}
	for _, raw := range []string{
		strings.TrimSuffix(valid, "}") + `,"unknown":1}`,
		valid + `{}`,
	} {
		if _, err := DecodeTimelinePreviewPayload(raw); err == nil {
			t.Fatalf("严格 JSON 应拒绝: %s", raw)
		}
	}
}

func TestTimelinePreviewGenerator取消时清理临时目录(t *testing.T) {
	root := t.TempDir()
	payload := TimelinePreviewPayload{SpaceID: "space-a", MediaID: 1, ProfileID: DefaultTimelinePreviewProfile().ID, SourceFingerprint: "source-a", GenerationID: "generation-a"}
	output := TimelinePreviewGenerationPath(root, payload)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	generator := NewFFmpegTimelinePreviewGenerator("ffmpeg-not-needed")
	err := generator.Generate(ctx, TimelinePreviewGenerateRequest{SourcePath: "missing.mp4", OutputDir: output, Duration: time.Minute, Profile: DefaultTimelinePreviewProfile()})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("取消应返回 context.Canceled: %v", err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("取消不得发布正式目录: %v", err)
	}
	matches, globErr := filepath.Glob(filepath.Join(filepath.Dir(output), ".generation-a.tmp-*"))
	if globErr != nil || len(matches) != 0 {
		t.Fatalf("取消后临时目录未清理: matches=%v err=%v", matches, globErr)
	}
}

func TestTimelinePreviewGenerator构造后切换全局路径使用新FFmpeg(t *testing.T) {
	oldPath := GetFFmpegPath()
	t.Cleanup(func() { SetFFmpegPath(oldPath) })
	SetFFmpegPath("构造时不存在的-ffmpeg")
	generator := NewFFmpegTimelinePreviewGenerator("")
	binary, marker := buildTimelineFFmpegRecorder(t)
	SetFFmpegPath(binary)
	request := TimelinePreviewGenerateRequest{
		SourcePath: "source.mp4", OutputDir: filepath.Join(t.TempDir(), "published"),
		Duration: time.Second, Profile: DefaultTimelinePreviewProfile(),
	}
	if err := generator.Generate(context.Background(), request); err == nil {
		t.Fatal("捕获命令应主动返回错误")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("Generate 应执行切换后的 FFmpeg 路径: %v", err)
	}
}

func TestTimelinePreviewGenerator显式路径不受全局切换影响(t *testing.T) {
	oldPath := GetFFmpegPath()
	t.Cleanup(func() { SetFFmpegPath(oldPath) })
	binary, marker := buildTimelineFFmpegRecorder(t)
	generator := NewFFmpegTimelinePreviewGenerator(binary)
	SetFFmpegPath("全局切换后的-ffmpeg")
	request := TimelinePreviewGenerateRequest{
		SourcePath: "source.mp4", OutputDir: filepath.Join(t.TempDir(), "published"),
		Duration: time.Second, Profile: DefaultTimelinePreviewProfile(),
	}
	if err := generator.Generate(context.Background(), request); err == nil {
		t.Fatal("捕获命令应主动返回错误")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("显式路径应保持构造时注入值: %v", err)
	}
}

func buildTimelineFFmpegRecorder(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "main.go")
	binary := filepath.Join(dir, "timeline-ffmpeg-recorder.exe")
	marker := filepath.Join(dir, "executed.marker")
	program := `package main

import "os"

func main() {
	_ = os.WriteFile(os.Getenv("JIANVIDEO_TIMELINE_FFMPEG_MARKER"), []byte("executed"), 0o600)
	os.Exit(1)
}
`
	if err := os.WriteFile(source, []byte(program), 0o600); err != nil {
		t.Fatalf("写入 FFmpeg 捕获程序失败: %v", err)
	}
	if output, err := exec.Command("go", "build", "-o", binary, source).CombinedOutput(); err != nil {
		t.Fatalf("构建 FFmpeg 捕获程序失败: %v: %s", err, output)
	}
	t.Setenv("JIANVIDEO_TIMELINE_FFMPEG_MARKER", marker)
	return binary, marker
}

func TestTimelinePreviewGenerator真实FFmpeg生成分页Sprite和VTT(t *testing.T) {
	ffmpeg := ffmpegPathFromEnvOrPath(t)
	if ffmpeg == "" {
		t.Skip("环境无 ffmpeg，跳过时间轴预览真实集成测试")
	}
	root := t.TempDir()
	source := filepath.Join(root, "source.mp4")
	makeTimelineTestSource(t, ffmpeg, source)
	profile := DefaultTimelinePreviewProfile()
	output := filepath.Join(root, "published")
	request := TimelinePreviewGenerateRequest{SourcePath: source, OutputDir: output, Duration: 126 * time.Second, Profile: profile}
	if err := NewFFmpegTimelinePreviewGenerator(ffmpeg).Generate(context.Background(), request); err != nil {
		t.Fatalf("生成时间轴预览失败: %v", err)
	}
	images, err := filepath.Glob(filepath.Join(output, "sprite-*.jpg"))
	if err != nil || len(images) < 2 {
		t.Fatalf("真实 fixture 应生成至少两张 sprite: images=%v err=%v", images, err)
	}
	assertTimelineSpritesDecodable(t, images, profile)
	vtt, err := os.ReadFile(filepath.Join(output, "index.vtt"))
	if err != nil || !strings.Contains(string(vtt), "sprite-002.jpg#xywh=") {
		t.Fatalf("VTT 应引用第二张 sprite: err=%v\n%s", err, vtt)
	}
}

func makeTimelineTestSource(t *testing.T, ffmpeg, target string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=duration=126:size=160x90:rate=1",
		"-c:v", "mpeg4", "-q:v", "8", target)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("创建时间轴 testsrc 失败: %v: %s", err, output)
	}
}

func assertTimelineSpritesDecodable(t *testing.T, paths []string, profile TimelinePreviewProfile) {
	t.Helper()
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			t.Fatalf("打开 sprite 失败: %v", err)
		}
		config, format, decodeErr := image.DecodeConfig(file)
		_ = file.Close()
		if decodeErr != nil || format != "jpeg" || config.Width != profile.CellWidth*profile.Columns {
			t.Fatalf("sprite 不可解码或尺寸错误: path=%s config=%+v format=%s err=%v", path, config, format, decodeErr)
		}
	}
}

func TestTimelinePreviewVTT覆盖完整时长且Cue不重叠(t *testing.T) {
	profile := DefaultTimelinePreviewProfile()
	vtt, cues, err := BuildTimelinePreviewVTT(12*time.Second, profile, 18)
	if err != nil {
		t.Fatalf("生成 VTT 失败: %v", err)
	}
	if cues != 3 || !strings.Contains(vtt, "00:00:10.000 --> 00:00:12.000") {
		t.Fatalf("VTT 尾 cue 应夹到完整时长: cues=%d\n%s", cues, vtt)
	}
	if strings.Count(vtt, " --> ") != cues || !strings.HasPrefix(vtt, "WEBVTT\n\n") {
		t.Fatalf("VTT 格式错误: %s", vtt)
	}
}

func TestTimelinePreviewProfileJSON稳定(t *testing.T) {
	data, err := json.Marshal(DefaultTimelinePreviewProfile())
	if err != nil {
		t.Fatalf("编码 profile 失败: %v", err)
	}
	if !strings.Contains(string(data), fmt.Sprintf(`"interval_ms":%d`, 5000)) {
		t.Fatalf("profile JSON 应使用稳定毫秒值: %s", data)
	}
}
