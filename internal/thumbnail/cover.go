package thumbnail

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/storage"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

const (
	// TaskTypeCoverGenerate 表示缺失封面候选的幂等生成任务。
	TaskTypeCoverGenerate = "cover.generate"
	// TaskTypeCoverRefresh 表示显式重新抽帧并替换旧候选的任务。
	TaskTypeCoverRefresh = "cover.refresh"

	// CoverSourceVideoFrame 表示从视频帧生成封面。
	CoverSourceVideoFrame = "video_frame"
	// CoverSourceImage 表示从图片生成封面。
	CoverSourceImage = "image"

	coverSize        = 640
	coverMaxAttempts = 3
)

// CoverGenerator 执行单个媒体指定时间点的封面文件生成。
type CoverGenerator func(context.Context, models.MediaFile, float64, string) error

// CoverTaskResult 表示封面生成任务入队结果。
type CoverTaskResult struct {
	TaskID int64  `json:"task_id"`
	Status string `json:"status"`
}

// CoverList 表示媒体当前封面及候选列表。
type CoverList struct {
	Cover      *models.MediaCover      `json:"cover,omitempty"`
	Candidates []models.CoverCandidate `json:"candidates"`
}

type coverPayload struct {
	SpaceID string `json:"space_id"`
	MediaID int64  `json:"media_id"`
	Refresh bool   `json:"refresh"`
}

type coverSpec struct {
	Source      string
	Timestamp   float64
	Fingerprint string
	Score       float64
	Path        string
}

// WithAudit 配置封面生成与人工选择审计记录器。
func (s *Service) WithAudit(recorder audit.Recorder) *Service {
	s.audit = recorder
	return s
}

// SetCoverGeneratorForTest 替换真实封面生成器，仅供测试使用。
func (s *Service) SetCoverGeneratorForTest(generator CoverGenerator) {
	if generator != nil {
		s.coverGenerator = generator
	}
}

// GenerateCovers 将封面生成或刷新写入通用任务队列。
func (s *Service) GenerateCovers(ctx context.Context, spaceID string, mediaID int64, refresh bool) (CoverTaskResult, error) {
	media, err := s.library.GetMediaFileByIDInSpace(spaceID, mediaID)
	if err != nil {
		return CoverTaskResult{}, err
	}
	taskType := TaskTypeCoverGenerate
	if refresh {
		taskType = TaskTypeCoverRefresh
	}
	payload := coverPayload{SpaceID: media.SpaceID, MediaID: media.ID, Refresh: refresh}
	data, err := json.Marshal(payload)
	if err != nil {
		return CoverTaskResult{}, fmt.Errorf("编码封面任务失败: %w", err)
	}
	task, err := s.tasks.Enqueue(ctx, tasksvc.EnqueueInput{
		Scope: models.TaskScopeSpace, SpaceID: media.SpaceID, Type: taskType,
		Priority: 80, MaxAttempts: coverMaxAttempts,
		IdempotencyKey: fmt.Sprintf("cover:%s:%s:%d", taskType, media.SpaceID, media.ID),
		PayloadJSON:    string(data), ResourceType: "media", ResourceID: strconv.FormatInt(media.ID, 10),
	})
	if err != nil {
		return CoverTaskResult{}, err
	}
	return CoverTaskResult{TaskID: task.ID, Status: task.Status}, nil
}

// ListCovers 返回指定 Space 媒体的封面选择与候选。
func (s *Service) ListCovers(ctx context.Context, spaceID string, mediaID int64) (CoverList, error) {
	if _, err := s.library.GetMediaFileByIDInSpace(spaceID, mediaID); err != nil {
		return CoverList{}, err
	}
	cover, err := s.library.GetMediaCoverInSpace(ctx, spaceID, mediaID)
	if err != nil {
		return CoverList{}, err
	}
	candidates, err := s.library.ListCoverCandidatesInSpace(ctx, spaceID, mediaID)
	if err != nil {
		return CoverList{}, err
	}
	return CoverList{Cover: cover, Candidates: candidates}, nil
}

// SelectCover 将当前封面切换为同一 Space、同一媒体下的既有候选。
func (s *Service) SelectCover(ctx context.Context, spaceID string, mediaID, candidateID int64) (*models.MediaCover, error) {
	selected, err := s.library.SelectCoverCandidateInSpace(ctx, spaceID, mediaID, candidateID)
	if err != nil {
		return nil, err
	}
	if err := s.recordCoverAudit(ctx, selected.SpaceID, mediaID, "cover.selected", selected); err != nil {
		return nil, err
	}
	return selected, nil
}

