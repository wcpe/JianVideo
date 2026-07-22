package subtitle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/transcoder"
)

// Extractor 按容器流索引把内嵌文本字幕提取到请求级临时文件。
type Extractor func(context.Context, string, int, string) error

// Service 管理统一字幕与音轨。
type Service struct {
	db        *gorm.DB
	dataDir   string
	audit     auditRecorder
	extractor Extractor
	remove    func(string) error
}

// NewService 创建字幕服务。
func NewService(db *gorm.DB, dataDir string) *Service {
	return &Service{db: db, dataDir: dataDir, extractor: extractEmbeddedSubtitle, remove: os.Remove}
}

// WithAudit 注入审计记录器。
func (s *Service) WithAudit(recorder auditRecorder) *Service {
	s.audit = recorder
	return s
}

// WithExtractor 覆盖内嵌字幕提取器，主要用于测试。
func (s *Service) WithExtractor(extractor Extractor) *Service {
	if extractor != nil {
		s.extractor = extractor
	}
	return s
}

// WithRemoveForTest 覆盖最终文件删除函数，仅供测试失败补偿。
func (s *Service) WithRemoveForTest(remove func(string) error) *Service {
	if remove != nil {
		s.remove = remove
	}
	return s
}

// List 聚合最新内嵌流、本地外挂与用户上传轨道。
func (s *Service) List(ctx context.Context, spaceID string, mediaID int64) (ListResponse, error) {
	media, err := s.media(ctx, spaceID, mediaID)
	if err != nil {
		return ListResponse{}, err
	}
	tracks, err := s.embeddedTracks(ctx, media)
	if err != nil {
		return ListResponse{}, err
	}
	sources := defaultSourceCapabilities()
	tracks, err = s.appendSidecarTracks(media, tracks, sources)
	if err != nil {
		return ListResponse{}, err
	}
	tracks, err = s.appendUploadedTracks(ctx, media, tracks)
	if err != nil {
		return ListResponse{}, err
	}
	sortTracks(tracks)
	return ListResponse{
		Tracks: tracks, Selection: selectionFromDefaults(tracks), Sources: sources, Backend: backendCapabilities(),
	}, nil
}

func (s *Service) media(ctx context.Context, spaceID string, mediaID int64) (models.MediaFile, error) {
	if !safeSegment(spaceID) || mediaID <= 0 {
		return models.MediaFile{}, ErrNotFound
	}
	var media models.MediaFile
	err := s.db.WithContext(ctx).Where("space_id = ? AND id = ? AND deleted_at IS NULL", spaceID, mediaID).First(&media).Error
	if errorsIsNotFound(err) {
		return models.MediaFile{}, ErrNotFound
	}
	return media, err
}

func (s *Service) embeddedTracks(ctx context.Context, media models.MediaFile) ([]Track, error) {
	var metadata models.MediaMetadata
	err := s.db.WithContext(ctx).Where("space_id = ? AND media_id = ? AND stale = ?", media.SpaceID, media.ID, false).
		Order("parsed_at DESC, id DESC").First(&metadata).Error
	if errorsIsNotFound(err) {
		return []Track{}, nil
	}
	if err != nil {
		return nil, err
	}
	var normalized normalizedMetadata
	if err := json.Unmarshal([]byte(metadata.NormalizedJSON), &normalized); err != nil {
		return nil, fmt.Errorf("解析规范化媒体元数据失败: %w", err)
	}
	return buildEmbeddedTracks(media, normalized), nil
}

func buildEmbeddedTracks(media models.MediaFile, normalized normalizedMetadata) []Track {
	tracks := make([]Track, 0, len(normalized.AudioStreams)+len(normalized.SubtitleStreams))
	for _, stream := range normalized.AudioStreams {
		tracks = append(tracks, audioTrack(media, stream))
	}
	for _, stream := range normalized.SubtitleStreams {
		tracks = append(tracks, embeddedSubtitleTrack(media, stream))
	}
	return tracks
}

func audioTrack(media models.MediaFile, stream streamMetadata) Track {
	index := stream.Index
	return Track{
		ID:   stableID(media.SpaceID, media.ID, KindAudio, SourceEmbedded, strconv.Itoa(index)),
		Kind: KindAudio, Label: trackLabel(stream, "音轨 "+strconv.Itoa(index)), Source: SourceEmbedded,
		Codec: stream.CodecName, Language: stream.Language, Title: stream.Title,
		Channels: stream.Channels, ChannelLayout: stream.ChannelLayout,
		Default: stream.Default, Forced: stream.Forced, Available: true,
		Capability: CapabilityUnsupported, UnsupportedReason: ReasonAudioSwitchUnsupported, StreamIndex: &index,
	}
}

func embeddedSubtitleTrack(media models.MediaFile, stream streamMetadata) Track {
	index := stream.Index
	available, reason := subtitleCodecCapability(stream.CodecName)
	capability := CapabilityUnsupported
	if available {
		capability = CapabilitySeamless
	}
	return Track{
		ID:   stableID(media.SpaceID, media.ID, KindSubtitle, SourceEmbedded, strconv.Itoa(index)),
		Kind: KindSubtitle, Label: trackLabel(stream, "字幕 "+strconv.Itoa(index)), Source: SourceEmbedded,
		Format: subtitleFormat(stream.CodecName), Codec: stream.CodecName, Language: stream.Language,
		Title: stream.Title, Default: stream.Default, Forced: stream.Forced, Available: available,
		Capability: capability, UnsupportedReason: reason, StreamIndex: &index,
	}
}

