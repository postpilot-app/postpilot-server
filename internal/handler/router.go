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
		// 需要认证的接口
		protected := v1.Group("", apiAuth)
		{
			protected.POST("/upload", r.upload.Upload)
			protected.POST("/ai/generate", r.ai.Generate)
			protected.POST("/publish", r.publish.Publish)
			protected.GET("/auth/meta/status", r.auth.MetaStatus)
		}

		// OAuth 回调 (浏览器跳转，也需要验证但支持 query param)
		oauthAuth := v1.Group("/auth", apiAuth)
		{
			oauthAuth.GET("/meta/login", r.auth.MetaLogin)
			oauthAuth.GET("/meta/callback", r.auth.MetaCallback)
		}
	}
}
