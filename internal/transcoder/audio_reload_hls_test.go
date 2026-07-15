package transcoder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/wcpe/JianVideo/internal/db/models"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

func intPointer(value int) *int {
	return &value
}

func TestAudioReloadProfileIDIsSafeAndDeterministic(t *testing.T) {
	trackID := "../音轨/track:2?language=jpn"
	sum := sha256.Sum256([]byte(trackID))
	want := "audio-h264-aac-" + hex.EncodeToString(sum[:12])

	got := AudioReloadProfileID(trackID)
	if got != want {
		t.Fatalf("音轨 profile 派生不符: got=%q want=%q", got, want)
	}
	if !regexp.MustCompile(`^audio-h264-aac-[0-9a-f]{24}$`).MatchString(got) {
		t.Fatalf("音轨 profile 不是安全路径 token: %q", got)
	}
	if AudioReloadProfileID(trackID) != got || AudioReloadProfileID(trackID+"-other") == got {
		t.Fatal("音轨 profile 应稳定且区分不同 track ID")
	}
}

func TestIsAudioReloadProfileIDMatchesOnlyDerivedFormat(t *testing.T) {
	valid := AudioReloadProfileID("audio-track")
	if !IsAudioReloadProfileID(valid) {
		t.Fatalf("派生音轨 profile 应被识别: %q", valid)
	}
	for _, profileID := range []string{
		"audio-h264-aac-",
		"audio-h264-aac-deadbeefdeadbeefdeadbee",
		"audio-h264-aac-deadbeefdeadbeefdeadbeef0",
		"audio-h264-aac-DEADBEEFDEADBEEFDEADBEEF",
		"audio-h264-aac-deadbeefdeadbeefdeadbeeg",
		valid + "-extra",
		" " + valid,
	} {
		if IsAudioReloadProfileID(profileID) {
			t.Fatalf("非派生格式不得识别为音轨 profile: %q", profileID)
		}
	}
}

func TestNormalizeHLSPreviewRequestValidatesAudioBinding(t *testing.T) {
	trackID := "audio-track-jpn"
	profileID := AudioReloadProfileID(trackID)
	valid := HLSPreviewRequest{
		SpaceID: "space-a", MediaID: 42, ProfileID: profileID, Codec: "h264",
		AudioTrackID: trackID, AudioStreamIndex: intPointer(2), SourceFingerprint: "source-fingerprint",
	}

	payload, err := normalizeHLSPreviewRequest(valid)
	if err != nil {
		t.Fatalf("合法音轨 payload 应通过校验: %v", err)
	}
	if payload.AudioTrackID != trackID || payload.AudioStreamIndex == nil || *payload.AudioStreamIndex != 2 {
		t.Fatalf("音轨绑定未写入 payload: %+v", payload)
	}

	tests := []struct {
		name   string
		mutate func(*HLSPreviewRequest)
	}{
		{name: "缺少流索引", mutate: func(request *HLSPreviewRequest) { request.AudioStreamIndex = nil }},
		{name: "缺少 track ID", mutate: func(request *HLSPreviewRequest) { request.AudioTrackID = "" }},
		{name: "负流索引", mutate: func(request *HLSPreviewRequest) { request.AudioStreamIndex = intPointer(-1) }},
		{name: "非 h264 编码", mutate: func(request *HLSPreviewRequest) { request.Codec = "h265" }},
		{name: "profile 与 track 不匹配", mutate: func(request *HLSPreviewRequest) { request.ProfileID = "h264" }},
		{name: "缺少源指纹", mutate: func(request *HLSPreviewRequest) { request.SourceFingerprint = "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := valid
			test.mutate(&request)
			if _, err := normalizeHLSPreviewRequest(request); err == nil {
				t.Fatal("无效音轨绑定应被拒绝")
			}
		})
	}

	ordinary, err := normalizeHLSPreviewRequest(HLSPreviewRequest{
		SpaceID: "space-a", MediaID: 42, ProfileID: "mobile", Codec: "h265",
	})
	if err != nil {
		t.Fatalf("普通 preview 旧 payload 不应受音轨校验影响: %v", err)
	}
	if ordinary.AudioTrackID != "" || ordinary.AudioStreamIndex != nil || ordinary.Codec != "h265" {
		t.Fatalf("普通 preview 行为发生变化: %+v", ordinary)
	}
}

