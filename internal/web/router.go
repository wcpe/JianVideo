package web

import (
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"jianvideo/config"
	"jianvideo/internal/api"
	"jianvideo/internal/auth"
	"jianvideo/internal/library"
	"jianvideo/internal/player"
)

// NewRouter 创建并配置路由
// frontendDist 为嵌入的前端静态资源（由 main 通过 go:embed 传入）
func NewRouter(cfg *config.Config, db *gorm.DB, hlsMgr *player.HLSManager, frontendDist fs.FS) *gin.Engine {
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
	authMW := auth.Middleware(cfg.JWTSecret)

	// 确保默认用户存在
	if err := svc.CreateDefaultUser(); err != nil {
		gin.DefaultErrorWriter.Write([]byte("创建默认用户失败: " + err.Error() + "\n"))
	}

	// 创建 API Handler
	libSvc := library.NewService(db)
	apiHandler := api.NewHandler(libSvc)

	// 注册 API 路由（库路由）
	api.RegisterRoutes(r, apiHandler)

	// HLS 路由
	if hlsMgr != nil {
		api.RegisterHLSRoutes(r, hlsMgr)
	}

	// 认证路由
	apiGroup := r.Group("/api")
	{
		authGroup := apiGroup.Group("/auth")
		{
			authGroup.POST("/login", handleLogin(svc, cfg))
			authGroup.POST("/logout", handleLogout)
		}

		protected := apiGroup.Group("")
		protected.Use(authMW)
		{
			protected.GET("/me", handleMe)
		}
	}

	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	return r
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
