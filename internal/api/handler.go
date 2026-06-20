package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"jianvideo/internal/library"
	"jianvideo/internal/player"
	"jianvideo/internal/smb"
	"jianvideo/internal/transcoder"
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
	library *library.Service
	hlsDir  string             // HLS 切片输出根目录
	hlsMgr  *player.HLSManager // 用于写入 master.m3u8
}

// NewHandler 创建处理器。
func NewHandler(lib *library.Service) *Handler {
	return &Handler{library: lib}
}

// WithHLSPreSlice 注入 HLS 预切片所需的目录与 HLSManager。
// 任一参数为空则禁用预切片（用于测试或无 ffmpeg 环境）。
func (h *Handler) WithHLSPreSlice(hlsDir string, hlsMgr *player.HLSManager) *Handler {
	h.hlsDir = hlsDir
	h.hlsMgr = hlsMgr
	return h
}

// ListLibraryPaths GET /api/library/paths
func (h *Handler) ListLibraryPaths(c *gin.Context) {
	items, err := h.library.ListLibraryPaths()
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

	// 从环境变量读取主密码，未设置时使用默认值
	masterPwd := os.Getenv("SMB_MASTER_PASSWORD")
	if masterPwd == "" {
		masterPwd = "default-master-password"
		log.Printf("[WARN] SMB_MASTER_PASSWORD 未设置，使用默认主密码，请在生产环境中设置环境变量")
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

	items, total, err := h.library.ListMediaFiles(libraryID, sort, search, page, pageSize)
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
		Host      string `json:"host" binding:"required"`
		Username  string `json:"username"`
		Password  string `json:"password"`
		Share     string `json:"share"`
		MasterPwd string `json:"master_password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "请求参数错误"})
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

	if err := store.Save(req.MasterPwd, creds); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "SAVE_FAILED", "message": "保存凭据失败"})
		return
	}

	h.library.SetSMBCredentialStore(store)
	c.Status(http.StatusNoContent)
}

// ScanLibrary POST /api/library/scan/:id
// 启动异步扫描，立即返回 {"status": "scanning"}。
// 扫描进度通过 GET /api/library/scan/progress SSE 端点获取。
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

	h.library.StartAsyncScan(id, lp.Path, lp.Type)

	// 扫描启动后，对媒体库中所有视频文件触发 HLS 预切片（如果启用了）。
	// 预切片失败不阻塞扫描响应（仅记日志）。
	if transcoder.IsFFmpegAvailable() && h.hlsDir != "" && h.hlsMgr != nil {
		go h.preSliceAllVideos(context.Background())
	}

	c.JSON(http.StatusOK, gin.H{"status": "scanning"})
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

	// 查找缩略图文件
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
	mediaType, ok := h.library.MediaTypeByPathForLibrary(mf.LibraryID, mf.FilePath)
	if !ok || mediaType != library.MediaTypeImage {
		c.JSON(http.StatusBadRequest, gin.H{"code": "NOT_IMAGE", "message": "仅支持图片 raw 访问"})
		return
	}
	if strings.HasPrefix(mf.FilePath, "smb://") {
		c.JSON(http.StatusBadRequest, gin.H{"code": "UNSUPPORTED_PATH", "message": "暂不支持 SMB 图片 raw 访问"})
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
