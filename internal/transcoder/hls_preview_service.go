package transcoder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/wcpe/JianVideo/internal/db/models"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

const (
	// TaskTypeHLSPreview 是 FR2-008 统一 HLS 预览任务类型。
	TaskTypeHLSPreview = "transcode.hls.preview"
	// DefaultHLSPreviewProfile 是旧 HLS URL 映射的默认 profile。
	DefaultHLSPreviewProfile = "h264"
	// HLSPreviewMaxAttempts 是 HLS 预览任务自动尝试上限。
	HLSPreviewMaxAttempts    = 3
	audioReloadProfilePrefix = "audio-h264-aac-"
)

var (
	hlsPathTokenPattern       = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)
	audioReloadProfilePattern = regexp.MustCompile(`^` + regexp.QuoteMeta(audioReloadProfilePrefix) + `[0-9a-f]{24}$`)
)

// AudioReloadProfileID 为音轨 ID 派生安全稳定的 H.264/AAC profile。
func AudioReloadProfileID(trackID string) string {
	sum := sha256.Sum256([]byte(trackID))
	return audioReloadProfilePrefix + hex.EncodeToString(sum[:12])
}

// IsAudioReloadProfileID 判断 profile 是否为规范音轨 reload 派生格式。
func IsAudioReloadProfileID(profileID string) bool {
	return audioReloadProfilePattern.MatchString(profileID)
}

// IsAudioReloadProfileNamespace 判断 profile 是否占用音轨 reload 保留命名空间。
func IsAudioReloadProfileNamespace(profileID string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(profileID)), audioReloadProfilePrefix)
}

// HLSPreviewPayload 是统一任务持久化的 HLS 预览参数快照。
type HLSPreviewPayload struct {
	LegacyTable       string `json:"legacy_table,omitempty"`
	LegacyID          int64  `json:"legacy_id,omitempty"`
	SpaceID           string `json:"space_id"`
	MediaID           int64  `json:"media_id"`
	PresetID          int64  `json:"preset_id,omitempty"`
	ProfileID         string `json:"profile_id"`
	Codec             string `json:"codec"`
	Width             int    `json:"width"`
	Height            int    `json:"height"`
	AudioTrackID      string `json:"audio_track_id,omitempty"`
	AudioStreamIndex  *int   `json:"audio_stream_index,omitempty"`
	SourceFingerprint string `json:"source_fingerprint,omitempty"`
	ForceRebuild      bool   `json:"force_rebuild"`
}

// HLSPreviewRequest 描述 HLS 预览任务入队参数。
type HLSPreviewRequest struct {
	SpaceID           string
	MediaID           int64
	PresetID          int64
	ProfileID         string
	Codec             string
	Width             int
	Height            int
	Priority          int
	AudioTrackID      string
	AudioStreamIndex  *int
	SourceFingerprint string
	ForceRebuild      bool
}

// AudioReloadRequest 描述本地音轨 reload 的 HLS 任务参数。
type AudioReloadRequest struct {
	SpaceID           string
	MediaID           int64
	AudioTrackID      string
	AudioStreamIndex  int
	Width             int
	Height            int
	SourceFingerprint string
}

// MediaIdentity 描述入队时媒体源的身份快照。
type MediaIdentity struct {
	SpaceID          string
	MediaID          int64
	Path             string
	Size             int64
	ModifiedAt       time.Time
	ContentHash      string
	ContentHashStale bool
}

// AudioTrackIdentity 描述入队时内嵌音轨的稳定身份。
type AudioTrackIdentity struct {
	ID            string
	Index         int
	Codec         string
	Language      string
	Title         string
	Channels      int
	ChannelLayout string
}