// CurrentCoverPath 返回当前封面缓存路径；缓存缺失时返回空路径供调用方回退普通缩略图。
func (s *Service) CurrentCoverPath(ctx context.Context, spaceID string, mediaID int64) (string, error) {
	cover, err := s.library.GetMediaCoverInSpace(ctx, spaceID, mediaID)
	if err != nil || cover == nil || cover.SelectedAssetID <= 0 || cover.SelectedFingerprint == "" {
		return "", err
	}
	path, err := CoverPathFor(s.dataDir, cover.SpaceID, cover.MediaID, cover.SelectedFingerprint)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return path, nil
}

// CoverCandidatePath 返回只读候选图片路径并校验候选归属。
func (s *Service) CoverCandidatePath(ctx context.Context, spaceID string, mediaID, candidateID int64) (string, error) {
	candidates, err := s.library.ListCoverCandidatesInSpace(ctx, spaceID, mediaID)
	if err != nil {
		return "", err
	}
	for _, candidate := range candidates {
		if candidate.ID != candidateID {
			continue
		}
		path, err := CoverPathFor(s.dataDir, candidate.SpaceID, candidate.MediaID, candidate.Fingerprint)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(path); err != nil {
			return "", err
		}
		return path, nil
	}
	return "", gormRecordNotFound()
}

// CoverPathFor 按 Space、媒体和稳定指纹构造封面缓存路径。
func CoverPathFor(dataDir, spaceID string, mediaID int64, fingerprint string) (string, error) {
	if !safeSegment(strings.TrimSpace(spaceID)) || mediaID <= 0 || !safeFingerprint(fingerprint) {
		return "", errors.New("封面缓存路径参数无效")
	}
	return filepath.Join(filepath.Clean(dataDir), "covers", spaceID, strconv.FormatInt(mediaID, 10), fingerprint+".jpg"), nil
}

// CoverCandidateTimestamps 按 10/30/50/70/90 百分位生成规则化时间点，并对极短视频毫秒级去重。
func CoverCandidateTimestamps(duration float64) []float64 {
	if duration <= 0 {
		return []float64{0}
	}
	result := make([]float64, 0, 5)
	for _, ratio := range []float64{0.1, 0.3, 0.5, 0.7, 0.9} {
		timestamp := math.Round(duration*ratio*1000) / 1000
		if timestamp >= duration {
			timestamp = math.Max(0, math.Floor((duration-0.001)*1000)/1000)
		}
		if timestamp >= 0 && timestamp < duration && !slices.Contains(result, timestamp) {
			result = append(result, timestamp)
		}
	}
	if len(result) == 0 {
		return []float64{0}
	}
	return result
}

func (s *Service) handleCoverGenerate(ctx context.Context, task models.Task) error {
	var payload coverPayload
	if err := decodePayload(task.PayloadJSON, &payload); err != nil {
		return err
	}
	if err := validateTask(task, task.Type, payload.SpaceID, "media", strconv.FormatInt(payload.MediaID, 10)); err != nil {
		return err
	}
	if task.Type != TaskTypeCoverGenerate && task.Type != TaskTypeCoverRefresh {
		return errors.New("封面任务类型无效")
	}
	if payload.Refresh != (task.Type == TaskTypeCoverRefresh) {
		return errors.New("封面任务刷新语义不匹配")
	}
	media, err := s.library.GetMediaFileByIDInSpace(payload.SpaceID, payload.MediaID)
	if err != nil {
		return err
	}
	if err := validateCoverSource(*media); err != nil {
		return err
	}
	specs, err := s.coverSpecs(*media)
	if err != nil {
		return err
	}
	candidates := make([]models.CoverCandidate, 0, len(specs))
	keepPaths := make([]string, 0, len(specs))
	for index, spec := range specs {
		if err := s.generateCoverSpec(ctx, *media, spec, payload.Refresh); err != nil {
			return err
		}
		asset, err := s.cache.RegisterFile(ctx, storage.RegisterInput{
			SpaceID: media.SpaceID, LibraryID: media.LibraryID, MediaID: media.ID,
			Kind: storage.CacheKindCover, Variant: spec.Fingerprint,
			CacheKey: fmt.Sprintf("%s/%d/%s", media.SpaceID, media.ID, spec.Fingerprint), Path: spec.Path,
		})
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		candidates = append(candidates, models.CoverCandidate{
			SpaceID: media.SpaceID, MediaID: media.ID, AssetID: asset.ID, Source: spec.Source,
			TimestampSeconds: spec.Timestamp, Fingerprint: spec.Fingerprint, Score: spec.Score,
			CreatedAt: now, UpdatedAt: now,
		})
		keepPaths = append(keepPaths, spec.Path)
		progress := 10 + (index+1)*75/len(specs)
		if err := s.tasks.UpdateProgress(ctx, task.ID, tasksvc.ProgressInput{Progress: progress, Checkpoint: spec.Fingerprint}); err != nil {
			return err
		}
	}
	cover, saved, err := s.library.SaveGeneratedCoverCandidates(ctx, media.SpaceID, media.ID, candidates, payload.Refresh)
	if err != nil {
		return err
	}
	if payload.Refresh {
		if err := s.cache.DeleteMediaKindAssetsExcept(ctx, media.SpaceID, media.ID, storage.CacheKindCover, keepPaths); err != nil {
			return err
		}
	}
	metadata := map[string]any{"candidate_count": len(saved), "refresh": payload.Refresh, "selected_asset_id": cover.SelectedAssetID}
	if err := s.recordCoverAudit(ctx, media.SpaceID, media.ID, "cover.generated", metadata); err != nil {
		return err
	}
	return s.tasks.UpdateProgress(ctx, task.ID, tasksvc.ProgressInput{Progress: 95, Checkpoint: "封面候选已生成"})
}