func (s *Service) appendSidecarTracks(media models.MediaFile, tracks []Track, sources map[string]SourceCapability) ([]Track, error) {
	if strings.HasPrefix(strings.ToLower(media.FilePath), "smb://") {
		sources[SourceSidecar] = SourceCapability{Available: false, Capability: CapabilityUnsupported, UnsupportedReason: ReasonSMBSidecarUnsupported}
		return tracks, nil
	}
	files, err := transcoder.FindSubtitleFiles(media.FilePath)
	if err != nil {
		return nil, fmt.Errorf("发现外挂字幕失败: %w", err)
	}
	for _, file := range files {
		tracks = append(tracks, sidecarTrack(media, file))
	}
	return tracks, nil
}

func sidecarTrack(media models.MediaFile, file transcoder.SubtitleFile) Track {
	ref := normalizedSidecarRef(media.FilePath, file.Path)
	title, language := sidecarTitleLanguage(media.FilePath, file.Path)
	return Track{
		ID:   stableID(media.SpaceID, media.ID, KindSubtitle, SourceSidecar, ref),
		Kind: KindSubtitle, Label: filepath.Base(file.Path), Source: SourceSidecar,
		Format: file.Format, Language: language, Title: title, Available: true,
		Capability: CapabilitySeamless, path: file.Path,
	}
}

func (s *Service) appendUploadedTracks(ctx context.Context, media models.MediaFile, tracks []Track) ([]Track, error) {
	var rows []models.MediaSubtitleTrack
	err := s.db.WithContext(ctx).Where("space_id = ? AND media_id = ? AND source = ?", media.SpaceID, media.ID, SourceUploaded).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		tracks = append(tracks, uploadedTrack(row))
	}
	return tracks, nil
}

func uploadedTrack(row models.MediaSubtitleTrack) Track {
	label := row.Title
	if label == "" {
		label = "上传字幕"
	}
	return Track{
		ID: row.ID, Kind: KindSubtitle, Label: label, Source: SourceUploaded,
		Format: row.Format, Language: row.Language, Title: row.Title,
		Default: row.IsDefault, Forced: row.IsForced, Available: true,
		Capability: CapabilitySeamless,
	}
}

func sortTracks(tracks []Track) {
	sort.SliceStable(tracks, func(i, j int) bool {
		left, right := tracks[i], tracks[j]
		if left.Default != right.Default {
			return left.Default
		}
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		leftKey := strings.ToLower(left.Language + "\x00" + left.Title + "\x00" + left.ID)
		rightKey := strings.ToLower(right.Language + "\x00" + right.Title + "\x00" + right.ID)
		return leftKey < rightKey
	})
}

func stableID(spaceID string, mediaID int64, kind, source, ref string) string {
	value := strings.Join([]string{spaceID, strconv.FormatInt(mediaID, 10), kind, source, ref}, "\x00")
	sum := sha256.Sum256([]byte(value))
	return sourcePrefix(source) + "-" + hex.EncodeToString(sum[:12])
}

func sourcePrefix(source string) string {
	switch source {
	case SourceEmbedded:
		return "emb"
	case SourceSidecar:
		return "sid"
	default:
		return "upl"
	}
}

func defaultSourceCapabilities() map[string]SourceCapability {
	return map[string]SourceCapability{
		SourceEmbedded: {Available: true, Capability: CapabilitySeamless},
		SourceSidecar:  {Available: true, Capability: CapabilitySeamless},
		SourceUploaded: {Available: true, Capability: CapabilitySeamless},
	}
}

func backendCapabilities() map[string]SourceCapability {
	return map[string]SourceCapability{
		KindAudio:    {Available: false, Capability: CapabilityUnsupported, UnsupportedReason: ReasonAudioSwitchUnsupported},
		KindSubtitle: {Available: true, Capability: CapabilitySeamless},
	}
}

func selectionFromDefaults(tracks []Track) map[string]SelectionState {
	selection := map[string]SelectionState{KindAudio: {}, KindSubtitle: {}}
	for index := range tracks {
		track := tracks[index]
		if track.Kind != KindAudio || !track.Default || !track.Available {
			continue
		}
		id := track.ID
		selection[KindAudio] = SelectionState{SelectedTrackID: &id}
		break
	}
	return selection
}

type normalizedMetadata struct {
	AudioStreams    []streamMetadata `json:"audio_streams"`
	SubtitleStreams []streamMetadata `json:"subtitle_streams"`
}

type streamMetadata struct {
	Index         int    `json:"index"`
	CodecName     string `json:"codec_name"`
	Language      string `json:"language"`
	Title         string `json:"title"`
	Channels      int    `json:"channels"`
	ChannelLayout string `json:"channel_layout"`
	Default       bool   `json:"default"`
	Forced        bool   `json:"forced"`
}
