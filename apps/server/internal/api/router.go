package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/auth"
	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/playback"
	"github.com/wcpe/JianVideo/internal/player"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
	"github.com/wcpe/JianVideo/internal/transcoder"
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
	mediaTypes := r.Group("/api/media-types")
	{
		mediaTypes.GET("", h.ListMediaTypes)
		mediaTypes.GET("/rules", h.ListMediaTypes)
		mediaTypes.POST("/rules", h.CreateMediaTypeRule)
		mediaTypes.PUT("/rules/:id", h.UpdateMediaTypeRule)
		mediaTypes.DELETE("/rules/:id", h.DeleteMediaTypeRule)
	}

	lib := r.Group("/api/library")
	{
		lib.GET("/paths", h.ListLibraryPaths)
		lib.POST("/paths", h.CreateLibraryPath)
		lib.PUT("/paths/:id", h.UpdateLibraryPath)
		lib.DELETE("/paths/:id", h.DeleteLibraryPath)
		lib.GET("/kinds", h.ListLibraryKinds)

		lib.GET("/media", h.ListMediaFiles)
		lib.GET("/media/:id", h.GetMediaFile)
		// 内容分级（FR2-051）
		lib.PUT("/media/:id/content-rating", h.UpdateMediaContentRating)
		lib.GET("/media/:id/covers", h.GetMediaCovers)
		lib.POST("/media/:id/covers/generate", h.GenerateMediaCovers)
		lib.PUT("/media/:id/cover", h.SelectMediaCover)
		lib.GET("/media/:id/covers/:candidate_id/image", h.GetCoverCandidateImage)
		lib.GET("/media/:id/metadata", h.GetMediaMetadata)
		lib.POST("/media/:id/metadata/refresh", h.RefreshMediaMetadata)
		lib.GET("/media/:id/chapters", h.GetMediaChapters)
		lib.GET("/media/:id/bookmarks", h.ListMediaBookmarks)
		lib.POST("/media/:id/bookmarks", h.CreateMediaBookmark)
		lib.PUT("/media/:id/bookmarks/:bookmark_id", h.UpdateMediaBookmark)
		lib.DELETE("/media/:id/bookmarks/:bookmark_id", h.DeleteMediaBookmark)
		lib.GET("/media/:id/inference", h.GetMediaInference)
		lib.PUT("/media/:id/inference", h.UpdateMediaInference)
		// 剧集下一集（FR2-047）：同 Space 同 title 按季/集定位
		lib.GET("/media/:id/next-episode", h.GetNextEpisode)
		lib.GET("/media/:id/raw", h.GetRawImage)
		lib.POST("/media/:id/image-export", h.ImageExportMedia)
		lib.POST("/media/:id/clip-export", h.ClipExportMedia)
		lib.GET("/exports/:task_id/download", h.DownloadExportArtifact)
		lib.GET("/media/:id/download", h.DownloadMediaFile)
		lib.PUT("/media/:id/rename", h.RenameMediaFile)
		lib.PUT("/media/:id/move", h.MoveMediaFile)
		lib.POST("/media/:id/metadata/writeback", h.WritebackMediaMetadata)
		lib.PUT("/media/:id/display-name", h.UpdateDisplayName)
		lib.PUT("/media/:id/notes", h.UpdateMediaNotes)
		lib.DELETE("/media/:id", h.DeleteMediaFile)

		// 批量软删（FR-69）：单事务对多个 media_id 复用软删，进回收站
		lib.POST("/media/batch-delete", h.BatchDeleteMediaFiles)
		// 批量转码 / 索引层移动（FR2-053）
		lib.POST("/media/batch-transcode", h.BatchTranscodeMediaFiles)
		lib.POST("/media/batch-move", h.BatchMoveMediaFiles)
		lib.POST("/inference/backfill", h.BackfillMediaInferences)
		lib.POST("/metadata/backfill", h.BackfillMediaMetadata)

		// 批量打包下载（FR-91）：将选中媒体原文件流式打包为 zip 附件，smb 项跳过
		lib.GET("/media/batch-download", h.BatchDownloadMediaFiles)

		// 感知哈希去重（FR-70）：扫描计算缺失 dHash + 查询重复组
		lib.POST("/duplicates/scan", h.ScanDuplicates)
		lib.GET("/duplicates", h.ListDuplicates)
		lib.POST("/file-hashes/backfill", h.BackfillFileHashes)
		lib.GET("/duplicates/exact", h.ListExactDuplicates)

		// 软删除与回收站（FR-25）：列出已软删项、还原
		lib.GET("/recycle", h.ListRecycleMediaFiles)
		lib.POST("/media/:id/restore", h.RestoreMediaFile)

		// 回收站清理（FR-26）：源文件按盘符回收站路径移动并删记录
		lib.POST("/recycle/cleanup", h.RecycleCleanup)
		// 回收站到期自动清理（FR2-054）：preview 不改数据；run 有界实跑
		lib.POST("/recycle/auto-cleanup/preview", h.RecycleAutoCleanupPreview)
		lib.POST("/recycle/auto-cleanup/run", h.RecycleAutoCleanupRun)

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

		// 观看历史与继续观看（FR2-045）：统一读取 watch_states 真源
		lib.GET("/watch-history", h.WatchHistory)
		lib.GET("/continue-watching", h.ContinueWatching)

		// 那年今日（FR-72）：往年同一天拍摄的媒体回忆列表
		lib.GET("/on-this-day", h.OnThisDay)

		// 最近查看（FR-120）：记录媒体被打开的时刻 + 按最近打开倒序的回忆列表
		lib.PUT("/media/:id/viewed", h.MarkMediaViewed)
		lib.GET("/recently-viewed", h.RecentlyViewed)

		// 观看热力与统计（FR-75）：已看/未看、最近观看时间线、续播位置热力、各库/类型分布、观看次数 Top N
		lib.GET("/stats", h.WatchStats)

		// 媒体库总量聚合（FR-117）：媒体总数、视频/图片拆分、占用空间、总时长、启用库数与各库分组，供首页概览看板
		lib.GET("/summary", h.LibrarySummary)

		// 媒体增长趋势（FR-118）：按 added_at 本地时区天分桶的新增 count/SUM(size)/SUM(duration) 全时段序列，供统计页媒体增长曲线
		lib.GET("/trends", h.MediaTrends)

		lib.GET("/thumbnail/:id", h.GetThumbnail)
		lib.POST("/thumbnails/backfill", h.BackfillThumbnails)

		// Web 上传入库（FR-149，见 ADR-0051）：multipart 接收图片/视频，落盘到库内目标位置后触发增量扫描
		lib.POST("/upload", h.UploadMedia)

		lib.POST("/scan/:id", h.ScanLibrary)
		lib.GET("/scan/progress", h.ScanProgressSSE)
		// 扫描任务队列（FR-29）：列任务与当前进行中任务
		lib.GET("/scan/tasks", h.ListScanTasks)
		lib.POST("/scan/tasks/:id/cancel", h.CancelScanTask)
		lib.POST("/scan/tasks/:id/retry", h.RetryScanTask)

		// 媒体健康巡检（FR-73）：触发后台巡检、查进度、列问题清单
		lib.POST("/health/scan", h.StartHealthScan)
		lib.GET("/health/status", h.HealthStatus)
		lib.GET("/health/issues", h.HealthIssues)
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
		// 合集顺序邻项（FR2-047）：上一首/下一首
		albums.GET("/:id/neighbor", h.GetAlbumNeighbor)
	}

	// 分享链接管理（FR-43，鉴权后）：创建 / 列出 / 撤销分享。
	// 公开免登访问端点 /api/share/:token 由 web 层 RegisterShareRoutes 单独注册（需 playback 服务）。
	shares := r.Group("/api/shares")
	{
		shares.POST("", h.CreateShare)
		shares.GET("", h.ListShares)
		shares.DELETE("/:token", h.RevokeShare)
	}

	// 用户与 Space 成员（FR2-010）；会话全撤（FR2-062 后置）
	users := r.Group("/api/users")
	{
		users.GET("", h.ListUsers)
		users.POST("", h.CreateUser)
		users.PUT("/:id/status", h.SetUserStatus)
		users.DELETE("/:id/sessions", h.RevokeUserSessions)
	}
	spaces := r.Group("/api/spaces")
	{
		spaces.GET("", h.ListSpaces)
		spaces.POST("", h.CreateSpace)
		// 读成员：handler 内 RequireRole(viewer)
		spaces.GET("/:id/members", h.ListSpaceMembers)

		// owner-only：挂 RequireSpaceRole；handler 内再 RequireRole 作双保险（FR2-010 后置）。
		// /api/spaces 不在全局 SpaceOwnerGuard 前缀内，须在此用 path :id 解析 Space。
		if h != nil && h.auth != nil {
			ownerOnly := spaces.Group("", auth.RequireSpaceRole(h.auth, models.SpaceRoleOwner))
			{
				ownerOnly.POST("/:id/members", h.AddSpaceMember)
				ownerOnly.DELETE("/:id/members/:user_id", h.RemoveSpaceMember)
				ownerOnly.POST("/:id/transfer-owner", h.TransferSpaceOwner)
				// 家长控制（FR2-051）
				ownerOnly.PUT("/:id/parental", h.UpdateSpaceParentalPolicy)
				ownerOnly.PUT("/:id/members/:user_id/max-rating", h.UpdateMemberMaxRating)
			}
		} else {
			// 测试装配可能未注入 auth：回退无中间件（handler 内仍校验）。
			spaces.POST("/:id/members", h.AddSpaceMember)
			spaces.DELETE("/:id/members/:user_id", h.RemoveSpaceMember)
			spaces.POST("/:id/transfer-owner", h.TransferSpaceOwner)
			spaces.PUT("/:id/parental", h.UpdateSpaceParentalPolicy)
			spaces.PUT("/:id/members/:user_id/max-rating", h.UpdateMemberMaxRating)
		}
	}

	// 字幕与观看状态路由（不需要 playback 服务，作用于 media_files）
	sub := r.Group("/api/play")
	{
		sub.GET("/:id/tracks", h.GetTracks)
		sub.POST("/:id/audio-reload", h.CreateAudioReload)
		sub.GET("/:id/subtitles", h.GetSubtitles)
		sub.POST("/:id/subtitles", h.UploadSubtitle)
		sub.GET("/:id/subtitles/:track_id", h.GetSubtitleContent)
		sub.GET("/:id/subtitles/:track_id/content", h.GetSubtitleTrackContent)
		sub.DELETE("/:id/subtitles/:track_id", h.DeleteSubtitleTrack)

		// 观看状态真源（FR2-045）与旧端点兼容适配
		sub.GET("/:id/watch-state", h.GetWatchState)
		sub.PUT("/:id/watch-state", h.UpdateWatchState)
		sub.PUT("/:id/position", h.UpdateWatchPosition)
		sub.PUT("/:id/watched", h.MarkWatched)

		// HLS 状态与显式 ABR 生成：状态查询不入队，高成本任务仅由 POST 创建。
		sub.GET("/:id/hls-status", h.HLSStatus)
		sub.POST("/:id/hls-abr", h.CreateHLSABR)

		// 时间轴预览：缺失查询幂等入队，重建显式创建新 generation，资源按完整身份读取。
		sub.GET("/:id/timeline-preview", h.GetTimelinePreview)
		sub.POST("/:id/timeline-preview/rebuild", h.RebuildTimelinePreview)
		sub.GET("/:id/timeline-preview/resources/:profile/:fingerprint/:generation/:resource", h.GetTimelinePreviewResource)

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

	// 转码预设与预生成队列（FR-77）：预设 CRUD + 入预生成队列 + 列任务
	transcode := r.Group("/api/transcode")
	{
		transcode.GET("/presets", h.ListTranscodePresets)
		transcode.POST("/presets", h.CreateTranscodePreset)
		transcode.PUT("/presets/:id", h.UpdateTranscodePreset)
		transcode.DELETE("/presets/:id", h.DeleteTranscodePreset)
		transcode.POST("/tasks", h.CreateTranscodeTask)
		transcode.GET("/tasks", h.ListTranscodeTasks)
	}

	// 系统诊断（FR-21）：系统信息查询与编解码器实测
	sys := r.Group("/api/system")
	{
		sys.GET("/info", h.SystemInfo)
		// 系统指标时序与当前快照（FR-119）：下采样 points + current，供监控页折线与当前值卡
		sys.GET("/metrics", h.SystemMetrics)
		sys.POST("/codec-test", h.CodecTest)
		sys.POST("/cache/clean", h.CleanSystemCache)
		// 环境变量查看（FR-56，只读，敏感脱敏）
		sys.GET("/env", h.SystemEnv)
		// FFmpeg 路径检测（FR-56）：保存前先验路径是否可用
		sys.POST("/ffmpeg/detect", h.DetectFFmpeg)
		// 代理连通性测试（FR-89）：保存前先验代理是否可达，用临时 client、不污染运行期代理
		sys.POST("/proxy/test", h.TestProxy)
		// 外部工具下载（FR2-022）：源列表、安装状态与下载入队
		sys.GET("/tools", h.ListTools)
		sys.GET("/tools/sources", h.ListToolSources)
		sys.POST("/tools/download", h.DownloadTool)
		// 远程自更新（FR-46）：检测 / 应用 / 回滚
		sys.GET("/update/check", h.CheckUpdate)
		sys.POST("/update/apply", h.ApplyUpdate)
		sys.POST("/update/rollback", h.RollbackUpdate)
		// 自更新下载进度轮询（FR-90）
		sys.GET("/update/progress", h.UpdateProgress)
	}

	// 运行期设置（FR-24）：键值读写
	settingsGroup := r.Group("/api/settings")
	{
		settingsGroup.GET("/storage", h.GetStorageSettings)
		settingsGroup.GET("/definitions", h.GetSettingDefinitions)
		settingsGroup.GET("", h.GetSettings)
		settingsGroup.PUT("", h.UpdateSettings)
	}

	// 审计事件查询（FR2-040）：Space scoped 默认隔离，scope=system 查询系统级事件。
	auditGroup := r.Group("/api/audit")
	{
		auditGroup.GET("/events", h.ListAuditEvents)
	}

	// 操作可回滚中心（FR2-041）
	rb := r.Group("/api/rollback")
	{
		rb.GET("/events", h.ListRollbackEvents)
		rb.POST("/apply", h.ApplyRollback)
	}

	// 通用异步任务中心（FR2-037）：新状态为 pending/running/succeeded/failed/canceled。
	tasks := r.Group("/api/tasks")
	{
		tasks.GET("", h.ListTasks)
		tasks.GET("/stats", h.TaskStats)
		tasks.GET("/:id", h.GetTask)
		tasks.POST("/:id/cancel", h.CancelTask)
		tasks.POST("/:id/retry", h.RetryTask)
	}

	// v2 契约表面（FR2-071）：与 media-client/mock 对齐；单挂路由，不 RegisterHandlers。
	v2 := r.Group("/api/v2")
	{
		v2.GET("/media", h.ListMediaV2)
		v2.GET("/media/:id", h.GetMediaV2)
		v2.GET("/tasks/:id", h.GetTaskV2)
	}

	// 存储与缓存管理（FR2-048）：可重建缓存资产统计、盘点、dry-run 与安全清理。
	cache := r.Group("/api/storage/cache")
	{
		cache.GET("/summary", h.StorageCacheSummary)
		cache.GET("/assets", h.StorageCacheAssets)
		cache.POST("/inventory", h.StorageCacheInventory)
		cache.POST("/clean", h.StorageCacheClean)
	}

	// 播放路由（可选）；测试路径注入 library + max，生产 stream 由 web 层挂载。
	if len(pbSvc) > 0 && pbSvc[0] != nil {
		RegisterPlaybackRoutesWithLibrary(r, pbSvc[0], h.library, h.ViewerMaxContentRating)
	}
}

