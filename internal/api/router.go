package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/playback"
	"github.com/wcpe/JianVideo/internal/player"
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
// pbSvc 可选：传入时同时注册播放相关路由（流式 / Seek / 进度 / 缓冲）。
// hlsMgr 可选：传入时注册 HLS 切片路由。
func RegisterRoutes(r *gin.Engine, h *Handler, pbSvc ...*playback.Service) {
	lib := r.Group("/api/library")
	{
		lib.GET("/paths", h.ListLibraryPaths)
		lib.POST("/paths", h.CreateLibraryPath)
		lib.DELETE("/paths/:id", h.DeleteLibraryPath)

		lib.GET("/media", h.ListMediaFiles)
		lib.GET("/media/:id", h.GetMediaFile)
		lib.GET("/media/:id/raw", h.GetRawImage)
		lib.GET("/media/:id/download", h.DownloadMediaFile)
		lib.PUT("/media/:id/rename", h.RenameMediaFile)
		lib.PUT("/media/:id/display-name", h.UpdateDisplayName)
		lib.DELETE("/media/:id", h.DeleteMediaFile)

		// 软删除与回收站（FR-25）：列出已软删项、还原
		lib.GET("/recycle", h.ListRecycleMediaFiles)
		lib.POST("/media/:id/restore", h.RestoreMediaFile)

		// 回收站清理（FR-26）：源文件按盘符回收站路径移动并删记录
		lib.POST("/recycle/cleanup", h.RecycleCleanup)

		// 收藏与标签（FR-41）
		lib.PUT("/media/:id/favorite", h.SetMediaFavorite)
		lib.GET("/media/:id/tags", h.ListMediaTags)
		lib.POST("/media/:id/tags", h.AddMediaTag)
		lib.DELETE("/media/:id/tags/:tag_id", h.RemoveMediaTag)
		lib.GET("/tags", h.ListTags)
		lib.POST("/tags", h.CreateTag)

		lib.GET("/extensions", h.ListMediaExtensions)
		lib.POST("/extensions", h.AddMediaExtension)
		lib.DELETE("/extensions", h.DeleteMediaExtension)

		lib.GET("/browse", h.BrowseDirectory)

		// 继续观看（FR-44）：有进度且未看完的媒体列表
		lib.GET("/continue-watching", h.ContinueWatching)

		lib.GET("/thumbnail/:id", h.GetThumbnail)

		lib.POST("/scan/:id", h.ScanLibrary)
		lib.GET("/scan/progress", h.ScanProgressSSE)
		// 扫描任务队列（FR-29）：列任务与当前进行中任务
		lib.GET("/scan/tasks", h.ListScanTasks)
	}

	// 相册（FR-40）：相册增删、成员增删与浏览
	albums := r.Group("/api/albums")
	{
		albums.GET("", h.ListAlbums)
		albums.POST("", h.CreateAlbum)
		albums.DELETE("/:id", h.DeleteAlbum)

		albums.GET("/:id/items", h.ListAlbumItems)
		albums.POST("/:id/items", h.AddAlbumItem)
		albums.DELETE("/:id/items/:mediaId", h.RemoveAlbumItem)
	}

	// 分享链接管理（FR-43，鉴权后）：创建 / 列出 / 撤销分享。
	// 公开免登访问端点 /api/share/:token 由 web 层 RegisterShareRoutes 单独注册（需 playback 服务）。
	shares := r.Group("/api/shares")
	{
		shares.POST("", h.CreateShare)
		shares.GET("", h.ListShares)
		shares.DELETE("/:token", h.RevokeShare)
	}

	// 字幕与观看状态路由（不需要 playback 服务，作用于 media_files）
	sub := r.Group("/api/play")
	{
		sub.GET("/:id/subtitles", h.GetSubtitles)
		sub.GET("/:id/subtitles/:index", h.GetSubtitleContent)

		// 续播与观看状态（FR-44）：用户观看位置，区别于 playback 的转码/缓冲进度
		sub.PUT("/:id/position", h.UpdateWatchPosition)
		sub.PUT("/:id/watched", h.MarkWatched)

		// 端到端编码协商（FR-53）：客户端上报能力，后端协商实际编码与播放路径
		sub.POST("/:id/negotiate", h.Negotiate)
	}

	// SMB 凭据管理
	smbGroup := r.Group("/api/smb")
	{
		smbGroup.POST("/credentials", h.SaveSMBCredentials)
	}

	// 硬件加速能力查询：读编码器实测缓存派生，与 /api/system/info 同源（FR-49）
	r.GET("/api/transcode/hwaccel", h.HWAccel)

	// 系统诊断（FR-21）：系统信息查询与编解码器实测
	sys := r.Group("/api/system")
	{
		sys.GET("/info", h.SystemInfo)
		sys.POST("/codec-test", h.CodecTest)
		// 环境变量查看（FR-56，只读，敏感脱敏）
		sys.GET("/env", h.SystemEnv)
		// FFmpeg 路径检测（FR-56）：保存前先验路径是否可用
		sys.POST("/ffmpeg/detect", h.DetectFFmpeg)
		// 远程自更新（FR-46）：检测 / 应用 / 回滚
		sys.GET("/update/check", h.CheckUpdate)
		sys.POST("/update/apply", h.ApplyUpdate)
		sys.POST("/update/rollback", h.RollbackUpdate)
	}

	// 运行期设置（FR-24）：键值读写
	settingsGroup := r.Group("/api/settings")
	{
		settingsGroup.GET("", h.GetSettings)
		settingsGroup.PUT("", h.UpdateSettings)
	}

	// 播放路由（可选）
	if len(pbSvc) > 0 && pbSvc[0] != nil {
		RegisterPlaybackRoutes(r, pbSvc[0])
	}
}

