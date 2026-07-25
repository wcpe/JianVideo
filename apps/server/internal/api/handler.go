package api

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	aisvc "github.com/wcpe/JianVideo/internal/ai"
	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/auth"
	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/metrics"
	"github.com/wcpe/JianVideo/internal/playback"
	"github.com/wcpe/JianVideo/internal/player"
	"github.com/wcpe/JianVideo/internal/rollback"
	"github.com/wcpe/JianVideo/internal/settings"
	"github.com/wcpe/JianVideo/internal/share"
	"github.com/wcpe/JianVideo/internal/smb"
	"github.com/wcpe/JianVideo/internal/space"
	"github.com/wcpe/JianVideo/internal/storage"
	"github.com/wcpe/JianVideo/internal/subtitle"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
	thumbsvc "github.com/wcpe/JianVideo/internal/thumbnail"
	"github.com/wcpe/JianVideo/internal/tools"
	"github.com/wcpe/JianVideo/internal/transcoder"
	"github.com/wcpe/JianVideo/internal/update"
)

// Handler API 请求处理器。
type Handler struct {
	library   *library.Service
	settings  *settings.Service  // 运行期设置读写（FR-24）
	scanQueue *library.TaskQueue // 扫描任务队列（FR-29），未注入时扫描回退直接异步执行
	hlsDir    string             // HLS 切片输出根目录
	hlsMgr    *player.HLSManager // 用于写入 master.m3u8
	version   string             // 应用版本号，由 main 经 ldflags 注入
	share     *share.Service     // 分享链接读写（FR-43），未注入时分享端点不可用
	updateSvc *update.Service    // 二进制自更新服务（FR-46），无外部依赖恒可用

	// 硬件加速能力服务（FR-49）：编码器实测唯一真源 + SQLite 缓存，未注入时回退冷态默认
	capability *transcoder.CapabilityService

	// 播放服务（FR-53）：协商端点记录会话实际编码与路径，未注入时跳过记录
	playback *playback.Service

	settingsReload func() // 设置变更后回调，用于定时扫描周期热生效（FR-28），可空

	// debugLogApply 调试日志开关应用回调（FR-110）：设置保存含 debug_log 时调用，运行期即时切换 GORM 日志级别。可空。
	debugLogApply func(bool)

	// 运行环境信息（FR-60）：由 main 注入，用于系统诊断「运行环境」展示。
	startTime time.Time // 进程启动时刻，用于计算运行时长；零值表示未注入（运行时长退化为 0）
	dbPath    string    // SQLite 数据库文件路径，未注入时为空串

	// 媒体健康巡检服务（FR-73）：未注入时健康端点返回 503。
	health *library.HealthService

	// 转码预设与 HLS preview 统一任务服务（FR2-008）：旧预生成队列仅保留历史兼容。
	presets     *transcoder.PresetStore
	pregenQueue *transcoder.PregenQueue
	hlsPreview  *transcoder.HLSPreviewService
	hlsABR      *transcoder.ABRService

	// 系统指标采样器（FR-119）：未注入时 /api/system/metrics 返回 503。
	metrics *metrics.Sampler

	// 审计事件服务（FR2-040）：未注入时审计查询端点返回 503，业务接入保持可测试。
	audit audit.Recorder

	// 通用异步任务队列中心（FR2-037）：未注入时新任务端点返回 503，旧队列端点保持原兼容行为。
	tasks       *tasksvc.Service
	taskWorkers *tasksvc.WorkerRegistry

	// 外部工具下载管理器（FR2-022）：未注入时工具下载端点返回 503。
	tools *tools.Manager

	// 存储与缓存管理（FR2-048）：缓存资产登记、统计、盘点与白名单清理。
	cache *storage.Service

	// 分档缩略图任务服务（FR2-028）：按需/批量入队、Space 路径隔离与缓存登记。
	thumbnail *thumbsvc.Service

	// 时间轴预览服务（FR2-029）：状态、入队、重建与受控资源读取。
	timelinePreview TimelinePreviewGateway

	// 字幕与音轨服务（FR2-044）：稳定轨道 API、上传、内容与删除。
	subtitle *subtitle.Service

	// 认证与 Space 成员（FR2-010）：用户管理与角色解析；未注入时相关端点 503。
	auth  *auth.Service
	space *space.Service

	// 操作可回滚中心（FR2-041）：未注入时相关端点 503。
	rollback *rollback.Service

	// AI 可替换管线（FR2-011）：未注入时 /api/ai 返回 503。
	ai *aisvc.Service
}

// NewHandler 创建处理器。
func NewHandler(lib *library.Service) *Handler {
	return &Handler{
		library:   lib,
		updateSvc: update.NewService(),
	}
}

// WithVersion 注入应用版本号，供系统诊断接口展示。
func (h *Handler) WithVersion(v string) *Handler {
	h.version = v
	return h
}

// WithSettings 注入运行期设置服务，启用 /api/settings 端点。
func (h *Handler) WithSettings(svc *settings.Service) *Handler {
	h.settings = svc
	return h
}

// WithStartTime 注入进程启动时刻（FR-60），供系统诊断计算运行时长。
func (h *Handler) WithStartTime(t time.Time) *Handler {
	h.startTime = t
	return h
}

// WithDBPath 注入 SQLite 数据库文件路径（FR-60），供系统诊断「运行环境」展示。
func (h *Handler) WithDBPath(path string) *Handler {
	h.dbPath = path
	return h
}

// WithHealthService 注入媒体健康巡检服务（FR-73），启用健康巡检与问题清单端点。
// 未注入时相关端点返回 503，保持无服务环境可用。
func (h *Handler) WithHealthService(svc *library.HealthService) *Handler {
	h.health = svc
	return h
}

// WithTranscodePresets 注入转码预设存储与预生成队列（FR-77），启用预设 CRUD 与预生成端点。
// 未注入时相关端点返回 503，保持无服务环境可用。
func (h *Handler) WithTranscodePresets(store *transcoder.PresetStore, queue *transcoder.PregenQueue) *Handler {
	h.presets = store
	h.pregenQueue = queue
	return h
}

// WithHLSPreview 注入统一 HLS 预览任务服务。
func (h *Handler) WithHLSPreview(service *transcoder.HLSPreviewService) *Handler {
	h.hlsPreview = service
	return h
}

// WithHLSABR 注入多码率 HLS 任务服务。
func (h *Handler) WithHLSABR(service *transcoder.ABRService) *Handler {
	h.hlsABR = service
	return h
}

// WithMetrics 注入系统指标采样器（FR-119），启用 /api/system/metrics 端点。
// 未注入时该端点返回 503，保持无采样器环境可用。
func (h *Handler) WithMetrics(sampler *metrics.Sampler) *Handler {
	h.metrics = sampler
	return h
}

// WithAudit 注入审计服务，启用审计查询端点。
func (h *Handler) WithAudit(rec audit.Recorder) *Handler {
	h.audit = rec
	return h
}

