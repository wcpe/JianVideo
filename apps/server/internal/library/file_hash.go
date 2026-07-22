package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wcpe/JianVideo/internal/db/models"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

const (
	// ContentHashAlgoSHA256 是 FR2-061 内容哈希的唯一算法标识。
	ContentHashAlgoSHA256 = "sha256"
	// TaskTypeFileHashBackfill 是通用任务中心中的内容哈希回填任务类型。
	TaskTypeFileHashBackfill = "library.file_hash_backfill"
)

const (
	contentHashBackfillBatchSize = 200
	exactDuplicateGroupLimit     = 100
)

// FileContentHashResult 表示单文件内容哈希计算结果。
type FileContentHashResult struct {
	Hash       string
	Algo       string
	Size       int64
	ModifiedAt time.Time
}

// ContentHashBackfillResult 表示一次内容哈希回填的汇总。
type ContentHashBackfillResult struct {
	Total    int `json:"total"`
	Computed int `json:"computed"`
	Failed   int `json:"failed"`
}

// ContentHashBackfillProgress 接收已处理数与总数，用于通用任务进度同步。
type ContentHashBackfillProgress func(done, total int) error

type contentHashBackfillState struct {
	result ContentHashBackfillResult
	done   int
	lastID int64
}

// ExactDuplicateGroup 是按内容 SHA-256 分组的精确重复媒体。
type ExactDuplicateGroup struct {
	ContentHash string             `json:"content_hash"`
	FileSize    int64              `json:"file_size"`
	Items       []models.MediaFile `json:"items"`
}

// ComputeFileContentHash 流式计算本地源文件 SHA-256，不把大文件一次性读入内存。
func ComputeFileContentHash(path string) (FileContentHashResult, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return FileContentHashResult{}, errors.New("文件路径不能为空")
	}
	if strings.HasPrefix(path, "smb://") {
		return FileContentHashResult{}, errors.New("暂不支持 SMB 内容哈希计算")
	}
	file, err := os.Open(filepath.FromSlash(path))
	if err != nil {
		return FileContentHashResult{}, fmt.Errorf("打开文件失败: %w", err)
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return FileContentHashResult{}, fmt.Errorf("读取文件信息失败: %w", err)
	}
	h := sha256.New()
	buf := make([]byte, 1024*1024)
	if _, err := io.CopyBuffer(h, file, buf); err != nil {
		return FileContentHashResult{}, fmt.Errorf("读取文件失败: %w", err)
	}
	return FileContentHashResult{
		Hash:       hex.EncodeToString(h.Sum(nil)),
		Algo:       ContentHashAlgoSHA256,
		Size:       info.Size(),
		ModifiedAt: info.ModTime(),
	}, nil
}

// BackfillContentHashes 回填指定 Space 中缺失、过期或算法不一致的内容哈希。
func (s *Service) BackfillContentHashes(ctx context.Context, spaceID string, progress ContentHashBackfillProgress) (ContentHashBackfillResult, error) {
	spaceID = normalizeSpaceID(spaceID)
	total, err := s.countContentHashBackfillCandidates(ctx, spaceID)
	if err != nil {
		return ContentHashBackfillResult{}, err
	}
	state := contentHashBackfillState{result: ContentHashBackfillResult{Total: total}}
	for {
		if err := ctx.Err(); err != nil {
			return state.result, err
		}
		rows, err := s.nextContentHashBackfillBatch(ctx, spaceID, state.lastID)
		if err != nil {
			return state.result, err
		}
		if len(rows) == 0 {
			break
		}
		if err := s.backfillContentHashBatch(ctx, rows, total, &state, progress); err != nil {
			return state.result, err
		}
	}
	if state.result.Failed > 0 && state.result.Computed == 0 {
		return state.result, fmt.Errorf("内容哈希回填全部失败: failed=%d", state.result.Failed)
	}
	if err := s.RefreshContentHashGroups(ctx, spaceID); err != nil {
		return state.result, err
	}
	return state.result, nil
}

func (s *Service) countContentHashBackfillCandidates(ctx context.Context, spaceID string) (int, error) {
	return s.mediaRepo.CountContentHashBackfill(ctx, spaceID)
}

func (s *Service) nextContentHashBackfillBatch(ctx context.Context, spaceID string, lastID int64) ([]models.MediaFile, error) {
	return s.mediaRepo.ListContentHashBackfillBatch(ctx, spaceID, lastID, contentHashBackfillBatchSize)
}

func (s *Service) backfillOneContentHash(ctx context.Context, mf models.MediaFile) error {
	result, err := ComputeFileContentHash(mf.FilePath)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	updates := map[string]any{
		"content_hash":             result.Hash,
		"content_hash_algo":        result.Algo,
		"content_hash_computed_at": now,
		"content_hash_stale":       false,
		"file_size":                result.Size,
		"modified_at":              result.ModifiedAt,
		"file_state":               models.MediaFileStateAvailable,
	}
	return s.mediaRepo.UpdateContentHash(ctx, mf.SpaceID, mf.ID, updates)
}

