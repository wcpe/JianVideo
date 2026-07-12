package transcoder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wcpe/JianVideo/internal/db/models"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

const (
	// TaskTypeHLSABR 是多码率 HLS 转码任务类型。
	TaskTypeHLSABR = "transcode.hls.abr"
	// ABRProfileID 是 H.264 + MPEG-TS 自适应播放 profile。
	ABRProfileID = "abr-h264"
	// ABRMaxAttempts 是 ABR 任务最大尝试次数。
	ABRMaxAttempts = 3
)

// ABRRequest 描述显式 ABR 入队请求。
type ABRRequest struct {
	SpaceID           string
	MediaID           int64
	SourceWidth       int
	SourceHeight      int
	Ladder            []string
	Priority          int
	ForceRebuild      bool
	HWAccelPreference string
}

// ABRPayload 是持久化任务参数快照。
type ABRPayload struct {
	SpaceID           string              `json:"space_id"`
	MediaID           int64               `json:"media_id"`
	ProfileID         string              `json:"profile_id"`
	Codec             string              `json:"codec"`
	Ladder            []QualityDefinition `json:"ladder"`
	HWAccelPreference string              `json:"hwaccel_preference"`
	ForceRebuild      bool                `json:"force_rebuild"`
}

// ABRExecFunc 执行一次 ABR 任务。
type ABRExecFunc func(context.Context, int64, ABRPayload) error

// ABRService 管理 ABR 任务入队、执行与状态查询。
type ABRService struct {
	tasks   *tasksvc.Service
	workers *tasksvc.WorkerRegistry
	hlsRoot string
	exec    ABRExecFunc
}

// NewABRService 创建 ABR 任务服务。
func NewABRService(tasks *tasksvc.Service, workers *tasksvc.WorkerRegistry, hlsRoot string, exec ABRExecFunc) *ABRService {
	return &ABRService{tasks: tasks, workers: workers, hlsRoot: hlsRoot, exec: exec}
}

// RegisterWorker 注册 ABR worker。
func (s *ABRService) RegisterWorker(concurrency int) error {
	if s.tasks == nil || s.workers == nil || s.exec == nil {
		return errors.New("ABR 任务服务依赖未配置")
	}
	if concurrency <= 0 {
		concurrency = tasksvc.DefaultConcurrency(TaskTypeHLSABR)
	}
	return s.workers.Register(TaskTypeHLSABR, concurrency, s.handleTask)
}

// Enqueue 显式创建 ABR 任务并复用相同活动任务。
func (s *ABRService) Enqueue(ctx context.Context, request ABRRequest) (*models.Task, error) {
	if s.tasks == nil {
		return nil, errors.New("ABR 任务服务未配置")
	}
	spaceID := normalizeTaskSpaceID(request.SpaceID)
	if request.MediaID <= 0 {
		return nil, errors.New("媒体 ID 无效")
	}
	ladder, err := ABRLadderForSource(request.SourceWidth, request.SourceHeight, request.Ladder)
	if err != nil {
		return nil, err
	}
	payload := ABRPayload{
		SpaceID: spaceID, MediaID: request.MediaID, ProfileID: ABRProfileID, Codec: DefaultTargetCodec,
		Ladder: ladder, HWAccelPreference: NormalizeHWAccelMode(request.HWAccelPreference), ForceRebuild: request.ForceRebuild,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("编码 ABR payload 失败: %w", err)
	}
	return s.tasks.Enqueue(ctx, tasksvc.EnqueueInput{
		Scope: models.TaskScopeSpace, SpaceID: spaceID, Type: TaskTypeHLSABR,
		Priority: request.Priority, MaxAttempts: ABRMaxAttempts,
		IdempotencyKey: abrIdempotencyKey(payload), PayloadJSON: string(data),
		ResourceType: "media", ResourceID: strconv.FormatInt(request.MediaID, 10),
	})
}

// Status 返回 ABR profile 可用性与最近任务。
func (s *ABRService) Status(ctx context.Context, spaceID string, mediaID int64) (HLSPreviewStatus, error) {
	spaceID = normalizeTaskSpaceID(spaceID)
	outputDir, err := HLSProfileDir(s.hlsRoot, spaceID, mediaID, ABRProfileID)
	if err != nil {
		return HLSPreviewStatus{}, err
	}
	status := HLSPreviewStatus{ProfileID: ABRProfileID, URL: HLSProfileURL(mediaID, ABRProfileID, DefaultTargetCodec)}
	if _, err := os.Stat(filepath.Join(outputDir, "master.m3u8")); err == nil {
		status.Available = true
	} else if !os.IsNotExist(err) {
		return HLSPreviewStatus{}, err
	}
	page, err := s.tasks.List(ctx, tasksvc.Query{
		Scope: models.TaskScopeSpace, SpaceID: spaceID, Type: TaskTypeHLSABR,
		ResourceType: "media", ResourceID: strconv.FormatInt(mediaID, 10), Limit: 1,
	})
	if err != nil {
		return HLSPreviewStatus{}, err
	}
	if len(page.Items) > 0 {
		status.Task = &page.Items[0]
	}
	return status, nil
}

func (s *ABRService) handleTask(ctx context.Context, task models.Task) error {
	payload, err := decodeABRPayload(task.PayloadJSON)
	if err != nil {
		return err
	}
	if err := validateABRTask(task, payload); err != nil {
		return err
	}
	if err := s.tasks.UpdateProgress(ctx, task.ID, tasksvc.ProgressInput{Progress: 5, Checkpoint: "已校验 ABR 任务"}); err != nil {
		return err
	}
	if err := s.exec(ctx, task.ID, payload); err != nil {
		return err
	}
	return s.tasks.UpdateProgress(ctx, task.ID, tasksvc.ProgressInput{Progress: 95, Checkpoint: "已完成 ABR 产物登记"})
}

func decodeABRPayload(raw string) (ABRPayload, error) {
	var payload ABRPayload
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return payload, fmt.Errorf("ABR payload 无效: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return payload, errors.New("ABR payload 包含多余内容")
	}
	return payload, nil
}

func validateABRTask(task models.Task, payload ABRPayload) error {
	if task.Type != TaskTypeHLSABR || task.Scope != models.TaskScopeSpace || task.SpaceID == nil {
		return errors.New("ABR 任务信封无效")
	}
	if payload.SpaceID != strings.TrimSpace(*task.SpaceID) || payload.MediaID <= 0 || payload.ProfileID != ABRProfileID || payload.Codec != DefaultTargetCodec || len(payload.Ladder) == 0 {
		return errors.New("ABR 任务 payload 与信封不匹配")
	}
	return nil
}

func abrIdempotencyKey(payload ABRPayload) string {
	names := make([]string, len(payload.Ladder))
	for index := range payload.Ladder {
		names[index] = payload.Ladder[index].Name
	}
	return fmt.Sprintf("hls-abr:%s:%d:%s:%s", payload.SpaceID, payload.MediaID, payload.ProfileID, strings.Join(names, ","))
}
