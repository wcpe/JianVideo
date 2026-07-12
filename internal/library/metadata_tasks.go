package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/wcpe/JianVideo/internal/db/models"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

const (
	// TaskTypeMetadataParse 是单媒体元数据解析任务。
	TaskTypeMetadataParse = "metadata.parse"
	// TaskTypeMetadataBackfill 是批量元数据回填任务。
	TaskTypeMetadataBackfill = "metadata.backfill"
)

// MetadataTaskEnqueuer 是元数据扫描 hook 需要的最小任务接口。
type MetadataTaskEnqueuer interface {
	Enqueue(context.Context, tasksvc.EnqueueInput) (*models.Task, error)
}

type metadataTaskPayload struct {
	SpaceID            string `json:"space_id"`
	LibraryID          int64  `json:"library_id,omitempty"`
	MediaID            int64  `json:"media_id,omitempty"`
	FileSize           int64  `json:"file_size,omitempty"`
	ModifiedAtUnixNano int64  `json:"modified_at_unix_nano,omitempty"`
}

// EnqueueMetadataParse 将单媒体解析任务幂等入队。
func EnqueueMetadataParse(ctx context.Context, tasks MetadataTaskEnqueuer, media models.MediaFile) (*models.Task, error) {
	if tasks == nil {
		return nil, errors.New("任务中心未启用")
	}
	payload := metadataTaskPayload{
		SpaceID: media.SpaceID, LibraryID: media.LibraryID, MediaID: media.ID,
		FileSize: media.FileSize, ModifiedAtUnixNano: media.ModifiedAt.UnixNano(),
	}
	return tasks.Enqueue(ctx, metadataTaskInput(TaskTypeMetadataParse, payload))
}

// EnqueueMetadataBackfill 将 Space 或指定库的批量回填任务幂等入队。
func EnqueueMetadataBackfill(ctx context.Context, tasks MetadataTaskEnqueuer, spaceID string, libraryID int64) (*models.Task, error) {
	if tasks == nil {
		return nil, errors.New("任务中心未启用")
	}
	payload := metadataTaskPayload{SpaceID: normalizeSpaceID(spaceID), LibraryID: libraryID}
	return tasks.Enqueue(ctx, metadataTaskInput(TaskTypeMetadataBackfill, payload))
}

func metadataTaskInput(taskType string, payload metadataTaskPayload) tasksvc.EnqueueInput {
	data, _ := json.Marshal(payload)
	key := fmt.Sprintf("metadata-backfill:%s:%d", payload.SpaceID, payload.LibraryID)
	resourceType, resourceID := "library", strconv.FormatInt(payload.LibraryID, 10)
	if taskType == TaskTypeMetadataParse {
		key = fmt.Sprintf("metadata-parse:%s:%d:%d:%d", payload.SpaceID, payload.MediaID, payload.FileSize, payload.ModifiedAtUnixNano)
		resourceType, resourceID = "media", strconv.FormatInt(payload.MediaID, 10)
	}
	return tasksvc.EnqueueInput{
		Scope: models.TaskScopeSpace, SpaceID: payload.SpaceID, Type: taskType,
		Priority: 0, MaxAttempts: 3, IdempotencyKey: key, PayloadJSON: string(data),
		ResourceType: resourceType, ResourceID: resourceID,
	}
}

// RegisterMetadataWorkers 注册单文件解析与批量回填 worker。
func RegisterMetadataWorkers(registry *tasksvc.WorkerRegistry, tasks *tasksvc.Service, service *Service) error {
	if registry == nil || tasks == nil || service == nil {
		return errors.New("元数据 worker 依赖不能为空")
	}
	if err := registry.Register(TaskTypeMetadataParse, tasksvc.DefaultConcurrency(TaskTypeMetadataParse), metadataParseHandler(service)); err != nil {
		return err
	}
	return registry.Register(TaskTypeMetadataBackfill, tasksvc.DefaultConcurrency(TaskTypeMetadataBackfill), metadataBackfillHandler(service, registry))
}

func metadataParseHandler(service *Service) tasksvc.Handler {
	return func(ctx context.Context, task models.Task) error {
		payload, err := parseMetadataTask(task, true)
		if err != nil {
			return err
		}
		_, err = service.parseAndStoreMetadataForFingerprint(ctx, payload.SpaceID, payload.MediaID, metadataTaskFingerprint(payload))
		return err
	}
}

func metadataBackfillHandler(service *Service, registry *tasksvc.WorkerRegistry) tasksvc.Handler {
	return func(ctx context.Context, task models.Task) error {
		payload, err := parseMetadataTask(task, false)
		if err != nil {
			return err
		}
		lastID := metadataCheckpointID(task.Checkpoint)
		return service.BackfillMediaMetadata(ctx, payload.SpaceID, payload.LibraryID, lastID, func(done, total int, mediaID int64) error {
			progress := 100
			if total > 0 {
				progress = done * 100 / total
			}
			return registry.UpdateProgress(ctx, task.ID, tasksvc.ProgressInput{Progress: progress, Checkpoint: fmt.Sprintf("media:%d", mediaID)})
		})
	}
}

func parseMetadataTask(task models.Task, requireMedia bool) (metadataTaskPayload, error) {
	if task.Scope != models.TaskScopeSpace || task.SpaceID == nil {
		return metadataTaskPayload{}, errors.New("元数据任务必须归属 Space")
	}
	var payload metadataTaskPayload
	if err := json.Unmarshal([]byte(task.PayloadJSON), &payload); err != nil {
		return payload, fmt.Errorf("解析元数据任务参数失败: %w", err)
	}
	payload.SpaceID = strings.TrimSpace(payload.SpaceID)
	if payload.SpaceID == "" || payload.SpaceID != strings.TrimSpace(*task.SpaceID) {
		return payload, errors.New("元数据任务 Space 与参数不一致")
	}
	if requireMedia && payload.MediaID <= 0 {
		return payload, errors.New("元数据解析任务缺少媒体 ID")
	}
	return payload, nil
}

func metadataTaskFingerprint(payload metadataTaskPayload) *metadataFileFingerprint {
	if payload.ModifiedAtUnixNano == 0 {
		return nil
	}
	return &metadataFileFingerprint{FileSize: payload.FileSize, ModifiedAtUnixNano: payload.ModifiedAtUnixNano}
}

func metadataCheckpointID(checkpoint string) int64 {
	raw := strings.TrimPrefix(strings.TrimSpace(checkpoint), "media:")
	value, _ := strconv.ParseInt(raw, 10, 64)
	return value
}
