package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"jianvideo/internal/db/models"
	"jianvideo/internal/playback"
)

// ─── 管理端点（鉴权后 /api/shares，受 APIGuard 保护）─────────────────────

// CreateShare POST /api/shares
// 为指定媒体 / 相册创建分享链接。请求体 {resource_type, resource_id, expires_in_hours?}，
// expires_in_hours>0 设过期、否则永不过期。
func (h *Handler) CreateShare(c *gin.Context) {
	if h.share == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SHARE_UNAVAILABLE", "message": "分享服务未启用"})
		return
	}
	var req struct {
		ResourceType   string `json:"resource_type"`
		ResourceID     int64  `json:"resource_id"`
		ExpiresInHours int    `json:"expires_in_hours"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "请求参数错误"})
		return
	}

	// 校验被分享资源存在
	switch req.ResourceType {
	case models.ShareResourceMedia:
		if _, err := h.library.GetMediaFileByID(req.ResourceID); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": "RESOURCE_NOT_FOUND", "message": "媒体不存在"})
			return
		}
	case models.ShareResourceAlbum:
		if _, err := h.library.GetAlbumByID(req.ResourceID); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": "RESOURCE_NOT_FOUND", "message": "相册不存在"})
			return
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_TYPE", "message": "非法分享资源类型"})
		return
	}

	var expiresAt *time.Time
	if req.ExpiresInHours > 0 {
		t := time.Now().Add(time.Duration(req.ExpiresInHours) * time.Hour)
		expiresAt = &t
	}
	sh, err := h.share.Create(req.ResourceType, req.ResourceID, expiresAt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "CREATE_FAILED", "message": "创建分享失败"})
		return
	}
	c.JSON(http.StatusCreated, sh)
}

// ListShares GET /api/shares 列出全部分享（含已过期，供管理展示）。
func (h *Handler) ListShares(c *gin.Context) {
	if h.share == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SHARE_UNAVAILABLE", "message": "分享服务未启用"})
		return
	}
	shares, err := h.share.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询分享失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"shares": shares})
}

// RevokeShare DELETE /api/shares/:token 撤销分享。
func (h *Handler) RevokeShare(c *gin.Context) {
	if h.share == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SHARE_UNAVAILABLE", "message": "分享服务未启用"})
		return
	}
	if err := h.share.Revoke(c.Param("token")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "REVOKE_FAILED", "message": "撤销分享失败"})
		return
	}
	c.Status(http.StatusNoContent)
}

// ─── 公开端点（免登 /api/share/:token，APIGuard 已豁免）─────────────────

// shareAuth 校验 token + 过期并把 share 存入 context；失败统一 404，不泄露分享是否存在。
func (h *Handler) shareAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.share == nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"code": "SHARE_UNAVAILABLE", "message": "分享服务未启用"})
			return
		}
		sh, err := h.share.Get(c.Param("token"))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"code": "SHARE_NOT_FOUND", "message": "分享不存在或已过期"})
			return
		}
		c.Set("share", sh)
		c.Next()
	}
}

// currentShare 从 context 取已由 shareAuth 校验的 share。
func currentShare(c *gin.Context) *models.Share {
	v, _ := c.Get("share")
	sh, _ := v.(*models.Share)
	return sh
}

// shareAllowsMedia 范围校验：mediaID 是否在分享范围内（== 被分享媒体，或 ∈ 被分享相册成员）。
func (h *Handler) shareAllowsMedia(sh *models.Share, mediaID int64) bool {
	switch sh.ResourceType {
	case models.ShareResourceMedia:
		return sh.ResourceID == mediaID
	case models.ShareResourceAlbum:
		ok, err := h.library.IsMediaInAlbum(sh.ResourceID, mediaID)
		return err == nil && ok
	}
	return false
}

// resolveShareMedia 取路径 :mediaId、做范围校验并取媒体记录；任一不满足直接写响应并返回 nil。
// 越权 / 不在范围 / 不存在一律 404，不区分以免泄露。
func (h *Handler) resolveShareMedia(c *gin.Context) *models.MediaFile {
	sh := currentShare(c)
	mediaID, err := strconv.ParseInt(c.Param("mediaId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return nil
	}
	if sh == nil || !h.shareAllowsMedia(sh, mediaID) {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "资源不存在"})
		return nil
	}
	mf, err := h.library.GetMediaFileByID(mediaID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
		return nil
	}
	return mf
}

// ShareInfo GET /api/share/:token 返回分享元信息（媒体或相册成员）。
func (h *Handler) ShareInfo(c *gin.Context) {
	sh := currentShare(c)
	resp := gin.H{"resource_type": sh.ResourceType, "expires_at": sh.ExpiresAt}
	switch sh.ResourceType {
	case models.ShareResourceMedia:
		mf, err := h.library.GetMediaFileByID(sh.ResourceID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
			return
		}
		resp["media"] = mf
	case models.ShareResourceAlbum:
		album, err := h.library.GetAlbumByID(sh.ResourceID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "相册不存在"})
			return
		}
		items, err := h.library.ListAlbumItems(sh.ResourceID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询相册成员失败"})
			return
		}
		resp["album"] = album
		resp["items"] = items
	}
	c.JSON(http.StatusOK, resp)
}

// ShareRaw GET /api/share/:token/media/:mediaId/raw 图片在线查看。
func (h *Handler) ShareRaw(c *gin.Context) {
	mf := h.resolveShareMedia(c)
	if mf == nil {
		return
	}
	h.serveRawImage(c, mf)
}

// ShareThumbnail GET /api/share/:token/media/:mediaId/thumbnail 缩略图。
func (h *Handler) ShareThumbnail(c *gin.Context) {
	mf := h.resolveShareMedia(c)
	if mf == nil {
		return
	}
	h.serveThumbnail(c, mf)
}

// ShareDownload GET /api/share/:token/media/:mediaId/download 原文件下载。
func (h *Handler) ShareDownload(c *gin.Context) {
	mf := h.resolveShareMedia(c)
	if mf == nil {
		return
	}
	h.serveDownload(c, mf)
}

// RegisterShareRoutes 注册公开分享路由（免登，APIGuard 已豁免 /api/share/ 前缀）。
// 视频在线播放复用 playback.StreamFile（渐进式 + Range），不公开转码 / HLS 管线（安全边界）。
func RegisterShareRoutes(r *gin.Engine, h *Handler, pbSvc *playback.Service) {
	grp := r.Group("/api/share/:token", h.shareAuth())
	grp.GET("", h.ShareInfo)
	grp.GET("/media/:mediaId/raw", h.ShareRaw)
	grp.GET("/media/:mediaId/thumbnail", h.ShareThumbnail)
	grp.GET("/media/:mediaId/download", h.ShareDownload)
	grp.GET("/media/:mediaId/stream", func(c *gin.Context) {
		mf := h.resolveShareMedia(c)
		if mf == nil {
			return
		}
		if pbSvc == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": "STREAM_UNAVAILABLE", "message": "播放服务未启用"})
			return
		}
		if strings.HasPrefix(mf.FilePath, "smb://") {
			c.JSON(http.StatusBadRequest, gin.H{"code": "UNSUPPORTED_PATH", "message": "暂不支持 SMB 流"})
			return
		}
		pbSvc.StreamFile(c.Writer, c.Request, mf.ID, mf.FilePath, mf.FileSize, mf.Duration)
	})
}
