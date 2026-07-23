package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

type coverGenerateRequest struct {
	Refresh bool `json:"refresh"`
}

type coverSelectRequest struct {
	CandidateID int64 `json:"candidate_id" binding:"required"`
}

type coverCandidateResponse struct {
	models.CoverCandidate
	ImageURL string `json:"image_url"`
}

// GetMediaCovers GET /api/library/media/:id/covers。
func (h *Handler) GetMediaCovers(c *gin.Context) {
	spaceID, mediaID, ok := h.coverRequestScope(c)
	if !ok {
		return
	}
	result, err := h.thumbnail.ListCovers(c.Request.Context(), spaceID, mediaID)
	if err != nil {
		h.writeCoverError(c, err)
		return
	}
	candidates := make([]coverCandidateResponse, 0, len(result.Candidates))
	for _, candidate := range result.Candidates {
		candidates = append(candidates, coverCandidateResponse{
			CoverCandidate: candidate,
			ImageURL:       fmt.Sprintf("/api/library/media/%d/covers/%d/image", mediaID, candidate.ID),
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"cover": result.Cover, "candidates": candidates,
		"cover_url": fmt.Sprintf("/api/library/thumbnail/%d", mediaID),
	})
}

// GenerateMediaCovers POST /api/library/media/:id/covers/generate。
func (h *Handler) GenerateMediaCovers(c *gin.Context) {
	spaceID, mediaID, ok := h.coverRequestScope(c)
	if !ok {
		return
	}
	var request coverGenerateRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_REQUEST", "message": "请求格式无效"})
			return
		}
	}
	result, err := h.thumbnail.GenerateCovers(c.Request.Context(), spaceID, mediaID, request.Refresh)
	if err != nil {
		h.writeCoverError(c, err)
		return
	}
	if h.taskWorkers != nil {
		h.taskWorkers.Wake()
	}
	c.JSON(http.StatusAccepted, gin.H{"status": result.Status, "task_id": result.TaskID})
}

// SelectMediaCover PUT /api/library/media/:id/cover。
func (h *Handler) SelectMediaCover(c *gin.Context) {
	spaceID, mediaID, ok := h.coverRequestScope(c)
	if !ok {
		return
	}
	var request coverSelectRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.CandidateID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_REQUEST", "message": "candidate_id 必须大于 0"})
		return
	}
	selected, err := h.thumbnail.SelectCover(c.Request.Context(), spaceID, mediaID, request.CandidateID)
	if err != nil {
		h.writeCoverError(c, err)
		return
	}
	c.JSON(http.StatusOK, selected)
}

// GetCoverCandidateImage GET /api/library/media/:id/covers/:candidate_id/image。
func (h *Handler) GetCoverCandidateImage(c *gin.Context) {
	spaceID, mediaID, ok := h.coverRequestScope(c)
	if !ok {
		return
	}
	candidateID, err := strconv.ParseInt(c.Param("candidate_id"), 10, 64)
	if err != nil || candidateID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_CANDIDATE_ID", "message": "候选 ID 无效"})
		return
	}
	path, err := h.thumbnail.CoverCandidatePath(c.Request.Context(), spaceID, mediaID, candidateID)
	if err != nil {
		h.writeCoverError(c, err)
		return
	}
	c.File(path)
}

func (h *Handler) coverRequestScope(c *gin.Context) (string, int64, bool) {
	if h.thumbnail == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "COVER_UNAVAILABLE", "message": "封面服务未启用"})
		return "", 0, false
	}
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return "", 0, false
	}
	mediaID, ok := parseMediaID(c)
	if !ok {
		return "", 0, false
	}
	// 读路径：不可见 id → 404（FR2-051）
	if _, err := h.loadMediaForViewer(c, spaceID, mediaID); err != nil {
		h.writeCoverError(c, err)
		return "", 0, false
	}
	return spaceID, mediaID, true
}

func (h *Handler) writeCoverError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) || err.Error() == "封面候选不存在" {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体或封面候选不存在"})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"code": "COVER_ERROR", "message": err.Error()})
}
