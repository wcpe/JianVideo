package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
)

const (
	// HighConfidenceThreshold 是自动推断可替代原始文件名展示的最低置信度。
	HighConfidenceThreshold = 0.75
	// InferenceRuleVersion 标记当前本地规则版本，供后续 backfill 判断来源。
	InferenceRuleVersion = "fr2-031-v1"
	// InferenceSourceRule 表示结果来自本地离线规则。
	InferenceSourceRule = "offline_rule"
	// InferenceSourceManual 表示结果来自用户人工纠正。
	InferenceSourceManual = "manual"
)

var (
	seriesENPattern = regexp.MustCompile(`(?i)(.*?)\bS(\d{1,2})E(\d{1,3})\b(.*)`)
	seriesCNPattern = regexp.MustCompile(`(?i)(.*?)第\s*(\d{1,2})\s*季\s*第\s*(\d{1,3})\s*集(.*)`)
	yearPattern     = regexp.MustCompile(`(?i)(.*?)(19\d{2}|20\d{2})(.*)`)
	qualityTokens   = map[string]bool{
		"480p": true, "720p": true, "1080p": true, "2160p": true, "4k": true, "8k": true,
		"bluray": true, "bdrip": true, "webrip": true, "web-dl": true, "hdrip": true,
		"hdtv": true, "x264": true, "x265": true, "h264": true, "h265": true, "aac": true,
	}
)

// InferenceInput 是离线推断纯函数输入。
type InferenceInput struct {
	FilePath    string
	FileName    string
	LibraryKind string
}

// InferenceResult 是离线规则解析出的候选结果。
type InferenceResult struct {
	Kind         string
	Title        string
	Year         int
	Season       int
	Episode      int
	EpisodeTitle string
	Confidence   float64
	Source       string
	RuleVersion  string
}

// InferenceManualInput 是人工纠正请求。
type InferenceManualInput struct {
	Kind         string `json:"kind"`
	Title        string `json:"title"`
	Year         int    `json:"year"`
	Season       int    `json:"season"`
	Episode      int    `json:"episode"`
	EpisodeTitle string `json:"episode_title"`
}

// InferenceConfig 控制本地影视推断是否运行。
type InferenceConfig struct {
	Enabled           bool
	DisabledLibraries map[int64]bool
}

// InferenceConfigProvider 按 Space 与库读取推断开关。
type InferenceConfigProvider func(spaceID string, libraryID int64) InferenceConfig

// WithInferenceConfigProvider 注入影视推断配置读取函数。
func (s *Service) WithInferenceConfigProvider(provider InferenceConfigProvider) *Service {
	s.inferenceConfig = provider
	return s
}

// InferMediaTitle 从文件名和目录本地推断影视信息，不访问网络。
func InferMediaTitle(input InferenceInput) InferenceResult {
	kind, err := normalizeLibraryKind(input.LibraryKind)
	if err != nil {
		kind = models.LibraryKindMixed
	}
	result := InferenceResult{Kind: kind, Source: InferenceSourceRule, RuleVersion: InferenceRuleVersion}
	if kind == models.LibraryKindHomeVideo {
		return result
	}
	name := strings.TrimSpace(input.FileName)
	if name == "" {
		name = filepath.Base(input.FilePath)
	}
	base := stripExtension(name)
	if parsed, ok := parseSeries(base, kind); ok {
		if parsed.Title == "" {
			parsed.Title = parentDirectoryTitle(input.FilePath)
		}
		return parsed
	}
	if parsed, ok := parseMovie(base, input.FilePath, kind); ok {
		return parsed
	}
	result.Title = cleanupTitle(base)
	if result.Title != "" {
		result.Confidence = 0.4
	}
	return result
}

