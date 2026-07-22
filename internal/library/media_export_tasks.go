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

// 导出任务类型常量（FR2-038、FR2-039）。
const (
	TaskTypeImageExport = "media.image.export"
	TaskTypeVideoClip   = "media.video.clip"
)

// exportTaskPayload 是任务 payload 的统一结构。
type exportTaskPayload struct {
	SpaceID string             `json:"space_id"`
	MediaID int64              `json:"media_id"`
	Image   *ImageExportParams `json:"image,omitempty"`
	Clip    *VideoClipParams   `json:"clip,omitempty"`
}

// ExportTaskResult 是任务完成时回写到 Checkpoint 的结果摘要（轻量，不带产物路径以免过宽）。
type ExportTaskResult struct {
	OutputPath  string  `json:"output_path,omitempty"`
	Filename    string  `json:"filename,omitempty"`
	Format      string  `json:"format,omitempty"`
	SizeBytes   int64   `json:"size_bytes,omitempty"`
	DurationSec float64 `json:"duration_sec,omitempty"`
}

// exportTaskEnqueuer 暴露给 API 层用于入队的最小接口。
type exportTaskEnqueuer interface {
	Enqueue(ctx context.Context, input tasksvc.EnqueueInput) (*models.Task, error)
}

// ExportTaskRunner 描述任务执行所需的运行时依赖。
type ExportTaskRunner struct {
	dataDir string
	library *Service
	tasks   *tasksvc.Service
}

// NewExportTaskRunner 创建导出任务执行器。
func NewExportTaskRunner(dataDir string, svc *Service, tasks *tasksvc.Service) *ExportTaskRunner {
	return &ExportTaskRunner{dataDir: dataDir, library: svc, tasks: tasks}
}

// RegisterExportWorkers 注册图片导出与视频粗剪 worker。
func (r *ExportTaskRunner) RegisterExportWorkers(registry *tasksvc.WorkerRegistry) error {
	if registry == nil {
		return errors.New("worker 注册表为空")
	}
	if err := registry.Register(TaskTypeImageExport, tasksvc.DefaultConcurrency(TaskTypeImageExport), r.handleImageExport); err != nil {
		return err
	}
	return registry.Register(TaskTypeVideoClip, tasksvc.DefaultConcurrency(TaskTypeVideoClip), r.handleVideoClip)
}

// EnqueueImageExport 入队图片导出任务。
func EnqueueImageExport(ctx context.Context, enq exportTaskEnqueuer, spaceID string, mediaID int64, params ImageExportParams) (*models.Task, error) {
	if enq == nil {
		return nil, errors.New("任务中心未启用")
	}
	spaceID = normalizeSpaceID(spaceID)
	payload := exportTaskPayload{SpaceID: spaceID, MediaID: mediaID, Image: &params}
	buf, _ := json.Marshal(payload)
	return enq.Enqueue(ctx, tasksvc.EnqueueInput{
		Scope:          models.TaskScopeSpace,
		SpaceID:        spaceID,
		Type:           TaskTypeImageExport,
		Priority:       0,
		MaxAttempts:    2,
		IdempotencyKey: ImageExportFingerprint(spaceID, mediaID, params),
		PayloadJSON:    string(buf),
		ResourceType:   "media",
		ResourceID:     strconv.FormatInt(mediaID, 10),
	})
}

// EnqueueVideoClip 入队视频粗剪任务。
func EnqueueVideoClip(ctx context.Context, enq exportTaskEnqueuer, spaceID string, mediaID int64, params VideoClipParams) (*models.Task, error) {
	if enq == nil {
		return nil, errors.New("任务中心未启用")
	}
	spaceID = normalizeSpaceID(spaceID)
	payload := exportTaskPayload{SpaceID: spaceID, MediaID: mediaID, Clip: &params}
	buf, _ := json.Marshal(payload)
	return enq.Enqueue(ctx, tasksvc.EnqueueInput{
		Scope:          models.TaskScopeSpace,
		SpaceID:        spaceID,
		Type:           TaskTypeVideoClip,
		Priority:       0,
		MaxAttempts:    2,
		IdempotencyKey: VideoClipFingerprint(spaceID, mediaID, params),
		PayloadJSON:    string(buf),
		ResourceType:   "media",
		ResourceID:     strconv.FormatInt(mediaID, 10),
	})
}