// WithRollback 注入回滚中心（FR2-041）。
func (h *Handler) WithRollback(svc *rollback.Service) *Handler {
	h.rollback = svc
	return h
}

// WithAI 注入 AI 可替换管线服务（FR2-011）。
func (h *Handler) WithAI(svc *aisvc.Service) *Handler {
	h.ai = svc
	return h
}

// WithTasks 注入通用任务服务，启用 /api/tasks 端点。
func (h *Handler) WithTasks(svc *tasksvc.Service) *Handler {
	h.tasks = svc
	return h
}

// WithTaskWorkers 注入通用任务 worker 注册表，供业务入队后唤醒后台处理。
func (h *Handler) WithTaskWorkers(workers *tasksvc.WorkerRegistry) *Handler {
	h.taskWorkers = workers
	return h
}

// WithTools 注入外部工具下载管理器，启用 /api/system/tools 端点。
func (h *Handler) WithTools(manager *tools.Manager) *Handler {
	h.tools = manager
	return h
}

// WithCache 注入缓存资产服务，启用 /api/storage/cache/* 端点。
func (h *Handler) WithCache(svc *storage.Service) *Handler {
	h.cache = svc
	return h
}

// WithThumbnail 注入分档缩略图任务服务。
func (h *Handler) WithThumbnail(svc *thumbsvc.Service) *Handler {
	h.thumbnail = svc
	return h
}

// WithTimelinePreview 注入时间轴预览网关。
func (h *Handler) WithTimelinePreview(gateway TimelinePreviewGateway) *Handler {
	h.timelinePreview = gateway
	return h
}

// WithSubtitle 注入字幕与音轨服务。
func (h *Handler) WithSubtitle(service *subtitle.Service) *Handler {
	h.subtitle = service
	return h
}

// WithScanQueue 注入扫描任务队列，启用扫描入队与任务列表端点（FR-29）。
// 未注入时 ScanLibrary 回退原直接异步执行、任务列表返回空，保持无队列环境可用。
func (h *Handler) WithScanQueue(q *library.TaskQueue) *Handler {
	h.scanQueue = q
	return h
}

// WithSettingsReload 注入设置变更回调（FR-28）：设置保存成功后调用，
// 用于让定时扫描周期等运行期配置即时生效，无需重启。
func (h *Handler) WithSettingsReload(fn func()) *Handler {
	h.settingsReload = fn
	return h
}

// WithDebugLogApply 注入调试日志开关应用回调（FR-110）：设置保存含 debug_log 时调用，
// 运行期即时切换 GORM 日志级别（开启=详细、关闭=安静），无需重启。未注入时仅落库不即时切换。
func (h *Handler) WithDebugLogApply(fn func(bool)) *Handler {
	h.debugLogApply = fn
	return h
}

// WithShareService 注入分享链接服务（FR-43）。
func (h *Handler) WithShareService(svc *share.Service) *Handler {
	h.share = svc
	return h
}

// WithCapabilityService 注入硬件加速能力服务（FR-49）。
// 未注入时硬件加速相关端点回退冷态默认（软件兜底），保证无服务环境可用。
func (h *Handler) WithCapabilityService(svc *transcoder.CapabilityService) *Handler {
	h.capability = svc
	return h
}

// WithPlayback 注入播放服务，启用协商端点的会话记录（FR-53）。
// 未注入时协商仍工作，仅不记录会话实际编码与路径。
func (h *Handler) WithPlayback(svc *playback.Service) *Handler {
	h.playback = svc
	return h
}

// HWAccel GET /api/transcode/hwaccel
// 返回与 SystemInfo 同源的硬件加速能力（读能力服务缓存派生）。
func (h *Handler) HWAccel(c *gin.Context) {
	var info *transcoder.HWAccelInfo
	if h.capability != nil {
		info = h.capability.Capabilities(c.Request.Context())
	} else {
		info = transcoder.BuildCapabilities(nil)
	}
	c.JSON(http.StatusOK, info)
}

// versionOrDefault 返回版本号，未注入时回退 "dev"。
func (h *Handler) versionOrDefault() string {
	if h.version == "" {
		return "dev"
	}
	return h.version
}

// WithHLSPreSlice 注入按需 HLS 生成所需的目录与 HLSManager。
// 任一参数为空则禁用按需 HLS 生成（用于测试或无 ffmpeg 环境）。
func (h *Handler) WithHLSPreSlice(hlsDir string, hlsMgr *player.HLSManager) *Handler {
	h.hlsDir = hlsDir
	h.hlsMgr = hlsMgr
	return h
}

// ListLibraryPaths GET /api/library/paths
// 每项附带 media_count（该库已索引、未软删的媒体文件数量），供存储库卡片展示。
func (h *Handler) ListLibraryPaths(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	items, err := h.library.ListLibraryPathViewsInSpace(spaceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// ListLibraryKinds 返回内置媒体库分型目录。
func (h *Handler) ListLibraryKinds(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"items": library.Kinds()})
}

// CreateLibraryPath POST /api/library/paths
func (h *Handler) CreateLibraryPath(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	var req struct {
		Path        string `json:"path" binding:"required"`
		Type        string `json:"type" binding:"required"`
		Label       string `json:"label"`
		LibraryKind string `json:"library_kind"`
		SMBHost     string `json:"smb_host"`
		SMBUsername string `json:"smb_username"`
		SMBPassword string `json:"smb_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "请求参数错误"})
		return
	}

	lp, err := h.library.CreateLibraryPathWithKindInSpace(spaceID, req.Path, req.Type, req.Label, req.LibraryKind)
	if err != nil {
		if errors.Is(err, library.ErrInvalidLibraryKind) {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_LIBRARY_KIND", "message": "媒体库分型不支持"})
			return
		}
		if req.Type == "local" || req.Type == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PATH", "message": "本地路径不可访问或不是目录"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": "CREATE_FAILED", "message": "添加失败"})
		return
	}

	// SMB 类型：保存凭据
	if req.Type == "smb" && req.SMBHost != "" {
		h.saveSMBConfig(c, req.SMBHost, req.SMBUsername, req.SMBPassword)
	}

	c.JSON(http.StatusCreated, lp)
}

// saveSMBConfig 保存 SMB 凭据到加密配置文件。
func (h *Handler) saveSMBConfig(c *gin.Context, host, username, password string) {
	// 统一使用 smb.MasterPassword() 作为主密码来源；未配置则拒绝以弱默认密钥保存，仅记录 ERROR
	masterPwd, err := smb.MasterPassword()
	if err != nil {
		log.Printf("[ERROR] 无法保存 SMB 凭据: %v", err)
		return
	}

	dataDir := "data"
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		log.Printf("[WARN] 创建数据目录失败: %v", err)
		return
	}

	store := smb.NewCredentialStore(dataDir)
	creds := &smb.Credentials{
		Host:     host,
		Username: username,
		Password: password,
	}

	if err := store.Save(masterPwd, creds); err != nil {
		log.Printf("[WARN] 保存 SMB 凭据失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "SMB_CONFIG_FAILED", "message": "SMB 凭据保存失败"})
		return
	}

	// 设置到 library service
	h.library.SetSMBCredentialStore(store)
	log.Printf("[INFO] SMB 凭据已保存: host=%s, user=%s", host, username)
}

// DeleteLibraryPath DELETE /api/library/paths/:id
func (h *Handler) DeleteLibraryPath(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}

	if err := h.library.DeleteLibraryPathInSpace(spaceID, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DELETE_FAILED", "message": "删除失败"})
		return
	}
	c.Status(http.StatusNoContent)
}

// UpdateLibraryPath PUT /api/library/paths/:id
func (h *Handler) UpdateLibraryPath(c *gin.Context) {
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
		Label       *string `json:"label"`
		Enabled     *bool   `json:"enabled"`
		LibraryKind *string `json:"library_kind"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "请求参数错误"})
		return
	}
	lp, err := h.library.UpdateLibraryPathWithKindInSpace(spaceID, id, req.Label, req.Enabled, req.LibraryKind)
	if err != nil {
		if errors.Is(err, library.ErrInvalidLibraryKind) {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_LIBRARY_KIND", "message": "媒体库分型不支持"})
			return
		}
		status := http.StatusInternalServerError
		if errors.Is(err, gorm.ErrRecordNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"code": "UPDATE_FAILED", "message": "更新失败"})
		return
	}
	c.JSON(http.StatusOK, lp)
}

