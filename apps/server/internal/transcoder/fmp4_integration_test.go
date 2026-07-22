package transcoder

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ffprobeProbe 用 ffprobe 读取文件的容器格式与首个视频流编码。
type ffprobeProbe struct {
	Format struct {
		FormatName string `json:"format_name"`
	} `json:"format"`
	Streams []struct {
		CodecType   string `json:"codec_type"`
		CodecName   string `json:"codec_name"`
		CodecTagStr string `json:"codec_tag_string"`
	} `json:"streams"`
}

// probeWithFFprobe 拼接 init+首个 m4s 后用 ffprobe 读取容器与编码（真机验证用）。
func probeWithFFprobe(t *testing.T, ffprobePath, outputDir string) ffprobeProbe {
	t.Helper()
	initSeg := filepath.Join(outputDir, fmp4InitFilename)
	mediaSeg := filepath.Join(outputDir, "seg_000.m4s")
	initData, err := os.ReadFile(initSeg)
	if err != nil {
		t.Fatalf("读取 init segment 失败: %v", err)
	}
	mediaData, err := os.ReadFile(mediaSeg)
	if err != nil {
		t.Fatalf("读取 media segment 失败: %v", err)
	}
	full := filepath.Join(t.TempDir(), "full.mp4")
	if err := os.WriteFile(full, append(initData, mediaData...), 0o644); err != nil {
		t.Fatalf("写合并文件失败: %v", err)
	}
	out, err := exec.Command(ffprobePath,
		"-v", "error", "-print_format", "json",
		"-show_format", "-show_streams", full).Output()
	if err != nil {
		t.Fatalf("ffprobe 执行失败: %v", err)
	}
	var p ffprobeProbe
	if err := json.Unmarshal(out, &p); err != nil {
		t.Fatalf("ffprobe 输出解析失败: %v\n%s", err, out)
	}
	return p
}

// videoStream 返回首个视频流的编码名与 tag。
func videoStream(p ffprobeProbe) (codec, tag string) {
	for _, s := range p.Streams {
		if s.CodecType == "video" {
			return s.CodecName, s.CodecTagStr
		}
	}
	return "", ""
}

// TestRunFMP4ToDir_RealMachine 真机端到端：用软件编码器（libx265/libsvtav1/libvpx-vp9）
// 经 RunFMP4ToDir 实际转出 fMP4/CMAF 分片，ffprobe 断言容器=mov/mp4 且编码正确。
// 覆盖 FR-51 验收标准的「真机软件维度」。环境无 ffmpeg/ffprobe 时跳过。
func TestRunFMP4ToDir_RealMachine(t *testing.T) {
	ffmpegPath := ffmpegPathFromEnvOrPath(t)
	if ffmpegPath == "" {
		t.Skip("环境无 ffmpeg，跳过 fMP4 真机端到端测试")
	}
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("环境无 ffprobe，跳过 fMP4 真机端到端测试")
	}
	SetFFmpegPath(ffmpegPath)
	t.Cleanup(func() { SetFFmpegPath("") })

	// 生成 2 秒源视频
	srcDir := t.TempDir()
	inputPath := filepath.Join(srcDir, "src.mp4")
	gen := exec.Command(ffmpegPath,
		"-y", "-f", "lavfi", "-i", "testsrc=duration=2:size=320x240:rate=24",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest",
		inputPath)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("生成源视频失败: %v\n%s", err, out)
	}

	cases := []struct {
		name      string
		codec     string
		encoder   string
		wantCodec string
		wantTag   string // ffprobe codec_tag_string 期望子串
	}{
		{"H265", "h265", "libx265", "hevc", "hvc1"},
		{"AV1", "av1", "libsvtav1", "av1", "av01"},
		{"VP9", "vp9", "libvpx-vp9", "vp9", "vp09"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 该软件编码器须编入当前 ffmpeg，否则跳过（不算失败）
			compiled := listCompiledEncoders(t.Context())
			if !compiled[tc.encoder] {
				t.Skipf("当前 ffmpeg 未编入 %s，跳过", tc.encoder)
			}

			outputDir := filepath.Join(t.TempDir(), "fmp4")
			// 空 results → SelectFMP4Encoder 走软件兜底，正好用 tc.encoder
			res, err := RunFMP4ToDir(t.Context(), 1, inputPath, tc.codec, outputDir, nil)
			if err != nil {
				t.Fatalf("RunFMP4ToDir(%s) 失败: %v", tc.codec, err)
			}
			if res.Encoder != tc.encoder {
				t.Fatalf("期望软件兜底编码器 %s，实得 %s", tc.encoder, res.Encoder)
			}

			// 校验产物：init.mp4 + seg_000.m4s + index.m3u8
			for _, name := range []string{fmp4InitFilename, "seg_000.m4s", fmp4ManifestFilename} {
				if _, err := os.Stat(filepath.Join(outputDir, name)); err != nil {
					t.Fatalf("产物 %s 未生成: %v", name, err)
				}
			}

			// 清单须是 HLS-fMP4（含 EXT-X-MAP + ENDLIST，VOD）
			manifest, err := os.ReadFile(res.ManifestPath)
			if err != nil {
				t.Fatalf("读取清单失败: %v", err)
			}
			m := string(manifest)
			if !strings.Contains(m, "#EXT-X-MAP:URI=\"init.mp4\"") {
				t.Errorf("清单缺 EXT-X-MAP init 引用:\n%s", m)
			}
			if !strings.Contains(m, "#EXT-X-ENDLIST") {
				t.Errorf("清单缺 EXT-X-ENDLIST（VOD）:\n%s", m)
			}

			// ffprobe 断言：容器=mov/mp4，视频编码与 tag 正确
			p := probeWithFFprobe(t, ffprobePath, outputDir)
			if !strings.Contains(p.Format.FormatName, "mp4") && !strings.Contains(p.Format.FormatName, "mov") {
				t.Errorf("容器格式期望 mov/mp4，实得 %q", p.Format.FormatName)
			}
			vc, vtag := videoStream(p)
			if vc != tc.wantCodec {
				t.Errorf("视频编码期望 %s，实得 %s", tc.wantCodec, vc)
			}
			if !strings.Contains(vtag, tc.wantTag) {
				t.Errorf("视频 tag 期望含 %s，实得 %s", tc.wantTag, vtag)
			}

			// MIME 串与 tag 自洽
			mime := FMP4CodecMIME(tc.codec)
			if !strings.Contains(mime, tc.wantTag) {
				t.Errorf("MIME 串 %q 应含 %s", mime, tc.wantTag)
			}
			t.Logf("真机验证通过: codec=%s encoder=%s container=%s vcodec=%s tag=%s mime=%q",
				tc.codec, res.Encoder, p.Format.FormatName, vc, vtag, mime)
		})
	}
}
