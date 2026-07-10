package transcoder

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wcpe/JianVideo/internal/player"
)

// probeOutputTailLimit 分辨率探测失败时，错误中保留的 ffmpeg 输出尾部最大字符数。
const probeOutputTailLimit = 500

// PreSliceResult PreSlice 的执行结果摘要。
type PreSliceResult struct {
	MediaID    int64
	OutputDir  string
	Qualities  []string
	MasterPath string
}

// PreSliceWithCodec 是带目标编码维度的预切片分发入口（FR-51 播放/转码输出分发点）。
//
//   - 目标编码为 h264（或空、未知）→ 调用 PreSlice，走现有多码率 MPEG-TS/HLS 路径（分支实现不动）。
//   - 目标编码为 h265/av1/vp9 → 走 fMP4/CMAF 路径，输出 init.mp4 + seg_NNN.m4s + index.m3u8。
//
// fMP4 路径产物与 HLS 产物同目录策略（hlsDir/{mediaID}/），复用现有静态服务端点。
// 编码器选取读进程级实测快照（FR-49），无硬件可用时软件兜底。
func PreSliceWithCodec(
	ctx context.Context,
	mediaID int64,
	inputPath string,
	srcWidth int,
	srcHeight int,
	codec string,
	hlsMgr *player.HLSManager,
	hlsDir string,
) (*PreSliceResult, error) {
	return PreSliceWithCodecAndPolicy(ctx, mediaID, inputPath, srcWidth, srcHeight, codec, DefaultHardwarePolicy(), hlsMgr, hlsDir)
}

// PreSliceWithCodecAndPolicy 按目标编码与硬件策略执行预切片。
func PreSliceWithCodecAndPolicy(
	ctx context.Context,
	mediaID int64,
	inputPath string,
	srcWidth int,
	srcHeight int,
	codec string,
	policy HardwarePolicy,
	hlsMgr *player.HLSManager,
	hlsDir string,
) (*PreSliceResult, error) {
	if SelectOutputPath(codec) == OutputPathTS {
		return PreSliceWithPolicy(ctx, mediaID, inputPath, srcWidth, srcHeight, policy, hlsMgr, hlsDir)
	}

	if hlsDir == "" {
		return nil, fmt.Errorf("hlsDir 不能为空")
	}
	outputDir := filepath.Join(hlsDir, fmt.Sprintf("%d", mediaID))
	var results []EncoderProbeResult
	if snap := probeSnapshot.Load(); snap != nil {
		results = *snap
	}
	res, err := RunFMP4ToDirWithPolicy(ctx, mediaID, inputPath, codec, outputDir, results, policy)
	if err != nil {
		return nil, err
	}
	return &PreSliceResult{
		MediaID:    mediaID,
		OutputDir:  res.OutputDir,
		Qualities:  []string{res.Codec},
		MasterPath: res.ManifestPath,
	}, nil
}

// PreSlice 为单个媒体文件同步执行 ffmpeg 多码率切片，输出 HLS 产物到 hlsDir/{mediaID}/。
//
// 流程：
//  1. 若 srcWidth/srcHeight 为 0，先用 ffmpeg 探测源分辨率
//  2. 根据源分辨率选码率档位（QualitiesForResolution）
//  3. ffmpeg 输出 {quality}.m3u8 + {quality}_segment_NNN.ts 到 hlsDir/{mediaID}/
//  4. 拼出 master.m3u8 内容（含码率/分辨率）并由 hlsMgr 写入 master.m3u8
//
// 失败时返回 error 并清理已生成的部分切片目录，避免残留脏数据。
func PreSlice(
	ctx context.Context,
	mediaID int64,
	inputPath string,
	srcWidth int,
	srcHeight int,
	hlsMgr *player.HLSManager,
	hlsDir string,
) (*PreSliceResult, error) {
	return PreSliceWithPolicy(ctx, mediaID, inputPath, srcWidth, srcHeight, DefaultHardwarePolicy(), hlsMgr, hlsDir)
}