// ListMediaFiles GET /api/library/media
func (h *Handler) ListMediaFiles(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	libraryID, _ := strconv.ParseInt(c.Query("library_id"), 10, 64)
	sort := c.DefaultQuery("sort", "time_desc")
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		log.Printf("[WARN] 分页参数 page 解析失败，使用默认值: %s", c.Query("page"))
		page = 1
	}
	pageSize, err := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if err != nil || pageSize < 1 || pageSize > 100 {
		log.Printf("[WARN] 分页参数 page_size 解析失败，使用默认值: %s", c.Query("page_size"))
		pageSize = 20
	}
	search := c.Query("search")

	// 收藏/标签筛选（FR-41）：favorite=true、tag_id=N
	filter := parseMediaFilter(c, libraryID, sort, search)
	filter.SpaceID = spaceID
	// 家长控制（FR2-051）：注入调用者有效最高可见分级。
	filter.MaxContentRating = h.viewerMaxContentRating(c, spaceID)
	result, err := h.library.ListMediaFilesPage(filter, library.MediaPageRequest{
		Page:     page,
		PageSize: pageSize,
		Cursor:   c.Query("cursor"),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"items":       result.Items,
		"total":       result.Total,
		"page":        result.Page,
		"page_size":   result.PageSize,
		"next_cursor": result.NextCursor,
	})
}

// GetMediaFile GET /api/library/media/:id
func (h *Handler) GetMediaFile(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}

	mf, err := h.loadMediaForViewer(c, spaceID, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
		return
	}
	c.JSON(http.StatusOK, mf)
}

// loadMediaForViewer 按访客 max_rating 取媒体（FR2-051）；不可见 → not found。
// 所有「按 media id 读内容」入口应统一走此 helper（公开分享除外）。
func (h *Handler) loadMediaForViewer(c *gin.Context, spaceID string, id int64) (*models.MediaFile, error) {
	return h.LoadMediaForViewer(c, spaceID, id)
}

// LoadMediaForViewer 导出给 web 层 stream 等装配点使用（FR2-051）。
func (h *Handler) LoadMediaForViewer(c *gin.Context, spaceID string, id int64) (*models.MediaFile, error) {
	maxR := h.viewerMaxContentRating(c, spaceID)
	return h.library.GetMediaFileByIDInSpaceForViewer(spaceID, id, maxR)
}

// ViewerMaxContentRating 导出给 HLS 路由装配使用（FR2-051）。
func (h *Handler) ViewerMaxContentRating(c *gin.Context, spaceID string) string {
	return h.viewerMaxContentRating(c, spaceID)
}

// viewerMaxContentRating 解析当前用户在 Space 的有效最高可见分级；无 space 服务时不限制。
// 不写 HTTP 响应，避免列表路径双写。
func (h *Handler) viewerMaxContentRating(c *gin.Context, spaceID string) string {
	if h.space == nil || h.auth == nil {
		return ""
	}
	var userID int64
	if v, ok := c.Get("user_id"); ok {
		switch id := v.(type) {
		case int:
			userID = int64(id)
		case int64:
			userID = id
		}
	}
	if userID == 0 {
		username, _ := c.Get("username")
		name, _ := username.(string)
		if strings.TrimSpace(name) == "" {
			return ""
		}
		u, err := h.auth.FindUserByUsername(name)
		if err != nil || u == nil {
			return ""
		}
		userID = int64(u.ID)
	}
	maxR, err := h.space.EffectiveMaxRating(spaceID, userID)
	if err != nil {
		return ""
	}
	return maxR
}

// filterMediaByMaxRating 列表结果按访客 max 过滤（FR2-051）；max 空不限制。
func filterMediaByMaxRating(items []models.MediaFile, maxR string) []models.MediaFile {
	if strings.TrimSpace(maxR) == "" {
		return items
	}
	out := make([]models.MediaFile, 0, len(items))
	for i := range items {
		if models.ContentVisible(items[i].ContentRating, maxR) {
			out = append(out, items[i])
		}
	}
	return out
}

// filterWatchMediaByMaxRating 观看态列表按访客 max 过滤（FR2-051）。
func filterWatchMediaByMaxRating(items []library.WatchMediaItem, maxR string) []library.WatchMediaItem {
	if strings.TrimSpace(maxR) == "" {
		return items
	}
	out := make([]library.WatchMediaItem, 0, len(items))
	for i := range items {
		if models.ContentVisible(items[i].Media.ContentRating, maxR) {
			out = append(out, items[i])
		}
	}
	return out
}

// DeleteMediaFile DELETE /api/library/media/:id
func (h *Handler) DeleteMediaFile(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}

	if err := h.library.DeleteMediaFileInSpace(spaceID, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DELETE_FAILED", "message": "删除失败"})
		return
	}
	c.Status(http.StatusNoContent)
}

// BatchDeleteMediaFiles POST /api/library/media/batch-delete
// 批量软删媒体文件（FR-69）：body {ids:[...]}，单事务内复用 FR-25 软删（仅置 deleted_at、不动磁盘），
// 跳过不存在/已软删 id。返回实际软删条数。
func (h *Handler) BatchDeleteMediaFiles(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": "请求体无效"})
		return
	}

	deleted, err := h.library.BatchDeleteMediaFilesInSpace(spaceID, req.IDs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DELETE_FAILED", "message": "批量删除失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": deleted})
}

