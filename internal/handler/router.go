package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/postpilot-dev/postpilot-server/internal/config"
	"github.com/postpilot-dev/postpilot-server/internal/middleware"
)

type Router struct {
	cfg     *config.Config
	upload  *UploadHandler
	ai      *AIHandler
	publish *PublishHandler
	auth    *AuthHandler
}

func NewRouter(cfg *config.Config, upload *UploadHandler, ai *AIHandler, publish *PublishHandler, auth *AuthHandler) *Router {
	return &Router{
		cfg:     cfg,
		upload:  upload,
		ai:      ai,
		publish: publish,
		auth:    auth,
	}
}

func (r *Router) Setup(engine *gin.Engine) {
	// Health check (公开，不需要认证)
	engine.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status": "ok",
			"mode":   r.cfg.App.Mode,
		})
	})

	// API Key 认证中间件
	apiAuth := middleware.APIKeyAuth(r.cfg)

	v1 := engine.Group("/api/v1")
	{
		// 授权相关接口: 仅需 API Key (用于连接/查状态)
		authGroup := v1.Group("/auth", apiAuth)
		{
			authGroup.POST("/meta/connect", r.auth.MetaConnect)
			authGroup.GET("/meta/status", r.auth.MetaStatus)
			authGroup.POST("/meta/disconnect", r.auth.MetaDisconnect)
			authGroup.GET("/meta/debug", r.auth.MetaDebug)
			authGroup.GET("/meta/login", r.auth.MetaLogin)
			authGroup.GET("/meta/callback", r.auth.MetaCallback)
		}

		// 业务接口: 需要 API Key + Session Token (Facebook 授权后获得)
		sessionAuth := r.auth.SessionAuthMiddleware()
		business := v1.Group("", apiAuth, sessionAuth)
		{
			business.POST("/upload", r.upload.Upload)
			business.POST("/ai/generate", r.ai.Generate)
			business.POST("/publish", r.publish.Publish)
		}
	}
}
