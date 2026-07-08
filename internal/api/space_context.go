package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/db/models"
)

const spaceHeader = "X-JianVideo-Space-Id"

func (h *Handler) resolveSpaceID(c *gin.Context) (string, bool) {
	spaceID := strings.TrimSpace(c.GetHeader(spaceHeader))
	if spaceID == "" {
		return models.DefaultSpaceID, true
	}
	if !validSpaceID(spaceID) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_SPACE", "message": "Space ID 不合法"})
		return "", false
	}
	exists, err := h.library.SpaceExists(spaceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "校验 Space 失败"})
		return "", false
	}
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"code": "SPACE_NOT_FOUND", "message": "Space 不存在"})
		return "", false
	}
	return spaceID, true
}

func validSpaceID(spaceID string) bool {
	if spaceID == "" || len(spaceID) > 128 {
		return false
	}
	for _, ch := range spaceID {
		if ch >= 'a' && ch <= 'z' {
			continue
		}
		if ch >= 'A' && ch <= 'Z' {
			continue
		}
		if ch >= '0' && ch <= '9' {
			continue
		}
		if ch == '-' || ch == '_' || ch == '.' {
			continue
		}
		return false
	}
	return true
}