// ListRecycleMediaFiles GET /api/library/recycle
// 列出回收站内全部已软删的媒体文件（FR-25）。
func (h *Handler) ListRecycleMediaFiles(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	items, err := h.library.ListDeletedMediaFilesInSpace(spaceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// RestoreMediaFile POST /api/library/media/:id/restore
// 从回收站还原指定媒体，使其回到常规列表（FR-25）。
func (h *Handler) RestoreMediaFile(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}

	if err := h.library.RestoreMediaFileInSpace(spaceID, id); err != nil {
		if errors.Is(err, library.ErrRecycleMediaNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "回收站中不存在该媒体文件"})
			return
		}
		log.Printf("[ERROR] 回收站还原失败: spaceID=%s, mediaID=%d, err=%v", spaceID, id, err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "RESTORE_UNAVAILABLE", "message": "回收站还原暂时不可用"})
		return
	}
	c.Status(http.StatusNoContent)
}

// RecycleCleanup POST /api/library/recycle/cleanup
// 清理回收站（FR-26）：把全部软删项的源文件移动到其所在盘符对应的回收站目录（按删除日期分子目录），
// 移动成功后删除 media_files 记录。盘符回收站路径取自 FR-24 设置键 recycle_bin_paths（JSON：盘符→目录）。
// 存在软删项所在盘符未配置（含 SMB/无盘符项）时返回 409，不移动任何文件。
func (h *Handler) RecycleCleanup(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	if h.settings == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SETTINGS_UNAVAILABLE", "message": "设置服务未启用"})
		return
	}

	// 读取并解析每盘符回收站路径配置（JSON 字符串：盘符 → 目录）
	raw, err := h.settings.Get(settings.KeyRecycleBinPaths)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "读取回收站路径配置失败"})
		return
	}
	drivePaths := map[string]string{}
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &drivePaths); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "INVALID_RECYCLE_CONFIG", "message": "回收站路径配置不是合法 JSON"})
			return
		}
	}

	result, err := h.library.CleanupRecycleInSpace(spaceID, drivePaths)
	if err != nil {
		if errors.Is(err, library.ErrRecycleBinPathUnset) {
			c.JSON(http.StatusConflict, gin.H{"code": "RECYCLE_PATH_UNSET", "message": err.Error()})
			return
		}
		var recovery *library.RecycleRecoveryError
		if errors.As(err, &recovery) {
			writeRecycleRecoveryError(c, result, recovery)
			return
		}
		writeRecycleCleanupError(c, spaceID, result, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"moved": result.Moved, "failed": result.Failed})
}

// RecycleAutoCleanupPreview POST /api/library/recycle/auto-cleanup/preview（FR2-054）
// 预览本 Space 到期自动清理候选，不改数据；保留天数/开关按当前 Space ForSpace 解析。
func (h *Handler) RecycleAutoCleanupPreview(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	drivePaths, before, limit, ok := h.recycleAutoCleanupParams(c, spaceID)
	if !ok {
		return
	}
	result, err := h.library.PreviewAutoCleanupInSpace(spaceID, drivePaths, before, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "预览自动清理失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"candidate":      result.Candidate,
		"skipped":        result.Skipped,
		"missing_drives": result.MissingDrives,
		"media_ids":      result.MediaIDs,
		"before":         before.UTC().Format(time.RFC3339),
		"limit":          limit,
	})
}

// RecycleAutoCleanupRun POST /api/library/recycle/auto-cleanup/run（FR2-054）
// 执行本 Space 有界到期自动清理；缺盘符跳过单条；开关与天数按当前 Space ForSpace 解析。
func (h *Handler) RecycleAutoCleanupRun(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	drivePaths, before, limit, ok := h.recycleAutoCleanupParams(c, spaceID)
	if !ok {
		return
	}
	// days=0 或开关关闭：拒绝实跑（preview 仍可看候选）
	if h.settings != nil {
		if !h.settings.RecycleAutoCleanupEnabledForSpace(spaceID) || h.settings.RecycleRetentionDaysForSpace(spaceID) <= 0 {
			c.JSON(http.StatusConflict, gin.H{
				"code":    "AUTO_CLEANUP_DISABLED",
				"message": "自动清理未启用或保留天数为 0",
			})
			return
		}
	}
	result, err := h.library.AutoCleanupExpiredInSpace(spaceID, drivePaths, before, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "自动清理失败"})
		return
	}
	if h.audit != nil {
		_ = h.audit.Record(c.Request.Context(), audit.EventInput{
			Scope:        audit.ScopeSpace,
			SpaceID:      spaceID,
			ActorType:    "user",
			ActorID:      actorIDFromContext(c),
			Action:       "recycle.auto_cleanup",
			ResourceType: "recycle",
			ResourceID:   spaceID,
			After: map[string]any{
				"candidate":      result.Candidate,
				"moved":          result.Moved,
				"failed":         result.Failed,
				"skipped":        result.Skipped,
				"missing_drives": result.MissingDrives,
			},
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"candidate":      result.Candidate,
		"moved":          result.Moved,
		"failed":         result.Failed,
		"skipped":        result.Skipped,
		"missing_drives": result.MissingDrives,
		"media_ids":      result.MediaIDs,
		"before":         before.UTC().Format(time.RFC3339),
		"limit":          limit,
	})
}

// recycleAutoCleanupParams 解析盘符配置、到期阈值与批量上限（days 按 spaceID ForSpace）。
func (h *Handler) recycleAutoCleanupParams(c *gin.Context, spaceID string) (drivePaths map[string]string, before time.Time, limit int, ok bool) {
	if h.settings == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SETTINGS_UNAVAILABLE", "message": "设置服务未启用"})
		return nil, time.Time{}, 0, false
	}
	raw, err := h.settings.Get(settings.KeyRecycleBinPaths)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "读取回收站路径配置失败"})
		return nil, time.Time{}, 0, false
	}
	drivePaths = map[string]string{}
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &drivePaths); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "INVALID_RECYCLE_CONFIG", "message": "回收站路径配置不是合法 JSON"})
			return nil, time.Time{}, 0, false
		}
	}
	days := h.settings.RecycleRetentionDaysForSpace(spaceID)
	// days=0 时 preview 仍返回「无到期阈值」语义：before=零时刻 → 不选中任何项
	if days == 0 {
		before = time.Time{}
	} else {
		before = time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	}
	limit = 50
	if q := strings.TrimSpace(c.Query("limit")); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	return drivePaths, before, limit, true
}

func writeRecycleCleanupError(c *gin.Context, spaceID string, result library.CleanupResult, err error) {
	log.Printf("[ERROR] 回收站清理失败: spaceID=%s, moved=%d, failed=%d, err=%v", spaceID, result.Moved, result.Failed, err)
	status := http.StatusInternalServerError
	code := "CLEANUP_FAILED"
	message := "清理回收站失败"
	if isSQLiteBusyOrLocked(err) {
		status = http.StatusServiceUnavailable
		code = "CLEANUP_UNAVAILABLE"
		message = "回收站清理暂时不可用"
	}
	c.JSON(status, gin.H{"code": code, "message": message})
}

