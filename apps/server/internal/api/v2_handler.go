package api

import (
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/openapi"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

// ListMediaV2 GET /api/v2/media
// 契约 MediaPage 形态（FR2-071）；查询仍委托 library，不直连 GORM。
func (h *Handler) ListMediaV2(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	page := parsePositiveInt(c.DefaultQuery("page", "1"), 1)
	pageSize := parsePositiveInt(c.DefaultQuery("page_size", "20"), 20)
	if pageSize > 100 {
		pageSize = 100
	}
	result, err := h.library.ListMediaFilesPage(library.MediaFilter{
		SpaceID:          spaceID,
		Sort:             "time_desc",
		MaxContentRating: h.viewerMaxContentRating(c, spaceID),
	}, library.MediaPageRequest{Page: page, PageSize: pageSize})
	if err != nil {
		c.JSON(http.StatusInternalServerError, openapi.Error{Code: "INTERNAL", Message: "查询失败"})
		return
	}
	items := make([]openapi.MediaItem, 0, len(result.Items))
	for i := range result.Items {
		items = append(items, toOpenAPIMediaItem(&result.Items[i], h.library))
	}
	c.JSON(http.StatusOK, openapi.MediaPage{
		Items:    items,
		Page:     result.Page,
		PageSize: result.PageSize,
		Total:    int(result.Total),
	})
}

// GetMediaV2 GET /api/v2/media/:id
func (h *Handler) GetMediaV2(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, openapi.Error{Code: "INVALID_ID", Message: "无效的媒体 ID"})
		return
	}
	mf, err := h.loadMediaForViewer(c, spaceID, id)
	if err != nil {
		c.JSON(http.StatusNotFound, openapi.Error{Code: "NOT_FOUND", Message: "媒体文件不存在"})
		return
	}
	c.JSON(http.StatusOK, toOpenAPIMediaItem(mf, h.library))
}

// GetTaskV2 GET /api/v2/tasks/:id
// 契约 TaskItem 形态；与 /api/tasks/:id 同源任务服务，响应字段收窄到契约。
func (h *Handler) GetTaskV2(c *gin.Context) {
	if h.tasks == nil {
		c.JSON(http.StatusServiceUnavailable, openapi.Error{Code: "TASKS_UNAVAILABLE", Message: "任务服务未启用"})
		return
	}
	id, ok := parseTaskID(c)
	if !ok {
		return
	}
	query, ok := h.taskQuery(c)
	if !ok {
		return
	}
	task, err := h.tasks.Get(c.Request.Context(), id, query)
	if err != nil {
		writeTaskV2Error(c, err)
		return
	}
	c.JSON(http.StatusOK, toOpenAPITaskItem(task))
}

func toOpenAPIMediaItem(mf *models.MediaFile, lib *library.Service) openapi.MediaItem {
	title := strings.TrimSpace(mf.DisplayName)
	if title == "" {
		title = mf.FileName
	}
	kind := openapi.Video
	if lib != nil && lib.IsImageFile(mf.FilePath) {
		kind = openapi.Image
	} else if isImageFormat(mf.Format, mf.FilePath) {
		kind = openapi.Image
	}
	created := mf.AddedAt
	if created.IsZero() {
		created = time.Now().UTC()
	}
	return openapi.MediaItem{
		Id:              strconv.FormatInt(mf.ID, 10),
		SpaceId:         mf.SpaceID,
		Title:           title,
		Kind:            kind,
		DurationSeconds: float32(mf.Duration),
		CreatedAt:       created.UTC(),
	}
}

func isImageFormat(format, filePath string) bool {
	ext := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(format), "."))
	if ext == "" {
		ext = strings.ToLower(strings.TrimPrefix(filepath.Ext(filePath), "."))
	}
	switch ext {
	case "jpg", "jpeg", "png", "gif", "webp", "bmp", "heic", "heif", "tiff", "tif", "avif":
		return true
	default:
		return false
	}
}

func toOpenAPITaskItem(task *models.Task) openapi.TaskItem {
	var errText *string
	if strings.TrimSpace(task.Error) != "" {
		e := task.Error
		errText = &e
	}
	status, err := tasksvc.NormalizeStatus(task.Status)
	if err != nil {
		status = task.Status // 未知状态原样透传，避免契约体空 status
	}
	return openapi.TaskItem{
		Id:        strconv.FormatInt(task.ID, 10),
		SpaceId:   task.SpaceID,
		Type:      task.Type,
		Status:    openapi.TaskItemStatus(status),
		Priority:  task.Priority,
		Progress:  float32(task.Progress) / 100,
		Error:     errText,
		CreatedAt: task.CreatedAt.UTC(),
		UpdatedAt: task.UpdatedAt.UTC(),
	}
}

func writeTaskV2Error(c *gin.Context, err error) {
	if errors.Is(err, tasksvc.ErrTaskNotFound) {
		c.JSON(http.StatusNotFound, openapi.Error{Code: "TASK_NOT_FOUND", Message: "任务不存在"})
		return
	}
	c.JSON(http.StatusBadRequest, openapi.Error{Code: "TASK_OPERATION_FAILED", Message: err.Error()})
}
