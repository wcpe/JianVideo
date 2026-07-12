package transcoder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/wcpe/JianVideo/internal/db/models"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

const (
	// TaskTypeHLSPreview 是 FR2-008 统一 HLS 预览任务类型。
	TaskTypeHLSPreview = "transcode.hls.preview"
	// DefaultHLSPreviewProfile 是旧 HLS URL 映射的默认 profile。
	DefaultHLSPreviewProfile = "h264"
	// HLSPreviewMaxAttempts 是 HLS 预览任务自动尝试上限。
	HLSPreviewMaxAttempts = 3
)

var hlsPathTokenPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

// HLSPreviewPayload 是统一任务持久化的 HLS 预览参数快照。
type HLSPreviewPayload struct {
	LegacyTable  string `json:"legacy_table,omitempty"`
	LegacyID     int64  `json:"legacy_id,omitempty"`
	SpaceID      string `json:"space_id"`
	MediaID      int64  `json:"media_id"`
	PresetID     int64  `json:"preset_id,omitempty"`
	ProfileID    string `json:"profile_id"`
	Codec        string `json:"codec"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	ForceRebuild bool   `json:"force_rebuild"`
}

// HLSPreviewRequest 描述 HLS 预览任务入队参数。
type HLSPreviewRequest struct {
	SpaceID      string
	MediaID      int64
	PresetID     int64
	ProfileID    string
	Codec        string
	Width        int
	Height       int
	Priority     int
	ForceRebuild bool
}

// HLSPreviewExecFunc 执行一个 HLS 预览任务。
type HLSPreviewExecFunc func(context.Context, int64, HLSPreviewPayload) error

// HLSPreviewStatus 描述指定媒体 profile 的可用性与最近任务。
type HLSPreviewStatus struct {
	Available bool
	ProfileID string
	URL       string
	Task      *models.Task
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
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("编码 HLS 预览任务失败: %w", err)
	}
	return s.tasks.Enqueue(ctx, tasksvc.EnqueueInput{
		Scope: models.TaskScopeSpace, SpaceID: payload.SpaceID, Type: TaskTypeHLSPreview,
		Priority: request.Priority, MaxAttempts: HLSPreviewMaxAttempts,
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
	profileID, err := normalizeHLSProfile(profileID)
	if err != nil {
		return HLSPreviewStatus{}, err
	}
	page, err := s.tasks.List(ctx, tasksvc.Query{
		Scope: models.TaskScopeSpace, SpaceID: spaceID, Type: TaskTypeHLSPreview,
		ResourceType: "media", ResourceID: strconv.FormatInt(mediaID, 10), Page: 1, PageSize: 100,
	})
	if err != nil {
		return HLSPreviewStatus{}, err
	}
	task := latestHLSProfileTask(page.Items, mediaID, profileID)
	codec := hlsProfileTaskCodec(task)
	return HLSPreviewStatus{
		Available: s.profileAvailable(spaceID, mediaID, profileID, codec), ProfileID: profileID,
		URL: HLSProfileURL(mediaID, profileID, codec), Task: task,
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
	count := countHLSFiles(s.root, payload)
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
	return HLSPreviewPayload{
		SpaceID: spaceID, MediaID: request.MediaID, PresetID: request.PresetID,
		ProfileID: profileID, Codec: codec, Width: request.Width, Height: request.Height,
		ForceRebuild: request.ForceRebuild,
	}, nil
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
	_, err := normalizeHLSPreviewRequest(HLSPreviewRequest{
		SpaceID: payload.SpaceID, MediaID: payload.MediaID, PresetID: payload.PresetID,
		ProfileID: payload.ProfileID, Codec: payload.Codec, Width: payload.Width, Height: payload.Height,
		ForceRebuild: payload.ForceRebuild,
	})
	return payload, err
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

// HLSProfileDir 返回 Space/media/profile 隔离的 HLS 产物目录。
func HLSProfileDir(root, spaceID string, mediaID int64, profileID string) (string, error) {
	payload, err := normalizeHLSPreviewRequest(HLSPreviewRequest{SpaceID: spaceID, MediaID: mediaID, ProfileID: profileID, Codec: profileID})
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Clean(root), payload.SpaceID, strconv.FormatInt(mediaID, 10), payload.ProfileID), nil
}

// HLSProfileURL 返回指定 profile 的播放 URL；默认 H.264 profile 保持旧 URL。
func HLSProfileURL(mediaID int64, profileID, codec string) string {
	manifest := hlsProfileManifest(codec)
	if profileID == DefaultHLSPreviewProfile {
		return fmt.Sprintf("/api/play/hls/%d/%s", mediaID, manifest)
	}
	return fmt.Sprintf("/api/play/hls/%d/profiles/%s/%s", mediaID, profileID, manifest)
}

func hlsPreviewKey(payload HLSPreviewPayload) string {
	return fmt.Sprintf("hls-preview:%s:%d:%s", payload.SpaceID, payload.MediaID, payload.ProfileID)
}

func (s *HLSPreviewService) profileAvailable(spaceID string, mediaID int64, profileID, codec string) bool {
	dir, err := HLSProfileDir(s.root, spaceID, mediaID, profileID)
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

func countHLSFiles(root string, payload HLSPreviewPayload) int {
	dir, err := HLSProfileDir(root, payload.SpaceID, payload.MediaID, payload.ProfileID)
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