// ResolveInferenceDisplayName 按 FR2-031 展示优先级返回最终显示名。
func ResolveInferenceDisplayName(mf models.MediaFile, inf *models.MediaInference) string {
	if inf != nil && inf.Manual && strings.TrimSpace(inf.Title) != "" {
		return strings.TrimSpace(inf.Title)
	}
	if name := strings.TrimSpace(mf.DisplayName); name != "" {
		return name
	}
	if inf != nil && !inf.Manual && inf.Confidence >= HighConfidenceThreshold && strings.TrimSpace(inf.Title) != "" {
		return strings.TrimSpace(inf.Title)
	}
	return mf.FileName
}

// GetMediaInferenceInSpace 查询指定媒体的推断结果。
func (s *Service) GetMediaInferenceInSpace(spaceID string, mediaID int64) (*models.MediaInference, error) {
	var inf models.MediaInference
	err := s.db.Where("space_id = ? AND media_id = ?", normalizeSpaceID(spaceID), mediaID).First(&inf).Error
	return &inf, err
}

// InferAndStoreMediaInSpace 对单个媒体执行自动推断；人工值和关闭开关不会被覆盖。
func (s *Service) InferAndStoreMediaInSpace(spaceID string, mediaID int64) (*models.MediaInference, error) {
	inf, _, err := s.inferAndStoreMediaInSpace(spaceID, mediaID)
	return inf, err
}

func (s *Service) inferAndStoreMediaInSpace(spaceID string, mediaID int64) (*models.MediaInference, bool, error) {
	if !s.db.Migrator().HasTable(&models.MediaInference{}) {
		return nil, false, nil
	}
	mf, lp, err := s.mediaWithLibrary(spaceID, mediaID)
	if err != nil {
		return nil, false, err
	}
	if !s.inferenceEnabled(lp.SpaceID, lp.ID) || lp.LibraryKind == models.LibraryKindHomeVideo {
		return nil, false, nil
	}
	existing, err := s.GetMediaInferenceInSpace(lp.SpaceID, mediaID)
	if err == nil && existing.Manual {
		return existing, false, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}
	result := InferMediaTitle(InferenceInput{FilePath: mf.FilePath, FileName: mf.FileName, LibraryKind: lp.LibraryKind})
	if strings.TrimSpace(result.Title) == "" {
		return nil, false, nil
	}
	inf := inferenceFromResult(mediaID, lp.SpaceID, result)
	before := existing
	if errors.Is(err, gorm.ErrRecordNotFound) {
		before = nil
	}
	if s.beforeAutoInferenceSave != nil {
		s.beforeAutoInferenceSave()
	}
	changed, err := s.saveAutoInference(&inf, before)
	if err != nil {
		return nil, false, err
	}
	stored, err := s.GetMediaInferenceInSpace(lp.SpaceID, mediaID)
	return stored, changed && err == nil, err
}

// UpsertManualInferenceInSpace 保存人工纠正结果，并与审计事件同事务提交。
func (s *Service) UpsertManualInferenceInSpace(spaceID string, mediaID int64, input InferenceManualInput) (*models.MediaInference, error) {
	if !s.db.Migrator().HasTable(&models.MediaInference{}) {
		return nil, fmt.Errorf("媒体推断表未初始化")
	}
	mf, lp, err := s.mediaWithLibrary(spaceID, mediaID)
	if err != nil {
		return nil, err
	}
	kind, err := normalizeManualKind(input.Kind, lp.LibraryKind)
	if err != nil {
		return nil, err
	}
	inf := manualInferenceFromInput(mf, lp, kind, input)
	before := s.inferenceBeforeAudit(lp.SpaceID, mf.ID)
	if err := s.saveInference(&inf, before); err != nil {
		return nil, err
	}
	return s.GetMediaInferenceInSpace(lp.SpaceID, mf.ID)
}

func normalizeManualKind(raw, fallback string) (string, error) {
	kind := strings.TrimSpace(raw)
	if kind == "" {
		kind = fallback
	}
	return normalizeLibraryKind(kind)
}

