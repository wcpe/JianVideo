package web

import (
	"database/sql"
	"net/http"

	"jianvideo/config"
	"jianvideo/internal/auth"

	"github.com/gin-gonic/gin"
)

// NewRouter 创建并配置路由
func NewRouter(cfg *config.Config, db *sql.DB) *gin.Engine {
	r := gin.Default()

	svc := auth.NewService(db, cfg.JWTSecret)
	authMW := auth.Middleware(cfg.JWTSecret)

	// 确保默认用户存在
	if err := svc.CreateDefaultUser(); err != nil {
		// 启动时创建失败只记录日志，不中断启动
		// 生产环境应接入正式日志库，此处用 gin 自带的 Error 日志
		gin.DefaultErrorWriter.Write([]byte("创建默认用户失败: " + err.Error() + "\n"))
	}

	api := r.Group("/api")
	{
		authGroup := api.Group("/auth")
		{
			authGroup.POST("/login", handleLogin(svc, cfg))
			authGroup.POST("/logout", handleLogout)
		}

		// 受保护的路由
		protected := api.Group("")
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
