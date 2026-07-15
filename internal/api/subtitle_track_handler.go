package api

import (
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/subtitle"
	"github.com/wcpe/JianVideo/internal/transcoder"
)

const subtitleMultipartOverhead int64 = 256 << 10

// GetTracks 返回统一音轨、字幕轨和后端能力。
func (h *Handler) GetTracks(c *gin.Context) {
	service, spaceID, mediaID, ok := h.subtitleRequest(c)
	if !ok {
		return
	}
	response, err := service.List(c.Request.Context(), spaceID, mediaID)
	if err != nil {
		writeSubtitleError(c, err)
		return
	}
	media, err := h.library.GetMediaFileByIDInSpace(spaceID, mediaID)
	if err != nil {
		writeSubtitleError(c, subtitle.ErrNotFound)
		return
	}
	decorateAudioReloadCapabilities(&response, media.FilePath, h.hlsPreview != nil, transcoder.IsFFmpegAvailable(), h.hardwarePolicy())
	c.JSON(http.StatusOK, response)
}

func decorateAudioReloadCapabilities(response *subtitle.ListResponse, filePath string, previewAvailable, ffmpegAvailable bool, policy transcoder.HardwarePolicy) {
	if response == nil {
		return
	}
	backendReason := subtitle.ReasonAudioSwitchUnsupported
	hasAudio := false
	hasReload := false
	reloadTargets := countAudioReloadTargets(response.Tracks)
	for index := range response.Tracks {
		track := &response.Tracks[index]
		if track.Kind != subtitle.KindAudio {
			continue
		}
		hasAudio = true
		capability := audioReloadCapability(*track, filePath, previewAvailable, ffmpegAvailable, policy)
		capability = requireMultipleAudioReloadTargets(capability, reloadTargets)
		track.Capability = capability.Capability
		track.UnsupportedReason = capability.UnsupportedReason
		if capability.Available {
			hasReload = true
			continue
		}
		backendReason = capability.UnsupportedReason
	}
	if hasReload {
		response.Backend[subtitle.KindAudio] = subtitle.SourceCapability{Available: true, Capability: subtitle.CapabilityReload}
		return
	}
	if hasAudio {
		response.Backend[subtitle.KindAudio] = subtitle.SourceCapability{
			Available: false, Capability: subtitle.CapabilityUnsupported, UnsupportedReason: backendReason,
		}
	}
}

func countAudioReloadTargets(tracks []subtitle.Track) int {
	count := 0
	for _, track := range tracks {
		if track.Kind == subtitle.KindAudio && track.Source == subtitle.SourceEmbedded && track.Available && track.StreamIndex != nil && *track.StreamIndex >= 0 {
			count++
		}
	}
	return count
}

func requireMultipleAudioReloadTargets(capability subtitle.SourceCapability, targetCount int) subtitle.SourceCapability {
	if !capability.Available || targetCount >= 2 {
		return capability
	}
	return subtitle.SourceCapability{
		Available: false, Capability: subtitle.CapabilityUnsupported,
		UnsupportedReason: subtitle.ReasonAudioSwitchUnsupported,
	}
}

func audioReloadCapability(track subtitle.Track, filePath string, previewAvailable, ffmpegAvailable bool, policy transcoder.HardwarePolicy) subtitle.SourceCapability {
	reason := audioReloadUnsupportedReason(track, filePath, previewAvailable, ffmpegAvailable, policy)
	if reason != "" {
		return subtitle.SourceCapability{Available: false, Capability: subtitle.CapabilityUnsupported, UnsupportedReason: reason}
	}
	return subtitle.SourceCapability{Available: true, Capability: subtitle.CapabilityReload}
}

func audioReloadUnsupportedReason(track subtitle.Track, filePath string, previewAvailable, ffmpegAvailable bool, policy transcoder.HardwarePolicy) string {
	if track.Kind != subtitle.KindAudio || track.Source != subtitle.SourceEmbedded || !track.Available {
		return subtitle.ReasonAudioSwitchUnsupported
	}
	if track.StreamIndex == nil || *track.StreamIndex < 0 {
		return subtitle.ReasonAudioStreamIndexUnavailable
	}
	if strings.HasPrefix(strings.ToLower(filePath), "smb://") {
		return subtitle.ReasonSMBAudioReloadUnsupported
	}
	if !previewAvailable {
		return subtitle.ReasonAudioSwitchUnsupported
	}
	if !ffmpegAvailable {
		return subtitle.ReasonAudioReloadFFmpegUnavailable
	}
	if _, _, _, err := transcoder.SelectCurrentEncoderForCodecWithPolicy(transcoder.DefaultTargetCodec, policy); err != nil {
		return subtitle.ReasonAudioHardwareUnavailable
	}
	return ""
}

