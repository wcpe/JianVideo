package transcoder

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // 注册 JPEG 解码器，供 image.DecodeConfig 校验时间轴 sprite。
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// TimelinePreviewFormatJPEG 是首版时间轴预览图片格式。
	TimelinePreviewFormatJPEG = "jpeg"
)

// TimelinePreviewProfile 定义会影响预览产物身份的全部参数。
type TimelinePreviewProfile struct {
	ID        string        `json:"id"`
	Version   int           `json:"version"`
	Interval  time.Duration `json:"-"`
	CellWidth int           `json:"cell_width"`
	Columns   int           `json:"columns"`
	Rows      int           `json:"rows"`
	Format    string        `json:"format"`
}

// MarshalJSON 使用稳定毫秒值序列化 profile。
func (p TimelinePreviewProfile) MarshalJSON() ([]byte, error) {
	type profileJSON struct {
		ID, Format               string
		Version, IntervalMS      int
		CellWidth, Columns, Rows int
	}
	value := profileJSON{p.ID, p.Format, p.Version, int(p.Interval / time.Millisecond), p.CellWidth, p.Columns, p.Rows}
	return json.Marshal(struct {
		ID         string `json:"id"`
		Version    int    `json:"version"`
		IntervalMS int    `json:"interval_ms"`
		CellWidth  int    `json:"cell_width"`
		Columns    int    `json:"columns"`
		Rows       int    `json:"rows"`
		Format     string `json:"format"`
	}{value.ID, value.Version, value.IntervalMS, value.CellWidth, value.Columns, value.Rows, value.Format})
}

// DefaultTimelinePreviewProfile 返回首版默认 profile。
func DefaultTimelinePreviewProfile() TimelinePreviewProfile {
	profile := TimelinePreviewProfile{Version: 1, Interval: 5 * time.Second, CellWidth: 160, Columns: 5, Rows: 5, Format: TimelinePreviewFormatJPEG}
	profile.ID = TimelinePreviewProfileID(profile)
	return profile
}

// TimelinePreviewProfileID 根据产物参数计算稳定 profile ID。
func TimelinePreviewProfileID(profile TimelinePreviewProfile) string {
	identity := fmt.Sprintf("v=%d;interval_ms=%d;width=%d;columns=%d;rows=%d;format=%s",
		profile.Version, profile.Interval/time.Millisecond, profile.CellWidth, profile.Columns, profile.Rows, profile.Format)
	sum := sha256.Sum256([]byte(identity))
	return fmt.Sprintf("timeline-v%d-%s", profile.Version, hex.EncodeToString(sum[:8]))
}

// TimelineSourceFingerprint 优先使用有效 SHA-256，否则基于文件状态生成 stat-v1 指纹。
func TimelineSourceFingerprint(path, contentHash, algorithm string, stale bool) (string, error) {
	hash := strings.ToLower(strings.TrimSpace(contentHash))
	if !stale && strings.EqualFold(strings.TrimSpace(algorithm), "sha256") && validSHA256(hash) {
		return "sha256-" + hash, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	identity := fmt.Sprintf("stat-v1:%d:%d", info.Size(), info.ModTime().UnixNano())
	sum := sha256.Sum256([]byte(identity))
	return "stat-v1-" + hex.EncodeToString(sum[:]), nil
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// NewTimelinePreviewGenerationID 创建不可复用的 generation ID。
func NewTimelinePreviewGenerationID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("创建时间轴预览 generation 失败: %w", err)
	}
	return "generation-" + hex.EncodeToString(data), nil
}

// TimelinePreviewPayload 是时间轴预览任务的严格参数快照。
type TimelinePreviewPayload struct {
	SpaceID           string `json:"space_id"`
	MediaID           int64  `json:"media_id"`
	ProfileID         string `json:"profile_id"`
	SourceFingerprint string `json:"source_fingerprint"`
	GenerationID      string `json:"generation_id"`
	ForceRebuild      bool   `json:"force_rebuild"`
}