// RegisterPlaybackRoutes 仅注册播放相关路由（流式 / Seek / 进度 / 缓冲）。
// 拆分出来便于在已经走过 RegisterRoutes 的引擎上单独补挂，避免重复注册。
// 注意：生产 stream 由 web.registerStreamRoute 挂载（含 max_rating）；此处 stream 为测试/兼容降级。
func RegisterPlaybackRoutes(r *gin.Engine, pbSvc *playback.Service) {
	RegisterPlaybackRoutesWithLibrary(r, pbSvc, nil, nil)
}

// RegisterPlaybackRoutesWithLibrary 同 RegisterPlaybackRoutes，并按 library + max 过滤 stream（FR2-051）。
func RegisterPlaybackRoutesWithLibrary(r *gin.Engine, pbSvc *playback.Service, libraryService *library.Service, maxRatingResolver func(*gin.Context, string) string) {
	play := r.Group("/api/play")
	{
		play.GET("/:id/stream", func(c *gin.Context) {
			id, ok := parseMediaID(c)
			if !ok {
				return
			}
			if libraryService == nil {
				pbSvc.StreamFile(c.Writer, c.Request, id, "", 0, 0)
				return
			}
			spaceID := c.GetString("space_id")
			if spaceID == "" {
				spaceID = models.DefaultSpaceID
			}
			maxR := ""
			if maxRatingResolver != nil {
				maxR = maxRatingResolver(c, spaceID)
			}
			mf, err := libraryService.GetMediaFileByIDInSpaceForViewer(spaceID, id, maxR)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
				return
			}
			pbSvc.StreamFile(c.Writer, c.Request, id, mf.FilePath, mf.FileSize, mf.Duration)
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
// taskServices 可选；未传入时会安全拒绝所有 task-scoped 音轨 HLS URL。
// maxRatingResolver 可选：解析当前访客 max_rating（FR2-051）；nil 表示不限制分级。
func RegisterHLSRoutes(r *gin.Engine, hlsMgr *player.HLSManager, hlsDir string, libraryService *library.Service, taskServices ...*tasksvc.Service) {
	RegisterHLSRoutesWithMaxRating(r, hlsMgr, hlsDir, libraryService, nil, taskServices...)
}

// RegisterHLSRoutesWithMaxRating 同 RegisterHLSRoutes，并注入访客 max 解析（FR2-051）。
func RegisterHLSRoutesWithMaxRating(r *gin.Engine, hlsMgr *player.HLSManager, hlsDir string, libraryService *library.Service, maxRatingResolver func(*gin.Context, string) string, taskServices ...*tasksvc.Service) {
	var taskService *tasksvc.Service
	if len(taskServices) > 0 {
		taskService = taskServices[0]
	}
	r.GET("/api/play/hls/*path", func(c *gin.Context) {
		handleHLSRequest(c, hlsMgr, hlsDir, libraryService, taskService, maxRatingResolver)
	})
}

func handleHLSRequest(c *gin.Context, hlsMgr *player.HLSManager, hlsDir string, libraryService *library.Service, taskService *tasksvc.Service, maxRatingResolver func(*gin.Context, string) string) {
	mediaID, rest, ok := parseHLSRequestPath(c)
	spaceID := c.GetString("space_id")
	if spaceID == "" {
		spaceID = models.DefaultSpaceID
	}
	maxR := ""
	if maxRatingResolver != nil {
		maxR = maxRatingResolver(c, spaceID)
	}
	if !ok || !mediaBelongsToRequestedSpace(c, libraryService, mediaID, maxR) {
		return
	}
	audioRoute, err := parseAudioHLSRoute(rest)
	if err != nil {
		writeInvalidHLSPath(c)
		return
	}
	if audioRoute != nil && !audioHLSTaskPlayable(c, taskService, spaceID, mediaID, *audioRoute) {
		writeHLSUnavailable(c)
		return
	}
	serveHLSAsset(c, hlsMgr, hlsDir, spaceID, mediaID, rest)
}

func serveHLSAsset(c *gin.Context, hlsMgr *player.HLSManager, hlsDir, spaceID string, mediaID int64, rest string) {
	file, info, servedPath, err := openHLSFile(hlsDir, spaceID, mediaID, rest)
	if err == nil {
		defer func() { _ = file.Close() }()
		c.Header("Content-Type", detectHLSMimeType(servedPath))
		http.ServeContent(c.Writer, c.Request, filepath.Base(servedPath), info.ModTime(), file)
		return
	}
	if rest == "master" || rest == "master.m3u8" {
		serveLegacyMaster(c, hlsMgr, mediaID)
		return
	}
	if os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "文件不存在"})
		return
	}
	writeInvalidHLSPath(c)
}

