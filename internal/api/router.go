package api

import (
	"database/sql"
	"net/http"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"jianvideo/internal/db/models"
	"jianvideo/internal/playback"
)

// RegisterRoutes 注册 API 路由。
// pbSvc 可选：传入时注册播放相关路由，不传入时跳过。
func RegisterRoutes(r *gin.Engine, h *Handler, pbSvc ...*playback.Service) {
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

	// 播放路由（可选）
	if len(pbSvc) > 0 && pbSvc[0] != nil {
		svc := pbSvc[0]
		play := r.Group("/api/play")
		{
			play.GET("/:id/stream", func(c *gin.Context) {
				id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
				svc.StreamFile(c.Writer, c.Request, id, "", 0, 0)
			})
			play.POST("/:id/seek", func(c *gin.Context) {
				id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
				var req struct {
					Position float64 `json:"position"`
				}
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "请求参数错误"})
					return
				}
				resp, err := svc.HandleSeek(id, req.Position)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"code": "SEEK_FAILED", "message": err.Error()})
					return
				}
				c.JSON(http.StatusOK, resp)
			})
			play.GET("/:id/progress", func(c *gin.Context) {
				id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
				progress, err := svc.GetProgress(id)
				if err != nil {
					c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": err.Error()})
					return
				}
				c.JSON(http.StatusOK, progress)
			})
			play.POST("/:id/buffer", func(c *gin.Context) {
				id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
				var report playback.BufferReport
				if err := c.ShouldBindJSON(&report); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "请求参数错误"})
					return
				}
				svc.HandleBufferReport(id, report)
				c.Status(http.StatusOK)
			})
		}
	}
}

// setupTestDB 创建测试用的内存数据库。
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}
	// 自动迁移
	db.AutoMigrate(
		&models.LibraryPath{},
		&models.MediaFile{},
		&models.User{},
		&models.PlaybackSession{},
	)
	return db
}

// Ensure sql is used (prevent unused import).
var _ *sql.DB