func TestDecodeHLSPreviewTaskRejectsInvalidAudioPayload(t *testing.T) {
	spaceID := "space-a"
	trackID := "audio-track-jpn"
	base := HLSPreviewPayload{
		SpaceID: spaceID, MediaID: 42, ProfileID: AudioReloadProfileID(trackID), Codec: "h264",
		AudioTrackID: trackID, AudioStreamIndex: intPointer(2), SourceFingerprint: "source-fingerprint",
	}
	tests := []struct {
		name   string
		mutate func(*HLSPreviewPayload)
	}{
		{name: "字段不成对", mutate: func(payload *HLSPreviewPayload) { payload.AudioStreamIndex = nil }},
		{name: "索引为负", mutate: func(payload *HLSPreviewPayload) { payload.AudioStreamIndex = intPointer(-1) }},
		{name: "编码不是 h264", mutate: func(payload *HLSPreviewPayload) { payload.Codec = "h265" }},
		{name: "profile 伪造", mutate: func(payload *HLSPreviewPayload) { payload.ProfileID = "audio-h264-aac-deadbeefdeadbeefdeadbeef" }},
		{name: "profile 含额外空白", mutate: func(payload *HLSPreviewPayload) { payload.ProfileID = " " + payload.ProfileID }},
		{name: "缺少源指纹", mutate: func(payload *HLSPreviewPayload) { payload.SourceFingerprint = "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := base
			test.mutate(&payload)
			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("编码测试 payload 失败: %v", err)
			}
			task := models.Task{
				Scope: models.TaskScopeSpace, SpaceID: &spaceID, Type: TaskTypeHLSPreview,
				ResourceID: "42", PayloadJSON: string(data),
			}
			if _, err := decodeHLSPreviewTask(task); err == nil {
				t.Fatal("非法持久化音轨 payload 应被拒绝")
			}
		})
	}
}

func TestEnqueueAudioReloadIsIdempotentAndStatusBindsTrack(t *testing.T) {
	svc, tasks, _, _, _ := newHLSPreviewTestService(t, func(context.Context, int64, HLSPreviewPayload) error { return nil })
	request := AudioReloadRequest{
		SpaceID: "space-a", MediaID: 42, AudioTrackID: "audio-track-jpn", AudioStreamIndex: 2,
		Width: 1280, Height: 720, SourceFingerprint: "source-fingerprint",
	}
	first, err := svc.EnqueueAudioReload(context.Background(), request)
	if err != nil {
		t.Fatalf("音轨 reload 入队失败: %v", err)
	}
	second, err := svc.EnqueueAudioReload(context.Background(), request)
	if err != nil {
		t.Fatalf("重复音轨 reload 入队失败: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("未完成音轨 reload 应复用幂等任务: first=%d second=%d", first.ID, second.ID)
	}
	stored, err := tasks.Get(context.Background(), first.ID, tasksvc.Query{SpaceID: "space-a"})
	if err != nil {
		t.Fatalf("读取音轨 reload 任务失败: %v", err)
	}
	var payload HLSPreviewPayload
	if err := json.Unmarshal([]byte(stored.PayloadJSON), &payload); err != nil {
		t.Fatalf("解析音轨 reload payload 失败: %v", err)
	}
	if payload.ProfileID != AudioReloadProfileID(request.AudioTrackID) || payload.Codec != "h264" || !payload.ForceRebuild {
		t.Fatalf("音轨 reload 固定 profile/codec/rebuild 失败: %+v", payload)
	}
	if payload.AudioStreamIndex == nil || *payload.AudioStreamIndex != request.AudioStreamIndex {
		t.Fatalf("音轨 reload 流索引未持久化: %+v", payload)
	}

	status, err := svc.StatusTask(context.Background(), request.SpaceID, request.MediaID, payload.ProfileID, first.ID)
	if err != nil {
		t.Fatalf("查询音轨 reload 状态失败: %v", err)
	}
	if status.Task == nil || status.Task.ID != first.ID || status.EffectiveTrackID != "" {
		t.Fatalf("未完成任务不得声明有效音轨: %+v", status)
	}
}

func TestAudioStreamArgsUseExactGlobalMapAndOrdinaryPreviewIsCompatible(t *testing.T) {
	pipeline := &Pipeline{encoderName: "libx264"}
	multi := NewMultiPipeline(pipeline)
	audioArgs := multi.BuildArgsWithAudioStream("input.mkv", []string{"480p"}, 3)
	if !containsArgPair(audioArgs, "-map", "0:3") {
		t.Fatalf("精确音轨入口缺少全局 stream map: %v", audioArgs)
	}
	if slices.Contains(audioArgs, "0:a") || slices.Contains(audioArgs, "0:a?") {
		t.Fatalf("精确音轨入口禁止使用宽泛音频 map: %v", audioArgs)
	}

	ordinaryArgs := multi.BuildArgs("input.mkv", []string{"480p"})
	if !containsArgPair(ordinaryArgs, "-map", "0:a") {
		t.Fatalf("普通 preview 应保持原有音频映射: %v", ordinaryArgs)
	}
}

func TestAudioReloadSourceFingerprintTracksMediaAndTrackIdentity(t *testing.T) {
	media := MediaIdentity{SpaceID: "space-a", MediaID: 42, Path: "movie.mkv", Size: 100, ContentHash: "hash"}
	track := AudioTrackIdentity{ID: "audio-1", Index: 2, Codec: "aac", Language: "zh", Title: "主音轨", Channels: 6, ChannelLayout: "5.1"}
	fingerprint := AudioReloadSourceFingerprint(media, track)
	if fingerprint == "" || AudioReloadSourceFingerprint(media, track) != fingerprint {
		t.Fatal("音轨源指纹必须非空且稳定")
	}
	media.Size++
	if AudioReloadSourceFingerprint(media, track) == fingerprint {
		t.Fatal("媒体身份变化必须改变源指纹")
	}
	media.Size--
	track.Title = "备用音轨"
	if AudioReloadSourceFingerprint(media, track) == fingerprint {
		t.Fatal("轨道身份变化必须改变源指纹")
	}
}

func TestSelectCurrentEncoderForCodecWithPolicyUsesProbeSnapshot(t *testing.T) {
	setProbeSnapshot(nil)
	t.Cleanup(clearProbeSnapshot)
	policy := HardwarePolicy{Mode: "qsv", Fallback: false}
	if _, _, _, err := SelectCurrentEncoderForCodecWithPolicy(DefaultTargetCodec, policy); err == nil {
		t.Fatal("强制不可用硬件且关闭回退时必须判定不可执行")
	}
	setProbeSnapshot([]EncoderProbeResult{{Encoder: "h264_qsv", Family: "qsv", Codec: "h264", TestedOK: true}})
	encoder, _, hardware, err := SelectCurrentEncoderForCodecWithPolicy(DefaultTargetCodec, policy)
	if err != nil || encoder != "h264_qsv" || !hardware {
		t.Fatalf("当前 probe 快照中的可用硬件应通过策略检查: encoder=%s hardware=%t err=%v", encoder, hardware, err)
	}
}

func containsArgPair(args []string, key, value string) bool {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == key && args[index+1] == value {
			return true
		}
	}
	return false
}

