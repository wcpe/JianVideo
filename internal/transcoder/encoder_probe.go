package transcoder

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"strings"
	"time"
)

// EncoderFamily 标识编码器所属的加速家族。
type EncoderFamily string

const (
	familySoftware     EncoderFamily = "software"
	familyQSV          EncoderFamily = "qsv"
	familyVAAPI        EncoderFamily = "vaapi"
	familyNVENC        EncoderFamily = "nvenc"
	familyAMF          EncoderFamily = "amf"
	familyVideoToolbox EncoderFamily = "videotoolbox"
	familyVulkan       EncoderFamily = "vulkan"
)

// EncoderCandidate 描述一个待实测的编码器候选。
type EncoderCandidate struct {
	Encoder string        // ffmpeg 编码器名，如 "h264_qsv"
	Family  EncoderFamily // 加速家族
	Codec   string        // "h264" / "h265"
}

// EncoderProbeResult 单个编码器的实测结果。
type EncoderProbeResult struct {
	Encoder  string `json:"encoder"`
	Family   string `json:"family"`
	Codec    string `json:"codec"`
	Compiled bool   `json:"compiled"`  // 是否编入当前 ffmpeg
	TestedOK bool   `json:"tested_ok"` // 试编码是否成功
	Detail   string `json:"detail"`    // 失败时的错误尾部 / 说明
}

// probeEncodeTimeout 单个试编码的超时上限。
const probeEncodeTimeout = 20 * time.Second

// vaapiDevice 默认 VAAPI 渲染设备路径（Linux）。
const vaapiDevice = "/dev/dri/renderD128"

// candidateEncoders 返回所有待实测的编码器候选（顺序即展示顺序）。
// hevc 编码器的 codec 字段统一记为 "h265"（与现有约定一致）。
func candidateEncoders() []EncoderCandidate {
	return []EncoderCandidate{
		// 软件编码：H.264 / H.265 / AV1（用 libsvtav1）/ VP9
		{"libx264", familySoftware, "h264"},
		{"libx265", familySoftware, "h265"},
		{"libsvtav1", familySoftware, "av1"},
		{"libvpx-vp9", familySoftware, "vp9"},
		// Intel QSV
		{"h264_qsv", familyQSV, "h264"},
		{"hevc_qsv", familyQSV, "h265"},
		{"av1_qsv", familyQSV, "av1"},
		{"vp9_qsv", familyQSV, "vp9"},
		// NVIDIA NVENC
		{"h264_nvenc", familyNVENC, "h264"},
		{"hevc_nvenc", familyNVENC, "h265"},
		{"av1_nvenc", familyNVENC, "av1"},
		// AMD AMF
		{"h264_amf", familyAMF, "h264"},
		{"hevc_amf", familyAMF, "h265"},
		{"av1_amf", familyAMF, "av1"},
		// VAAPI
		{"h264_vaapi", familyVAAPI, "h264"},
		{"hevc_vaapi", familyVAAPI, "h265"},
		{"av1_vaapi", familyVAAPI, "av1"},
		{"vp9_vaapi", familyVAAPI, "vp9"},
		// Apple VideoToolbox
		{"h264_videotoolbox", familyVideoToolbox, "h264"},
		{"hevc_videotoolbox", familyVideoToolbox, "h265"},
		// Vulkan
		{"h264_vulkan", familyVulkan, "h264"},
		{"hevc_vulkan", familyVulkan, "h265"},
		{"av1_vulkan", familyVulkan, "av1"},
	}
}

// parseCompiledEncoders 解析 `ffmpeg -encoders` 输出，返回编入的编码器名集合。
// 输出形如：
//
//	Encoders:
//	 V..... = Video
//	 ...
//	 ------
//	 V....D libx264   libx264 H.264 / ...
//
// 取分隔线 "------" 之后每行的第二个字段为编码器名。
func parseCompiledEncoders(out string) map[string]bool {
	set := make(map[string]bool)
	seenSep := false
	for _, line := range strings.Split(out, "\n") {
		if !seenSep {
			if strings.Contains(line, "------") {
				seenSep = true
			}
			continue
		}
		fields := strings.Fields(line)
		// 第一字段是能力标志（如 "V....D"），第二字段是编码器名
		if len(fields) >= 2 {
			set[fields[1]] = true
		}
	}
	return set
}

