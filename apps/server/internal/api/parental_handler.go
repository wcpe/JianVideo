package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"errors"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/auth"
	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/space"
)

// UpdateMediaContentRating PUT /api/library/media/:id/content-rating（owner/editor）。
// 请求体：{ "content_rating": "PG-13" }；空串清除为未分级。
func (h *Handler) UpdateMediaContentRating(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}
	var req struct {
		ContentRating string `json:"content_rating"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": "请求体无效"})
		return
	}
	if !models.ValidContentRating(req.ContentRating) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_RATING", "message": "非法内容分级"})
		return
	}
	before, _ := h.library.GetMediaFileByIDInSpace(spaceID, id)
	if err := h.library.UpdateMediaContentRatingInSpace(spaceID, id, req.ContentRating); err != nil {
		if errorsIsNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"code": "UPDATE_FAILED", "message": err.Error()})
		return
	}
	if h.audit != nil {
		_ = h.audit.Record(c.Request.Context(), audit.EventInput{
			Scope:        audit.ScopeSpace,
			SpaceID:      spaceID,
			ActorType:    "user",
			ActorID:      actorIDFromContext(c),
			Action:       "media.content_rating_updated",
			ResourceType: "media",
			ResourceID:   fmt.Sprintf("%d", id),
			Before:       map[string]any{"content_rating": mediaRatingOf(before)},
			After:        map[string]any{"content_rating": models.NormalizeContentRating(req.ContentRating)},
		})
	}
	c.Status(http.StatusNoContent)
}

// UpdateSpaceParentalPolicy PUT /api/spaces/:id/parental（owner；需 password）。
// 请求体：{ "password": "...", "default_max_rating": "PG-13" }
func (h *Handler) UpdateSpaceParentalPolicy(c *gin.Context) {
	if h.space == nil || h.auth == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SPACE_UNAVAILABLE", "message": "Space 服务未启用"})
		return
	}
	spaceID := strings.TrimSpace(c.Param("id"))
	userID, ok := h.currentUserID(c)
	if !ok {
		return
	}
	if err := h.space.RequireRole(spaceID, int64(userID), models.SpaceRoleOwner); err != nil {
		writeSpaceErr(c, err)
		return
	}
	var req struct {
		Password         string `json:"password"`
		DefaultMaxRating string `json:"default_max_rating"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": "请求体无效"})
		return
	}
	username, _ := c.Get("username")
	name, _ := username.(string)
	if err := h.auth.VerifyPassword(name, req.Password); err != nil {
		if err == auth.ErrCurrentPasswordWrong {
			c.JSON(http.StatusUnauthorized, gin.H{"code": "PASSWORD_REQUIRED", "message": "密码确认失败"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"code": "PASSWORD_REQUIRED", "message": "密码确认失败"})
		return
	}
	before, _ := h.space.GetSpace(spaceID)
	if err := h.space.SetSpaceDefaultMaxRating(spaceID, req.DefaultMaxRating); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "UPDATE_FAILED", "message": err.Error()})
		return
	}
	if h.audit != nil {
		_ = h.audit.Record(c.Request.Context(), audit.EventInput{
			Scope:        audit.ScopeSpace,
			SpaceID:      spaceID,
			ActorType:    "user",
			ActorID:      actorIDFromContext(c),
			Action:       "space.parental_updated",
			ResourceType: "space",
			ResourceID:   spaceID,
			Before:       map[string]any{"default_max_rating": spaceDefaultRating(before)},
			After:        map[string]any{"default_max_rating": models.NormalizeContentRating(req.DefaultMaxRating)},
		})
	}
	c.Status(http.StatusNoContent)
}

// UpdateMemberMaxRating PUT /api/spaces/:id/members/:user_id/max-rating（owner；需 password）。
// 请求体：{ "password": "...", "max_rating": "PG" }；max_rating 空串清除覆盖。
func (h *Handler) UpdateMemberMaxRating(c *gin.Context) {
	if h.space == nil || h.auth == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SPACE_UNAVAILABLE", "message": "Space 服务未启用"})
		return
	}
	spaceID := strings.TrimSpace(c.Param("id"))
	memberID, err := strconv.ParseInt(c.Param("user_id"), 10, 64)
	if err != nil || memberID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的用户 ID"})
		return
	}
	userID, ok := h.currentUserID(c)
	if !ok {
		return
	}
	if err := h.space.RequireRole(spaceID, int64(userID), models.SpaceRoleOwner); err != nil {
		writeSpaceErr(c, err)
		return
	}
	var req struct {
		Password  string  `json:"password"`
		MaxRating *string `json:"max_rating"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": "请求体无效"})
		return
	}
	username, _ := c.Get("username")
	name, _ := username.(string)
	if err := h.auth.VerifyPassword(name, req.Password); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": "PASSWORD_REQUIRED", "message": "密码确认失败"})
		return
	}
	// 允许 max_rating 缺省为清除
	var ratingPtr *string
	if req.MaxRating != nil {
		ratingPtr = req.MaxRating
	} else {
		empty := ""
		ratingPtr = &empty
	}
	if err := h.space.SetMemberMaxRating(spaceID, memberID, ratingPtr); err != nil {
		if err == space.ErrNotMember {
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "成员不存在"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"code": "UPDATE_FAILED", "message": err.Error()})
		return
	}
	if h.audit != nil {
		after := any(nil)
		if ratingPtr != nil && strings.TrimSpace(*ratingPtr) != "" {
			after = models.NormalizeContentRating(*ratingPtr)
		}
		_ = h.audit.Record(c.Request.Context(), audit.EventInput{
			Scope:        audit.ScopeSpace,
			SpaceID:      spaceID,
			ActorType:    "user",
			ActorID:      actorIDFromContext(c),
			Action:       "space.member_max_rating_updated",
			ResourceType: "space_member",
			ResourceID:   fmt.Sprintf("%s:%d", spaceID, memberID),
			After:        map[string]any{"user_id": memberID, "max_rating": after},
		})
	}
	c.Status(http.StatusNoContent)
}

func mediaRatingOf(mf *models.MediaFile) string {
	if mf == nil {
		return ""
	}
	return mf.ContentRating
}

func spaceDefaultRating(sp *models.Space) string {
	if sp == nil {
		return ""
	}
	return sp.DefaultMaxRating
}

func errorsIsNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}