func (s *Service) coverSpecs(media models.MediaFile) ([]coverSpec, error) {
	mediaType, ok := s.library.MediaTypeByPathInSpace(media.SpaceID, media.LibraryID, media.FilePath)
	if !ok || mediaType != library.MediaTypeImage && mediaType != library.MediaTypeVideo {
		return nil, errors.New("媒体类型不支持封面生成")
	}
	source := CoverSourceImage
	timestamps := []float64{0}
	if mediaType == library.MediaTypeVideo {
		source = CoverSourceVideoFrame
		timestamps = CoverCandidateTimestamps(media.Duration)
	}
	result := make([]coverSpec, 0, len(timestamps))
	for _, timestamp := range timestamps {
		fingerprint := coverFingerprint(media, source, timestamp)
		path, err := CoverPathFor(s.dataDir, media.SpaceID, media.ID, fingerprint)
		if err != nil {
			return nil, err
		}
		score := 1.0
		if media.Duration > 0 {
			score = 1 - math.Abs(timestamp/media.Duration-0.5)
		}
		result = append(result, coverSpec{Source: source, Timestamp: timestamp, Fingerprint: fingerprint, Score: score, Path: path})
	}
	return result, nil
}

func (s *Service) generateCoverSpec(ctx context.Context, media models.MediaFile, spec coverSpec, refresh bool) error {
	if refresh {
		if err := os.Remove(spec.Path); err != nil && !os.IsNotExist(err) {
			return err
		}
	} else if _, err := os.Stat(spec.Path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return s.coverGenerator(ctx, media, spec.Timestamp, spec.Path)
}

func (s *Service) generateCoverFile(ctx context.Context, media models.MediaFile, timestamp float64, outputPath string) error {
	mediaType, ok := s.library.MediaTypeByPathInSpace(media.SpaceID, media.LibraryID, media.FilePath)
	if !ok {
		return errors.New("媒体类型不支持封面生成")
	}
	if mediaType == library.MediaTypeImage {
		return library.GenerateThumbnailFile(ctx, media.FilePath, mediaType, coverSize, outputPath)
	}
	return library.GenerateVideoFrameFile(ctx, media.FilePath, timestamp, coverSize, outputPath)
}

func validateCoverSource(media models.MediaFile) error {
	if media.FileState == models.MediaFileStateMissing {
		return errors.New("源媒体已失效，不能刷新封面")
	}
	if strings.HasPrefix(strings.ToLower(media.FilePath), "smb://") {
		return errors.New("当前封面生成仅支持本地媒体")
	}
	info, err := os.Stat(media.FilePath)
	if err != nil || info.IsDir() {
		return errors.New("源媒体不可访问，不能刷新封面")
	}
	return nil
}

func coverFingerprint(media models.MediaFile, source string, timestamp float64) string {
	snapshot := fmt.Sprintf("%d|%s|%d|%d|%s|%.3f", media.ID, source, media.FileSize, media.ModifiedAt.UnixNano(), media.ContentHash, timestamp)
	digest := sha256.Sum256([]byte(snapshot))
	return hex.EncodeToString(digest[:16])
}

func safeFingerprint(value string) bool {
	if len(value) != 32 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func (s *Service) recordCoverAudit(ctx context.Context, spaceID string, mediaID int64, action string, metadata any) error {
	if s.audit == nil {
		return nil
	}
	return s.audit.Record(ctx, audit.EventInput{
		Scope: audit.ScopeSpace, SpaceID: spaceID, ActorType: audit.ActorSystem,
		Action: action, ResourceType: "media", ResourceID: strconv.FormatInt(mediaID, 10), Metadata: metadata,
	})
}

func gormRecordNotFound() error {
	return errors.New("封面候选不存在")
}