// DecodeTimelinePreviewPayload 严格解析任务 payload。
func DecodeTimelinePreviewPayload(raw string) (TimelinePreviewPayload, error) {
	var payload TimelinePreviewPayload
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return payload, fmt.Errorf("时间轴预览任务 payload 无效: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return payload, errors.New("时间轴预览任务 payload 包含多余内容")
	}
	if err := validateTimelinePreviewPayload(payload); err != nil {
		return payload, err
	}
	return payload, nil
}

// TimelinePreviewTaskKey 返回包含完整任务身份的幂等键。
func TimelinePreviewTaskKey(payload TimelinePreviewPayload) string {
	return fmt.Sprintf("preview.timeline.generate:%s:%d:%s:%s:%s", payload.SpaceID, payload.MediaID, payload.ProfileID, payload.SourceFingerprint, payload.GenerationID)
}

// TimelinePreviewCacheKey 返回与任务键独立的缓存键。
func TimelinePreviewCacheKey(payload TimelinePreviewPayload) string {
	return fmt.Sprintf("timeline_preview:%s:%d:%s:%s:%s", payload.SpaceID, payload.MediaID, payload.ProfileID, payload.SourceFingerprint, payload.GenerationID)
}

// TimelinePreviewGenerationPath 返回正式 generation 目录。
func TimelinePreviewGenerationPath(dataDir string, payload TimelinePreviewPayload) string {
	return filepath.Join(dataDir, "timeline_previews", payload.SpaceID, strconv.FormatInt(payload.MediaID, 10), payload.ProfileID, payload.SourceFingerprint, payload.GenerationID)
}

func validateTimelinePreviewPayload(payload TimelinePreviewPayload) error {
	values := []string{payload.SpaceID, payload.ProfileID, payload.SourceFingerprint, payload.GenerationID}
	if payload.MediaID <= 0 {
		return errors.New("时间轴预览媒体身份无效")
	}
	for _, value := range values {
		if !validTimelinePreviewToken(value) {
			return errors.New("时间轴预览路径身份无效")
		}
	}
	return nil
}

func validTimelinePreviewToken(value string) bool {
	if value == "" || value == "." || value == ".." || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("._-", char) {
			continue
		}
		return false
	}
	return true
}

// TimelinePreviewGenerateRequest 描述一次真实产物生成。
type TimelinePreviewGenerateRequest struct {
	SourcePath string
	OutputDir  string
	Duration   time.Duration
	Profile    TimelinePreviewProfile
}

// TimelinePreviewGenerator 生成并原子发布一个 generation。
type TimelinePreviewGenerator interface {
	Generate(context.Context, TimelinePreviewGenerateRequest) error
}

// FFmpegTimelinePreviewGenerator 使用 ffmpeg 生成分页 sprite 和 VTT。
type FFmpegTimelinePreviewGenerator struct {
	ffmpeg string
}

// NewFFmpegTimelinePreviewGenerator 创建真实生成器；空路径会在每次生成时读取全局配置。
func NewFFmpegTimelinePreviewGenerator(ffmpeg string) *FFmpegTimelinePreviewGenerator {
	if strings.TrimSpace(ffmpeg) == "" {
		ffmpeg = ""
	}
	return &FFmpegTimelinePreviewGenerator{ffmpeg: ffmpeg}
}

// Generate 在同级临时目录生成、校验并原子发布产物。
func (g *FFmpegTimelinePreviewGenerator) Generate(ctx context.Context, request TimelinePreviewGenerateRequest) error {
	if err := validateGenerateRequest(ctx, request); err != nil {
		return err
	}
	if exists, err := timelineDirExists(request.OutputDir); err != nil {
		return err
	} else if exists {
		return validatePublishedTimelineDir(request)
	}
	tempDir, err := makeTimelineTempDir(request.OutputDir)
	if err != nil {
		return err
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(tempDir)
		}
	}()
	if err := g.generateSprites(ctx, tempDir, request); err != nil {
		return err
	}
	if err := writeAndValidateTimelineVTT(tempDir, request); err != nil {
		return err
	}
	if err := publishTimelineDir(ctx, tempDir, request.OutputDir); err != nil {
		return err
	}
	published = true
	return nil
}

