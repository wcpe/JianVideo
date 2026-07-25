package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/ai"
	"github.com/wcpe/JianVideo/internal/db/models"
)

// AIStatus 返回 AI 能力状态。
func (h *Handler) AIStatus(c *gin.Context) {
	if h.ai == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "AI_UNAVAILABLE", "message": "AI 服务未启用"})
		return
	}
	enabled, modelsList, nodes, err := h.ai.Status(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "读取 AI 状态失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"enabled": enabled, "models": modelsList, "nodes": nodes})
}

// ListAIModels 列出已注册模型。
func (h *Handler) ListAIModels(c *gin.Context) {
	if h.ai == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "AI_UNAVAILABLE", "message": "AI 服务未启用"})
		return
	}
	_, modelsList, _, err := h.ai.Status(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "读取模型失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": modelsList})
}

// ListAINodes 列出推理节点。
func (h *Handler) ListAINodes(c *gin.Context) {
	if h.ai == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "AI_UNAVAILABLE", "message": "AI 服务未启用"})
		return
	}
	_, _, nodes, err := h.ai.Status(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "读取节点失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": nodes})
}

// UpdateAIModelStatus 更新模型 available/disabled。
func (h *Handler) UpdateAIModelStatus(c *gin.Context) {
	if h.ai == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "AI_UNAVAILABLE", "message": "AI 服务未启用"})
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的模型 ID"})
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_REQUEST", "message": "需要 status"})
		return
	}
	if err := h.ai.SetModelStatus(c.Request.Context(), id, body.Status, actorIDFromContext(c)); err != nil {
		writeAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// UpdateAINodeEnabled 更新节点启用状态。
func (h *Handler) UpdateAINodeEnabled(c *gin.Context) {
	if h.ai == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "AI_UNAVAILABLE", "message": "AI 服务未启用"})
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的节点 ID"})
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Enabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_REQUEST", "message": "需要 enabled"})
		return
	}
	if err := h.ai.SetNodeEnabled(c.Request.Context(), id, *body.Enabled, actorIDFromContext(c)); err != nil {
		writeAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// EnqueueAIInfer 入队 AI 推理。
func (h *Handler) EnqueueAIInfer(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	if h.ai == nil || h.tasks == nil || h.taskWorkers == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "AI_UNAVAILABLE", "message": "AI 或任务服务未启用"})
		return
	}
	var req struct {
		MediaID  int64  `json:"media_id"`
		TaskType string `json:"task_type"`
		ModelID  string `json:"model_id"`
		NodeID   string `json:"node_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.MediaID <= 0 || strings.TrimSpace(req.TaskType) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_REQUEST", "message": "需要 media_id 与 task_type"})
		return
	}
	if !ai.ValidTaskType(req.TaskType) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_TASK_TYPE", "message": "task_type 不支持"})
		return
	}
	// 媒体必须对当前 Space 可见
	if _, err := h.library.GetMediaFileByIDInSpace(spaceID, req.MediaID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体不存在"})
		return
	}
	task, err := h.ai.EnqueueInfer(c.Request.Context(), spaceID, req.MediaID, req.TaskType, req.ModelID, req.NodeID, actorIDFromContext(c))
	if err != nil {
		writeAIError(c, err)
		return
	}
	h.taskWorkers.Wake()
	c.JSON(http.StatusAccepted, gin.H{"status": task.Status, "task_id": task.ID})
}

// ListAIResults 列 media 的 AI 结果。
func (h *Handler) ListAIResults(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	if h.ai == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "AI_UNAVAILABLE", "message": "AI 服务未启用"})
		return
	}
	mediaID, err := strconv.ParseInt(strings.TrimSpace(c.Query("media_id")), 10, 64)
	if err != nil || mediaID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_REQUEST", "message": "需要 media_id"})
		return
	}
	items, err := h.ai.ListResults(c.Request.Context(), spaceID, mediaID)
	if err != nil {
		writeAIError(c, err)
		return
	}
	if items == nil {
		items = []models.AIResult{}
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// RebuildAIResults 重建（删除非 manual）结果。
func (h *Handler) RebuildAIResults(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	if h.ai == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "AI_UNAVAILABLE", "message": "AI 服务未启用"})
		return
	}
	var req struct {
		MediaID  int64  `json:"media_id"`
		TaskType string `json:"task_type"`
		BatchID  string `json:"batch_id"`
	}
	_ = c.ShouldBindJSON(&req)
	n, err := h.ai.RebuildResults(c.Request.Context(), spaceID, req.MediaID, req.TaskType, req.BatchID, actorIDFromContext(c))
	if err != nil {
		writeAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": n})
}

// SemanticSearchAI 语义搜索（FR2-012）。
func (h *Handler) SemanticSearchAI(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	if h.ai == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "AI_UNAVAILABLE", "message": "AI 服务未启用"})
		return
	}
	q := strings.TrimSpace(c.Query("q"))
	topK := 10
	if raw := strings.TrimSpace(c.Query("top_k")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			topK = n
		}
	}
	if q == "" {
		var body struct {
			Q    string `json:"q"`
			TopK int    `json:"top_k"`
		}
		_ = c.ShouldBindJSON(&body)
		q = strings.TrimSpace(body.Q)
		if body.TopK > 0 {
			topK = body.TopK
		}
	}
	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_REQUEST", "message": "需要 q"})
		return
	}
	hits, err := h.ai.SemanticSearch(c.Request.Context(), spaceID, q, topK)
	if err != nil {
		writeAIError(c, err)
		return
	}
	if hits == nil {
		hits = []ai.SearchHit{}
	}
	c.JSON(http.StatusOK, gin.H{"items": hits})
}

// ConfirmAIResult 人工确认结果（FR2-012 二切）。
func (h *Handler) ConfirmAIResult(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	if h.ai == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "AI_UNAVAILABLE", "message": "AI 服务未启用"})
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的结果 ID"})
		return
	}
	if err := h.ai.ConfirmResult(c.Request.Context(), spaceID, id, actorIDFromContext(c)); err != nil {
		writeAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// RejectAIResult 驳回并删除非 manual 结果。
func (h *Handler) RejectAIResult(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	if h.ai == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "AI_UNAVAILABLE", "message": "AI 服务未启用"})
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的结果 ID"})
		return
	}
	if err := h.ai.RejectResult(c.Request.Context(), spaceID, id, actorIDFromContext(c)); err != nil {
		writeAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ListAIDuplicates AI 相似候选（FR2-012 二切）。
func (h *Handler) ListAIDuplicates(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	if h.ai == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "AI_UNAVAILABLE", "message": "AI 服务未启用"})
		return
	}
	threshold := 0.92
	if raw := strings.TrimSpace(c.Query("threshold")); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil && v > 0 && v <= 1 {
			threshold = v
		}
	}
	groups, err := h.ai.FindDuplicateCandidates(c.Request.Context(), spaceID, threshold)
	if err != nil {
		writeAIError(c, err)
		return
	}
	if groups == nil {
		groups = []ai.DuplicateGroup{}
	}
	c.JSON(http.StatusOK, gin.H{"items": groups})
}

func writeAIError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ai.ErrAIDisabled):
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "AI_DISABLED", "message": "AI 能力未启用或未配置模型/节点"})
	case errors.Is(err, ai.ErrInvalidInput):
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_REQUEST", "message": err.Error()})
	case errors.Is(err, ai.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "AI 操作失败"})
	}
}
