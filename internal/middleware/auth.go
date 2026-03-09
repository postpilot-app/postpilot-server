package middleware

import (
	"crypto/subtle"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/postpilot-dev/postpilot-server/internal/config"
)

// APIKeyAuth 验证请求的 API Key (self 模式)
// 请求需要在 Header 中携带: X-API-Key: <key>
// 或者 query parameter: ?api_key=<key>
func APIKeyAuth(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 如果没配置 API Key，跳过验证 (本地开发)
		if cfg.App.APIKey == "" {
			c.Next()
			return
		}

		// 从 Header 获取
		key := c.GetHeader("X-API-Key")
		if key == "" {
			// 从 query parameter 获取 (方便 OAuth 回调等浏览器场景)
			key = c.Query("api_key")
		}
		if key == "" {
			// 从 Authorization Bearer 获取
			auth := c.GetHeader("Authorization")
			if len(auth) > 7 && auth[:7] == "Bearer " {
				key = auth[7:]
			}
		}

		if key == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "missing API key. Use X-API-Key header, Bearer token, or api_key query parameter",
			})
			return
		}

		// 常数时间比较，防止 timing attack
		if subtle.ConstantTimeCompare([]byte(key), []byte(cfg.App.APIKey)) != 1 {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "invalid API key",
			})
			return
		}

		c.Next()
	}
}
