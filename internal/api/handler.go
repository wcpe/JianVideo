package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/playback"
	"github.com/wcpe/JianVideo/internal/player"
	"github.com/wcpe/JianVideo/internal/settings"
	"github.com/wcpe/JianVideo/internal/share"
	"github.com/wcpe/JianVideo/internal/smb"
	"github.com/wcpe/JianVideo/internal/transcoder"
	"github.com/wcpe/JianVideo/internal/update"
)

// SubtitleTrack 表示一个外挂字幕轨道。
type SubtitleTrack struct {
	Index    int    `json:"index"`
	FileName string `json:"file_name"`
	Format   string `json:"format"`
	URL      string `json:"url"`
}

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
}

// NewHandler 创建处理器。
func NewHandler(lib *library.Service) *Handler {
	return &Handler{library: lib, updateSvc: update.NewService()}
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

// WithHLSPreSlice 注入 HLS 预切片所需的目录与 HLSManager。
// 任一参数为空则禁用预切片（用于测试或无 ffmpeg 环境）。
func (h *Handler) WithHLSPreSlice(hlsDir string, hlsMgr *player.HLSManager) *Handler {
	h.hlsDir = hlsDir
	h.hlsMgr = hlsMgr
	return h
}

// ListLibraryPaths GET /api/library/paths
// 每项附带 media_count（该库已索引、未软删的媒体文件数量），供存储库卡片展示。
func (h *Handler) ListLibraryPaths(c *gin.Context) {
	items, err := h.library.ListLibraryPathViews()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// CreateLibraryPath POST /api/library/paths
func (h *Handler) CreateLibraryPath(c *gin.Context) {
	var req struct {
		Path        string `json:"path" binding:"required"`
		Type        string `json:"type" binding:"required"`
		Label       string `json:"label"`
		SMBHost     string `json:"smb_host"`
		SMBUsername string `json:"smb_username"`
		SMBPassword string `json:"smb_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "请求参数错误"})
		return
	}

	lp, err := h.library.CreateLibraryPath(req.Path, req.Type, req.Label)
	if err != nil {
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

	dataDir := filepath.Join("data")
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
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}

	if err := h.library.DeleteLibraryPath(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DELETE_FAILED", "message": "删除失败"})
		return
	}
	c.Status(http.StatusNoContent)
}

// ListMediaFiles GET /api/library/media
func (h *Handler) ListMediaFiles(c *gin.Context) {
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
	items, total, err := h.library.ListMediaFilesFiltered(filter, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetMediaFile GET /api/library/media/:id
func (h *Handler) GetMediaFile(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}

	mf, err := h.library.GetMediaFileByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
		return
	}
	c.JSON(http.StatusOK, mf)
}

// DeleteMediaFile DELETE /api/library/media/:id
func (h *Handler) DeleteMediaFile(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}

	if err := h.library.DeleteMediaFile(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DELETE_FAILED", "message": "删除失败"})
		return
	}
	c.Status(http.StatusNoContent)
}

// ListRecycleMediaFiles GET /api/library/recycle
// 列出回收站内全部已软删的媒体文件（FR-25）。
func (h *Handler) ListRecycleMediaFiles(c *gin.Context) {
	items, err := h.library.ListDeletedMediaFiles()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// RestoreMediaFile POST /api/library/media/:id/restore
// 从回收站还原指定媒体，使其回到常规列表（FR-25）。
func (h *Handler) RestoreMediaFile(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}

	if err := h.library.RestoreMediaFile(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "回收站中不存在该媒体文件"})
		return
	}
	c.Status(http.StatusNoContent)
}

// RecycleCleanup POST /api/library/recycle/cleanup
// 清理回收站（FR-26）：把全部软删项的源文件移动到其所在盘符对应的回收站目录（按删除日期分子目录），
// 移动成功后删除 media_files 记录。盘符回收站路径取自 FR-24 设置键 recycle_bin_paths（JSON：盘符→目录）。
// 存在软删项所在盘符未配置（含 SMB/无盘符项）时返回 409，不移动任何文件。
func (h *Handler) RecycleCleanup(c *gin.Context) {
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

	result, err := h.library.CleanupRecycle(drivePaths)
	if err != nil {
		if errors.Is(err, library.ErrRecycleBinPathUnset) {
			c.JSON(http.StatusConflict, gin.H{"code": "RECYCLE_PATH_UNSET", "message": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": "CLEANUP_FAILED", "message": "清理回收站失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"moved": result.Moved, "failed": result.Failed})
}

// RenameMediaFile PUT /api/library/media/:id/rename
// 请求体：{"new_name": "新文件名.mp4"}，磁盘改名 + 更新数据库记录。
func (h *Handler) RenameMediaFile(c *gin.Context) {
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

	mf, err := h.library.RenameMediaFile(id, req.NewName)
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

// UpdateDisplayName PUT /api/library/media/:id/display-name
// 请求体：{"display_name": "..."}，仅更新库内显示名，不动磁盘文件名。空串表示清除显示名。
func (h *Handler) UpdateDisplayName(c *gin.Context) {
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

	mf, err := h.library.UpdateDisplayName(id, req.DisplayName)
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

// BrowseDirectory GET /api/library/browse
func (h *Handler) BrowseDirectory(c *gin.Context) {
	libraryID, err := strconv.ParseInt(c.Query("library_id"), 10, 64)
	if err != nil || libraryID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_LIBRARY_ID", "message": "无效的 library_id"})
		return
	}

	parentPath := c.Query("parent_path")
	if parentPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARENT_PATH", "message": "parent_path 不能为空"})
		return
	}

	resp, err := h.library.BrowseDirectory(libraryID, parentPath)
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

	dataDir := filepath.Join("data")
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
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}

	lp, err := h.library.GetLibraryPathByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "目录不存在"})
		return
	}

	// 缺省/非法值回退增量，向后兼容既有调用方（FR-27）
	mode := library.NormalizeScanMode(c.Query("mode"))

	// 扫描触发后，对媒体库中所有视频文件触发 HLS 预切片（如果启用了）。
	// 预切片失败不阻塞扫描响应（仅记日志）。
	if transcoder.IsFFmpegAvailable() && h.hlsDir != "" && h.hlsMgr != nil {
		go h.preSliceAllVideos(context.Background())
	}

	// 队列已注入：入队排队、单 worker 串行执行（FR-29）
	if h.scanQueue != nil {
		taskID, err := h.scanQueue.Enqueue(id, lp.Path, lp.Type, mode)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "ENQUEUE_FAILED", "message": "扫描入队失败"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "queued", "task_id": taskID})
		return
	}

	// 未注入队列：回退原直接异步扫描
	h.library.StartAsyncScan(id, lp.Path, lp.Type, mode)
	c.JSON(http.StatusOK, gin.H{"status": "scanning"})
}

// ListScanTasks GET /api/library/scan/tasks
// 返回扫描任务列表（按入队时间倒序）与当前进行中的任务（FR-29）。
// 当前 running 任务的进度用实时全局扫描状态覆盖，已完成任务用其持久化的 scanned_files。
func (h *Handler) ListScanTasks(c *gin.Context) {
	if h.scanQueue == nil {
		c.JSON(http.StatusOK, gin.H{"tasks": []models.ScanTask{}, "current": nil})
		return
	}

	tasks, err := h.scanQueue.ListTasks()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询任务失败"})
		return
	}

	// 单 worker 串行 ⇒ 全局 ScanStatus 始终对应当前 running 任务；把实时进度覆盖到该任务
	live := library.GetScanStatus()
	var current *models.ScanTask
	for i := range tasks {
		if tasks[i].Status == models.ScanTaskStatusRunning {
			if live.Status == "scanning" {
				tasks[i].ScannedFiles = live.ScannedFiles
				tasks[i].TotalFiles = live.TotalFiles
			}
			current = &tasks[i]
			break
		}
	}

	c.JSON(http.StatusOK, gin.H{"tasks": tasks, "current": current})
}

// ScanProgressSSE GET /api/library/scan/progress
// 以 SSE 流推送当前扫描进度，每 500ms 轮询一次、仅在状态变化时推送。
// 连接持续保持打开（不在 completed/error 后主动关闭），以便接收后续扫描进度；
// 仅在客户端断开（ctx.Done）时结束。避免浏览器 EventSource 在终态后反复重连成风暴。
func (h *Handler) ScanProgressSSE(c *gin.Context) {
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

// preSliceAllVideos 对媒体库中所有视频文件触发预切片（异步执行）。
func (h *Handler) preSliceAllVideos(ctx context.Context) {
	files, _, err := h.library.ListMediaFiles(0, "", "", 1, 10000)
	if err != nil {
		log.Printf("[WARN] 预切片：获取媒体列表失败: %v", err)
		return
	}
	for _, mf := range files {
		if strings.HasPrefix(mf.FilePath, "smb://") {
			continue
		}
		if _, err := os.Stat(mf.FilePath); err != nil {
			continue
		}
		if _, err := transcoder.PreSlice(ctx, mf.ID, mf.FilePath, mf.Width, mf.Height, h.hlsMgr, h.hlsDir); err != nil {
			log.Printf("[WARN] 预切片失败: mediaID=%d, err=%v", mf.ID, err)
		}
	}
}

// GetThumbnail GET /api/library/thumbnail/:id
func (h *Handler) GetThumbnail(c *gin.Context) {
	id, ok := parseMediaID(c)
	if !ok {
		return
	}

	mf, err := h.library.GetMediaFileByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
		return
	}
	h.serveThumbnail(c, mf)
}

// serveThumbnail 回传缩略图（不存在则异步生成并返回 202）。抽出供鉴权版与分享版（FR-43）共用。
func (h *Handler) serveThumbnail(c *gin.Context, mf *models.MediaFile) {
	thumbnailPath := library.FindThumbnailPath(mf.FilePath)
	if _, err := os.Stat(thumbnailPath); err != nil {
		// 缩略图不存在，异步生成后返回 202
		go library.GenerateThumbnail(mf.FilePath)
		c.JSON(http.StatusAccepted, gin.H{"code": "GENERATING", "message": "缩略图生成中"})
		return
	}

	c.File(thumbnailPath)
}

// GetRawImage GET /api/library/media/:id/raw
func (h *Handler) GetRawImage(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}

	mf, err := h.library.GetMediaFileByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
		return
	}
	h.serveRawImage(c, mf)
}

// serveRawImage 回传图片原始内容（HEIC/RAW 经 ImageMagick 转 JPEG，FR-37）。
// 抽出供鉴权版 GetRawImage 与分享版（FR-43）共用，行为一致。
func (h *Handler) serveRawImage(c *gin.Context, mf *models.MediaFile) {
	mediaType, ok := h.library.MediaTypeByPathForLibrary(mf.LibraryID, mf.FilePath)
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

// DownloadMediaFile GET /api/library/media/:id/download
// 鉴权后回传媒体原始文件（图片与视频一视同仁，不转码/不转换），以附件形式下载。
// 经 c.File 流式回传，天然支持 HTTP Range（断点续传）。
func (h *Handler) DownloadMediaFile(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}

	mf, err := h.library.GetMediaFileByID(id)
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

// ListMediaExtensions GET /api/library/extensions
func (h *Handler) ListMediaExtensions(c *gin.Context) {
	libraryID, err := strconv.ParseInt(c.Query("library_id"), 10, 64)
	if err != nil || libraryID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_LIBRARY_ID", "message": "无效的 library_id"})
		return
	}

	items, err := h.library.ListMediaExtensions(libraryID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// AddMediaExtension POST /api/library/extensions
func (h *Handler) AddMediaExtension(c *gin.Context) {
	var req struct {
		LibraryID int64  `json:"library_id" binding:"required"`
		Extension string `json:"extension" binding:"required"`
		Type      string `json:"type" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "请求参数错误"})
		return
	}
	if err := h.library.AddMediaExtension(req.LibraryID, req.Extension, req.Type); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_EXTENSION", "message": err.Error()})
		return
	}
	c.Status(http.StatusCreated)
}