// PreSliceWithPolicy 为 H.264/TS 路径按硬件策略执行预切片。
func PreSliceWithPolicy(
	ctx context.Context,
	mediaID int64,
	inputPath string,
	srcWidth int,
	srcHeight int,
	policy HardwarePolicy,
	hlsMgr *player.HLSManager,
	hlsDir string,
) (*PreSliceResult, error) {
	if !IsFFmpegAvailable() {
		return nil, fmt.Errorf("ffmpeg 不可用，无法预切片")
	}
	if hlsDir == "" {
		return nil, fmt.Errorf("hlsDir 不能为空")
	}
	if hlsMgr == nil {
		return nil, fmt.Errorf("hlsMgr 不能为空")
	}

	// 源分辨率缺失时探测一次
	if srcWidth <= 0 || srcHeight <= 0 {
		w, h, err := probeResolution(ctx, inputPath)
		if err != nil {
			log.Printf("[WARN] 探测分辨率失败，使用默认档位: mediaID=%d, err=%v", mediaID, err)
		} else {
			srcWidth, srcHeight = w, h
		}
	}

	// 选档位：源分辨率未知时退到最低档。
	qualityNames := QualitiesForResolution(srcWidth, srcHeight)
	if len(qualityNames) == 0 {
		qualityNames = []string{"480p"}
	}

	// 为本次预切片创建独立子目录（避免与追播模式并行写同一目录）
	outputDir := filepath.Join(hlsDir, fmt.Sprintf("%d", mediaID))
	if err := os.RemoveAll(outputDir); err != nil {
		return nil, fmt.Errorf("清理旧切片失败: %w", err)
	}
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return nil, fmt.Errorf("创建切片目录失败: %w", err)
	}

	// 给切片过程加个硬上限，避免卡死 ScanLibrary 响应
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	if err := runMultiToDirWithPolicy(runCtx, inputPath, qualityNames, outputDir, policy); err != nil {
		// 失败清理；与成功日志对称，记录 mediaID/输出目录/档位上下文便于排查
		_ = os.RemoveAll(outputDir)
		log.Printf("[ERROR] HLS 预切片失败（ffmpeg 切片）: mediaID=%d, outputDir=%s, qualities=%v, err=%v",
			mediaID, outputDir, qualityNames, err)
		return nil, fmt.Errorf("ffmpeg 切片失败: %w", err)
	}

	// 校验 ffmpeg 真的产出了 m3u8
	if err := verifySliceOutputs(outputDir, qualityNames); err != nil {
		_ = os.RemoveAll(outputDir)
		log.Printf("[ERROR] HLS 预切片失败（m3u8 校验）: mediaID=%d, outputDir=%s, qualities=%v, err=%v",
			mediaID, outputDir, qualityNames, err)
		return nil, err
	}

	// 拼 master.m3u8
	masterContent := buildMasterM3U8(qualityNames, qualityLadders)
	if err := hlsMgr.SaveMasterM3U8(mediaID, masterContent); err != nil {
		_ = os.RemoveAll(outputDir)
		log.Printf("[ERROR] HLS 预切片失败（保存 master.m3u8）: mediaID=%d, outputDir=%s, err=%v",
			mediaID, outputDir, err)
		return nil, fmt.Errorf("保存 master.m3u8 失败: %w", err)
	}

	log.Printf("[INFO] HLS 预切片完成: mediaID=%d, outputDir=%s, qualities=%v",
		mediaID, outputDir, qualityNames)
	return &PreSliceResult{
		MediaID:    mediaID,
		OutputDir:  outputDir,
		Qualities:  qualityNames,
		MasterPath: filepath.Join(outputDir, "master.m3u8"),
	}, nil
}

func runMultiToDirWithPolicy(ctx context.Context, inputPath string, qualityNames []string, outputDir string, policy HardwarePolicy) error {
	pipeline, err := NewPipelineForCodecWithPolicy(DefaultTargetCodec, policy)
	if err != nil {
		return err
	}
	err = NewMultiPipeline(pipeline).RunMultiToDir(ctx, inputPath, qualityNames, outputDir)
	if err == nil || pipeline.deviceType == "" || !policy.Fallback {
		return err
	}
	log.Printf("[WARN] HLS 硬件转码失败，改用软件回退: encoder=%s, err=%v", pipeline.encoderName, err)
	if outputDir != "" {
		_ = os.RemoveAll(outputDir)
	}
	software, softwareErr := NewPipelineForCodecWithPolicy(DefaultTargetCodec, HardwarePolicy{Mode: HWAccelModeSoftware, Fallback: true})
	if softwareErr != nil {
		return err
	}
	return NewMultiPipeline(software).RunMultiToDir(ctx, inputPath, qualityNames, outputDir)
}