func validateGenerateRequest(ctx context.Context, request TimelinePreviewGenerateRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	profile := request.Profile
	if request.SourcePath == "" || request.OutputDir == "" || request.Duration <= 0 {
		return errors.New("时间轴预览生成参数无效")
	}
	if profile.ID != TimelinePreviewProfileID(profile) || profile.Interval <= 0 || profile.CellWidth <= 0 || profile.Columns <= 0 || profile.Rows <= 0 || profile.Format != TimelinePreviewFormatJPEG {
		return errors.New("时间轴预览 profile 无效")
	}
	return nil
}

func timelineDirExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}

func validatePublishedTimelineDir(request TimelinePreviewGenerateRequest) error {
	_, imageCount, err := validateTimelineSprites(request.OutputDir, request.Profile)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(request.OutputDir, "index.vtt"))
	if err != nil {
		return err
	}
	expected := (timelineCueCount(request.Duration, request.Profile.Interval) + request.Profile.Columns*request.Profile.Rows - 1) / (request.Profile.Columns * request.Profile.Rows)
	if imageCount != expected {
		return errors.New("已发布的时间轴 sprite 数量无效")
	}
	return validateTimelineVTTReferences(request.OutputDir, string(data), imageCount)
}

func makeTimelineTempDir(output string) (string, error) {
	parent := filepath.Dir(output)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return "", err
	}
	return os.MkdirTemp(parent, "."+filepath.Base(output)+".tmp-")
}

func (g *FFmpegTimelinePreviewGenerator) ffmpegPath() string {
	if g.ffmpeg != "" {
		return g.ffmpeg
	}
	return GetFFmpegPath()
}

func (g *FFmpegTimelinePreviewGenerator) generateSprites(ctx context.Context, tempDir string, request TimelinePreviewGenerateRequest) error {
	profile := request.Profile
	cueCount := timelineCueCount(request.Duration, profile.Interval)
	pageSize := profile.Columns * profile.Rows
	pageCount := (cueCount + pageSize - 1) / pageSize
	filter := timelineSpriteFilter(request, pageCount*pageSize-cueCount)
	target := filepath.Join(tempDir, "sprite-%03d.jpg")
	args := []string{"-hide_banner", "-loglevel", "error", "-y", "-i", request.SourcePath, "-an", "-vf", filter, "-frames:v", strconv.Itoa(pageCount), "-pix_fmt", "yuvj420p", "-strict", "unofficial", "-q:v", "3", target}
	// #nosec G204 -- 生成参数已由 validateGenerateRequest 校验，且 exec.CommandContext 不经过 shell 解释参数。
	output, err := exec.CommandContext(ctx, g.ffmpegPath(), args...).CombinedOutput()
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return fmt.Errorf("ffmpeg 生成时间轴 sprite 失败: %w: %s", err, strings.TrimSpace(string(output)))
}

func timelineSpriteFilter(request TimelinePreviewGenerateRequest, paddingFrames int) string {
	profile := request.Profile
	filters := []string{
		"trim=duration=" + ffmpegSeconds(request.Duration), "setpts=PTS-STARTPTS",
	}
	if paddingFrames > 0 {
		padding := time.Duration(paddingFrames) * profile.Interval
		filters = append(filters, "tpad=stop_mode=clone:stop_duration="+ffmpegSeconds(padding))
	}
	filters = append(filters,
		"fps=1/"+ffmpegSeconds(profile.Interval)+":start_time=0",
		fmt.Sprintf("scale=%d:-2", profile.CellWidth),
		fmt.Sprintf("tile=%dx%d:nb_frames=%d:padding=0:margin=0", profile.Columns, profile.Rows, profile.Columns*profile.Rows),
	)
	return strings.Join(filters, ",")
}

func writeAndValidateTimelineVTT(tempDir string, request TimelinePreviewGenerateRequest) error {
	cellHeight, imageCount, err := validateTimelineSprites(tempDir, request.Profile)
	if err != nil {
		return err
	}
	vtt, _, err := BuildTimelinePreviewVTT(request.Duration, request.Profile, cellHeight)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tempDir, "index.vtt"), []byte(vtt), 0o600); err != nil {
		return err
	}
	return validateTimelineVTTReferences(tempDir, vtt, imageCount)
}

