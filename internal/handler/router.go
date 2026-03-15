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
	post    *PostHandler
}

func NewRouter(cfg *config.Config, upload *UploadHandler, ai *AIHandler, publish *PublishHandler, auth *AuthHandler, post *PostHandler) *Router {
	return &Router{
		cfg:     cfg,
		upload:  upload,
		ai:      ai,
		publish: publish,
		auth:    auth,
		post:    post,
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

	// JWT 认证中间件 (Walker IAM RS256 JWKS)
	jwtAuth := middleware.JWTAuth()

	v1 := engine.Group("/api/v1")
	{
		// Meta OAuth 绑定接口: 需要 JWT (登录用户绑定社交平台)
		authGroup := v1.Group("/auth", jwtAuth)
		{
			authGroup.POST("/meta/connect", r.auth.MetaConnect)
			authGroup.GET("/meta/status", r.auth.MetaStatus)
			authGroup.POST("/meta/disconnect", r.auth.MetaDisconnect)
			authGroup.GET("/meta/debug", r.auth.MetaDebug)
			authGroup.GET("/meta/login", r.auth.MetaLogin)
			authGroup.GET("/meta/callback", r.auth.MetaCallback)
		}

		// 业务接口: 需要 JWT
		business := v1.Group("", jwtAuth)
		{
			business.POST("/upload", r.upload.Upload)
			business.POST("/ai/generate", r.ai.Generate)
			business.POST("/publish", r.publish.Publish)
			business.GET("/posts", r.post.ListPosts)
			business.DELETE("/posts/:id", r.post.DeletePost)
		}
	}
}
