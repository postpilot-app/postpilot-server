package iamsdk

import "github.com/golang-jwt/jwt/v5"

type IAMClaims struct {
	UserID     uint   `json:"user_id"`
	Username   string `json:"username"`
	TenantCode string `json:"tenant_code"`
	TenantID   uint   `json:"tenant_id"`
	Role       string `json:"role"`
	AppCode    string `json:"app_code,omitempty"`
	jwt.RegisteredClaims
}

var adminRoles = map[string]struct{}{
	"admin":          {},
	"super_admin":    {},
	"platform_admin": {},
}

func (c *IAMClaims) IsAdmin() bool {
	_, ok := adminRoles[c.Role]
	return ok
}

func (c *IAMClaims) HasRole(roles ...string) bool {
	for _, r := range roles {
		if c.Role == r {
			return true
		}
	}
	return false
}
