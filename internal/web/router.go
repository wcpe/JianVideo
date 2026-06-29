// Package web 装配 HTTP 路由，整合认证、媒体库、播放与前端静态资源服务。
package web

import (
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/config"
	"github.com/wcpe/JianVideo/internal/api"
	"github.com/wcpe/JianVideo/internal/auth"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/playback"
	"github.com/wcpe/JianVideo/internal/player"
	"github.com/wcpe/JianVideo/internal/settings"
)

// NewRouter 创建并配置路由
// frontendDist 为嵌入的前端静态资源（由 main 通过 go:embed 传入）
// 可选参数 pbSvc 用于挂载 /api/play/:id/stream 等播放路由（HLS 不可用时回退）。
// 可选参数 overrideHandler 用于注入预配置（已挂 HLS 预切片依赖）的 Handler；传 nil 则新建默认 handler。
func NewRouter(cfg *config.Config, db *gorm.DB, hlsMgr *player.HLSManager, frontendDist fs.FS, overrideHandler *api.Handler, pbSvc ...*playback.Service) *gin.Engine {
	r := gin.Default()

	// 静态文件服务（前端嵌入）
	if frontendDist != nil {
		assetsFS, _ := fs.Sub(frontendDist, "frontend/dist/assets")
		r.StaticFS("/assets", http.FS(assetsFS))
		r.NoRoute(func(c *gin.Context) {
			// 非 /api 路径：先尝试命中内嵌 dist 根下的真实文件（PWA 的 sw.js/manifest.webmanifest/
			// workbox-*.js、favicon 等），命中即按真实 MIME 返回；否则走 SPA 兜底。
			// 修复：此前根级资源被一律当 SPA 返回 index.html，致 Service Worker 无法注册、manifest 无效，PWA 失效（FR-45）。
			reqPath := strings.TrimPrefix(c.Request.URL.Path, "/")
			if reqPath != "" && !strings.HasPrefix(c.Request.URL.Path, "/api/") {
				if data, err := fs.ReadFile(frontendDist, "frontend/dist/"+reqPath); err == nil {
					ctype := mime.TypeByExtension(filepath.Ext(reqPath))
					if filepath.Ext(reqPath) == ".webmanifest" {
						ctype = "application/manifest+json"
					}
					if ctype == "" {
						ctype = http.DetectContentType(data)
					}
					c.Data(http.StatusOK, ctype, data)
					return
				}
			}
			// SPA 回退：未匹配的路径返回 index.html
			indexData, err := fs.ReadFile(frontendDist, "frontend/dist/index.html")
			if err != nil {
				c.Status(http.StatusNotFound)
				return
			}
			c.Data(http.StatusOK, "text/html; charset=utf-8", indexData)
		})
	}

	// 认证（从 gorm.DB 提取底层 sql.DB）
	sqlDB, _ := db.DB()
	svc := auth.NewService(sqlDB, cfg.JWTSecret)

	// 全局鉴权中间件：保护除 /api/auth/ 外的所有 /api/* 路由（FR-13）。
	// 必须在注册业务路由之前挂上，确保库 / 播放 / HLS / 设置 / 相册等全部受保护。
	r.Use(auth.APIGuard(cfg.JWTSecret))

	// FR-109：不再自动创建 admin/admin 默认账户。系统无用户时由前端初始化引导页
	// 经 /api/auth/setup 创建首个账户（见下方认证路由）。

	// 创建 API Handler：优先使用调用方注入的（可能已挂 HLS 预切片依赖），否则新建默认。
	libSvc := library.NewService(db)
	var apiHandler *api.Handler
	if overrideHandler != nil {
		apiHandler = overrideHandler
	} else {
		apiHandler = api.NewHandler(libSvc).WithSettings(settings.NewService(db))
	}

	// 注册 API 路由（库路由）
	api.RegisterRoutes(r, apiHandler)

	// HLS 路由：master 动态读取 + 其余静态文件服务挂载 hlsDir
	hlsDir := filepath.Join(filepath.Dir(cfg.DBPath), "hls")
	if hlsMgr != nil {
		api.RegisterHLSRoutes(r, hlsMgr, hlsDir)
	}

	// 播放路由（可选）：当传入 pbSvc 时挂载 /api/play/:id/stream，
	// handler 内部查 db 拿到 filePath 后转发给 pbSvc.StreamFile。
	var playbackSvc *playback.Service
	if len(pbSvc) > 0 && pbSvc[0] != nil {
		playbackSvc = pbSvc[0]
		registerStreamRoute(r, libSvc, playbackSvc)
	}

	// 公开分享路由（FR-43，免登）：APIGuard 已豁免 /api/share/ 前缀，
	// 路由内经 shareAuth 自校验 token；视频流复用 playback（渐进式，不公开转码/HLS）。
	api.RegisterShareRoutes(r, apiHandler, playbackSvc)

	// 认证路由
	apiGroup := r.Group("/api")
	{
		authGroup := apiGroup.Group("/auth")
		{
			authGroup.POST("/login", handleLogin(svc, cfg))
			authGroup.POST("/logout", handleLogout)
			// 首次初始化（FR-109）：免登查询是否需初始化 + 无用户时创建首个账户并自动登录
			authGroup.GET("/setup-status", handleSetupStatus(svc))
			authGroup.POST("/setup", handleSetup(svc, cfg))
		}

		// /api/me 由全局 APIGuard 统一保护，无需再单独挂中间件。
		apiGroup.GET("/me", handleMe)
		// 修改密码（FR-108）：受 APIGuard 保护（须已登录），从认证上下文取当前用户名
		apiGroup.POST("/me/password", handleChangePassword(svc))
	}

	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	return r
}