type audioProbe struct {
	Programs []struct {
		Tags    map[string]string `json:"tags"`
		Streams []struct {
			CodecName string            `json:"codec_name"`
			CodecType string            `json:"codec_type"`
			Tags      map[string]string `json:"tags"`
		} `json:"streams"`
	} `json:"programs"`
}

type fixtureProbe struct {
	Streams []struct {
		CodecName     string `json:"codec_name"`
		CodecType     string `json:"codec_type"`
		NBReadPackets string `json:"nb_read_packets"`
	} `json:"streams"`
}

type audioTarget struct {
	mediaID     int64
	streamIndex int
	language    string
	title       string
}

func TestPreSliceAudioTrackRealFFmpeg(t *testing.T) {
	ffmpegPath := ffmpegPathFromEnvOrPath(t)
	if ffmpegPath == "" {
		t.Skip("环境无 ffmpeg，跳过精确音轨 HLS 端到端测试")
	}
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("环境无 ffprobe，跳过精确音轨 HLS 端到端测试")
	}
	previousFFmpegPath := GetFFmpegPath()
	SetFFmpegPath(ffmpegPath)
	t.Cleanup(func() { SetFFmpegPath(previousFFmpegPath) })

	root := t.TempDir()
	textSubtitlePath := filepath.Join(root, "text.srt")
	if err := os.WriteFile(textSubtitlePath, []byte("1\n00:00:00,100 --> 00:00:00,800\n文本字幕\n"), 0o600); err != nil {
		t.Fatalf("写入文本字幕 fixture 失败: %v", err)
	}
	imageSubtitlePath := filepath.Join(root, "image.sup")
	if err := os.WriteFile(imageSubtitlePath, minimalPGSFixture(), 0o600); err != nil {
		t.Fatalf("写入图片字幕 fixture 失败: %v", err)
	}
	inputPath := filepath.Join(root, "dual-audio.mkv")
	generate := exec.Command(ffmpegPath,
		"-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=duration=1:size=320x240:rate=24",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-f", "lavfi", "-i", "sine=frequency=880:duration=1",
		"-i", textSubtitlePath,
		"-f", "sup", "-i", imageSubtitlePath,
		"-map", "0:v:0", "-map", "1:a:0", "-map", "2:a:0", "-map", "3:0", "-map", "4:0",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac",
		"-c:s:0", "srt", "-c:s:1", "copy", "-t", "1",
		"-metadata:s:a:0", "language=eng", "-metadata:s:a:0", "title=English",
		"-metadata:s:a:1", "language=jpn", "-metadata:s:a:1", "title=Japanese",
		"-metadata:s:s:0", "language=zho", "-metadata:s:s:0", "title=文本字幕",
		"-metadata:s:s:1", "language=zho", "-metadata:s:s:1", "title=图片字幕",
		inputPath,
	)
	if output, err := generate.CombinedOutput(); err != nil {
		t.Fatalf("生成双 AAC fixture 失败: %v\n%s", err, output)
	}
	assertFixtureStreams(t, ffprobePath, inputPath)

	targets := []audioTarget{
		{mediaID: 42, streamIndex: 1, language: "eng", title: "English"},
		{mediaID: 43, streamIndex: 2, language: "jpn", title: "Japanese"},
	}
	for _, target := range targets {
		t.Run(target.language, func(t *testing.T) {
			outputDir := filepath.Join(root, "selected-"+target.language)
			_, err := PreSliceAudioTrackWithPolicyToDir(
				t.Context(), target.mediaID, inputPath, 320, 240, target.streamIndex,
				HardwarePolicy{Mode: HWAccelModeSoftware, Fallback: true}, outputDir,
			)
			if err != nil {
				t.Fatalf("精确音轨 HLS 转码失败: %v", err)
			}
			assertHLSAudioTarget(t, ffprobePath, outputDir, target)
		})
	}

	failedDir := filepath.Join(root, "invalid-index")
	if _, err := PreSliceAudioTrackWithPolicyToDir(
		t.Context(), 44, inputPath, 320, 240, 99,
		HardwarePolicy{Mode: HWAccelModeSoftware, Fallback: true}, failedDir,
	); err == nil {
		t.Fatal("不存在的全局 stream index 应转码失败")
	}
	if _, statErr := os.Stat(failedDir); !os.IsNotExist(statErr) {
		t.Fatalf("错误 stream index 失败后应清理输出目录: err=%v", statErr)
	}
}