func (s *Service) backfillContentHashBatch(ctx context.Context, rows []models.MediaFile, total int, state *contentHashBackfillState, progress ContentHashBackfillProgress) error {
	for i := range rows {
		state.lastID = rows[i].ID
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.backfillOneContentHash(ctx, rows[i]); err != nil {
			state.result.Failed++
			log.Printf("[WARN] 内容哈希回填跳过: id=%d, path=%s, err=%v", rows[i].ID, rows[i].FilePath, err)
		} else {
			state.result.Computed++
		}
		state.done++
		if progress != nil {
			if err := progress(state.done, total); err != nil {
				return err
			}
		}
	}
	return nil
}

// HandleContentHashBackfillTask 是 FR2-037 通用任务中心的内容哈希回填 worker 处理器。
func (s *Service) HandleContentHashBackfillTask(ctx context.Context, tasks *tasksvc.Service, task models.Task) error {
	spaceID := models.DefaultSpaceID
	if task.SpaceID != nil {
		spaceID = *task.SpaceID
	}
	_, err := s.BackfillContentHashes(ctx, spaceID, func(done, total int) error {
		if tasks == nil || total <= 0 {
			return nil
		}
		current, err := tasks.Get(ctx, task.ID, tasksvc.Query{Scope: models.TaskScopeSpace, SpaceID: spaceID})
		if err != nil {
			return err
		}
		if current.Status == models.TaskStatusCanceled {
			return context.Canceled
		}
		progress := done * 100 / total
		if progress > 100 {
			progress = 100
		}
		return tasks.UpdateProgress(ctx, task.ID, tasksvc.ProgressInput{
			Progress:   progress,
			Checkpoint: fmt.Sprintf("%d/%d", done, total),
		})
	})
	return err
}

// FindExactDuplicateGroups 查询指定 Space 内未软删且内容哈希有效的精确重复组。
func (s *Service) FindExactDuplicateGroups() ([]ExactDuplicateGroup, error) {
	return s.FindExactDuplicateGroupsInSpace(models.DefaultSpaceID)
}

// FindExactDuplicateGroupsInSpace 查询指定 Space 内未软删且内容哈希有效的精确重复组。
func (s *Service) FindExactDuplicateGroupsInSpace(spaceID string) ([]ExactDuplicateGroup, error) {
	items, err := s.mediaRepo.ListExactDuplicateMedia(spaceID)
	if err != nil {
		return nil, err
	}
	return exactDuplicateGroupsFromItems(items), nil
}

func activeMediaFileStateCondition() string {
	return "(media_files.file_state IS NULL OR media_files.file_state = '' OR media_files.file_state = '" + models.MediaFileStateAvailable + "')"
}

func exactDuplicateGroupsFromItems(items []models.MediaFile) []ExactDuplicateGroup {
	groups := make([]ExactDuplicateGroup, 0)
	seen := make(map[string]int)
	for _, item := range items {
		key := fmt.Sprintf("%d:%s", item.FileSize, item.ContentHash)
		if index, ok := seen[key]; ok {
			groups[index].Items = append(groups[index].Items, item)
			continue
		}
		seen[key] = len(groups)
		groups = append(groups, ExactDuplicateGroup{
			ContentHash: item.ContentHash,
			FileSize:    item.FileSize,
			Items:       []models.MediaFile{item},
		})
	}
	filtered := groups[:0]
	for _, group := range groups {
		if len(group.Items) >= 2 {
			filtered = append(filtered, group)
		}
	}
	return filtered
}

// RefreshContentHashGroups 重建指定 Space 的内容哈希重复组快照。
func (s *Service) RefreshContentHashGroups(ctx context.Context, spaceID string) error {
	return s.mediaRepo.RefreshContentHashGroups(ctx, spaceID)
}

func mediaFileFingerprintChanged(mf *models.MediaFile, size int64, modifiedAt time.Time) bool {
	if mf == nil {
		return false
	}
	return mf.FileSize != size || !mf.ModifiedAt.Equal(modifiedAt)
}

func contentHashShouldBecomeStale(mf *models.MediaFile, size int64, modifiedAt time.Time) bool {
	if mf == nil || mf.ContentHash == "" || mf.ContentHashStale {
		return false
	}
	return mediaFileFingerprintChanged(mf, size, modifiedAt)
}

func addContentHashStaleUpdate(updates map[string]any, mf *models.MediaFile, size int64, modifiedAt time.Time) {
	if contentHashShouldBecomeStale(mf, size, modifiedAt) {
		updates["content_hash_stale"] = true
	}
}
