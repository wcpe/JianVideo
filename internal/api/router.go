package api

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"jianvideo/internal/db/models"
	"jianvideo/internal/player"
	"jianvideo/internal/playback"
)

// RegisterRoutes 注册 API 路由。
// pbSvc 可选：传入时注册播放相关路由。
// hlsMgr 可选：传入时注册 HLS 切片路由。
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

// RegisterHLSRoutes 注册 HLS 切片和 m3u8 路由。
func RegisterHLSRoutes(r *gin.Engine, hlsMgr *player.HLSManager) {
	hls := r.Group("/api/play/hls")
	{
		hls.GET("/:id/index.m3u8", func(c *gin.Context) {
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
				return
			}
			content, err := hlsMgr.GetM3U8(id)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": err.Error()})
				return
			}
			c.Data(http.StatusOK, "application/vnd.apple.mpegurl", []byte(content))
		})
		hls.GET("/:id/:segment", func(c *gin.Context) {
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
				return
			}
			segment := c.Param("segment")
			if !strings.HasSuffix(segment, ".ts") {
				c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_SEGMENT", "message": "仅支持 .ts 切片"})
				return
			}
			data, err := hlsMgr.GetSegment(id, segment)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": err.Error()})
				return
			}
			c.Data(http.StatusOK, "video/mp2t", data)
		})
	}
}

// setupTestDB 创建测试用的内存数据库。
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}
	db.AutoMigrate(
		&models.LibraryPath{},
		&models.MediaFile{},
		&models.User{},
		&models.PlaybackSession{},
	)
	return db
}
