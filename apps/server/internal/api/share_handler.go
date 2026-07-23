package api

import (
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/auth"
	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/playback"
	thumbsvc "github.com/wcpe/JianVideo/internal/thumbnail"
)

// share.accessed 进程内采样：每 token 在窗口内最多记 1 条，避免公开访问刷爆审计。
var (
	shareAccessedLast     sync.Map // token -> time.Time
	shareAccessedInterval = 60 * time.Second
)

// setShareAccessedIntervalForTest 覆盖采样间隔，仅供测试；返回还原函数。
func setShareAccessedIntervalForTest(d time.Duration) (restore func()) {
	old := shareAccessedInterval
	shareAccessedInterval = d
	return func() { shareAccessedInterval = old }
}

// clearShareAccessedThrottleForTest 清空采样状态，仅供测试。
func clearShareAccessedThrottleForTest() {
	shareAccessedLast.Range(func(k, _ any) bool {
		shareAccessedLast.Delete(k)
		return true
	})
}

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
		// Password 可选访问密码（FR-78）；空表示不设密码，后端以 bcrypt 哈希存储。
		Password string `json:"password"`
		// MaxUses 可选最大访问次数（FR-78）；0 表示无限。
		MaxUses int `json:"max_uses"`
		// AllowDownload 是否允许公开下载（FR2-055）；缺省 true。
		AllowDownload *bool `json:"allow_download"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "请求参数错误"})
		return
	}

	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}

	// 校验被分享资源存在且属于当前 Space；媒体须对创建者可见（FR2-051，公开访问不套访客 max）
	switch req.ResourceType {
	case models.ShareResourceMedia:
		if _, err := h.loadMediaForViewer(c, spaceID, req.ResourceID); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": "RESOURCE_NOT_FOUND", "message": "媒体不存在"})
			return
		}
	case models.ShareResourceAlbum:
		if _, err := h.library.GetAlbumByIDInSpace(spaceID, req.ResourceID); err != nil {
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
	allowDownload := true
	if req.AllowDownload != nil {
		allowDownload = *req.AllowDownload
	}
	sh, err := h.share.CreateInSpace(spaceID, req.ResourceType, req.ResourceID, expiresAt, req.Password, req.MaxUses, allowDownload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "CREATE_FAILED", "message": "创建分享失败"})
		return
	}
	if h.audit != nil {
		_ = h.audit.Record(c.Request.Context(), audit.EventInput{
			Scope:        audit.ScopeSpace,
			SpaceID:      spaceID,
			ActorType:    "user",
			ActorID:      actorIDFromContext(c),
			Action:       "share.created",
			ResourceType: "share",
			ResourceID:   sh.Token,
			After: map[string]any{
				"resource_type":  sh.ResourceType,
				"resource_id":    sh.ResourceID,
				"allow_download": sh.AllowDownload,
				"max_uses":       sh.MaxUses,
			},
		})
	}
	c.JSON(http.StatusCreated, sh)
}

// ListShares GET /api/shares 列出全部分享（含已过期，供管理展示）。
func (h *Handler) ListShares(c *gin.Context) {
	if h.share == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SHARE_UNAVAILABLE", "message": "分享服务未启用"})
		return
	}
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	shares, err := h.share.ListInSpace(spaceID)
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
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	token := c.Param("token")
	if err := h.share.RevokeInSpace(spaceID, token); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "REVOKE_FAILED", "message": "撤销分享失败"})
		return
	}
	if h.audit != nil {
		_ = h.audit.Record(c.Request.Context(), audit.EventInput{
			Scope:        audit.ScopeSpace,
			SpaceID:      spaceID,
			ActorType:    "user",
			ActorID:      actorIDFromContext(c),
			Action:       "share.revoked",
			ResourceType: "share",
			ResourceID:   token,
		})
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
		ok, err := h.library.IsMediaInAlbumInSpace(sh.SpaceID, sh.ResourceID, mediaID)
		return err == nil && ok
	}
	return false
}

// resolveShareMedia 取路径 :mediaId、做范围校验并取媒体记录；任一不满足直接写响应并返回 nil。
// 越权 / 不在范围 / 不存在一律 404，不区分以免泄露。
// accessType 为 stream/download/raw/thumbnail，供 share.accessed 审计采样（FR2-055）。
func (h *Handler) resolveShareMedia(c *gin.Context, accessType string) *models.MediaFile {
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
	mf, err := h.library.GetMediaFileByIDInSpace(sh.SpaceID, mediaID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
		return nil
	}
	// 访问资源时校验密码（带 X-Share-Password 头时；FR-78）并原子消费一次访问额度。
	// 密码门禁的权威边界在 ShareInfo（无正确密码拿不到 mediaId）；此处对带头请求再校验，
	// 限次自增发生在每次成功的资源访问上。任一失败统一 404，不区分以免泄露。
	if err := h.share.VerifyPassword(sh, c.GetHeader("X-Share-Password")); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "分享不存在或已过期"})
		return nil
	}
	if err := h.share.ConsumeUse(sh.Token); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "分享不存在或已过期"})
		return nil
	}
	// MaxUses=0 时 ConsumeUse 仍成功：accessed 与 used_count 解耦，成功后仍可采样。
	h.maybeRecordShareAccessed(c, sh, mediaID, accessType)
	return mf
}

// maybeRecordShareAccessed 在资源访问成功路径采样写入 share.accessed（每 token 每窗口最多 1 条）。
// ShareInfo 不调用本函数；无 audit 注入时静默跳过。
func (h *Handler) maybeRecordShareAccessed(c *gin.Context, sh *models.Share, mediaID int64, accessType string) {
	if h.audit == nil || sh == nil {
		return
	}
	now := time.Now()
	if last, ok := shareAccessedLast.Load(sh.Token); ok {
		if t, ok := last.(time.Time); ok && now.Sub(t) < shareAccessedInterval {
			return
		}
	}
	shareAccessedLast.Store(sh.Token, now)
	_ = h.audit.Record(c.Request.Context(), audit.EventInput{
		Scope:        audit.ScopeSpace,
		SpaceID:      sh.SpaceID,
		ActorType:    "anonymous",
		Action:       "share.accessed",
		ResourceType: "share",
		ResourceID:   sh.Token,
		Metadata: map[string]any{
			"media_id":    mediaID,
			"access_type": accessType,
			"ip_hash":     auth.HashIP(c.ClientIP()),
		},
	})
}

// ShareInfo GET /api/share/:token 返回分享元信息（媒体或相册成员）。
// 密码门禁（FR-78）：分享设密码且未带 / 带错密码时，仅返回 {requires_password:true}、
// 不含任何 media/album 元信息（访客据此弹密码框，又不泄露内容、不区分过期/撤销）；
// 校验通过才返回完整元信息。本端点不消费访问额度（查看元信息不耗次）。
func (h *Handler) ShareInfo(c *gin.Context) {
	sh := currentShare(c)
	if err := h.share.VerifyPassword(sh, c.GetHeader("X-Share-Password")); err != nil {
		// 需要密码但未通过：只回提示，不泄露任何内容元信息。
		c.JSON(http.StatusOK, gin.H{"resource_type": sh.ResourceType, "requires_password": true})
		return
	}
	resp := gin.H{
		"resource_type":     sh.ResourceType,
		"expires_at":        sh.ExpiresAt,
		"requires_password": sh.PasswordHash != "",
		"allow_download":    sh.AllowDownload,
	}
	switch sh.ResourceType {
	case models.ShareResourceMedia:
		mf, err := h.library.GetMediaFileByIDInSpace(sh.SpaceID, sh.ResourceID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
			return
		}
		resp["media"] = mf
	case models.ShareResourceAlbum:
		album, err := h.library.GetAlbumByIDInSpace(sh.SpaceID, sh.ResourceID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "相册不存在"})
			return
		}
		items, err := h.library.ListAlbumItemsInSpace(sh.SpaceID, sh.ResourceID)
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
	mf := h.resolveShareMedia(c, "raw")
	if mf == nil {
		return
	}
	h.serveRawImage(c, mf)
}

// ShareThumbnail GET /api/share/:token/media/:mediaId/thumbnail 缩略图。
// FR2-055：仅读已有缓存文件，缺失返回占位 404，**不**入队生成/转码。
func (h *Handler) ShareThumbnail(c *gin.Context) {
	mf := h.resolveShareMedia(c, "thumbnail")
	if mf == nil {
		return
	}
	h.serveShareThumbnailExistingOnly(c, mf)
}

// ShareDownload GET /api/share/:token/media/:mediaId/download 原文件下载。
// FR2-055：allow_download=false 时统一 404，不区分原因。
func (h *Handler) ShareDownload(c *gin.Context) {
	sh := currentShare(c)
	if sh != nil && !sh.AllowDownload {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "资源不存在"})
		return
	}
	mf := h.resolveShareMedia(c, "download")
	if mf == nil {
		return
	}
	h.serveDownload(c, mf)
}

// serveShareThumbnailExistingOnly 公开分享缩略图：只服务已存在文件，禁止 Ensure/Enqueue/异步生成。
func (h *Handler) serveShareThumbnailExistingOnly(c *gin.Context, mf *models.MediaFile) {
	size := library.NormalizeThumbnailSize(parseThumbnailSize(c))
	if h.thumbnail != nil {
		coverPath, err := h.thumbnail.CurrentCoverPath(c.Request.Context(), mf.SpaceID, mf.ID)
		if err == nil && coverPath != "" {
			if _, statErr := os.Stat(coverPath); statErr == nil {
				c.File(coverPath)
				return
			}
		}
		// FR2-028 分档缓存路径（认证侧 Ensure 写入处）；只读已有文件，不入队
		if path, pathErr := thumbsvc.PathFor(h.thumbnail.DataDir(), mf.SpaceID, mf.ID, size); pathErr == nil {
			if _, statErr := os.Stat(path); statErr == nil {
				c.File(path)
				return
			}
		}
	}
	// 兼容历史 legacy 路径（扫描/旧 GenerateThumbnail 产物）
	thumbnailPath := library.ThumbnailPathForSize(mf.FilePath, size)
	if _, err := os.Stat(thumbnailPath); err == nil {
		c.File(thumbnailPath)
		return
	}
	// 缺失：占位拒绝，不触发生成队列（FR2-055 匿名成本门）。
	c.JSON(http.StatusNotFound, gin.H{"code": "THUMBNAIL_NOT_READY", "message": "缩略图不可用"})
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
		mf := h.resolveShareMedia(c, "stream")
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