// buildProbeArgs 按家族构建一次试编码的 ffmpeg 参数（试编码 5 帧到 null）。
// VAAPI、Vulkan 需显式初始化硬件设备并 hwupload，其余家族通用模板即可。
func buildProbeArgs(c EncoderCandidate) []string {
	const src = "color=c=black:s=256x256:r=5"
	if c.Family == familyVAAPI {
		return []string{
			"-hide_banner", "-loglevel", "error",
			"-init_hw_device", "vaapi=va:" + vaapiDevice,
			"-filter_hw_device", "va",
			"-f", "lavfi", "-i", src,
			"-frames:v", "5", "-an",
			"-vf", "format=nv12,hwupload",
			"-c:v", c.Encoder,
			"-f", "null", "-",
		}
	}
	if c.Family == familyVulkan {
		return []string{
			"-hide_banner", "-loglevel", "error",
			"-init_hw_device", "vulkan=vk:0",
			"-filter_hw_device", "vk",
			"-f", "lavfi", "-i", src,
			"-frames:v", "5", "-an",
			"-vf", "format=nv12,hwupload",
			"-c:v", c.Encoder,
			"-f", "null", "-",
		}
	}
	return []string{
		"-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", src,
		"-frames:v", "5", "-an",
		"-c:v", c.Encoder,
		"-f", "null", "-",
	}
}

// tailString 返回字符串末尾至多 n 个字符（用于截断 stderr）。
func tailString(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// runProbe 执行一次试编码，返回是否成功与失败时的 stderr 尾部。
func runProbe(ctx context.Context, args []string) (bool, string) {
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	cmd.WaitDelay = 3 * time.Second
	setProcessGroup(cmd)
	if err := cmd.Run(); err != nil {
		detail := tailString(stderr.String(), 500)
		if detail == "" {
			detail = err.Error()
		}
		return false, detail
	}
	return true, ""
}

// listCompiledEncoders 运行 `ffmpeg -encoders` 并解析编入集合；失败返回空集。
func listCompiledEncoders(ctx context.Context) map[string]bool {
	listCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(listCtx, ffmpegPath, "-hide_banner", "-encoders")
	out, err := cmd.Output()
	if err != nil {
		return map[string]bool{}
	}
	return parseCompiledEncoders(string(out))
}

// ProbeEncoders 实测所有候选编码器：先查编入集合，再对编入者逐个带超时试编码。
// 顺序执行避免并发抢占同一硬件编码器；ffmpeg 不可用时全部标记未编入。
func ProbeEncoders(ctx context.Context) []EncoderProbeResult {
	candidates := candidateEncoders()
	results := make([]EncoderProbeResult, 0, len(candidates))

	compiled := map[string]bool{}
	if IsFFmpegAvailable() {
		compiled = listCompiledEncoders(ctx)
	}

	for _, c := range candidates {
		res := EncoderProbeResult{Encoder: c.Encoder, Family: string(c.Family), Codec: c.Codec}
		if !compiled[c.Encoder] {
			res.Detail = "编码器未编入当前 ffmpeg"
			results = append(results, res)
			continue
		}
		res.Compiled = true
		encCtx, cancel := context.WithTimeout(ctx, probeEncodeTimeout)
		ok, detail := runProbe(encCtx, buildProbeArgs(c))
		cancel()
		res.TestedOK = ok
		res.Detail = detail
		results = append(results, res)
	}
	return results
}

// FFmpegVersion 返回 `ffmpeg -version` 首行；不可用时返回空串。
func FFmpegVersion(ctx context.Context) string {
	if !IsFFmpegAvailable() {
		return ""
	}
	verCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(verCtx, ffmpegPath, "-hide_banner", "-version")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
}
