package api

import (
	"github.com/gin-gonic/gin"
)

// RegisterRoutes 注册 API 路由。
func RegisterRoutes(r *gin.Engine, h *Handler) {
	lib := r.Group("/api/library")
	{
		lib.GET("/paths", h.ListLibraryPaths)
		lib.POST("/paths", h.CreateLibraryPath)
		lib.DELETE("/paths/:id", h.DeleteLibraryPath)

		lib.GET("/media", h.ListMediaFiles)
		lib.GET("/media/:id", h.GetMediaFile)
		lib.DELETE("/media/:id", h.DeleteMediaFile)

		lib.POST("/scan/:id", h.ScanLibrary)
	}
}