// AudioReloadSourceFingerprint 根据媒体与音轨身份生成稳定指纹。
func AudioReloadSourceFingerprint(media MediaIdentity, track AudioTrackIdentity) string {
	value := fmt.Sprintf("%s|%d|%s|%d|%d|%s|%t|%s|%d|%s|%s|%s|%d|%s", media.SpaceID, media.MediaID, media.Path, media.Size, media.ModifiedAt.UnixNano(), media.ContentHash, media.ContentHashStale, track.ID, track.Index, track.Codec, track.Language, track.Title, track.Channels, track.ChannelLayout)
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// HLSPreviewExecFunc 执行一个 HLS 预览任务。
type HLSPreviewExecFunc func(context.Context, int64, HLSPreviewPayload) error

// HLSPreviewStatus 描述指定媒体 profile 的可用性与最近任务。
type HLSPreviewStatus struct {
	Available        bool
	ProfileID        string
	URL              string
	Task             *models.Task
	EffectiveTrackID string `json:"effective_track_id,omitempty"`
}

// HLSPreviewService 把 HLS 预览接入通用任务中心。
type HLSPreviewService struct {
	tasks   *tasksvc.Service
	workers *tasksvc.WorkerRegistry
	root    string
	exec    HLSPreviewExecFunc
}

// NewHLSPreviewService 创建 HLS 预览任务服务。
func NewHLSPreviewService(tasks *tasksvc.Service, workers *tasksvc.WorkerRegistry, root string, exec HLSPreviewExecFunc) *HLSPreviewService {
	return &HLSPreviewService{tasks: tasks, workers: workers, root: filepath.Clean(root), exec: exec}
}

// RegisterWorker 注册单并发 HLS 预览 worker。
func (s *HLSPreviewService) RegisterWorker() error {
	if s.tasks == nil || s.workers == nil || s.exec == nil {
		return errors.New("HLS 预览任务服务未完整配置")
	}
	return s.workers.Register(TaskTypeHLSPreview, tasksvc.DefaultConcurrency(TaskTypeHLSPreview), s.handleTask)
}

// Enqueue 创建统一 HLS 预览任务。
func (s *HLSPreviewService) Enqueue(ctx context.Context, request HLSPreviewRequest) (*models.Task, error) {
	payload, err := normalizeHLSPreviewRequest(request)
	if err != nil {
		return nil, err
	}
	if IsAudioReloadProfileNamespace(payload.ProfileID) {
		return nil, errors.New("音轨 HLS profile 只能通过专用入口创建")
	}
	return s.enqueue(ctx, payload, request.Priority)
}

// EnqueueAudioReload 创建固定 H.264/AAC profile 的本地音轨 reload 任务。
func (s *HLSPreviewService) EnqueueAudioReload(ctx context.Context, request AudioReloadRequest) (*models.Task, error) {
	trackID := strings.TrimSpace(request.AudioTrackID)
	fingerprint := strings.TrimSpace(request.SourceFingerprint)
	if trackID == "" || request.AudioStreamIndex < 0 || fingerprint == "" {
		return nil, errors.New("HLS 音轨 reload 绑定或源指纹不完整")
	}
	streamIndex := request.AudioStreamIndex
	payload, err := normalizeHLSPreviewRequest(HLSPreviewRequest{
		SpaceID: request.SpaceID, MediaID: request.MediaID,
		ProfileID: AudioReloadProfileID(trackID), Codec: DefaultTargetCodec,
		Width: request.Width, Height: request.Height,
		AudioTrackID: trackID, AudioStreamIndex: &streamIndex, SourceFingerprint: fingerprint, ForceRebuild: true,
	})
	if err != nil {
		return nil, err
	}
	return s.enqueue(ctx, payload, 0)
}

func (s *HLSPreviewService) enqueue(ctx context.Context, payload HLSPreviewPayload, priority int) (*models.Task, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("编码 HLS 预览任务失败: %w", err)
	}
	return s.tasks.Enqueue(ctx, tasksvc.EnqueueInput{
		Scope: models.TaskScopeSpace, SpaceID: payload.SpaceID, Type: TaskTypeHLSPreview,
		Priority: priority, MaxAttempts: HLSPreviewMaxAttempts,
		IdempotencyKey: hlsPreviewKey(payload), PayloadJSON: string(data),
		ResourceType: "media", ResourceID: strconv.FormatInt(payload.MediaID, 10),
	})
}

// List 返回当前 Space 的 HLS 预览任务。
func (s *HLSPreviewService) List(ctx context.Context, spaceID, status string) (tasksvc.Page, error) {
	return s.tasks.List(ctx, tasksvc.Query{
		Scope: models.TaskScopeSpace, SpaceID: spaceID, Type: TaskTypeHLSPreview,
		Status: status, Page: 1, PageSize: 100,
	})
}

// Status 返回指定 profile 的可用性和最近任务。
func (s *HLSPreviewService) Status(ctx context.Context, spaceID string, mediaID int64, profileID string) (HLSPreviewStatus, error) {
	return s.status(ctx, spaceID, mediaID, profileID, 0)
}

// StatusTask 返回指定 Space 中精确任务的 HLS 状态。
func (s *HLSPreviewService) StatusTask(ctx context.Context, spaceID string, mediaID int64, profileID string, taskID int64) (HLSPreviewStatus, error) {
	return s.status(ctx, spaceID, mediaID, profileID, taskID)
}