func validateTimelineSprites(dir string, profile TimelinePreviewProfile) (int, int, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "sprite-*.jpg"))
	if err != nil || len(paths) == 0 {
		return 0, 0, errors.New("时间轴预览未生成 sprite")
	}
	cellHeight := 0
	for _, path := range paths {
		config, format, err := decodeImageConfig(path)
		if err != nil || format != "jpeg" || config.Width != profile.CellWidth*profile.Columns || config.Height%profile.Rows != 0 {
			return 0, 0, fmt.Errorf("时间轴 sprite 校验失败: %s", path)
		}
		if cellHeight == 0 {
			cellHeight = config.Height / profile.Rows
		}
		if config.Height/profile.Rows != cellHeight {
			return 0, 0, errors.New("时间轴 sprite 单元格高度不一致")
		}
	}
	return cellHeight, len(paths), nil
}

func decodeImageConfig(path string) (image.Config, string, error) {
	// #nosec G304 -- 路径仅来自受控目录下固定 sprite-*.jpg 模式的 filepath.Glob 结果。
	file, err := os.Open(path)
	if err != nil {
		return image.Config{}, "", err
	}
	defer func() { _ = file.Close() }()
	return image.DecodeConfig(file)
}

// BuildTimelinePreviewVTT 生成覆盖完整时长且不重叠的 WebVTT。
func BuildTimelinePreviewVTT(duration time.Duration, profile TimelinePreviewProfile, cellHeight int) (string, int, error) {
	if duration <= 0 || profile.Interval <= 0 || cellHeight <= 0 {
		return "", 0, errors.New("时间轴 VTT 参数无效")
	}
	cueCount := timelineCueCount(duration, profile.Interval)
	var builder strings.Builder
	builder.WriteString("WEBVTT\n\n")
	for index := 0; index < cueCount; index++ {
		writeTimelineCue(&builder, duration, profile, cellHeight, index)
	}
	return builder.String(), cueCount, nil
}

func writeTimelineCue(builder *strings.Builder, duration time.Duration, profile TimelinePreviewProfile, cellHeight, index int) {
	start := time.Duration(index) * profile.Interval
	end := minDuration(duration, start+profile.Interval)
	pageSize := profile.Columns * profile.Rows
	cell := index % pageSize
	x := cell % profile.Columns * profile.CellWidth
	y := cell / profile.Columns * cellHeight
	fmt.Fprintf(builder, "%s --> %s\n", formatVTTTime(start), formatVTTTime(end))
	fmt.Fprintf(builder, "sprite-%03d.jpg#xywh=%d,%d,%d,%d\n\n", index/pageSize+1, x, y, profile.CellWidth, cellHeight)
}

func validateTimelineVTTReferences(dir, vtt string, imageCount int) error {
	scanner := bufio.NewScanner(strings.NewReader(vtt))
	references := map[string]struct{}{}
	for scanner.Scan() {
		line := scanner.Text()
		if index := strings.Index(line, "#xywh="); index > 0 {
			references[line[:index]] = struct{}{}
		}
	}
	if scanner.Err() != nil || len(references) != imageCount {
		return fmt.Errorf("时间轴 VTT 引用数量与 sprite 不一致: references=%d images=%d", len(references), imageCount)
	}
	for name := range references {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("时间轴 VTT 引用不存在: %s", name)
		}
	}
	return nil
}

func publishTimelineDir(ctx context.Context, tempDir, outputDir string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := os.Stat(outputDir); err == nil {
		return errors.New("时间轴预览 generation 已存在")
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(tempDir, outputDir); err != nil {
		return fmt.Errorf("原子发布时间轴预览失败: %w", err)
	}
	return nil
}

func timelineCueCount(duration, interval time.Duration) int {
	return int((duration + interval - 1) / interval)
}

func formatVTTTime(value time.Duration) string {
	milliseconds := value.Milliseconds()
	hours := milliseconds / 3_600_000
	minutes := milliseconds / 60_000 % 60
	seconds := milliseconds / 1_000 % 60
	return fmt.Sprintf("%02d:%02d:%02d.%03d", hours, minutes, seconds, milliseconds%1000)
}

func ffmpegSeconds(value time.Duration) string {
	return strconv.FormatFloat(value.Seconds(), 'f', 3, 64)
}

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}
