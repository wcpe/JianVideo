package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/subtitle"
	"github.com/wcpe/JianVideo/internal/transcoder"
)

type audioReloadRequest struct {
	TrackID string `json:"track_id"`
}

type audioReloadTarget struct {
	spaceID     string
	mediaID     int64
	width       int
	height      int
	track       subtitle.Track
	streamIndex int
	fingerprint string
}

// CreateAudioReload 按稳定音轨 ID 创建固定 H.264/AAC 的 HLS 重载任务。
func (h *Handler) CreateAudioReload(c *gin.Context) {
	request, ok := decodeAudioReloadRequest(c)
	if !ok || !h.ensureHLSPreview(c) {
		return
	}
	target, ok := h.resolveAudioReloadTarget(c, request.TrackID)
	if !ok {
		return
	}
	task, err := h.hlsPreview.EnqueueAudioReload(c.Request.Context(), transcoder.AudioReloadRequest{
		SpaceID: target.spaceID, MediaID: target.mediaID, AudioTrackID: target.track.ID,
		AudioStreamIndex: target.streamIndex, Width: target.width, Height: target.height, SourceFingerprint: target.fingerprint,
	})
	if err != nil {
		writeAudioReloadEnqueueFailed(c)
		return
	}
	if h.taskWorkers != nil {
		h.taskWorkers.Wake()
	}
	profileID := transcoder.AudioReloadProfileID(target.track.ID)
	c.JSON(http.StatusAccepted, gin.H{
		"task_id": strconv.FormatInt(task.ID, 10), "profile_id": profileID,
		"url": transcoder.HLSProfileTaskURL(target.mediaID, profileID, transcoder.DefaultTargetCodec, task.ID), "requested_track_id": target.track.ID, "space_id": target.spaceID,
	})
}

func (h *Handler) ensureHLSPreview(c *gin.Context) bool {
	if h.hlsPreview != nil {
		return true
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{"code": "HLS_PREVIEW_UNAVAILABLE", "message": "HLS 预览服务未启用"})
	return false
}

func (h *Handler) resolveAudioReloadTarget(c *gin.Context, trackID string) (audioReloadTarget, bool) {
	service, spaceID, mediaID, ok := h.subtitleRequest(c)
	if !ok {
		return audioReloadTarget{}, false
	}
	media, err := h.library.GetMediaFileByIDInSpace(spaceID, mediaID)
	if err != nil {
		writeAudioReloadNotFound(c)
		return audioReloadTarget{}, false
	}
	response, ok := listAudioReloadTracks(c, service, spaceID, mediaID)
	if !ok {
		return audioReloadTarget{}, false
	}
	decorateAudioReloadCapabilities(&response, media.FilePath, true, transcoder.IsFFmpegAvailable(), h.hardwarePolicy())
	track := findTrackByID(response.Tracks, trackID)
	if track == nil {
		writeAudioReloadNotFound(c)
		return audioReloadTarget{}, false
	}
	if track.Capability != subtitle.CapabilityReload || track.StreamIndex == nil {
		writeAudioReloadUnsupported(c, track.UnsupportedReason)
		return audioReloadTarget{}, false
	}
	fileInfo, err := os.Stat(media.FilePath)
	if err != nil {
		writeAudioReloadEnqueueFailed(c)
		return audioReloadTarget{}, false
	}
	fingerprint := transcoder.AudioReloadSourceFingerprint(transcoder.MediaIdentity{
		SpaceID: media.SpaceID, MediaID: media.ID, Path: media.FilePath, Size: fileInfo.Size(),
		ModifiedAt: fileInfo.ModTime(), ContentHash: media.ContentHash, ContentHashStale: media.ContentHashStale,
	}, transcoder.AudioTrackIdentity{
		ID: track.ID, Index: *track.StreamIndex, Codec: track.Codec, Language: track.Language,
		Title: track.Title, Channels: track.Channels, ChannelLayout: track.ChannelLayout,
	})
	return audioReloadTarget{
		spaceID: spaceID, mediaID: mediaID, width: media.Width, height: media.Height,
		track: *track, streamIndex: *track.StreamIndex,
		fingerprint: fingerprint,
	}, true
}

func listAudioReloadTracks(c *gin.Context, service *subtitle.Service, spaceID string, mediaID int64) (subtitle.ListResponse, bool) {
	response, err := service.List(c.Request.Context(), spaceID, mediaID)
	if err == nil {
		return response, true
	}
	if errors.Is(err, subtitle.ErrNotFound) {
		writeAudioReloadNotFound(c)
	} else {
		writeAudioReloadEnqueueFailed(c)
	}
	return subtitle.ListResponse{}, false
}

func decodeAudioReloadRequest(c *gin.Context) (audioReloadRequest, bool) {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var request audioReloadRequest
	if err := decoder.Decode(&request); err != nil {
		writeInvalidAudioReloadRequest(c)
		return audioReloadRequest{}, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeInvalidAudioReloadRequest(c)
		return audioReloadRequest{}, false
	}
	request.TrackID = strings.TrimSpace(request.TrackID)
	if request.TrackID == "" {
		writeInvalidAudioReloadRequest(c)
		return audioReloadRequest{}, false
	}
	return request, true
}

func findTrackByID(tracks []subtitle.Track, trackID string) *subtitle.Track {
	for index := range tracks {
		if tracks[index].ID == trackID {
			return &tracks[index]
		}
	}
	return nil
}

func writeInvalidAudioReloadRequest(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_AUDIO_RELOAD_REQUEST", "message": "音轨重载请求无效"})
}

func writeAudioReloadNotFound(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体或音轨不存在"})
}

func writeAudioReloadUnsupported(c *gin.Context, reason string) {
	if reason == "" {
		reason = subtitle.ReasonAudioSwitchUnsupported
	}
	c.JSON(http.StatusUnprocessableEntity, gin.H{
		"code": "AUDIO_RELOAD_UNSUPPORTED", "message": "目标音轨不支持重载", "reason": reason,
	})
}

func writeAudioReloadEnqueueFailed(c *gin.Context) {
	c.JSON(http.StatusInternalServerError, gin.H{"code": "AUDIO_RELOAD_ENQUEUE_FAILED", "message": "音轨重载任务入队失败"})
}