func (s *HLSPreviewService) status(ctx context.Context, spaceID string, mediaID int64, profileID string, taskID int64) (HLSPreviewStatus, error) {
	profileID, err := normalizeHLSProfile(profileID)
	if err != nil {
		return HLSPreviewStatus{}, err
	}
	if IsAudioReloadProfileNamespace(profileID) {
		if !IsAudioReloadProfileID(profileID) {
			return HLSPreviewStatus{}, errors.New("HLS 音轨 profile 必须使用规范小写格式")
		}
		if taskID <= 0 {
			return HLSPreviewStatus{}, errors.New("HLS 音轨状态必须绑定 task_id")
		}
	}
	var task *models.Task
	if taskID > 0 {
		task, err = s.tasks.Get(ctx, taskID, tasksvc.Query{Scope: models.TaskScopeSpace, SpaceID: spaceID, Type: TaskTypeHLSPreview, ResourceType: "media", ResourceID: strconv.FormatInt(mediaID, 10)})
		if err != nil {
			return HLSPreviewStatus{}, err
		}
	} else {
		page, listErr := s.tasks.List(ctx, tasksvc.Query{
			Scope: models.TaskScopeSpace, SpaceID: spaceID, Type: TaskTypeHLSPreview,
			ResourceType: "media", ResourceID: strconv.FormatInt(mediaID, 10), Page: 1, PageSize: 100,
		})
		if listErr != nil {
			return HLSPreviewStatus{}, listErr
		}
		task = latestHLSProfileTask(page.Items, mediaID, profileID)
	}
	if task != nil {
		payload, payloadErr := decodeHLSPreviewTask(*task)
		if payloadErr != nil || payload.ProfileID != profileID {
			return HLSPreviewStatus{}, errors.New("HLS 预览任务与状态查询不匹配")
		}
	}
	codec := hlsProfileTaskCodec(task)
	available := s.profileAvailable(spaceID, mediaID, profileID, codec, taskID)
	if IsAudioReloadProfileID(profileID) {
		available = task != nil && task.Status == models.TaskStatusSucceeded && available
	}
	return HLSPreviewStatus{
		Available: available, ProfileID: profileID,
		URL: hlsStatusURL(mediaID, profileID, codec, taskID), Task: task,
		EffectiveTrackID: effectiveTrackID(task, available),
	}, nil
}

func (s *HLSPreviewService) handleTask(ctx context.Context, task models.Task) error {
	payload, err := decodeHLSPreviewTask(task)
	if err != nil {
		return err
	}
	if err := s.tasks.UpdateProgress(ctx, task.ID, tasksvc.ProgressInput{Progress: 5, Checkpoint: "已校验 HLS 预览任务"}); err != nil {
		return err
	}
	if err := s.exec(ctx, task.ID, payload); err != nil {
		return err
	}
	count := countHLSFiles(s.root, task.ID, payload)
	checkpoint := fmt.Sprintf("已生成 %d 个 HLS 产物文件", count)
	return s.tasks.UpdateProgress(ctx, task.ID, tasksvc.ProgressInput{Progress: 95, Checkpoint: checkpoint})
}

func normalizeHLSPreviewRequest(request HLSPreviewRequest) (HLSPreviewPayload, error) {
	spaceID := strings.TrimSpace(request.SpaceID)
	if spaceID == "" {
		spaceID = models.DefaultSpaceID
	}
	if !hlsPathTokenPattern.MatchString(spaceID) || request.MediaID <= 0 || request.Width < 0 || request.Height < 0 {
		return HLSPreviewPayload{}, errors.New("HLS 预览任务资源参数无效")
	}
	profileID, err := normalizeHLSProfile(request.ProfileID)
	if err != nil {
		return HLSPreviewPayload{}, err
	}
	codec := normalizeCodec(request.Codec)
	if codec == "" {
		codec = DefaultTargetCodec
	}
	trackID, streamIndex, err := normalizeAudioBinding(request.AudioTrackID, request.AudioStreamIndex, codec, profileID)
	if err != nil {
		return HLSPreviewPayload{}, err
	}
	fingerprint := strings.TrimSpace(request.SourceFingerprint)
	if IsAudioReloadProfileNamespace(profileID) && (trackID == "" || streamIndex == nil || fingerprint == "") {
		return HLSPreviewPayload{}, errors.New("HLS 音轨 reload 绑定或源指纹不完整")
	}
	return HLSPreviewPayload{
		SpaceID: spaceID, MediaID: request.MediaID, PresetID: request.PresetID,
		ProfileID: profileID, Codec: codec, Width: request.Width, Height: request.Height,
		AudioTrackID: trackID, AudioStreamIndex: streamIndex, SourceFingerprint: fingerprint, ForceRebuild: request.ForceRebuild,
	}, nil
}