func (r *ExportTaskRunner) handleImageExport(ctx context.Context, task models.Task) error {
	payload, err := parseExportTask(task, true)
	if err != nil {
		return err
	}
	mf, err := r.library.GetMediaFileByIDInSpace(payload.SpaceID, payload.MediaID)
	if err != nil {
		return fmt.Errorf("查询媒体失败: %w", err)
	}
	res, err := ExportImage(ctx, r.dataDir, payload.SpaceID, payload.MediaID, task.ID, mf.FilePath, *payload.Image)
	if err != nil {
		return err
	}
	result := ExportTaskResult{
		OutputPath: res.OutputPath,
		Filename:   res.OutputFilename,
		Format:     res.Format,
		SizeBytes:  res.SizeBytes,
	}
	buf, _ := json.Marshal(result)
	if r.tasks == nil {
		return nil
	}
	_ = r.tasks.UpdateProgress(ctx, task.ID, tasksvc.ProgressInput{Progress: 100, Checkpoint: string(buf)})
	return nil
}

func (r *ExportTaskRunner) handleVideoClip(ctx context.Context, task models.Task) error {
	payload, err := parseExportTask(task, false)
	if err != nil {
		return err
	}
	mf, err := r.library.GetMediaFileByIDInSpace(payload.SpaceID, payload.MediaID)
	if err != nil {
		return fmt.Errorf("查询媒体失败: %w", err)
	}
	res, err := ExportVideoClip(ctx, r.dataDir, payload.SpaceID, payload.MediaID, task.ID, mf.FilePath, *payload.Clip, mf.Duration)
	if err != nil {
		return err
	}
	result := ExportTaskResult{
		OutputPath:  res.OutputPath,
		Filename:    res.OutputFilename,
		Format:      res.Format,
		SizeBytes:   res.SizeBytes,
		DurationSec: res.DurationSec,
	}
	buf, _ := json.Marshal(result)
	if r.tasks == nil {
		return nil
	}
	_ = r.tasks.UpdateProgress(ctx, task.ID, tasksvc.ProgressInput{Progress: 100, Checkpoint: string(buf)})
	return nil
}

func parseExportTask(task models.Task, wantImage bool) (exportTaskPayload, error) {
	if task.Scope != models.TaskScopeSpace || task.SpaceID == nil {
		return exportTaskPayload{}, errors.New("导出任务必须归属 Space")
	}
	var payload exportTaskPayload
	if err := json.Unmarshal([]byte(task.PayloadJSON), &payload); err != nil {
		return payload, fmt.Errorf("解析导出任务失败: %w", err)
	}
	payload.SpaceID = strings.TrimSpace(payload.SpaceID)
	if payload.SpaceID == "" || payload.SpaceID != strings.TrimSpace(*task.SpaceID) {
		return payload, errors.New("导出任务 Space 与参数不一致")
	}
	if payload.MediaID <= 0 {
		return payload, errors.New("导出任务缺少媒体 ID")
	}
	if wantImage {
		if payload.Image == nil {
			return payload, errors.New("图片导出任务缺少参数")
		}
		if err := ValidateImageParams(*payload.Image); err != nil {
			return payload, err
		}
	} else {
		if payload.Clip == nil {
			return payload, errors.New("视频粗剪任务缺少参数")
		}
		if err := ValidateVideoClipParams(*payload.Clip, 0, 0); err != nil {
			return payload, err
		}
	}
	return payload, nil
}