func manualInferenceFromInput(mf *models.MediaFile, lp *models.LibraryPath, kind string, input InferenceManualInput) models.MediaInference {
	now := time.Now()
	return models.MediaInference{
		MediaID:      mf.ID,
		SpaceID:      lp.SpaceID,
		Kind:         kind,
		Title:        strings.TrimSpace(input.Title),
		Year:         input.Year,
		Season:       input.Season,
		Episode:      input.Episode,
		EpisodeTitle: strings.TrimSpace(input.EpisodeTitle),
		Confidence:   1,
		Source:       InferenceSourceManual,
		RuleVersion:  InferenceRuleVersion,
		Manual:       true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func (s *Service) inferenceBeforeAudit(spaceID string, mediaID int64) *models.MediaInference {
	if old, err := s.GetMediaInferenceInSpace(spaceID, mediaID); err == nil {
		return old
	}
	return nil
}

func (s *Service) saveInference(inf *models.MediaInference, before *models.MediaInference) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := upsertInferenceTx(tx, inf); err != nil {
			return err
		}
		return s.recordAuditTx(tx, audit.EventInput{
			Scope:        audit.ScopeSpace,
			SpaceID:      inf.SpaceID,
			ActorType:    audit.ActorSystem,
			Action:       "media.inference.updated",
			ResourceType: "media",
			ResourceID:   fmt.Sprintf("%d", inf.MediaID),
			Before:       inferenceAuditPayload(before),
			After:        inferenceAuditPayload(inf),
		})
	})
}

func (s *Service) saveAutoInference(inf *models.MediaInference, before *models.MediaInference) (bool, error) {
	changed := false
	err := s.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(autoInferenceConflict()).Create(inf)
		if result.Error != nil || result.RowsAffected == 0 {
			return result.Error
		}
		changed = true
		return s.recordAuditTx(tx, audit.EventInput{
			Scope:        audit.ScopeSpace,
			SpaceID:      inf.SpaceID,
			ActorType:    audit.ActorSystem,
			Action:       "media.inference.updated",
			ResourceType: "media",
			ResourceID:   fmt.Sprintf("%d", inf.MediaID),
			Before:       inferenceAuditPayload(before),
			After:        inferenceAuditPayload(inf),
		})
	})
	return changed, err
}

func autoInferenceConflict() clause.OnConflict {
	return clause.OnConflict{
		Columns:   []clause.Column{{Name: "media_id"}},
		DoUpdates: clause.AssignmentColumns(inferenceUpsertColumns()),
		Where: clause.Where{Exprs: []clause.Expression{
			clause.Eq{Column: clause.Column{Table: "media_inferences", Name: "manual"}, Value: false},
		}},
	}
}

func upsertInferenceTx(db *gorm.DB, inf *models.MediaInference) error {
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "media_id"}},
		DoUpdates: clause.AssignmentColumns(inferenceUpsertColumns()),
	}).Create(inf).Error
}

func inferenceUpsertColumns() []string {
	return []string{
		"space_id", "kind", "title", "year", "season", "episode", "episode_title",
		"confidence", "source", "rule_version", "manual", "updated_at",
	}
}

// InferenceBackfillProgress 是批量推断的逐媒体进度回调。
type InferenceBackfillProgress func(completed, total int, mediaID int64) error

// BackfillMediaInferencesInSpace 批量重跑自动推断，跳过人工值和关闭的库。
func (s *Service) BackfillMediaInferencesInSpace(spaceID string, libraryID int64) (int, error) {
	return s.BackfillMediaInferencesWithProgressInSpace(context.Background(), spaceID, libraryID, nil)
}

// BackfillMediaInferencesWithProgressInSpace 支持逐媒体进度和协作取消的批量推断。
func (s *Service) BackfillMediaInferencesWithProgressInSpace(
	ctx context.Context,
	spaceID string,
	libraryID int64,
	onProgress InferenceBackfillProgress,
) (int, error) {
	files, err := s.listInferenceBackfillFiles(ctx, spaceID, libraryID, false)
	if err != nil {
		return 0, err
	}
	return s.backfillInferenceFiles(ctx, files, onProgress)
}