const (
	sqliteBusyErrorCode   = 5
	sqliteLockedErrorCode = 6
	sqliteErrorPackage    = "github.com/mattn/go-sqlite3"
)

func isSQLiteBusyOrLocked(err error) bool {
	if code, ok := sqliteErrorCode(err); ok {
		return code == sqliteBusyErrorCode || code == sqliteLockedErrorCode
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, inner := range joined.Unwrap() {
			if isSQLiteBusyOrLocked(inner) {
				return true
			}
		}
		return false
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return isSQLiteBusyOrLocked(wrapped.Unwrap())
	}
	return false
}

func sqliteErrorCode(err error) (int64, bool) {
	value := reflect.ValueOf(err)
	for value.IsValid() && value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return 0, false
		}
		value = value.Elem()
	}
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return 0, false
	}
	typeInfo := value.Type()
	if typeInfo.PkgPath() != sqliteErrorPackage || typeInfo.Name() != "Error" {
		return 0, false
	}
	code := value.FieldByName("Code")
	if !code.IsValid() || !code.CanInt() {
		return 0, false
	}
	return code.Int(), true
}

func writeRecycleRecoveryError(c *gin.Context, result library.CleanupResult, recovery *library.RecycleRecoveryError) {
	log.Printf(
		"[ERROR] 回收站恢复异常: mediaID=%d, state=%s, source=%s, target=%s, action=%s, databaseErr=%v, recoveryErr=%v",
		recovery.MediaID, recovery.State, recovery.Source, recovery.Target, recovery.Action,
		recovery.DatabaseError, recovery.RecoveryError,
	)
	if isSQLiteBusyOrLocked(recovery.DatabaseError) || isSQLiteBusyOrLocked(recovery.RecoveryError) {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code": "CLEANUP_UNAVAILABLE", "message": "回收站清理暂时不可用",
			"moved": result.Moved, "failed": result.Failed,
		})
		return
	}
	status, code := http.StatusInternalServerError, "RECYCLE_RECOVERY_FAILED"
	if recovery.State == library.RecycleRecoveryStateSourceOccupied {
		status, code = http.StatusConflict, "RECYCLE_RECOVERY_CONFLICT"
	}
	c.JSON(status, gin.H{
		"code": code, "message": recovery.Error(), "moved": result.Moved, "failed": result.Failed,
		"media_id": recovery.MediaID, "source_name": filepath.Base(recovery.Source), "target_name": filepath.Base(recovery.Target),
		"state": recovery.State, "action": recovery.Action,
	})
}

// RenameMediaFile PUT /api/library/media/:id/rename
// 请求体：{"new_name": "新文件名.mp4"}，磁盘改名 + 更新数据库记录。
func (h *Handler) RenameMediaFile(c *gin.Context) {
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
		NewName string `json:"new_name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": "请求体无效"})
		return
	}

	mf, err := h.library.RenameMediaFileInSpace(spaceID, id, req.NewName)
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
		case errors.Is(err, library.ErrRenameTargetExists):
			c.JSON(http.StatusConflict, gin.H{"code": "TARGET_EXISTS", "message": err.Error()})
		case errors.Is(err, library.ErrInvalidNewName), errors.Is(err, library.ErrRenameUnsupported):
			c.JSON(http.StatusBadRequest, gin.H{"code": "RENAME_REJECTED", "message": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"code": "RENAME_FAILED", "message": "重命名失败"})
		}
		return
	}
	c.JSON(http.StatusOK, mf)
}

// MoveMediaFile PUT /api/library/media/:id/move
// 请求体：{"target_dir": "D:/Media/目标目录"}，同媒体库内移动文件并更新数据库记录。
func (h *Handler) MoveMediaFile(c *gin.Context) {
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
		TargetDir string `json:"target_dir"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": "请求体无效"})
		return
	}
	mf, err := h.library.MoveMediaFileInSpace(spaceID, id, req.TargetDir)
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
		case errors.Is(err, library.ErrRenameTargetExists):
			c.JSON(http.StatusConflict, gin.H{"code": "TARGET_EXISTS", "message": err.Error()})
		case errors.Is(err, library.ErrInvalidMoveTarget), errors.Is(err, library.ErrMoveUnsupported):
			c.JSON(http.StatusBadRequest, gin.H{"code": "MOVE_REJECTED", "message": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"code": "MOVE_FAILED", "message": "移动失败"})
		}
		return
	}
	c.JSON(http.StatusOK, mf)
}

// WritebackMediaMetadata POST /api/library/media/:id/metadata/writeback
// FR2-033 危险写回：将库内元数据写回原文件（仅图片）。
// 请求体：{ "confirm_writeback": true }；缺省或 false → 400 CONFIRM_REQUIRED。
// 成功：202 入队 metadata.writeback；viewer 由 Space 写守卫拦截（editor+）。
func (h *Handler) WritebackMediaMetadata(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}
	if h.tasks == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "TASKS_UNAVAILABLE", "message": "任务中心未启用"})
		return
	}
	var req struct {
		ConfirmWriteback bool `json:"confirm_writeback"`
	}
	// 允许空 body：视为未确认。
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		// 非 JSON 也按未确认处理，避免误触发写回。
		req.ConfirmWriteback = false
	}
	baseDir := "data"
	if strings.TrimSpace(h.dbPath) != "" {
		baseDir = filepath.Dir(h.dbPath)
	}
	task, err := library.EnqueueMetadataWriteback(c.Request.Context(), h.tasks, h.library, baseDir, spaceID, id, req.ConfirmWriteback)
	if err != nil {
		switch {
		case errors.Is(err, library.ErrWritebackConfirmRequired):
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    "CONFIRM_REQUIRED",
				"message": "写回原文件需 confirm_writeback=true，且不可逆；请先确认已备份",
			})
		case errors.Is(err, library.ErrWritebackVideoUnsupported):
			c.JSON(http.StatusBadRequest, gin.H{"code": "VIDEO_WRITEBACK_UNSUPPORTED", "message": err.Error()})
		case errors.Is(err, library.ErrWritebackSMBUnsupported):
			c.JSON(http.StatusBadRequest, gin.H{"code": "UNSUPPORTED_PATH", "message": err.Error()})
		case errors.Is(err, library.ErrWritebackNotImage):
			c.JSON(http.StatusBadRequest, gin.H{"code": "NOT_IMAGE", "message": err.Error()})
		case errors.Is(err, library.ErrWritebackNoFields):
			c.JSON(http.StatusBadRequest, gin.H{"code": "NO_FIELDS", "message": err.Error()})
		case errors.Is(err, gorm.ErrRecordNotFound):
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"code": "METADATA_WRITEBACK_FAILED", "message": "元数据写回入队失败: " + err.Error()})
		}
		return
	}
	h.triggerTaskWorkers()
	c.JSON(http.StatusAccepted, gin.H{
		"status":  task.Status,
		"task_id": strconv.FormatInt(task.ID, 10),
	})
}