// UploadSubtitle 接收单个 multipart 字幕并流式交给服务层。
func (h *Handler) UploadSubtitle(c *gin.Context) {
	service, spaceID, mediaID, ok := h.subtitleRequest(c)
	if !ok {
		return
	}
	part, err := subtitleUploadPart(c)
	if err != nil {
		writeSubtitleError(c, err)
		return
	}
	defer func() { _ = part.Close() }()
	track, err := service.Upload(c.Request.Context(), spaceID, mediaID, part.FileName(), part)
	if err != nil {
		writeSubtitleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, track)
}

// GetSubtitleTrackContent 返回稳定轨道 ID 对应的规范化 WebVTT。
func (h *Handler) GetSubtitleTrackContent(c *gin.Context) {
	service, spaceID, mediaID, ok := h.subtitleRequest(c)
	if !ok {
		return
	}
	content, err := service.Content(c.Request.Context(), spaceID, mediaID, c.Param("track_id"))
	if err != nil {
		writeSubtitleError(c, err)
		return
	}
	c.Data(http.StatusOK, "text/vtt; charset=utf-8", []byte(content))
}

// DeleteSubtitleTrack 仅删除当前 Space 中的上传字幕。
func (h *Handler) DeleteSubtitleTrack(c *gin.Context) {
	service, spaceID, mediaID, ok := h.subtitleRequest(c)
	if !ok {
		return
	}
	if err := service.Delete(c.Request.Context(), spaceID, mediaID, c.Param("track_id")); err != nil {
		writeSubtitleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) subtitleRequest(c *gin.Context) (*subtitle.Service, string, int64, bool) {
	if h.subtitle == nil {
		writeSubtitleError(c, errors.New(subtitle.ReasonSubtitleServiceUnavailable))
		return nil, "", 0, false
	}
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return nil, "", 0, false
	}
	mediaID, ok := parseMediaID(c)
	if !ok {
		return nil, "", 0, false
	}
	if mediaID <= 0 {
		writeSubtitleError(c, subtitle.ErrInvalid)
		return nil, "", 0, false
	}
	return h.subtitle, spaceID, mediaID, true
}

func subtitleUploadPart(c *gin.Context) (*subtitleUpload, error) {
	limit := subtitle.MaxUploadBytes + subtitleMultipartOverhead
	if c.Request.ContentLength > limit {
		return nil, subtitle.ErrTooLarge
	}
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		return nil, fmtSubtitleInvalid("请求必须是 multipart/form-data")
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	reader, err := c.Request.MultipartReader()
	if err != nil {
		return nil, fmtSubtitleInvalid("请求必须是 multipart/form-data")
	}
	part, err := reader.NextPart()
	if err != nil {
		return nil, subtitlePartError(err)
	}
	if part.FormName() != "file" || part.FileName() == "" {
		_ = part.Close()
		return nil, fmtSubtitleInvalid("请求必须恰好包含一个 file 文件字段")
	}
	if err := validateMultipartFileName(part); err != nil {
		_ = part.Close()
		return nil, err
	}
	return &subtitleUpload{part: part, reader: reader}, nil
}

func validateMultipartFileName(part *multipart.Part) error {
	_, params, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
	if err != nil {
		return fmtSubtitleInvalid("字幕文件名无效")
	}
	fileName := params["filename"]
	if fileName == "" || filepath.Base(fileName) != fileName || strings.Contains(fileName, "..") {
		return fmtSubtitleInvalid("字幕文件名无效")
	}
	return nil
}

type subtitleUpload struct {
	part   *multipart.Part
	reader *multipart.Reader
}

func (upload *subtitleUpload) Read(data []byte) (int, error) {
	count, err := upload.part.Read(data)
	if !errors.Is(err, io.EOF) {
		return count, err
	}
	if err := upload.validateEnd(); err != nil {
		return count, err
	}
	return count, io.EOF
}

func (upload *subtitleUpload) validateEnd() error {
	part, err := upload.reader.NextPart()
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return subtitlePartError(err)
	}
	_ = part.Close()
	return fmtSubtitleInvalid("只能上传一个字幕文件")
}

func (upload *subtitleUpload) FileName() string {
	return upload.part.FileName()
}

func (upload *subtitleUpload) Close() error {
	return upload.part.Close()
}

func subtitlePartError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return subtitle.ErrTooLarge
	}
	if errors.Is(err, io.EOF) {
		return fmtSubtitleInvalid("缺少字幕文件")
	}
	return fmtSubtitleInvalid("multipart 请求无效")
}