func parseHLSRequestPath(c *gin.Context) (int64, string, bool) {
	relPath := strings.TrimPrefix(c.Param("path"), "/")
	parts := strings.SplitN(relPath, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PATH", "message": "无效的路径"})
		return 0, "", false
	}
	mediaID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || mediaID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return 0, "", false
	}
	return mediaID, parts[1], true
}

func openHLSFile(root, spaceID string, mediaID int64, rest string) (*os.File, os.FileInfo, string, error) {
	candidates, err := hlsRelativeCandidates(spaceID, mediaID, rest)
	if err != nil {
		return nil, nil, "", err
	}
	var lastErr error
	for _, candidate := range candidates {
		file, info, openErr := player.OpenHLSFile(root, candidate)
		if openErr == nil {
			return file, info, candidate, nil
		}
		lastErr = openErr
	}
	return nil, nil, "", lastErr
}

func hlsRelativeCandidates(spaceID string, mediaID int64, rest string) ([]string, error) {
	media := strconv.FormatInt(mediaID, 10)
	if strings.HasPrefix(rest, "profiles/") {
		audioRoute, err := parseAudioHLSRoute(rest)
		if err != nil {
			return nil, err
		}
		if audioRoute != nil {
			return []string{strings.Join([]string{spaceID, media, audioRoute.profileID, "tasks", strconv.FormatInt(audioRoute.taskID, 10), audioRoute.asset}, "/")}, nil
		}
		parts := strings.Split(rest, "/")
		if len(parts) < 3 || parts[1] == "" {
			return nil, os.ErrNotExist
		}
		profileID := parts[1]
		if _, err := transcoder.HLSProfileDir(".", spaceID, mediaID, profileID); err != nil {
			return nil, err
		}
		return []string{strings.Join([]string{spaceID, media, profileID, strings.Join(parts[2:], "/")}, "/")}, nil
	}
	if rest == "master" {
		rest = "master.m3u8"
	}
	return []string{
		strings.Join([]string{spaceID, media, transcoder.DefaultHLSPreviewProfile, rest}, "/"),
		strings.Join([]string{media, rest}, "/"),
	}, nil
}