// UpdateDisplayName PUT /api/library/media/:id/display-name
// 请求体：{"display_name": "..."}，仅更新库内显示名，不动磁盘文件名。空串表示清除显示名。
func (h *Handler) UpdateDisplayName(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	id, ok := parseMediaID(c)
	if !ok {
		return
	}
	var req struct {
		DisplayName string `json:"display_name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": "请求体无效"})
		return
	}

	mf, err := h.library.UpdateDisplayNameInSpace(spaceID, id, req.DisplayName)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DISPLAY_NAME_FAILED", "message": "更新显示名失败"})
		return
	}
	c.JSON(http.StatusOK, mf)
}

// UpdateMediaNotes PUT /api/library/media/:id/notes
// 请求体：{"notes": "..."}，更新库内备注，空串表示清除备注（FR-137）。
func (h *Handler) UpdateMediaNotes(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	id, ok := parseMediaID(c)
	if !ok {
		return
	}
	var req struct {
		Notes string `json:"notes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": "请求体无效"})
		return
	}

	mf, err := h.library.UpdateMediaNotesInSpace(spaceID, id, req.Notes)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": "NOTES_FAILED", "message": "更新备注失败"})
		return
	}
	c.JSON(http.StatusOK, mf)
}

// BrowseDirectory GET /api/library/browse
// 按真实磁盘路径跨库浏览（FR-121）：parent_path 必填；sort 可选（name/size/type/time，缺省 name）；
// library_id 已弃用，仍接受但忽略（导航按真实路径跨库聚合，向后兼容）。
func (h *Handler) BrowseDirectory(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	parentPath := c.Query("parent_path")
	if parentPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARENT_PATH", "message": "parent_path 不能为空"})
		return
	}

	sortKey := c.DefaultQuery("sort", library.BrowseSortName)

	resp, err := h.library.BrowseDirectoryInSpace(spaceID, parentPath, sortKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "BROWSE_FAILED", "message": "浏览目录失败"})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// SaveSMBCredentials POST /api/smb/credentials
