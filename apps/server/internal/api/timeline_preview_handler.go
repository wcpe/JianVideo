package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	// TimelinePreviewAvailable 表示时间轴预览资源已经可用。
	TimelinePreviewAvailable = "available"
	// TimelinePreviewPending 表示时间轴预览正在等待或执行生成。
	TimelinePreviewPending = "pending"
)

var (
	// ErrTimelinePreviewInvalid 表示时间轴预览身份无效。
	ErrTimelinePreviewInvalid = errors.New("时间轴预览身份无效")
	// ErrTimelinePreviewNotFound 表示时间轴预览资源不存在。
	ErrTimelinePreviewNotFound = errors.New("时间轴预览资源不存在")
)

// TimelinePreviewIdentity 标识媒体的时间轴预览配置。
type TimelinePreviewIdentity struct {
	MediaID   int64
	ProfileID string
	SpaceID   string
}

// TimelinePreviewResourceIdentity 标识指定 generation 中的时间轴预览资源。
type TimelinePreviewResourceIdentity struct {
	TimelinePreviewIdentity
	GenerationID      string
	ResourceName      string
	SourceFingerprint string
}

// TimelinePreviewStatus 描述时间轴预览的生成与可用状态。
type TimelinePreviewStatus struct {
	GenerationID      string   `json:"generation_id,omitempty"`
	ProfileID         string   `json:"profile_id"`
	SourceFingerprint string   `json:"source_fingerprint,omitempty"`
	State             string   `json:"status"`
	Duration          float64  `json:"duration"`
	Version           int      `json:"version"`
	SpriteNames       []string `json:"-"`
	TaskID            int64    `json:"task_id,omitempty"`
}

// TimelinePreviewResource 描述可流式返回的时间轴预览资源。
type TimelinePreviewResource struct {
	Body        io.ReadCloser
	ContentType string
	Size        int64
}

// TimelinePreviewGateway 提供时间轴预览状态、生成与资源访问能力。
type TimelinePreviewGateway interface {
	Status(context.Context, TimelinePreviewIdentity) (TimelinePreviewStatus, error)
	Enqueue(context.Context, TimelinePreviewIdentity) (TimelinePreviewStatus, error)
	Rebuild(context.Context, TimelinePreviewIdentity) (TimelinePreviewStatus, error)
	OpenResource(context.Context, TimelinePreviewResourceIdentity) (TimelinePreviewResource, error)
}

// GetTimelinePreview 查询当前预览，缺失时仅幂等入队。
func (h *Handler) GetTimelinePreview(c *gin.Context) {
	identity, ok := h.timelineIdentity(c, c.Query("profile"))
	if !ok {
		return
	}
	status, err := h.timelinePreview.Status(c.Request.Context(), identity)
	if err != nil {
		h.writeTimelineError(c, err)
		return
	}
	if status.State == TimelinePreviewAvailable {
		h.writeTimelineStatus(c, http.StatusOK, identity.MediaID, status)
		return
	}
	status, err = h.timelinePreview.Enqueue(c.Request.Context(), identity)
	if err != nil {
		h.writeTimelineError(c, err)
		return
	}
	h.writeTimelineStatus(c, http.StatusAccepted, identity.MediaID, status)
}