func fmtSubtitleInvalid(message string) error {
	return errors.Join(subtitle.ErrInvalid, errors.New(message))
}

func writeSubtitleError(c *gin.Context, err error) {
	status, code, message := subtitleErrorResponse(err)
	c.JSON(status, gin.H{"code": code, "message": message})
}

func subtitleErrorResponse(err error) (int, string, string) {
	var tooLarge *http.MaxBytesError
	switch {
	case errors.Is(err, subtitle.ErrTooLarge) || errors.As(err, &tooLarge):
		return http.StatusRequestEntityTooLarge, "SUBTITLE_TOO_LARGE", "字幕文件超过 10 MiB"
	case errors.Is(err, subtitle.ErrNotFound):
		return http.StatusNotFound, "SUBTITLE_NOT_FOUND", "字幕轨道或媒体不存在"
	case errors.Is(err, subtitle.ErrUnsupported):
		code := subtitle.UnsupportedReason(err)
		if code == "" {
			code = "SUBTITLE_UNPROCESSABLE"
		}
		return http.StatusUnprocessableEntity, code, "字幕轨道不受支持"
	case errors.Is(err, subtitle.ErrUnprocessable):
		return http.StatusUnprocessableEntity, "SUBTITLE_UNPROCESSABLE", "字幕内容无法处理"
	case errors.Is(err, subtitle.ErrInvalid):
		return http.StatusBadRequest, "INVALID_SUBTITLE", "字幕请求无效"
	default:
		return http.StatusServiceUnavailable, "SUBTITLE_SERVICE_UNAVAILABLE", "字幕服务暂不可用"
	}
}

// GetSubtitles 保留旧字幕列表和数组索引兼容路径。
func (h *Handler) GetSubtitles(c *gin.Context) {
	h.getLegacySubtitles(c)
}

func (h *Handler) getLegacySubtitles(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	mediaID, ok := parseMediaID(c)
	if !ok {
		return
	}
	media, err := h.library.GetMediaFileByIDInSpace(spaceID, mediaID)
	if err != nil {
		writeSubtitleError(c, subtitle.ErrNotFound)
		return
	}
	if strings.HasPrefix(strings.ToLower(media.FilePath), "smb://") {
		c.JSON(http.StatusOK, gin.H{"tracks": []any{}, "sources": legacySMBCapability()})
		return
	}
	files, err := transcoder.FindSubtitleFiles(media.FilePath)
	if err != nil {
		writeSubtitleError(c, err)
		return
	}
	tracks := make([]gin.H, 0, len(files))
	for index, file := range files {
		url := "/api/play/" + strconv.FormatInt(mediaID, 10) + "/subtitles/" + strconv.Itoa(index)
		tracks = append(tracks, gin.H{"index": index, "file_name": filepath.Base(file.Path), "format": file.Format, "url": url})
	}
	c.JSON(http.StatusOK, gin.H{"tracks": tracks})
}

func legacySMBCapability() gin.H {
	return gin.H{subtitle.SourceSidecar: gin.H{
		"available": false, "capability": subtitle.CapabilityUnsupported,
		"unsupported_reason": subtitle.ReasonSMBSidecarUnsupported,
	}}
}

// GetSubtitleContent 保留旧数组索引内容路径。
func (h *Handler) GetSubtitleContent(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	mediaID, ok := parseMediaID(c)
	if !ok {
		return
	}
	content, err := h.legacySubtitleContent(spaceID, mediaID, c.Param("track_id"))
	if err != nil {
		writeSubtitleError(c, err)
		return
	}
	c.Data(http.StatusOK, "text/vtt; charset=utf-8", []byte(content))
}

func (h *Handler) legacySubtitleContent(spaceID string, mediaID int64, rawIndex string) (string, error) {
	index, err := strconv.Atoi(rawIndex)
	if err != nil || index < 0 {
		return "", subtitle.ErrInvalid
	}
	media, err := h.library.GetMediaFileByIDInSpace(spaceID, mediaID)
	if err != nil {
		return "", subtitle.ErrNotFound
	}
	files, err := transcoder.FindSubtitleFiles(media.FilePath)
	if err != nil {
		return "", err
	}
	if index >= len(files) {
		return "", subtitle.ErrNotFound
	}
	content, err := transcoder.ConvertSubtitleFileInRoot(filepath.Dir(media.FilePath), filepath.Base(files[index].Path))
	if errors.Is(err, transcoder.ErrSubtitleFileUnavailable) {
		return "", subtitle.ErrNotFound
	}
	if err != nil {
		return "", errors.Join(subtitle.ErrUnprocessable, err)
	}
	return content, nil
}
