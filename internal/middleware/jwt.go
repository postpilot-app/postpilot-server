package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	iamsdk "github.com/WNZhao/walker-iam-go"
)

var iamVerifier *iamsdk.Verifier

// InitIAMVerifier builds the shared Walker IAM verifier from the JWKS URL
// and enables JTI revocation check via IAM's /api/token/check endpoint.
func InitIAMVerifier(jwksURL string) error {
	base := iamsdk.IAMBaseFromJWKS(jwksURL)
	v, err := iamsdk.NewVerifier(jwksURL,
		iamsdk.WithLazyInit(true),
		iamsdk.WithJTIChecker(iamsdk.HTTPJTIChecker(base, nil)),
	)
	if err != nil {
		return fmt.Errorf("init IAM verifier: %w", err)
	}
	iamVerifier = v
	return nil
}

// JWTAuth validates a Bearer token via the IAM SDK and aborts on failure.
// Preserves legacy gin context keys (user_id / username / tenant_code /
// tenant_id / role) for existing handlers reading via GetUserID() and c.Get(...).
func JWTAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		tok, ok := extractBearer(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing Authorization header"})
			return
		}
		if iamVerifier == nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "IAM verifier not initialized"})
			return
		}
		claims, err := iamVerifier.ParseToken(c.Request.Context(), tok)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token: " + err.Error()})
			return
		}
		setLegacyKeys(c, claims)
		c.Next()
	}
}

// GetUserID extracts user_id from gin context (set by JWTAuth middleware).
func GetUserID(c *gin.Context) uint {
	if v, exists := c.Get("user_id"); exists {
		if u, ok := v.(uint); ok {
			return u
		}
	}
	return 0
}

func extractBearer(c *gin.Context) (string, bool) {
	h := c.GetHeader("Authorization")
	if h == "" || !strings.HasPrefix(h, "Bearer ") {
		return "", false
	}
	return h[7:], true
}

func setLegacyKeys(c *gin.Context, claims *iamsdk.IAMClaims) {
	c.Set("user_id", claims.UserID)
	c.Set("username", claims.Username)
	c.Set("tenant_code", claims.TenantCode)
	c.Set("tenant_id", claims.TenantID)
	c.Set("role", claims.Role)
}