type audioHLSRoute struct {
	profileID string
	taskID    int64
	asset     string
}

func parseAudioHLSRoute(rest string) (*audioHLSRoute, error) {
	if !strings.HasPrefix(rest, "profiles/") {
		return nil, nil
	}
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || !transcoder.IsAudioReloadProfileNamespace(parts[1]) {
		return nil, nil
	}
	profileID := parts[1]
	if !transcoder.IsAudioReloadProfileID(profileID) || len(parts) < 5 || parts[2] != "tasks" {
		return nil, os.ErrInvalid
	}
	taskID, err := strconv.ParseInt(parts[3], 10, 64)
	asset := strings.Join(parts[4:], "/")
	if err != nil || taskID <= 0 || strconv.FormatInt(taskID, 10) != parts[3] || asset == "" {
		return nil, os.ErrInvalid
	}
	return &audioHLSRoute{profileID: profileID, taskID: taskID, asset: asset}, nil
}

func audioHLSTaskPlayable(c *gin.Context, tasks *tasksvc.Service, spaceID string, mediaID int64, route audioHLSRoute) bool {
	if tasks == nil {
		return false
	}
	resourceID := strconv.FormatInt(mediaID, 10)
	task, err := tasks.Get(c.Request.Context(), route.taskID, tasksvc.Query{
		Scope: models.TaskScopeSpace, SpaceID: spaceID, Type: transcoder.TaskTypeHLSPreview,
		ResourceType: "media", ResourceID: resourceID,
	})
	if err != nil || task.Status != models.TaskStatusSucceeded {
		return false
	}
	return validAudioHLSTask(*task, spaceID, mediaID, route.profileID)
}