// BackfillMissingMediaInferencesWithProgressInSpace 只为尚无推断记录的媒体补齐结果。
func (s *Service) BackfillMissingMediaInferencesWithProgressInSpace(
	ctx context.Context,
	spaceID string,
	libraryID int64,
	onProgress InferenceBackfillProgress,
) (int, error) {
	files, err := s.listInferenceBackfillFiles(ctx, spaceID, libraryID, true)
	if err != nil {
		return 0, err
	}
	return s.backfillInferenceFiles(ctx, files, onProgress)
}

func (s *Service) backfillInferenceFiles(ctx context.Context, files []models.MediaFile, onProgress InferenceBackfillProgress) (int, error) {
	updated := 0
	for i := range files {
		if err := ctx.Err(); err != nil {
			return updated, err
		}
		_, stored, err := s.inferAndStoreMediaInSpace(files[i].SpaceID, files[i].ID)
		if err != nil {
			return updated, err
		}
		if stored {
			updated++
		}
		if onProgress != nil {
			if err := onProgress(i+1, len(files), files[i].ID); err != nil {
				return updated, err
			}
		}
	}
	return updated, nil
}

func (s *Service) listInferenceBackfillFiles(ctx context.Context, spaceID string, libraryID int64, missingOnly bool) ([]models.MediaFile, error) {
	if !s.db.Migrator().HasTable(&models.MediaInference{}) {
		return nil, nil
	}
	query := s.db.WithContext(ctx).Where("space_id = ? AND deleted_at IS NULL", normalizeSpaceID(spaceID))
	if libraryID > 0 {
		query = query.Where("library_id = ?", libraryID)
	}
	if missingOnly {
		inferred := s.db.Model(&models.MediaInference{}).Select("media_id").Where("space_id = ?", normalizeSpaceID(spaceID))
		query = query.Where("id NOT IN (?)", inferred)
	}
	var files []models.MediaFile
	if err := query.Order("id ASC").Find(&files).Error; err != nil {
		return nil, err
	}
	return files, nil
}

func parentDirectoryTitle(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	return cleanupTitle(filepath.Base(filepath.Dir(path)))
}

func parseSeries(base, kind string) (InferenceResult, bool) {
	if matches := seriesENPattern.FindStringSubmatch(base); len(matches) == 5 {
		return seriesResult(kind, matches[1], matches[2], matches[3], matches[4]), true
	}
	if matches := seriesCNPattern.FindStringSubmatch(base); len(matches) == 5 {
		return seriesResult(kind, matches[1], matches[2], matches[3], matches[4]), true
	}
	return InferenceResult{}, false
}

func seriesResult(kind, rawTitle, rawSeason, rawEpisode, rawEpisodeTitle string) InferenceResult {
	season, _ := strconv.Atoi(rawSeason)
	episode, _ := strconv.Atoi(rawEpisode)
	return InferenceResult{
		Kind:         kind,
		Title:        cleanupTitle(rawTitle),
		Season:       season,
		Episode:      episode,
		EpisodeTitle: cleanupTitle(rawEpisodeTitle),
		Confidence:   0.95,
		Source:       InferenceSourceRule,
		RuleVersion:  InferenceRuleVersion,
	}
}