// serveHLSSegment 统一处理 HLS 切片请求（兼容 /:id/segment/:segment 与 /:id/:name.ts 两种路径）。
func serveHLSSegment(c *gin.Context, hlsMgr *player.HLSManager) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}
	// 优先取 segment 参数；若为空则从 name 参数取（去掉 .ts 后缀）
	segment := c.Param("segment")
	if segment == "" {
		segment = strings.TrimSuffix(c.Param("name"), ".ts")
	}
	if segment == "" || strings.Contains(segment, "..") || strings.Contains(segment, "/") || strings.Contains(segment, `\`) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_SEGMENT", "message": "无效的切片名称"})
		return
	}
	quality := player.ExtractQualityFromSegment(segment)
	data, err := hlsMgr.GetSegment(id, quality, segment)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": err.Error()})
		return
	}
	c.Data(http.StatusOK, "video/mp2t", data)
}

// RegisterPlaybackRoutes 仅注册播放相关路由（流式 / Seek / 进度 / 缓冲）。
// 拆分出来便于在已经走过 RegisterRoutes 的引擎上单独补挂，避免重复注册。
func RegisterPlaybackRoutes(r *gin.Engine, pbSvc *playback.Service) {
	play := r.Group("/api/play")
	{
		play.GET("/:id/stream", func(c *gin.Context) {
			id, ok := parseMediaID(c)
			if !ok {
				return
			}
			pbSvc.StreamFile(c.Writer, c.Request, id, "", 0, 0)
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
			resp, err := pbSvc.HandleSeek(id, req.Position)
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
			progress, err := pbSvc.GetProgress(id)
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
			pbSvc.HandleBufferReport(id, report)
			c.Status(http.StatusOK)
		})
	}
}

// RegisterHLSRoutes 注册 HLS 切片和 m3u8 路由。
//
// 采用单条通配路由 + handler 内部分发的方式，避免 gin 路由树冲突。
// master 走动态读取（HLSManager），其余路径（m3u8、ts）走 hlsDir 静态文件。
//
// URL 格式：
//
//	/api/play/hls/{mediaID}/master            → master playlist（动态）
//	/api/play/hls/{mediaID}/{quality}.m3u8    → 单码率 m3u8（静态文件）
//	/api/play/hls/{mediaID}/{quality}_segment_NNN.ts → TS 切片（静态文件）
//
// master 内容里的 playlist 路径写 "{quality}.m3u8"（与 master 同目录），
// hls.js 拼出的 URL = /api/play/hls/{mediaID}/{quality}.m3u8 → 正好匹配静态文件。
func RegisterHLSRoutes(r *gin.Engine, hlsMgr *player.HLSManager, hlsDir string) {
	r.GET("/api/play/hls/*path", func(c *gin.Context) {
		relPath := c.Param("path")
		// 去掉前导 /
		if strings.HasPrefix(relPath, "/") {
			relPath = relPath[1:]
		}

		// 提取 mediaID（第一段）
		parts := strings.SplitN(relPath, "/", 2)
		if len(parts) == 0 || parts[0] == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PATH", "message": "无效的路径"})
			return
		}
		mediaID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
			return
		}
		rest := ""
		if len(parts) > 1 {
			rest = parts[1]
		}

		// master playlist 走动态读取
		if rest == "master" || rest == "master.m3u8" {
			content, err := hlsMgr.GetMasterM3U8(mediaID)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": err.Error()})
				return
			}
			c.Data(http.StatusOK, "application/vnd.apple.mpegurl", []byte(content))
			return
		}

		// 其余路径走静态文件服务
		fullPath := filepath.Join(hlsDir, relPath)
		if _, err := os.Stat(fullPath); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "文件不存在"})
			return
		}
		c.Header("Content-Type", detectHLSMimeType(relPath))
		c.File(fullPath)
	})
}

// detectHLSMimeType 根据 HLS 文件路径返回对应的 Content-Type。
// 识别 TS（H.264 路径）与 fMP4/CMAF（FR-51 高级编码路径）两类产物。
func detectHLSMimeType(path string) string {
	switch {
	case strings.HasSuffix(path, ".m3u8"):
		return "application/vnd.apple.mpegurl"
	case strings.HasSuffix(path, ".ts"):
		return "video/mp2t"
	case strings.HasSuffix(path, ".m4s"):
		// fMP4/CMAF media segment（moof+mdat）
		return "video/iso.segment"
	case strings.HasSuffix(path, ".mp4"):
		// fMP4/CMAF init segment（含 moov）
		return "video/mp4"
	default:
		return "application/octet-stream"
	}
}