func normalizeAudioBinding(trackID string, streamIndex *int, codec, profileID string) (string, *int, error) {
	trackID = strings.TrimSpace(trackID)
	if (trackID == "") != (streamIndex == nil) {
		return "", nil, errors.New("HLS 音轨 ID 与流索引必须成对提供")
	}
	if streamIndex == nil {
		return "", nil, nil
	}
	if *streamIndex < 0 {
		return "", nil, errors.New("HLS 音轨流索引不能为负数")
	}
	if codec != DefaultTargetCodec || profileID != AudioReloadProfileID(trackID) {
		return "", nil, errors.New("HLS 音轨 reload 必须使用派生的 H.264 profile")
	}
	indexCopy := *streamIndex
	return trackID, &indexCopy, nil
}

func normalizeHLSProfile(profileID string) (string, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		profileID = DefaultHLSPreviewProfile
	}
	if !hlsPathTokenPattern.MatchString(profileID) {
		return "", errors.New("HLS profile_id 无效")
	}
	return profileID, nil
}

func decodeHLSPreviewTask(task models.Task) (HLSPreviewPayload, error) {
	if task.Type != TaskTypeHLSPreview || task.Scope != models.TaskScopeSpace || task.SpaceID == nil {
		return HLSPreviewPayload{}, errors.New("HLS 预览任务信封无效")
	}
	var payload HLSPreviewPayload
	decoder := json.NewDecoder(strings.NewReader(task.PayloadJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return HLSPreviewPayload{}, fmt.Errorf("HLS 预览任务 payload 无效: %w", err)
	}
	if payload.SpaceID != *task.SpaceID || strconv.FormatInt(payload.MediaID, 10) != task.ResourceID {
		return HLSPreviewPayload{}, errors.New("HLS 预览任务 payload 与资源不匹配")
	}
	normalized, err := normalizeHLSPreviewRequest(HLSPreviewRequest{
		SpaceID: payload.SpaceID, MediaID: payload.MediaID, PresetID: payload.PresetID,
		ProfileID: payload.ProfileID, Codec: payload.Codec, Width: payload.Width, Height: payload.Height,
		AudioTrackID: payload.AudioTrackID, AudioStreamIndex: payload.AudioStreamIndex, SourceFingerprint: payload.SourceFingerprint,
		ForceRebuild: payload.ForceRebuild,
	})
	if err != nil {
		return HLSPreviewPayload{}, err
	}
	if payload.AudioTrackID == "" {
		return payload, nil
	}
	if payload.AudioTrackID != normalized.AudioTrackID || payload.Codec != normalized.Codec || payload.ProfileID != normalized.ProfileID {
		return HLSPreviewPayload{}, errors.New("HLS 音轨 reload payload 未规范化")
	}
	return normalized, nil
}

// HLSPreviewResolution 返回任务快照指定的预览尺寸，未指定的维度回退源媒体尺寸。
func HLSPreviewResolution(payload HLSPreviewPayload, sourceWidth, sourceHeight int) (int, int) {
	width, height := payload.Width, payload.Height
	if width <= 0 {
		width = sourceWidth
	}
	if height <= 0 {
		height = sourceHeight
	}
	return width, height
}

// HLSProfileDir 返回普通 Space/media/profile 隔离的 HLS 产物目录。
func HLSProfileDir(root, spaceID string, mediaID int64, profileID string) (string, error) {
	profileID, err := normalizeHLSProfile(profileID)
	if err != nil {
		return "", err
	}
	if IsAudioReloadProfileNamespace(profileID) {
		return "", errors.New("音轨 HLS profile 必须绑定 task_id")
	}
	return hlsProfileBaseDir(root, spaceID, mediaID, profileID)
}

// HLSProfileTaskDir 返回音轨 profile 按 task_id 隔离的 HLS 产物目录。
func HLSProfileTaskDir(root, spaceID string, mediaID int64, profileID string, taskID int64) (string, error) {
	if !IsAudioReloadProfileID(profileID) || taskID <= 0 {
		return "", errors.New("音轨 HLS profile 或 task_id 无效")
	}
	base, err := hlsProfileBaseDir(root, spaceID, mediaID, profileID)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "tasks", strconv.FormatInt(taskID, 10)), nil
}

