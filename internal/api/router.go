package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"jianvideo/internal/player"
	"jianvideo/internal/playback"
)

// parseMediaID 解析并校验路由中的 media ID 参数。
// 返回 (id, ok)；解析失败时已写入 400 响应，调用方直接返回即可。
func parseMediaID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return 0, false
	}
	return id, true
}

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

		lib.GET("/browse", h.BrowseDirectory)

		lib.POST("/scan/:id", h.ScanLibrary)
	}

	// 字幕路由（不需要 playback 服务）
	sub := r.Group("/api/play")
	{
		sub.GET("/:id/subtitles", h.GetSubtitles)
		sub.GET("/:id/subtitles/:index", h.GetSubtitleContent)
	}

	// SMB 凭据管理
	smbGroup := r.Group("/api/smb")
	{
		smbGroup.POST("/credentials", h.SaveSMBCredentials)
	}

	// 播放路由（可选）
	if len(pbSvc) > 0 && pbSvc[0] != nil {
		svc := pbSvc[0]
		play := r.Group("/api/play")
		{
			play.GET("/:id/stream", func(c *gin.Context) {
				id, ok := parseMediaID(c)
				if !ok {
					return
				}
				svc.StreamFile(c.Writer, c.Request, id, "", 0, 0)
			})
			play.POST("/:id/seek", func(c *gin.Context) {
				id, ok := parseMediaID(c)
				if !ok {
					return
				}
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
				id, ok := parseMediaID(c)
				if !ok {
					return
				}
				progress, err := svc.GetProgress(id)
				if err != nil {
					c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": err.Error()})
					return
				}
				c.JSON(http.StatusOK, progress)
			})
			play.POST("/:id/buffer", func(c *gin.Context) {
				id, ok := parseMediaID(c)
				if !ok {
					return
				}
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
		// master.m3u8 — ABR 多码率索引
		hls.GET("/:id/master.m3u8", func(c *gin.Context) {
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
				return
			}
			content, err := hlsMgr.GetMasterM3U8(id)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": err.Error()})
				return
			}
			c.Data(http.StatusOK, "application/vnd.apple.mpegurl", []byte(content))
		})
		hls.GET("/:id/:quality.m3u8", func(c *gin.Context) {
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
				return
			}
			quality := c.Param("quality")
			content, err := hlsMgr.GetM3U8(id, quality)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": err.Error()})
				return
			}
			c.Data(http.StatusOK, "application/vnd.apple.mpegurl", []byte(content))
		})
		hls.GET("/:id/segment/:segment", func(c *gin.Context) {
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
				return
			}
			segment := c.Param("segment")
			if !strings.HasSuffix(segment, ".ts") || strings.Contains(segment, "..") || strings.Contains(segment, "/") || strings.Contains(segment, `\`) {
				c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_SEGMENT", "message": "无效的切片名称"})
				return
			}
			// 从切片文件名解析码率档位（格式: {quality}_segment_xxx.ts）
			quality := player.ExtractQualityFromSegment(segment)
			data, err := hlsMgr.GetSegment(id, quality, segment)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": err.Error()})
				return
			}
			c.Data(http.StatusOK, "video/mp2t", data)
		})
	}
}

