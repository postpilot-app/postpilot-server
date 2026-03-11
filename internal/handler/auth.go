package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/postpilot-dev/postpilot-server/internal/config"
	"github.com/postpilot-dev/postpilot-server/internal/model"
	"github.com/postpilot-dev/postpilot-server/internal/service/platform"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AuthHandler struct {
	cfg    *config.Config
	client *platform.MetaClient
	db     *gorm.DB
	// 自用模式下，Token 缓存在内存 (从 DB 恢复); 多用户模式后按用户查 DB
	Tokens *MetaTokens
	// Session token: Facebook 授权成功后生成，所有业务接口需要此 token
	SessionToken string
}

// MetaTokens stores the resolved tokens for self mode
type MetaTokens struct {
	UserToken    string `json:"user_token"`
	PageID       string `json:"page_id"`
	PageName     string `json:"page_name"`
	PageToken    string `json:"page_token"`
	IGUserID     string `json:"ig_user_id"`
	ThreadsUID   string `json:"threads_uid"`
	// 用户非敏感信息
	UserName     string `json:"user_name"`
	UserPicture  string `json:"user_picture"`
}

// SessionAuthMiddleware validates the session token from Authorization header
func (h *AuthHandler) SessionAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.SessionToken == "" {
			log.Printf("[SessionAuth] no session token on server (not connected yet)")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "not_authorized", "message": "请先连接 Facebook 账号"})
			c.Abort()
			return
		}
		auth := c.GetHeader("Authorization")
		token := strings.TrimPrefix(auth, "Bearer ")
		if token == "" || token != h.SessionToken {
			log.Printf("[SessionAuth] mismatch: received=%q, expected=%q", token, h.SessionToken[:8]+"...")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_session", "message": "会话无效，请重新连接"})
			c.Abort()
			return
		}
		c.Next()
	}
}

func generateSessionToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func NewAuthHandler(cfg *config.Config, client *platform.MetaClient, db *gorm.DB) *AuthHandler {
	h := &AuthHandler{
		cfg:    cfg,
		client: client,
		db:     db,
		Tokens: &MetaTokens{},
	}

	// 自用模式启动时从 DB 恢复已保存的 Token
	if cfg.IsSelfMode() {
		h.restoreTokensFromDB()
	}

	return h
}

// restoreTokensFromDB loads saved platform accounts from DB into memory (self mode)
func (h *AuthHandler) restoreTokensFromDB() {
	var accounts []model.PlatformAccount
	if err := h.db.Where("user_id = ?", 0).Find(&accounts).Error; err != nil {
		log.Printf("[Auth] restore tokens from DB: %v", err)
		return
	}

	for _, acc := range accounts {
		switch acc.Platform {
		case "facebook":
			h.Tokens.PageToken = acc.AccessTokenEncrypted // self 模式暂不加密
			h.Tokens.PageName = acc.AccountName
			// 从 ExtraData 恢复 page_id, user_token, user_name, user_picture, session_token
			var extra map[string]string
			if err := json.Unmarshal([]byte(acc.ExtraData), &extra); err == nil {
				h.Tokens.PageID = extra["page_id"]
				h.Tokens.UserToken = extra["user_token"]
				h.Tokens.UserName = extra["user_name"]
				h.Tokens.UserPicture = extra["user_picture"]
				if st := extra["session_token"]; st != "" {
					h.SessionToken = st
				}
			}
		case "instagram":
			h.Tokens.IGUserID = acc.AccountName
		case "threads":
			h.Tokens.ThreadsUID = acc.AccountName
		}
	}

	if h.Tokens.PageToken != "" || h.SessionToken != "" {
		log.Printf("[Auth] restored from DB: page=%s(%s), ig=%s, threads=%s, user=%s, session=%v",
			h.Tokens.PageName, h.Tokens.PageID, h.Tokens.IGUserID, h.Tokens.ThreadsUID, h.Tokens.UserName, h.SessionToken != "")
	}
}

