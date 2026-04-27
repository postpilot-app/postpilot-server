package iamsdk

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	ctxKeyClaims     = "iam.claims"
	ctxKeyUserID     = "iam.user_id"
	ctxKeyUsername   = "iam.username"
	ctxKeyTenantCode = "iam.tenant_code"
	ctxKeyTenantID   = "iam.tenant_id"
	ctxKeyRole       = "iam.role"
)

func (v *Verifier) GinAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenStr, err := extractBearer(c.GetHeader("Authorization"))
		if err != nil {
			abortUnauthorized(c, err)
			return
		}
		claims, err := v.ParseToken(c.Request.Context(), tokenStr)
		if err != nil {
			abortUnauthorized(c, err)
			return
		}
		setClaims(c, claims)
		c.Next()
	}
}

func (v *Verifier) RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := ClaimsFromContext(c)
		if !ok {
			abortForbidden(c, "no claims in context")
			return
		}
		if !claims.HasRole(roles...) {
			abortForbidden(c, "role not permitted")
			return
		}
		c.Next()
	}
}

func (v *Verifier) RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := ClaimsFromContext(c)
		if !ok {
			abortForbidden(c, "no claims in context")
			return
		}
		if !claims.IsAdmin() {
			abortForbidden(c, "admin required")
			return
		}
		c.Next()
	}
}

func extractBearer(h string) (string, error) {
	if h == "" {
		return "", ErrNoToken
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", ErrMalformedAuth
	}
	return parts[1], nil
}

func setClaims(c *gin.Context, claims *IAMClaims) {
	c.Set(ctxKeyClaims, claims)
	c.Set(ctxKeyUserID, claims.UserID)
	c.Set(ctxKeyUsername, claims.Username)
	c.Set(ctxKeyTenantCode, claims.TenantCode)
	c.Set(ctxKeyTenantID, claims.TenantID)
	c.Set(ctxKeyRole, claims.Role)
}

func ClaimsFromContext(c *gin.Context) (*IAMClaims, bool) {
	v, ok := c.Get(ctxKeyClaims)
	if !ok {
		return nil, false
	}
	cl, ok := v.(*IAMClaims)
	return cl, ok
}

func UserIDFromContext(c *gin.Context) uint {
	v, _ := c.Get(ctxKeyUserID)
	u, _ := v.(uint)
	return u
}

func UsernameFromContext(c *gin.Context) string {
	v, _ := c.Get(ctxKeyUsername)
	s, _ := v.(string)
	return s
}

func TenantCodeFromContext(c *gin.Context) string {
	v, _ := c.Get(ctxKeyTenantCode)
	s, _ := v.(string)
	return s
}

func TenantIDFromContext(c *gin.Context) uint {
	v, _ := c.Get(ctxKeyTenantID)
	u, _ := v.(uint)
	return u
}

func RoleFromContext(c *gin.Context) string {
	v, _ := c.Get(ctxKeyRole)
	s, _ := v.(string)
	return s
}

func abortUnauthorized(c *gin.Context, err error) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
		"code":    401,
		"message": err.Error(),
	})
}

func abortForbidden(c *gin.Context, msg string) {
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
		"code":    403,
		"message": msg,
	})
}
