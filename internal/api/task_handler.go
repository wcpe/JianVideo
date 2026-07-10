package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/db/models"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

type taskResponse struct {
	ID           string     `json:"id"`
	Scope        string     `json:"scope"`
	SpaceID      *string    `json:"space_id"`
	Type         string     `json:"type"`
	Status       string     `json:"status"`
	Priority     int        `json:"priority"`
	Attempts     int        `json:"attempts"`
	MaxAttempts  int        `json:"max_attempts"`
	Progress     float64    `json:"progress"`
	Checkpoint   string     `json:"checkpoint,omitempty"`
	ResourceType string     `json:"resource_type,omitempty"`
	ResourceID   string     `json:"resource_id,omitempty"`
	Error        *string    `json:"error"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	NextRunAt    *time.Time `json:"next_run_at,omitempty"`
}

func (h *Handler) taskQuery(c *gin.Context) (tasksvc.Query, bool) {
	query := tasksvc.Query{
		Scope:        strings.TrimSpace(c.Query("scope")),
		Type:         strings.TrimSpace(c.Query("type")),
		Status:       strings.TrimSpace(c.Query("status")),
		ResourceType: strings.TrimSpace(c.Query("resource_type")),
		ResourceID:   strings.TrimSpace(c.Query("resource_id")),
		Page:         parsePositiveInt(c.Query("page"), 1),
		PageSize:     parsePositiveInt(c.Query("page_size"), 20),
	}
	if query.Scope == models.TaskScopeSystem {
		return query, true
	}
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return tasksvc.Query{}, false
	}
	query.Scope = models.TaskScopeSpace
	query.SpaceID = spaceID
	return query, true
}

// ListTasks GET /api/tasks
func (h *Handler) ListTasks(c *gin.Context) {
	if h.tasks == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "TASKS_UNAVAILABLE", "message": "任务服务未启用"})
		return
	}
	query, ok := h.taskQuery(c)
	if !ok {
		return
	}
	page, err := h.tasks.List(c.Request.Context(), query)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_TASK_QUERY", "message": err.Error()})
		return
	}
	items := make([]taskResponse, len(page.Items))
	for i := range page.Items {
		items[i] = toTaskResponse(&page.Items[i])
	}
	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"page":      page.Page,
		"page_size": page.Size,
		"total":     page.Total,
	})
}

// GetTask GET /api/tasks/:id
func (h *Handler) GetTask(c *gin.Context) {
	if h.tasks == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "TASKS_UNAVAILABLE", "message": "任务服务未启用"})
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
		writeTaskError(c, err)
		return
	}
	c.JSON(http.StatusOK, toTaskResponse(task))
}

// TaskStats GET /api/tasks/stats
func (h *Handler) TaskStats(c *gin.Context) {
	if h.tasks == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "TASKS_UNAVAILABLE", "message": "任务服务未启用"})
		return
	}
	query, ok := h.taskQuery(c)
	if !ok {
		return
	}
	stats, err := h.tasks.Stats(c.Request.Context(), query)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_TASK_QUERY", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"total":     stats.Total,
		"by_status": stats.ByStatus,
		"by_type":   stats.ByType,
	})
}

// CancelTask POST /api/tasks/:id/cancel
func (h *Handler) CancelTask(c *gin.Context) {
	h.changeTask(c, "cancel", func(id int64, query tasksvc.Query) error {
		return h.tasks.Cancel(c.Request.Context(), id, query)
	})
}

// RetryTask POST /api/tasks/:id/retry
func (h *Handler) RetryTask(c *gin.Context) {
	h.changeTask(c, "retry", func(id int64, query tasksvc.Query) error {
		return h.tasks.Retry(c.Request.Context(), id, query)
	})
}

func (h *Handler) changeTask(c *gin.Context, action string, change func(int64, tasksvc.Query) error) {
	if h.tasks == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "TASKS_UNAVAILABLE", "message": "任务服务未启用"})
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
		writeTaskError(c, err)
		return
	}
	handled, err := h.changeLegacyTask(task, action)
	if err != nil {
		writeTaskError(c, err)
		return
	}
	if !handled {
		err = change(id, query)
	}
	if err != nil {
		writeTaskError(c, err)
		return
	}
	if !handled && action == "retry" && h.taskWorkers != nil {
		h.taskWorkers.Wake()
	}
	task, err = h.tasks.Get(c.Request.Context(), id, query)
	if err != nil {
		writeTaskError(c, err)
		return
	}
	c.JSON(http.StatusOK, toTaskResponse(task))
}

func (h *Handler) changeLegacyTask(task *models.Task, action string) (bool, error) {
	if task.Type == "library.scan" && h.scanQueue != nil {
		legacyID, ok := parseLegacyTaskID(task.IdempotencyKey, "scan:")
		if !ok || task.SpaceID == nil {
			return false, nil
		}
		if action == "cancel" {
			return true, h.scanQueue.CancelTaskInSpace(*task.SpaceID, legacyID)
		}
		return true, h.scanQueue.RetryTaskInSpace(*task.SpaceID, legacyID)
	}
	if task.Type == "transcode.hls" && h.pregenQueue != nil {
		legacyID, ok := parseLegacyTaskID(task.IdempotencyKey, "transcode:")
		if !ok || task.SpaceID == nil {
			return false, nil
		}
		if action == "cancel" {
			return true, h.pregenQueue.CancelTaskInSpace(*task.SpaceID, legacyID)
		}
		return true, h.pregenQueue.RetryTaskInSpace(*task.SpaceID, legacyID)
	}
	return false, nil
}

func parseLegacyTaskID(key, prefix string) (int64, bool) {
	if !strings.HasPrefix(key, prefix) {
		return 0, false
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(key, prefix), 10, 64)
	return id, err == nil && id > 0
}

func parseTaskID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的任务 ID"})
		return 0, false
	}
	return id, true
}

func parsePositiveInt(raw string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func toTaskResponse(task *models.Task) taskResponse {
	var errText *string
	if strings.TrimSpace(task.Error) != "" {
		errText = &task.Error
	}
	return taskResponse{
		ID:           strconv.FormatInt(task.ID, 10),
		Scope:        task.Scope,
		SpaceID:      task.SpaceID,
		Type:         task.Type,
		Status:       task.Status,
		Priority:     task.Priority,
		Attempts:     task.Attempts,
		MaxAttempts:  task.MaxAttempts,
		Progress:     float64(task.Progress) / 100,
		Checkpoint:   task.Checkpoint,
		ResourceType: task.ResourceType,
		ResourceID:   task.ResourceID,
		Error:        errText,
		CreatedAt:    task.CreatedAt,
		UpdatedAt:    task.UpdatedAt,
		StartedAt:    task.StartedAt,
		FinishedAt:   task.FinishedAt,
		NextRunAt:    task.NextRunAt,
	}
}

func writeTaskError(c *gin.Context, err error) {
	if errors.Is(err, tasksvc.ErrTaskNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"code": "TASK_NOT_FOUND", "message": "任务不存在"})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"code": "TASK_OPERATION_FAILED", "message": err.Error()})
}