func validAudioHLSTask(task models.Task, spaceID string, mediaID int64, profileID string) bool {
	if task.Scope != models.TaskScopeSpace || task.SpaceID == nil || *task.SpaceID != spaceID ||
		task.Type != transcoder.TaskTypeHLSPreview || task.Status != models.TaskStatusSucceeded || task.ResourceType != "media" ||
		task.ResourceID != strconv.FormatInt(mediaID, 10) {
		return false
	}
	var payload transcoder.HLSPreviewPayload
	decoder := json.NewDecoder(strings.NewReader(task.PayloadJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return false
	}
	trackID := strings.TrimSpace(payload.AudioTrackID)
	fingerprint := strings.TrimSpace(payload.SourceFingerprint)
	return payload.SpaceID == spaceID && payload.MediaID == mediaID && payload.ProfileID == profileID &&
		payload.Codec == transcoder.DefaultTargetCodec && payload.Width >= 0 && payload.Height >= 0 &&
		trackID != "" && trackID == payload.AudioTrackID && profileID == transcoder.AudioReloadProfileID(trackID) &&
		payload.AudioStreamIndex != nil && *payload.AudioStreamIndex >= 0 &&
		fingerprint != "" && fingerprint == payload.SourceFingerprint
}

func writeHLSUnavailable(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "HLS 文件不可用"})
}

func writeInvalidHLSPath(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PATH", "message": "无效的 HLS 路径"})
}

func serveLegacyMaster(c *gin.Context, hlsMgr *player.HLSManager, mediaID int64) {
	content, err := hlsMgr.GetMasterM3U8(mediaID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "HLS 主清单不存在"})
		return
	}
	c.Data(http.StatusOK, "application/vnd.apple.mpegurl", []byte(content))
}

// mediaBelongsToRequestedSpace 校验媒体属于当前 Space 且对访客可见（FR2-051）。
// maxRating 由调用方注入；空表示不限制分级。
func mediaBelongsToRequestedSpace(c *gin.Context, libraryService *library.Service, mediaID int64, maxRating string) bool {
	spaceID := c.GetString("space_id")
	if spaceID == "" {
		spaceID = models.DefaultSpaceID
	}
	if _, err := libraryService.GetMediaFileByIDInSpaceForViewer(spaceID, mediaID, maxRating); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
		return false
	}
	return true
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
