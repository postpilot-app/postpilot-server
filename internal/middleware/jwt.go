package middleware

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	jwtv5 "github.com/golang-jwt/jwt/v5"
)

// IAMClaims represents the JWT claims from Walker IAM
type IAMClaims struct {
	UserID     uint   `json:"user_id"`
	Username   string `json:"username"`
	TenantCode string `json:"tenant_code"`
	TenantID   uint   `json:"tenant_id"`
	Role       string `json:"role"`
	AppCode    string `json:"app_code,omitempty"`
	jwtv5.RegisteredClaims
}

// jwksCache caches the RSA public key from IAM JWKS endpoint
type jwksCache struct {
	mu        sync.RWMutex
	publicKey *rsa.PublicKey
	fetchedAt time.Time
	jwksURL   string
}

var cache jwksCache

// InitJWKS sets the JWKS URL and fetches the public key
func InitJWKS(jwksURL string) error {
	cache.jwksURL = jwksURL
	return cache.refresh()
}

func (c *jwksCache) refresh() error {
	resp, err := http.Get(c.jwksURL)
	if err != nil {
		return fmt.Errorf("fetch JWKS: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read JWKS: %w", err)
	}

	var jwks struct {
		Keys []struct {
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
			Alg string `json:"alg"`
			Kid string `json:"kid"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(body, &jwks); err != nil {
		return fmt.Errorf("parse JWKS: %w", err)
	}

	if len(jwks.Keys) == 0 {
		return fmt.Errorf("no keys in JWKS")
	}

	key := jwks.Keys[0]
	nBytes, err := base64.RawURLEncoding.DecodeString(key.N)
	if err != nil {
		return fmt.Errorf("decode N: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(key.E)
	if err != nil {
		return fmt.Errorf("decode E: %w", err)
	}

	pubKey := &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: int(new(big.Int).SetBytes(eBytes).Int64()),
	}

	c.mu.Lock()
	c.publicKey = pubKey
	c.fetchedAt = time.Now()
	c.mu.Unlock()

	log.Printf("[JWKS] Public key fetched from %s", c.jwksURL)
	return nil
}

func (c *jwksCache) getKey() *rsa.PublicKey {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Refresh if older than 1 hour
	if time.Since(c.fetchedAt) > time.Hour {
		go func() {
			if err := c.refresh(); err != nil {
				log.Printf("[JWKS] refresh failed: %v", err)
			}
		}()
	}

	return c.publicKey
}

// JWTAuth validates JWT tokens signed by Walker IAM (RS256)
func JWTAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "missing or invalid Authorization header",
			})
			return
		}

		tokenStr := auth[7:]
		pubKey := cache.getKey()
		if pubKey == nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error": "JWKS public key not available",
			})
			return
		}

		token, err := jwtv5.ParseWithClaims(tokenStr, &IAMClaims{}, func(t *jwtv5.Token) (any, error) {
			if _, ok := t.Method.(*jwtv5.SigningMethodRSA); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return pubKey, nil
		})
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid or expired token: " + err.Error(),
			})
			return
		}

		claims, ok := token.Claims.(*IAMClaims)
		if !ok || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid token claims",
			})
			return
		}

		// Set user info in context for handlers
		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("tenant_code", claims.TenantCode)
		c.Set("tenant_id", claims.TenantID)
		c.Set("role", claims.Role)

		c.Next()
	}
}

// GetUserID extracts user_id from gin context (set by JWTAuth middleware)
func GetUserID(c *gin.Context) uint {
	if v, exists := c.Get("user_id"); exists {
		return v.(uint)
	}
	return 0
}