// RebuildTimelinePreview 创建新 generation，不在请求内执行转码。
func (h *Handler) RebuildTimelinePreview(c *gin.Context) {
	var request struct {
		ProfileID string `json:"profile_id"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "请求参数错误"})
		return
	}
	identity, ok := h.timelineIdentity(c, request.ProfileID)
	if !ok {
		return
	}
	status, err := h.timelinePreview.Rebuild(c.Request.Context(), identity)
	if err != nil {
		h.writeTimelineError(c, err)
		return
	}
	h.writeTimelineStatus(c, http.StatusAccepted, identity.MediaID, status)
}

// GetTimelinePreviewResource 按完整受控身份流式返回 VTT 或 sprite。
func (h *Handler) GetTimelinePreviewResource(c *gin.Context) {
	identity, ok := h.timelineResourceIdentity(c)
	if !ok {
		return
	}
	resource, err := h.timelinePreview.OpenResource(c.Request.Context(), identity)
	if err != nil {
		h.writeTimelineError(c, err)
		return
	}
	if resource.Body == nil || !validTimelineContentType(identity.ResourceName, resource.ContentType) {
		if resource.Body != nil {
			_ = resource.Body.Close()
		}
		h.writeTimelineError(c, ErrTimelinePreviewInvalid)
		return
	}
	defer func() { _ = resource.Body.Close() }()
	c.Header("Content-Type", resource.ContentType)
	if resource.Size >= 0 {
		c.Header("Content-Length", strconv.FormatInt(resource.Size, 10))
	}
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, resource.Body)
}

func (h *Handler) timelineIdentity(c *gin.Context, profileID string) (TimelinePreviewIdentity, bool) {
	if h.timelinePreview == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "TIMELINE_PREVIEW_UNAVAILABLE", "message": "时间轴预览服务未启用"})
		return TimelinePreviewIdentity{}, false
	}
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return TimelinePreviewIdentity{}, false
	}
	mediaID, ok := parseMediaID(c)
	if !ok || profileID != "" && !validTimelineToken(profileID) {
		if ok {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PROFILE", "message": "预览 profile 不合法"})
		}
		return TimelinePreviewIdentity{}, false
	}
	if _, err := h.loadMediaForViewer(c, spaceID, mediaID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
		return TimelinePreviewIdentity{}, false
	}
	return TimelinePreviewIdentity{MediaID: mediaID, ProfileID: profileID, SpaceID: spaceID}, true
}

func (h *Handler) timelineResourceIdentity(c *gin.Context) (TimelinePreviewResourceIdentity, bool) {
	base, ok := h.timelineIdentity(c, c.Param("profile"))
	if !ok {
		return TimelinePreviewResourceIdentity{}, false
	}
	fingerprint := c.Param("fingerprint")
	generation := c.Param("generation")
	resource := c.Param("resource")
	if !validTimelineToken(fingerprint) || !validTimelineToken(generation) || !validTimelineResource(resource) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_RESOURCE", "message": "预览资源身份不合法"})
		return TimelinePreviewResourceIdentity{}, false
	}
	return TimelinePreviewResourceIdentity{
		TimelinePreviewIdentity: base, GenerationID: generation,
		ResourceName: resource, SourceFingerprint: fingerprint,
	}, true
}

func (h *Handler) writeTimelineStatus(c *gin.Context, code int, mediaID int64, status TimelinePreviewStatus) {
	response := gin.H{
		"generation_id": status.GenerationID, "profile_id": status.ProfileID,
		"source_fingerprint": status.SourceFingerprint, "status": status.State, "task_id": status.TaskID,
		"duration": status.Duration, "version": status.Version,
	}
	if status.State == TimelinePreviewAvailable {
		response["vtt_url"] = timelineResourcePath(mediaID, status, "index.vtt")
		response["sprite_urls"] = timelineSpritePaths(mediaID, status)
	}
	c.JSON(code, response)
}

func timelineSpritePaths(mediaID int64, status TimelinePreviewStatus) map[string]string {
	urls := make(map[string]string, len(status.SpriteNames))
	for _, name := range status.SpriteNames {
		urls[name] = timelineResourcePath(mediaID, status, name)
	}
	return urls
}

func (h *Handler) writeTimelineError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrTimelinePreviewInvalid):
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PREVIEW", "message": err.Error()})
	case errors.Is(err, ErrTimelinePreviewNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "时间轴预览不存在"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"code": "TIMELINE_PREVIEW_FAILED", "message": "时间轴预览处理失败"})
	}
}

func timelineResourcePath(mediaID int64, status TimelinePreviewStatus, resource string) string {
	parts := []string{
		"/api/play", strconv.FormatInt(mediaID, 10), "timeline-preview/resources",
		status.ProfileID, status.SourceFingerprint, status.GenerationID, resource,
	}
	return path.Join(parts...)
}

func validTimelineResource(resource string) bool {
	if resource == "" || resource != path.Base(resource) || strings.ContainsAny(resource, `\\/`) {
		return false
	}
	if resource == "index.vtt" {
		return true
	}
	return strings.HasSuffix(resource, ".webp") || strings.HasSuffix(resource, ".jpg") || strings.HasSuffix(resource, ".png")
}

func validTimelineContentType(resource, contentType string) bool {
	if resource == "index.vtt" {
		return strings.HasPrefix(contentType, "text/vtt")
	}
	return strings.HasPrefix(contentType, "image/")
}

func validTimelineToken(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == ".." || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if !validTimelineCharacter(character) {
			return false
		}
	}
	return true
}

func validTimelineCharacter(character rune) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9' || character == '-' || character == '_' || character == '.'
}