// registerStreamRoute 在 web 层挂载 /api/play/:id/stream。
// 用于 HLS master.m3u8 不可用时的播放降级路径。
func registerStreamRoute(r *gin.Engine, libSvc *library.Service, pbSvc *playback.Service) {
	r.GET("/api/play/:id/stream", func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
			return
		}
		mf, err := libSvc.GetMediaFileByID(id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
			return
		}
		pbSvc.StreamFile(c.Writer, c.Request, id, mf.FilePath, mf.FileSize, mf.Duration)
	})
}

type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func handleLogin(svc *auth.Service, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req loginRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    "INVALID_INPUT",
				"message": "请求参数错误",
			})
			return
		}

		user, err := svc.Login(req.Username, req.Password)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    "INVALID_CREDENTIALS",
				"message": err.Error(),
			})
			return
		}

		token, err := auth.GenerateToken(user.Username, cfg.JWTSecret, cfg.JWTExpiresIn)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    "INTERNAL_ERROR",
				"message": "生成令牌失败",
			})
			return
		}

		auth.SetAuthCookie(c, token)

		c.JSON(http.StatusOK, gin.H{
			"username": user.Username,
		})
	}
}

func handleLogout(c *gin.Context) {
	auth.ClearAuthCookie(c)
	c.Status(http.StatusNoContent)
}

// handleSetupStatus 返回是否需要首次初始化（系统无任何用户），免登可查（FR-109）。
func handleSetupStatus(svc *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		need, err := svc.NeedsSetup()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "检查初始化状态失败"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"needs_setup": need})
	}
}

// handleSetup 完成首次初始化：仅在系统无用户时创建首个账户并自动登录（FR-109）。
// 已初始化返回 409，避免重复初始化劫持。
func handleSetup(svc *auth.Service, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req loginRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "请求参数错误"})
			return
		}
		need, err := svc.NeedsSetup()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "检查初始化状态失败"})
			return
		}
		if !need {
			c.JSON(http.StatusConflict, gin.H{"code": "ALREADY_INITIALIZED", "message": "系统已初始化"})
			return
		}
		user, err := svc.Setup(req.Username, req.Password)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": "SETUP_FAILED", "message": err.Error()})
			return
		}
		token, err := auth.GenerateToken(user.Username, cfg.JWTSecret, cfg.JWTExpiresIn)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "生成令牌失败"})
			return
		}
		auth.SetAuthCookie(c, token)
		c.JSON(http.StatusOK, gin.H{"username": user.Username})
	}
}

func handleMe(c *gin.Context) {
	username, _ := c.Get("username")
	c.JSON(http.StatusOK, gin.H{
		"username": username,
	})
}

type changePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required"`
}

// handleChangePassword 修改当前登录用户的密码（FR-108）。
// 受 APIGuard 保护：用户名取自认证上下文；当前密码不符返回 401，成功返回 204。
func handleChangePassword(svc *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		username, ok := c.Get("username")
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "未认证"})
			return
		}
		var req changePasswordRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "请求参数错误"})
			return
		}
		err := svc.ChangePassword(username.(string), req.OldPassword, req.NewPassword)
		if errors.Is(err, auth.ErrCurrentPasswordWrong) {
			c.JSON(http.StatusUnauthorized, gin.H{"code": "WRONG_PASSWORD", "message": "当前密码错误"})
			return
		}
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": "CHANGE_PASSWORD_FAILED", "message": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	}
}