// resPattern 匹配 ffmpeg -i 输出中的 "Video: h264 (...), yuv420p(...), 1920x1080" 之类。
var resPattern = regexp.MustCompile(`Video:[^,]+,\s*[^,]+,\s*(\d{2,5})x(\d{2,5})`)

// probeResolution 用 ffmpeg 探测源媒体分辨率（宽高）。
// 只跑一次，不实际转码，所以很快；10 秒硬上限防卡死。
func probeResolution(ctx context.Context, inputPath string) (int, int, error) {
	log.Printf("[DEBUG] probeResolution: inputPath=%s, ffmpegPath=%s", inputPath, GetFFmpegPath())
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := ffmpegCommandContext(probeCtx, "-hide_banner", "-i", inputPath)
	// ffmpeg 探测会 exit 1；忽略 exit 错误，从 stderr 抓尺寸
	out, _ := cmd.CombinedOutput()
	matches := resPattern.FindStringSubmatch(string(out))
	if len(matches) != 3 {
		// 解析失败时附带 ffmpeg 输出关键尾部，便于区分「文件损坏/格式不支持」与「正则不匹配」
		return 0, 0, fmt.Errorf("无法从 ffmpeg 输出解析分辨率; ffmpeg 输出: %s",
			tailString(string(out), probeOutputTailLimit))
	}
	w, _ := strconv.Atoi(matches[1])
	h, _ := strconv.Atoi(matches[2])
	if w == 0 || h == 0 {
		return 0, 0, fmt.Errorf("解析到无效分辨率 %dx%d", w, h)
	}
	return w, h, nil
}

// verifySliceOutputs 检查每个档位是否真的生成了 m3u8 文件。
func verifySliceOutputs(outputDir string, qualityNames []string) error {
	for _, q := range qualityNames {
		m3u8Path := filepath.Join(outputDir, fmt.Sprintf("%s.m3u8", q))
		if _, err := os.Stat(m3u8Path); err != nil {
			return fmt.Errorf("档位 %s 的 m3u8 未生成（输出目录: %s）: %w", q, outputDir, err)
		}
	}
	return nil
}

// buildMasterM3U8 拼装 master playlist 文本。
// qualities 按从高到低排序，videoRate "5000k" 解析为 5_000_000 bps。
func buildMasterM3U8(qualityNames []string, ladders []QualityDefinition) string {
	// 收集 (q, def) 并按分辨率降序
	type pair struct {
		name string
		def  QualityDefinition
	}
	var pairs []pair
	for _, name := range qualityNames {
		for _, q := range ladders {
			if q.Name == name {
				pairs = append(pairs, pair{name, q})
				break
			}
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].def.Height != pairs[j].def.Height {
			return pairs[i].def.Height > pairs[j].def.Height
		}
		return pairs[i].def.Width > pairs[j].def.Width
	})

	var sb strings.Builder
	sb.WriteString("#EXTM3U\n")
	sb.WriteString("#EXT-X-VERSION:3\n")
	for _, p := range pairs {
		bw := rateToBps(p.def.VideoRate)
		fmt.Fprintf(&sb,
			"#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d\n",
			bw, p.def.Width, p.def.Height,
		)
		// 与 master 同目录：hls.js 拼到 master URL 得到 /api/play/hls/:id/{quality}.m3u8，
		// 静态文件服务直接返回 hlsDir/{mediaID}/{quality}.m3u8
		fmt.Fprintf(&sb, "%s.m3u8\n", p.name)
	}
	return sb.String()
}

// rateToBps "5000k" → 5_000_000；"128k" → 128_000；纯数字按 bps。
func rateToBps(rate string) int {
	s := strings.TrimSpace(strings.ToLower(rate))
	if s == "" {
		return 0
	}
	mult := 1
	switch {
	case strings.HasSuffix(s, "k"):
		mult = 1000
		s = strings.TrimSuffix(s, "k")
	case strings.HasSuffix(s, "m"):
		mult = 1000_000
		s = strings.TrimSuffix(s, "m")
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n * mult
}