// savePlatformAccount upserts a platform account record (self mode: user_id=0)
func (h *AuthHandler) savePlatformAccount(platformName, accountName, token string, expiresAt *time.Time, extra map[string]string) {
	extraJSON, _ := json.Marshal(extra)
	acc := model.PlatformAccount{
		UserID:               0, // self mode
		Platform:             platformName,
		AccountName:          accountName,
		AccessTokenEncrypted: token, // self 模式暂存明文，Phase B 加密
		TokenExpiresAt:       expiresAt,
		ExtraData:            string(extraJSON),
		IsValid:              true,
	}

	// Upsert: 按 user_id + platform 唯一索引
	if err := h.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "platform"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"account_name", "access_token_encrypted", "token_expires_at", "extra_data", "is_valid", "updated_at",
		}),
	}).Create(&acc).Error; err != nil {
		log.Printf("[Auth] save %s account: %v", platformName, err)
	}
}

// MetaConnect receives a client-side Facebook SDK token (from Android/iOS)
// POST /api/v1/auth/meta/connect
func (h *AuthHandler) MetaConnect(c *gin.Context) {
	var req struct {
		AccessToken string   `json:"access_token" binding:"required"`
		Permissions []string `json:"permissions"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "access_token is required"})
		return
	}

	ctx := c.Request.Context()

	// Step 1: Exchange for long-lived token (60 days)
	longToken, expiresIn, err := h.client.ExchangeToken(ctx, h.cfg.Meta.AppID, h.cfg.Meta.AppSecret, req.AccessToken)
	if err != nil {
		log.Printf("[Auth] token exchange failed, using short token: %v", err)
		longToken = req.AccessToken
		expiresIn = 3600
	}
	log.Printf("[Auth/Connect] got long-lived token (expires in %d seconds)", expiresIn)

	tokenExpiry := time.Now().Add(time.Duration(expiresIn) * time.Second)

	// Step 2: Get user profile
	profile, err := h.client.GetUserProfile(ctx, longToken)
	if err != nil {
		log.Printf("[Auth/Connect] get user profile: %v", err)
		profile = &platform.UserProfile{}
	} else {
		h.Tokens.UserName = profile.Name
		h.Tokens.UserPicture = profile.PictureURL
		log.Printf("[Auth/Connect] user: %s (id: %s)", profile.Name, profile.ID)
	}

	// Debug: check token permissions
	perms, permErr := h.client.GetTokenPermissions(ctx, longToken)
	if permErr != nil {
		log.Printf("[Auth/Connect] check permissions failed: %v", permErr)
	} else {
		log.Printf("[Auth/Connect] granted permissions: %v", perms)
	}

	// Step 3: Get user's Facebook Pages and Instagram
	h.Tokens.UserToken = longToken

	// Extract all IDs from token's granular_scopes (most reliable method)
	scopeInfo := h.client.GetGrantedScopeInfo(ctx, longToken, h.cfg.Meta.AppID, h.cfg.Meta.AppSecret)

	// Try /me/accounts first for page info with access_token
	pages, err := h.client.GetUserPages(ctx, longToken)
	if err != nil {
		log.Printf("[Auth/Connect] get pages failed: %v", err)
	}

	if len(pages) > 0 {
		page := pages[0]
		h.Tokens.PageID = page.ID
		h.Tokens.PageName = page.Name
		h.Tokens.PageToken = page.AccessToken
	} else if len(scopeInfo.PageIDs) > 0 {
		// Fallback: use page ID from granular_scopes
		h.Tokens.PageID = scopeInfo.PageIDs[0]
		h.Tokens.PageToken = longToken // use user token as fallback
		log.Printf("[Auth/Connect] using page ID from granular_scopes: %s", h.Tokens.PageID)
	}

	if h.Tokens.PageID != "" {
		// Save Facebook/Page account to DB
		pageName := h.Tokens.PageName
		if pageName == "" {
			pageName = "Page:" + h.Tokens.PageID
		}
		h.Tokens.PageName = pageName
		h.savePlatformAccount("facebook", pageName, h.Tokens.PageToken, &tokenExpiry, map[string]string{
			"page_id":      h.Tokens.PageID,
			"user_token":   longToken,
			"user_name":    profile.Name,
			"user_picture": profile.PictureURL,
		})

		// Step 4: Get Instagram Business Account
		// First try from granular_scopes (direct, no extra API call)
		if len(scopeInfo.IGUserIDs) > 0 {
			igID := scopeInfo.IGUserIDs[0]
			h.Tokens.IGUserID = igID
			log.Printf("[Auth/Connect] IG business account (from token scopes): %s", igID)

			// Try to get IG username
			igUser, igErr := h.client.GetIGUsername(ctx, igID, longToken)
			if igErr != nil {
				log.Printf("[Auth/Connect] get IG username: %v", igErr)
				igUser = igID
			}
			h.Tokens.IGUserID = igUser
			h.savePlatformAccount("instagram", igUser, h.Tokens.PageToken, &tokenExpiry, map[string]string{
				"ig_user_id": igID,
				"page_id":    h.Tokens.PageID,
			})
		} else {
			// Fallback: try via page API
			igID, igErr := h.client.GetInstagramBusinessAccount(ctx, h.Tokens.PageID, h.Tokens.PageToken)
			if igErr != nil {
				log.Printf("[Auth/Connect] no IG business account: %v", igErr)
			} else {
				h.Tokens.IGUserID = igID
				log.Printf("[Auth/Connect] IG business account: %s", igID)
				h.savePlatformAccount("instagram", igID, h.Tokens.PageToken, &tokenExpiry, map[string]string{
					"ig_user_id": igID,
					"page_id":    h.Tokens.PageID,
				})
			}
		}
	} else {
		log.Printf("[Auth/Connect] no pages found, skipping page/IG setup")
	}

	// Step 5: Get Threads User ID
	threadsUID, err := h.client.GetThreadsUserID(ctx, longToken)
	if err != nil {
		log.Printf("[Auth/Connect] no Threads user: %v", err)
	} else {
		h.Tokens.ThreadsUID = threadsUID
		log.Printf("[Auth/Connect] Threads user: %s", threadsUID)
		h.savePlatformAccount("threads", threadsUID, longToken, &tokenExpiry, map[string]string{
			"threads_uid": threadsUID,
		})
	}

	// Generate session token
	h.SessionToken = generateSessionToken()
	log.Printf("[Auth/Connect] complete: page=%s(%s), ig=%s, threads=%s, user=%s, session=%s...",
		h.Tokens.PageName, h.Tokens.PageID, h.Tokens.IGUserID, h.Tokens.ThreadsUID, h.Tokens.UserName, h.SessionToken[:8])

	// Save session token in facebook extra_data
	h.savePlatformAccount("facebook", h.Tokens.PageName, h.Tokens.PageToken, &tokenExpiry, map[string]string{
		"page_id":       h.Tokens.PageID,
		"user_token":    longToken,
		"user_name":     profile.Name,
		"user_picture":  profile.PictureURL,
		"session_token": h.SessionToken,
	})

	c.JSON(http.StatusOK, gin.H{
		"status":        "connected",
		"session_token": h.SessionToken,
		"page":          h.Tokens.PageName,
		"page_id":       h.Tokens.PageID,
		"ig_user":       h.Tokens.IGUserID,
		"threads":       h.Tokens.ThreadsUID,
		"user_name":     h.Tokens.UserName,
		"user_picture":  h.Tokens.UserPicture,
	})
}

// MetaLogin redirects to Meta OAuth
// GET /api/v1/auth/meta/login
func (h *AuthHandler) MetaLogin(c *gin.Context) {
	// 优先使用 config_id (Facebook Login for Business)，否则用 scope (标准 Facebook Login)
	var authURL string
	if h.cfg.Meta.ConfigID != "" {
		authURL = fmt.Sprintf(
			"https://www.facebook.com/v22.0/dialog/oauth?client_id=%s&redirect_uri=%s&config_id=%s&response_type=code",
			h.cfg.Meta.AppID,
			url.QueryEscape(h.cfg.Meta.RedirectURI),
			h.cfg.Meta.ConfigID,
		)
	} else {
		scopes := "instagram_basic,instagram_content_publish,pages_show_list,pages_manage_posts"
		authURL = fmt.Sprintf(
			"https://www.facebook.com/v22.0/dialog/oauth?client_id=%s&redirect_uri=%s&scope=%s&response_type=code",
			h.cfg.Meta.AppID,
			url.QueryEscape(h.cfg.Meta.RedirectURI),
			scopes,
		)
	}

	c.Redirect(http.StatusTemporaryRedirect, authURL)
}

// MetaCallback handles OAuth callback
// GET /api/v1/auth/meta/callback
func (h *AuthHandler) MetaCallback(c *gin.Context) {
	code := c.Query("code")
	if code == "" {
		errMsg := c.Query("error_description")
		c.JSON(http.StatusBadRequest, gin.H{"error": "auth denied: " + errMsg})
		return
	}

	ctx := c.Request.Context()

	// Step 1: Exchange code for short-lived token
	shortToken, err := h.exchangeCode(code)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "exchange code: " + err.Error()})
		return
	}

	// Step 2: Exchange for long-lived token (60 days)
	longToken, expiresIn, err := h.client.ExchangeToken(ctx, h.cfg.Meta.AppID, h.cfg.Meta.AppSecret, shortToken)
	if err != nil {
		log.Printf("[Auth] token exchange failed, using short token: %v", err)
		longToken = shortToken
		expiresIn = 3600
	}
	log.Printf("[Auth] got long-lived token (expires in %d seconds)", expiresIn)

	tokenExpiry := time.Now().Add(time.Duration(expiresIn) * time.Second)

	// Step 3: Get user profile (non-sensitive info)
	profile, err := h.client.GetUserProfile(ctx, longToken)
	if err != nil {
		log.Printf("[Auth] get user profile: %v", err)
		profile = &platform.UserProfile{}
	} else {
		h.Tokens.UserName = profile.Name
		h.Tokens.UserPicture = profile.PictureURL
		log.Printf("[Auth] user: %s (id: %s)", profile.Name, profile.ID)
	}

	// Step 4: Get user's Facebook Pages
	pages, err := h.client.GetUserPages(ctx, longToken)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "get pages: " + err.Error()})
		return
	}

	if len(pages) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no Facebook Pages found. You need a Facebook Page to publish."})
		return
	}

	// Use first page (self mode)
	page := pages[0]
	h.Tokens.UserToken = longToken
	h.Tokens.PageID = page.ID
	h.Tokens.PageName = page.Name
	h.Tokens.PageToken = page.AccessToken

	// Save Facebook/Page account to DB
	h.savePlatformAccount("facebook", page.Name, page.AccessToken, &tokenExpiry, map[string]string{
		"page_id":      page.ID,
		"user_token":   longToken,
		"user_name":    profile.Name,
		"user_picture": profile.PictureURL,
	})

	// Step 5: Get Instagram Business Account
	igID, err := h.client.GetInstagramBusinessAccount(ctx, page.ID, page.AccessToken)
	if err != nil {
		log.Printf("[Auth] no IG business account: %v", err)
	} else {
		h.Tokens.IGUserID = igID
		log.Printf("[Auth] IG business account: %s", igID)
		h.savePlatformAccount("instagram", igID, page.AccessToken, &tokenExpiry, map[string]string{
			"ig_user_id": igID,
			"page_id":    page.ID,
		})
	}

	// Step 6: Get Threads User ID
	threadsUID, err := h.client.GetThreadsUserID(ctx, longToken)
	if err != nil {
		log.Printf("[Auth] no Threads user: %v", err)
	} else {
		h.Tokens.ThreadsUID = threadsUID
		log.Printf("[Auth] Threads user: %s", threadsUID)
		h.savePlatformAccount("threads", threadsUID, longToken, &tokenExpiry, map[string]string{
			"threads_uid": threadsUID,
		})
	}

	log.Printf("[Auth] Meta OAuth complete: page=%s(%s), ig=%s, threads=%s, user=%s",
		page.Name, page.ID, h.Tokens.IGUserID, h.Tokens.ThreadsUID, h.Tokens.UserName)

	c.JSON(http.StatusOK, gin.H{
		"status":       "connected",
		"page":         page.Name,
		"page_id":      page.ID,
		"ig_user":      h.Tokens.IGUserID,
		"threads":      h.Tokens.ThreadsUID,
		"user_name":    h.Tokens.UserName,
		"user_picture": h.Tokens.UserPicture,
	})
}

// MetaStatus returns current connection status
// GET /api/v1/auth/meta/status
func (h *AuthHandler) MetaStatus(c *gin.Context) {
	connected := h.Tokens.PageToken != ""
	resp := gin.H{
		"connected":    connected,
		"page_name":    h.Tokens.PageName,
		"page_id":      h.Tokens.PageID,
		"ig_user":      h.Tokens.IGUserID,
		"threads":      h.Tokens.ThreadsUID,
		"user_name":    h.Tokens.UserName,
		"user_picture": h.Tokens.UserPicture,
	}
	// 返回 session_token 以便客户端恢复 (比如 connect 响应丢失时)
	if connected && h.SessionToken != "" {
		resp["session_token"] = h.SessionToken
	}
	c.JSON(http.StatusOK, resp)
}

// MetaDebug runs various Graph API calls for debugging
// GET /api/v1/auth/meta/debug
func (h *AuthHandler) MetaDebug(c *gin.Context) {
	if h.Tokens.UserToken == "" {
		c.JSON(http.StatusOK, gin.H{"error": "no user token"})
		return
	}
	ctx := c.Request.Context()
	token := h.Tokens.UserToken
	results := gin.H{}

	// 1. /me?fields=id,name,accounts
	p1 := url.Values{"access_token": {token}, "fields": {"id,name,accounts{id,name,access_token}"}}
	r1, e1 := h.client.GraphGet(ctx, "/me", p1)
	results["me_with_accounts"] = fmt.Sprintf("result=%v, err=%v", r1, e1)

	// 2. /me/accounts with no fields
	p2 := url.Values{"access_token": {token}}
	r2, e2 := h.client.GraphGet(ctx, "/me/accounts", p2)
	results["me_accounts_no_fields"] = fmt.Sprintf("result=%v, err=%v", r2, e2)

	// 3. /me/accounts?fields=id,name
	p3 := url.Values{"access_token": {token}, "fields": {"id,name"}}
	r3, e3 := h.client.GraphGet(ctx, "/me/accounts", p3)
	results["me_accounts_id_name"] = fmt.Sprintf("result=%v, err=%v", r3, e3)

	// 4. /me/permissions
	p4 := url.Values{"access_token": {token}}
	r4, e4 := h.client.GraphGet(ctx, "/me/permissions", p4)
	results["me_permissions"] = fmt.Sprintf("result=%v, err=%v", r4, e4)

	// 5. Token debug info
	p5 := url.Values{"input_token": {token}, "access_token": {h.cfg.Meta.AppID + "|" + h.cfg.Meta.AppSecret}}
	r5, e5 := h.client.GraphGet(ctx, "/debug_token", p5)
	results["debug_token"] = fmt.Sprintf("result=%v, err=%v", r5, e5)

	c.JSON(http.StatusOK, results)
}

// MetaDisconnect revokes all permissions and clears tokens
// POST /api/v1/auth/meta/disconnect
func (h *AuthHandler) MetaDisconnect(c *gin.Context) {
	ctx := c.Request.Context()
	if h.Tokens.UserToken != "" {
		// Revoke all permissions via Graph API
		params := url.Values{"access_token": {h.Tokens.UserToken}}
		_, err := h.client.GraphDelete(ctx, "/me/permissions", params)
		if err != nil {
			log.Printf("[Auth/Disconnect] revoke permissions: %v", err)
		} else {
			log.Printf("[Auth/Disconnect] permissions revoked")
		}
	}
	// Clear in-memory tokens
	h.Tokens = &MetaTokens{}
	// Clear DB records
	if err := h.db.Where("user_id = ?", 0).Delete(&model.PlatformAccount{}).Error; err != nil {
		log.Printf("[Auth/Disconnect] clear DB: %v", err)
	}
	c.JSON(http.StatusOK, gin.H{"status": "disconnected"})
}

func (h *AuthHandler) exchangeCode(code string) (string, error) {
	params := url.Values{
		"client_id":     {h.cfg.Meta.AppID},
		"client_secret": {h.cfg.Meta.AppSecret},
		"redirect_uri":  {h.cfg.Meta.RedirectURI},
		"code":          {code},
	}

	resp, err := http.Get(fmt.Sprintf("https://graph.facebook.com/v22.0/oauth/access_token?%s", params.Encode()))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken string `json:"access_token"`
		Error       struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("no token: %s", result.Error.Message)
	}
	return result.AccessToken, nil
}