func minimalPGSFixture() []byte {
	return []byte{'P', 'G', 0, 0, 0, 0, 0, 0, 0, 0, 0x80, 0, 0}
}

func assertFixtureStreams(t *testing.T, ffprobePath, inputPath string) {
	t.Helper()
	output, err := exec.Command(ffprobePath,
		"-v", "error", "-print_format", "json", "-count_packets", "-show_streams", inputPath,
	).Output()
	if err != nil {
		t.Fatalf("ffprobe fixture 失败: %v", err)
	}
	var probe fixtureProbe
	if err := json.Unmarshal(output, &probe); err != nil {
		t.Fatalf("解析 fixture ffprobe 输出失败: %v\n%s", err, output)
	}
	codecCounts := map[string]int{}
	imagePackets := ""
	for _, stream := range probe.Streams {
		codecCounts[stream.CodecName]++
		if stream.CodecName == "hdmv_pgs_subtitle" {
			imagePackets = stream.NBReadPackets
		}
	}
	if codecCounts["aac"] != 2 || codecCounts["subrip"] != 1 || codecCounts["hdmv_pgs_subtitle"] != 1 {
		t.Fatalf("fixture 必须含双 AAC、文本字幕和图片字幕: codecs=%v", codecCounts)
	}
	if imagePackets != "1" {
		t.Fatalf("图片字幕必须含真实可枚举数据包: packets=%q", imagePackets)
	}
}

func assertHLSAudioTarget(t *testing.T, ffprobePath, outputDir string, target audioTarget) {
	t.Helper()
	probeOutput, err := exec.Command(ffprobePath,
		"-v", "error", "-print_format", "json", "-show_programs",
		filepath.Join(outputDir, "480p_000.ts"),
	).Output()
	if err != nil {
		t.Fatalf("ffprobe 精确音轨 HLS 失败: %v", err)
	}
	var probe audioProbe
	if err := json.Unmarshal(probeOutput, &probe); err != nil {
		t.Fatalf("解析 ffprobe 输出失败: %v\n%s", err, probeOutput)
	}
	if len(probe.Programs) != 1 {
		t.Fatalf("输出应恰有一个节目: %s", probeOutput)
	}
	program := probe.Programs[0]
	videoCount := 0
	audioCount := 0
	audioLanguage := ""
	for _, stream := range program.Streams {
		switch stream.CodecType {
		case "video":
			videoCount++
			if stream.CodecName != "h264" {
				t.Fatalf("输出视频必须为 H.264: %s", probeOutput)
			}
		case "audio":
			audioCount++
			audioLanguage = stream.Tags["language"]
			if stream.CodecName != "aac" {
				t.Fatalf("输出音轨必须为 AAC: %s", probeOutput)
			}
		}
	}
	if videoCount != 1 || audioCount != 1 {
		t.Fatalf("输出应恰有一条 H.264 视频和一条 AAC 音轨: %s", probeOutput)
	}
	if audioLanguage != target.language || !strings.EqualFold(program.Tags["service_name"], target.title) {
		t.Fatalf("输出目标音轨语言/标题不符: language=%q program=%v", audioLanguage, program.Tags)
	}
}