// HLSPreviewOutputDir 返回当前任务实际使用的 HLS 产物目录。
func HLSPreviewOutputDir(root string, taskID int64, payload HLSPreviewPayload) (string, error) {
	if IsAudioReloadProfileNamespace(payload.ProfileID) {
		return HLSProfileTaskDir(root, payload.SpaceID, payload.MediaID, payload.ProfileID, taskID)
	}
	return HLSProfileDir(root, payload.SpaceID, payload.MediaID, payload.ProfileID)
}

func hlsProfileBaseDir(root, spaceID string, mediaID int64, profileID string) (string, error) {
	spaceID = strings.TrimSpace(spaceID)
	if spaceID == "" {
		spaceID = models.DefaultSpaceID
	}
	if !hlsPathTokenPattern.MatchString(spaceID) || mediaID <= 0 || !hlsPathTokenPattern.MatchString(profileID) {
		return "", errors.New("HLS profile 目录参数无效")
	}
	return filepath.Join(filepath.Clean(root), spaceID, strconv.FormatInt(mediaID, 10), profileID), nil
}

// HLSProfileURL 返回指定普通 profile 的播放 URL；默认 H.264 profile 保持旧 URL。
func HLSProfileURL(mediaID int64, profileID, codec string) string {
	manifest := hlsProfileManifest(codec)
	if profileID == DefaultHLSPreviewProfile {
		return fmt.Sprintf("/api/play/hls/%d/%s", mediaID, manifest)
	}
	return fmt.Sprintf("/api/play/hls/%d/profiles/%s/%s", mediaID, profileID, manifest)
}

// HLSProfileTaskURL 返回绑定 task_id 的音轨 HLS 播放 URL。
func HLSProfileTaskURL(mediaID int64, profileID, codec string, taskID int64) string {
	return fmt.Sprintf("/api/play/hls/%d/profiles/%s/tasks/%d/%s", mediaID, profileID, taskID, hlsProfileManifest(codec))
}

func hlsStatusURL(mediaID int64, profileID, codec string, taskID int64) string {
	if IsAudioReloadProfileID(profileID) {
		return HLSProfileTaskURL(mediaID, profileID, codec, taskID)
	}
	return HLSProfileURL(mediaID, profileID, codec)
}

func hlsPreviewKey(payload HLSPreviewPayload) string {
	key := fmt.Sprintf("hls-preview:%s:%d:%s", payload.SpaceID, payload.MediaID, payload.ProfileID)
	if payload.SourceFingerprint != "" {
		return key + ":" + payload.SourceFingerprint
	}
	return key
}

func (s *HLSPreviewService) profileAvailable(spaceID string, mediaID int64, profileID, codec string, taskID int64) bool {
	payload := HLSPreviewPayload{SpaceID: spaceID, MediaID: mediaID, ProfileID: profileID}
	dir, err := HLSPreviewOutputDir(s.root, taskID, payload)
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, hlsProfileManifest(codec)))
	return err == nil
}

func hlsProfileManifest(codec string) string {
	if SelectOutputPath(codec) == OutputPathTS {
		return "master.m3u8"
	}
	return fmp4ManifestFilename
}

func hlsProfileTaskCodec(task *models.Task) string {
	if task == nil {
		return DefaultTargetCodec
	}
	var payload HLSPreviewPayload
	if json.Unmarshal([]byte(task.PayloadJSON), &payload) != nil {
		return DefaultTargetCodec
	}
	return payload.Codec
}

func hlsProfileTaskTrackID(task *models.Task) string {
	if task == nil {
		return ""
	}
	var payload HLSPreviewPayload
	if json.Unmarshal([]byte(task.PayloadJSON), &payload) != nil {
		return ""
	}
	return payload.AudioTrackID
}

func effectiveTrackID(task *models.Task, available bool) string {
	if task == nil || task.Status != models.TaskStatusSucceeded || !available {
		return ""
	}
	return hlsProfileTaskTrackID(task)
}

func latestHLSProfileTask(items []models.Task, mediaID int64, profileID string) *models.Task {
	for i := range items {
		if items[i].ResourceID != strconv.FormatInt(mediaID, 10) {
			continue
		}
		var payload HLSPreviewPayload
		if json.Unmarshal([]byte(items[i].PayloadJSON), &payload) == nil && payload.ProfileID == profileID {
			return &items[i]
		}
	}
	return nil
}

func countHLSFiles(root string, taskID int64, payload HLSPreviewPayload) int {
	dir, err := HLSPreviewOutputDir(root, taskID, payload)
	if err != nil {
		return 0
	}
	count := 0
	_ = filepath.WalkDir(dir, func(_ string, entry os.DirEntry, walkErr error) error {
		if walkErr == nil && !entry.IsDir() {
			count++
		}
		return nil
	})
	return count
}