// GetSubtitles 返回媒体文件的外挂字幕轨道列表。
// GET /api/play/:id/subtitles
func (h *Handler) GetSubtitles(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}

	mf, err := h.library.GetMediaFileByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
		return
	}

	// SMB 路径暂不支持字幕查找
	if strings.HasPrefix(mf.FilePath, "smb://") {
		c.JSON(http.StatusOK, gin.H{"tracks": []SubtitleTrack{}})
		return
	}

	files, err := transcoder.FindSubtitleFiles(mf.FilePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "字幕查找失败"})
		return
	}

	tracks := make([]SubtitleTrack, len(files))
	for i, f := range files {
		tracks[i] = SubtitleTrack{
			Index:    i,
			FileName: filepath.Base(f.Path),
			Format:   f.Format,
			URL:      fmt.Sprintf("/api/play/%d/subtitles/%d", id, i),
		}
	}

	c.JSON(http.StatusOK, gin.H{"tracks": tracks})
}

// GetSubtitleContent 返回指定字幕轨道的 WebVTT 内容。
// GET /api/play/:id/subtitles/:index
func (h *Handler) GetSubtitleContent(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}

	index, err := strconv.Atoi(c.Param("index"))
	if err != nil || index < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INDEX", "message": "无效的字幕索引"})
		return
	}

	mf, err := h.library.GetMediaFileByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
		return
	}

	files, err := transcoder.FindSubtitleFiles(mf.FilePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "字幕查找失败"})
		return
	}

	if index >= len(files) {
		c.JSON(http.StatusNotFound, gin.H{"code": "INDEX_OUT_OF_RANGE", "message": "字幕索引超出范围"})
		return
	}

	vtt, err := transcoder.ConvertSubtitleFile(files[index].Path)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "CONVERT_FAILED", "message": "字幕转换失败"})
		return
	}

	if vtt == "" {
		c.Status(http.StatusNoContent)
		return
	}

	c.Data(http.StatusOK, "text/vtt; charset=utf-8", []byte(vtt))
}