func parseMovie(base, path, kind string) (InferenceResult, bool) {
	if matches := yearPattern.FindStringSubmatch(base); len(matches) == 4 {
		year, _ := strconv.Atoi(matches[2])
		title := cleanupTitle(matches[1])
		if title != "" {
			return InferenceResult{Kind: kind, Title: title, Year: year, Confidence: movieConfidence(kind), Source: InferenceSourceRule, RuleVersion: InferenceRuleVersion}, true
		}
	}
	if kind == models.LibraryKindMovie {
		dirTitle := cleanupTitle(filepath.Base(filepath.Dir(path)))
		if dirTitle != "" && !qualityTokens[strings.ToLower(dirTitle)] {
			return InferenceResult{Kind: kind, Title: dirTitle, Confidence: 0.8, Source: InferenceSourceRule, RuleVersion: InferenceRuleVersion}, true
		}
	}
	return InferenceResult{}, false
}

func movieConfidence(kind string) float64 {
	if kind == models.LibraryKindMixed {
		return 0.8
	}
	return 0.9
}

func cleanupTitle(raw string) string {
	cleaned := strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(stripBrackets(raw))
	parts := strings.Fields(cleaned)
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if qualityTokens[strings.ToLower(part)] {
			continue
		}
		kept = append(kept, part)
	}
	return strings.TrimSpace(strings.Join(kept, " "))
}

func stripExtension(name string) string {
	ext := filepath.Ext(name)
	if ext == "" {
		return name
	}
	return strings.TrimSuffix(name, ext)
}

func stripBrackets(raw string) string {
	replacer := strings.NewReplacer("[", " ", "]", " ", "(", " ", ")", " ", "【", " ", "】", " ")
	return replacer.Replace(raw)
}

func inferenceFromResult(mediaID int64, spaceID string, result InferenceResult) models.MediaInference {
	now := time.Now()
	return models.MediaInference{
		MediaID:      mediaID,
		SpaceID:      normalizeSpaceID(spaceID),
		Kind:         result.Kind,
		Title:        result.Title,
		Year:         result.Year,
		Season:       result.Season,
		Episode:      result.Episode,
		EpisodeTitle: result.EpisodeTitle,
		Confidence:   result.Confidence,
		Source:       result.Source,
		RuleVersion:  result.RuleVersion,
		Manual:       false,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func (s *Service) mediaWithLibrary(spaceID string, mediaID int64) (*models.MediaFile, *models.LibraryPath, error) {
	var mf models.MediaFile
	if err := s.db.Where("space_id = ? AND id = ? AND deleted_at IS NULL", normalizeSpaceID(spaceID), mediaID).First(&mf).Error; err != nil {
		return nil, nil, err
	}
	lp, err := s.GetLibraryPathByIDInSpace(mf.SpaceID, mf.LibraryID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &mf, &models.LibraryPath{ID: mf.LibraryID, SpaceID: mf.SpaceID, LibraryKind: models.LibraryKindMixed}, nil
		}
		return nil, nil, err
	}
	return &mf, lp, nil
}

func (s *Service) inferenceEnabled(spaceID string, libraryID int64) bool {
	if s.inferenceConfig == nil {
		return true
	}
	cfg := s.inferenceConfig(spaceID, libraryID)
	if !cfg.Enabled {
		return false
	}
	return !cfg.DisabledLibraries[libraryID]
}

func inferenceAuditPayload(inf *models.MediaInference) map[string]any {
	if inf == nil {
		return nil
	}
	return map[string]any{
		"media_id":      inf.MediaID,
		"kind":          inf.Kind,
		"title":         inf.Title,
		"year":          inf.Year,
		"season":        inf.Season,
		"episode":       inf.Episode,
		"episode_title": inf.EpisodeTitle,
		"confidence":    inf.Confidence,
		"source":        inf.Source,
		"rule_version":  inf.RuleVersion,
		"manual":        inf.Manual,
	}
}

// ParseDisabledInferenceLibraries 解析每库关闭配置，供 main 注入 provider 时复用。
func ParseDisabledInferenceLibraries(raw string) map[int64]bool {
	var ids []int64
	result := map[int64]bool{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &ids); err != nil {
		return result
	}
	for _, id := range ids {
		if id > 0 {
			result[id] = true
		}
	}
	return result
}
