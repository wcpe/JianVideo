package web

import (
	"io/fs"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"jianvideo/config"
	"jianvideo/internal/api"
	"jianvideo/internal/auth"
	"jianvideo/internal/library"
	"jianvideo/internal/playback"
	"jianvideo/internal/player"
	"jianvideo/internal/settings"
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

	// 确保默认用户存在
	if err := svc.CreateDefaultUser(); err != nil {
		gin.DefaultErrorWriter.Write([]byte("创建默认用户失败: " + err.Error() + "\n"))
	}

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
	if len(pbSvc) > 0 && pbSvc[0] != nil {
		registerStreamRoute(r, libSvc, pbSvc[0])
	}

	// 认证路由
	apiGroup := r.Group("/api")
	{
		authGroup := apiGroup.Group("/auth")
		{
			authGroup.POST("/login", handleLogin(svc, cfg))
			authGroup.POST("/logout", handleLogout)
		}

		// /api/me 由全局 APIGuard 统一保护，无需再单独挂中间件。
		apiGroup.GET("/me", handleMe)
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

func handleMe(c *gin.Context) {
	username, _ := c.Get("username")
	c.JSON(http.StatusOK, gin.H{
		"username": username,
	})
}