// 保存 SMB 凭据（加密存储）。
func (h *Handler) SaveSMBCredentials(c *gin.Context) {
	var req struct {
		Host     string `json:"host" binding:"required"`
		Username string `json:"username"`
		Password string `json:"password"`
		Share    string `json:"share"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "请求参数错误"})
		return
	}

	// 统一使用 smb.MasterPassword() 作为主密码来源；未配置则拒绝以弱默认密钥保存，快速失败
	masterPwd, err := smb.MasterPassword()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SMB_MASTER_PASSWORD_UNSET", "message": "未配置 SMB_MASTER_PASSWORD 环境变量，无法保存 SMB 凭据"})
		return
	}

	dataDir := "data"
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "创建数据目录失败"})
		return
	}

	store := smb.NewCredentialStore(dataDir)
	creds := &smb.Credentials{
		Host:     req.Host,
		Username: req.Username,
		Password: req.Password,
		Share:    req.Share,
	}

	if err := store.Save(masterPwd, creds); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "SAVE_FAILED", "message": "保存凭据失败"})
		return
	}

	h.library.SetSMBCredentialStore(store)
	c.Status(http.StatusNoContent)
}

// ScanLibrary POST /api/library/scan/:id
// 触发扫描：查询参数 mode（full 全量含已删文件对账 / incremental 或缺省增量，FR-27）。
// 注入队列时建 pending 任务入队、串行执行（FR-29），返回 {"status":"queued","task_id":N}；
// 未注入队列时回退原直接异步扫描，返回 {"status":"scanning"}。
// 扫描进度通过 GET /api/library/scan/progress SSE 与 GET /api/library/scan/tasks 获取。
func (h *Handler) ScanLibrary(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}

	lp, err := h.library.GetLibraryPathByIDInSpace(spaceID, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "目录不存在"})
		return
	}

	// 缺省/非法值回退增量，向后兼容既有调用方（FR-27）
	mode := library.NormalizeScanMode(c.Query("mode"))

	// 队列已注入：入队排队、单 worker 串行执行（FR-29）
	if h.scanQueue != nil {
		taskID, err := h.scanQueue.EnqueueInSpace(spaceID, id, lp.Path, lp.Type, mode)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "ENQUEUE_FAILED", "message": "扫描入队失败"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "queued", "task_id": taskID})
		return
	}

	// 未注入队列：回退直接异步扫描；扫描只负责入库，不自动创建高成本转码。
	h.library.StartAsyncScanInSpace(spaceID, id, lp.Path, lp.Type, mode)
	c.JSON(http.StatusOK, gin.H{"status": "scanning"})
}

// ListScanTasks GET /api/library/scan/tasks
// 返回扫描任务列表（按入队时间倒序）与当前进行中的任务（FR-29）。
// 当前 running 任务的进度用实时全局扫描状态覆盖，已完成任务用其持久化的 scanned_files。
func (h *Handler) ListScanTasks(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	if h.scanQueue == nil {
		c.JSON(http.StatusOK, gin.H{"tasks": []models.ScanTask{}, "current": nil})
		return
	}

	tasks, err := h.scanQueue.ListTasksInSpace(spaceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询任务失败"})
		return
	}

	// 单 worker 串行 ⇒ 全局 ScanStatus 始终对应当前 running 任务；把实时进度覆盖到该任务
	live := library.GetScanStatus()
	var current *models.ScanTask
	for i := range tasks {
		if tasks[i].Status == models.ScanTaskStatusRunning {
			if live.Status == "scanning" && live.SpaceID == spaceID {
				tasks[i].ScannedFiles = live.ScannedFiles
				tasks[i].TotalFiles = live.TotalFiles
			}
			current = &tasks[i]
			break
		}
	}

	c.JSON(http.StatusOK, gin.H{"tasks": tasks, "current": current})
}

// CancelScanTask POST /api/library/scan/tasks/:id/cancel
// 取消尚未执行的扫描任务。
func (h *Handler) CancelScanTask(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	if h.scanQueue == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "QUEUE_UNAVAILABLE", "message": "扫描队列未启用"})
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}
	if err := h.scanQueue.CancelTaskInSpace(spaceID, id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "CANCEL_FAILED", "message": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// RetryScanTask POST /api/library/scan/tasks/:id/retry
// 重试失败或已取消的扫描任务。
func (h *Handler) RetryScanTask(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	if h.scanQueue == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "QUEUE_UNAVAILABLE", "message": "扫描队列未启用"})
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}
	if err := h.scanQueue.RetryTaskInSpace(spaceID, id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "RETRY_FAILED", "message": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// ScanProgressSSE GET /api/library/scan/progress
// 以 SSE 流推送当前扫描进度，每 500ms 轮询一次、仅在状态变化时推送。
// 连接持续保持打开（不在 completed/error 后主动关闭），以便接收后续扫描进度；
// 仅在客户端断开（ctx.Done）时结束。避免浏览器 EventSource 在终态后反复重连成风暴。
func (h *Handler) ScanProgressSSE(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastSent string // 上次推送的状态快照，仅在变化时推送，避免空转刷屏
	for {
		select {
		case <-ticker.C:
			status := library.GetScanStatus()
			if status.SpaceID != "" && status.SpaceID != spaceID {
				status = library.ScanStatus{Status: "idle"}
			}
			data, _ := json.Marshal(status)
			payload := string(data)
			if payload == lastSent {
				continue
			}
			c.SSEvent("progress", payload)
			c.Writer.Flush()
			lastSent = payload
		case <-c.Request.Context().Done():
			return
		}
	}
}

// GetThumbnail GET /api/library/thumbnail/:id
func (h *Handler) GetThumbnail(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	id, ok := parseMediaID(c)
	if !ok {
		return
	}

	mf, err := h.loadMediaForViewer(c, spaceID, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
		return
	}
	h.serveThumbnail(c, mf)
}

// serveThumbnail 优先回传当前封面，封面缓存缺失时回退 FR2-028 普通缩略图。
func (h *Handler) serveThumbnail(c *gin.Context, mf *models.MediaFile) {
	size := library.NormalizeThumbnailSize(parseThumbnailSize(c))
	if h.thumbnail != nil {
		coverPath, err := h.thumbnail.CurrentCoverPath(c.Request.Context(), mf.SpaceID, mf.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "COVER_ERROR", "message": "封面处理失败"})
			return
		}
		if coverPath != "" {
			c.File(coverPath)
			return
		}
	}
	if h.thumbnail == nil {
		h.serveLegacyThumbnail(c, mf, size)
		return
	}
	result, err := h.thumbnail.Ensure(c.Request.Context(), mf.SpaceID, mf.ID, []int{size})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "THUMBNAIL_ERROR", "message": "缩略图处理失败"})
		return
	}
	if result.Ready {
		c.File(result.Path)
		return
	}
	if h.taskWorkers != nil {
		h.taskWorkers.Wake()
	}
	c.JSON(http.StatusAccepted, gin.H{
		"code": "GENERATING", "message": "缩略图生成中", "task_id": result.TaskID, "sizes": result.Sizes,
	})
}

func (h *Handler) serveLegacyThumbnail(c *gin.Context, mf *models.MediaFile, size int) {
	thumbnailPath := library.ThumbnailPathForSize(mf.FilePath, size)
	if _, err := os.Stat(thumbnailPath); err != nil {
		go h.library.GenerateThumbnailSizeInSpace(mf.SpaceID, mf.LibraryID, mf.FilePath, size)
		c.JSON(http.StatusAccepted, gin.H{"code": "GENERATING", "message": "缩略图生成中"})
		return
	}
	h.registerCacheFile(c, mf, storage.CacheKindThumbnail, thumbnailPath, strconv.Itoa(size))
	c.File(thumbnailPath)
}

// parseThumbnailSize 解析缩略图 size 查询参数（缺省 / 非数字回 0，交由 NormalizeThumbnailSize 回落默认）。
func parseThumbnailSize(c *gin.Context) int {
	raw := c.Query("size")
	if raw == "" {
		return 0
	}
	size, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return size
}

// GetRawImage GET /api/library/media/:id/raw
func (h *Handler) GetRawImage(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}

	mf, err := h.loadMediaForViewer(c, spaceID, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
		return
	}
	h.serveRawImage(c, mf)
}

// serveRawImage 回传图片原始内容（HEIC/RAW 经 ImageMagick 转 JPEG，FR-37）。
// 抽出供鉴权版 GetRawImage 与分享版（FR-43）共用，行为一致。
func (h *Handler) serveRawImage(c *gin.Context, mf *models.MediaFile) {
	mediaType, ok := h.library.MediaTypeByPathInSpace(mf.SpaceID, mf.LibraryID, mf.FilePath)
	if !ok || mediaType != library.MediaTypeImage {
		c.JSON(http.StatusBadRequest, gin.H{"code": "NOT_IMAGE", "message": "仅支持图片 raw 访问"})
		return
	}
	if strings.HasPrefix(mf.FilePath, "smb://") {
		c.JSON(http.StatusBadRequest, gin.H{"code": "UNSUPPORTED_PATH", "message": "暂不支持 SMB 图片 raw 访问"})
		return
	}

	// HEIC/RAW 浏览器无法直接渲染，经外部 ImageMagick 转 JPEG 后返回（FR-37）
	if library.NeedsImageConvert(mf.FilePath) {
		if !library.IsMagickAvailable() {
			log.Printf("[WARN] magick 不可用，无法转换 HEIC/RAW: %s", mf.FilePath)
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": "MAGICK_UNAVAILABLE", "message": "未安装 ImageMagick，无法显示 HEIC/RAW 图片"})
			return
		}
		jpegPath, convErr := library.ConvertToJPEG(mf.FilePath)
		if convErr != nil {
			log.Printf("[ERROR] HEIC/RAW 转 JPEG 失败: %s, err=%v", mf.FilePath, convErr)
			c.JSON(http.StatusInternalServerError, gin.H{"code": "CONVERT_FAILED", "message": "图片转换失败"})
			return
		}
		h.registerCacheFile(c, mf, storage.CacheKindImageProxy, jpegPath, "")
		c.File(jpegPath)
		return
	}

	data, err := os.ReadFile(mf.FilePath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "FILE_NOT_FOUND", "message": "图片文件不可访问"})
		return
	}

	contentType := mime.TypeByExtension(filepath.Ext(mf.FilePath))
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	c.Data(http.StatusOK, contentType, data)
}

func (h *Handler) registerCacheFile(c *gin.Context, mf *models.MediaFile, kind, path, variant string) {
	if h.cache == nil {
		return
	}
	if _, err := h.cache.RegisterFile(c.Request.Context(), storage.RegisterInput{
		SpaceID:   mf.SpaceID,
		LibraryID: mf.LibraryID,
		MediaID:   mf.ID,
		Kind:      kind,
		Variant:   variant,
		Path:      path,
	}); err != nil {
		log.Printf("[WARN] 缓存资产登记失败: mediaID=%d, kind=%s, err=%v", mf.ID, kind, err)
	}
}

// DownloadMediaFile GET /api/library/media/:id/download
// 鉴权后回传媒体原始文件（图片与视频一视同仁，不转码/不转换），以附件形式下载。
// 经 c.File 流式回传，天然支持 HTTP Range（断点续传）。
func (h *Handler) DownloadMediaFile(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}

	mf, err := h.loadMediaForViewer(c, spaceID, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
		return
	}
	h.serveDownload(c, mf)
}

// serveDownload 以附件形式回传媒体原始文件。抽出供鉴权版与分享版（FR-43）共用。
func (h *Handler) serveDownload(c *gin.Context, mf *models.MediaFile) {
	if strings.HasPrefix(mf.FilePath, "smb://") {
		c.JSON(http.StatusBadRequest, gin.H{"code": "UNSUPPORTED_PATH", "message": "暂不支持 SMB 文件下载"})
		return
	}
	if _, err := os.Stat(mf.FilePath); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "FILE_NOT_FOUND", "message": "文件不可访问"})
		return
	}

	// 以附件下载，文件名用真实文件名；按 RFC 5987 编码以兼容中文/特殊字符并防头注入
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(mf.FileName))
	c.File(mf.FilePath)
}

// 批量打包下载上限（FR-91）：超限直接拒绝，避免一次性打包过大批量拖垮服务/浏览器
const (
	batchDownloadMaxCount = 500        // 单次最多打包文件数
	batchDownloadMaxBytes = 5 << 30    // 单次最多打包总大小（5 GiB）
	batchZipFileName      = "媒体打包.zip" // 下载附件名
)

// BatchDownloadMediaFiles GET /api/library/media/batch-download?ids=1,2,3
// 将选中媒体的原文件用 archive/zip 流式打包为附件下载（FR-91，扩 FR-42）。
// 边写边 flush、不一次性读入内存；smb:// 路径项与磁盘不可访问项跳过（响应头 X-Skipped 计数）；
// 用请求 context 控制取消，不设整体超时（规避大文件被客户端超时掐断）。
func (h *Handler) BatchDownloadMediaFiles(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	ids := parseBatchIDs(c.Query("ids"))
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_IDS", "message": "未提供有效的 ids"})
		return
	}
	if len(ids) > batchDownloadMaxCount {
		c.JSON(http.StatusBadRequest, gin.H{"code": "TOO_MANY", "message": "选中数量超过上限"})
		return
	}

	files, err := h.library.GetMediaFilesByIDsInSpace(spaceID, ids)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "QUERY_FAILED", "message": "查询失败"})
		return
	}
	// 家长控制（FR2-051）：批量下载仅含访客可见分级。
	files = filterMediaByMaxRating(files, h.viewerMaxContentRating(c, spaceID))

	// 预估总大小：超限在写任何字节前拒绝，便于客户端拿到 JSON 错误而非半截 zip
	var totalBytes int64
	for i := range files {
		totalBytes += files[i].FileSize
	}
	if totalBytes > batchDownloadMaxBytes {
		c.JSON(http.StatusBadRequest, gin.H{"code": "TOO_LARGE", "message": "选中文件总大小超过上限"})
		return
	}

	// 响应头：附件名按 RFC 5987 编码防中文乱码与头注入；chunked 流式输出
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(batchZipFileName))
	c.Header("Content-Type", "application/zip")
	c.Writer.WriteHeader(http.StatusOK)

	skipped := h.streamZip(c, files)
	// 跳过计数经尾部头回传（chunked 下浏览器仍可读到 trailer 之外的普通头需在写前设置，
	// 故 X-Skipped 仅作日志佐证，前端以 zip 内实际文件数为准）
	if skipped > 0 {
		log.Printf("[INFO] 批量打包下载跳过 %d 项（smb 或不可访问）", skipped)
	}
}

// streamZip 把 files 的原文件逐个写入 zip 并即时 flush，返回跳过项数。
// 跳过：smb:// 路径、打开失败的文件。任一文件中途出错记日志并继续，不中断整批。
func (h *Handler) streamZip(c *gin.Context, files []models.MediaFile) int {
	zw := zip.NewWriter(c.Writer)
	// 响应已边写边发，收尾关闭出错无从恢复，忽略
	defer func() { _ = zw.Close() }()
	flusher, _ := c.Writer.(http.Flusher)

	skipped := 0
	for i := range files {
		select {
		case <-c.Request.Context().Done():
			return skipped // 客户端断开，停止打包
		default:
		}
		if addZipEntry(zw, &files[i]) {
			// 中途 flush 仅为边写边推，出错由后续写入暴露，忽略
			_ = zw.Flush()
			if flusher != nil {
				flusher.Flush() // 边写边推，避免在内存中累积整包
			}
		} else {
			skipped++
		}
	}
	return skipped
}

// addZipEntry 把单个媒体原文件写入 zip。smb:// 或打开失败返回 false（跳过）。
func addZipEntry(zw *zip.Writer, mf *models.MediaFile) bool {
	if strings.HasPrefix(mf.FilePath, "smb://") {
		return false
	}
	f, err := os.Open(mf.FilePath)
	if err != nil {
		log.Printf("[WARN] 批量打包下载跳过不可访问文件 id=%d: %v", mf.ID, err)
		return false
	}
	// 只读打开的源文件，关闭出错无副作用，忽略
	defer func() { _ = f.Close() }()

	w, err := zw.Create(mf.FileName)
	if err != nil {
		log.Printf("[WARN] 批量打包下载创建 zip 条目失败 id=%d: %v", mf.ID, err)
		return false
	}
	if _, err := io.Copy(w, f); err != nil {
		log.Printf("[WARN] 批量打包下载写入文件出错 id=%d: %v", mf.ID, err)
		return false
	}
	return true
}

// parseBatchIDs 解析逗号分隔的 id 列表，忽略空白与非法项。
func parseBatchIDs(raw string) []int64 {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	ids := make([]int64, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		if err != nil || v <= 0 {
			continue
		}
		ids = append(ids, v)
	}
	return ids
}

// ListMediaExtensions GET /api/library/extensions
func (h *Handler) ListMediaExtensions(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	libraryID, err := strconv.ParseInt(c.Query("library_id"), 10, 64)
	if err != nil || libraryID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_LIBRARY_ID", "message": "无效的 library_id"})
		return
	}

	items, err := h.library.ListMediaExtensionsInSpace(spaceID, libraryID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// AddMediaExtension POST /api/library/extensions
func (h *Handler) AddMediaExtension(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	var req struct {
		LibraryID int64  `json:"library_id" binding:"required"`
		Extension string `json:"extension" binding:"required"`
		Type      string `json:"type" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "请求参数错误"})
		return
	}
	if err := h.library.AddMediaExtensionInSpace(spaceID, req.LibraryID, req.Extension, req.Type); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_EXTENSION", "message": err.Error()})
		return
	}
	c.Status(http.StatusCreated)
}

// DeleteMediaExtension DELETE /api/library/extensions
// 删除媒体库自定义后缀（FR-64）：内置后缀不可删，删除不存在的后缀返回 400。
func (h *Handler) DeleteMediaExtension(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	libraryID, err := strconv.ParseInt(c.Query("library_id"), 10, 64)
	if err != nil || libraryID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_LIBRARY_ID", "message": "无效的 library_id"})
		return
	}
	extension := c.Query("extension")
	if extension == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_EXTENSION", "message": "extension 不能为空"})
		return
	}
	if err := h.library.DeleteMediaExtensionInSpace(spaceID, libraryID, extension); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "DELETE_EXTENSION_FAILED", "message": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}
