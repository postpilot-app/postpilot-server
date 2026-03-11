package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
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
			// 从 ExtraData 恢复 page_id, user_token, user_name, user_picture
			var extra map[string]string
			if err := json.Unmarshal([]byte(acc.ExtraData), &extra); err == nil {
				h.Tokens.PageID = extra["page_id"]
				h.Tokens.UserToken = extra["user_token"]
				h.Tokens.UserName = extra["user_name"]
				h.Tokens.UserPicture = extra["user_picture"]
			}
		case "instagram":
			h.Tokens.IGUserID = acc.AccountName
		case "threads":
			h.Tokens.ThreadsUID = acc.AccountName
		}
	}

	if h.Tokens.PageToken != "" {
		log.Printf("[Auth] restored tokens from DB: page=%s(%s), ig=%s, threads=%s, user=%s",
			h.Tokens.PageName, h.Tokens.PageID, h.Tokens.IGUserID, h.Tokens.ThreadsUID, h.Tokens.UserName)
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

	// Step 3: Get user's Facebook Pages
	h.Tokens.UserToken = longToken
	pages, err := h.client.GetUserPages(ctx, longToken)
	if err != nil {
		log.Printf("[Auth/Connect] get pages failed (may lack permissions): %v", err)
	}

	if len(pages) > 0 {
		page := pages[0]
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

		// Step 4: Get Instagram Business Account
		igID, err := h.client.GetInstagramBusinessAccount(ctx, page.ID, page.AccessToken)
		if err != nil {
			log.Printf("[Auth/Connect] no IG business account: %v", err)
		} else {
			h.Tokens.IGUserID = igID
			log.Printf("[Auth/Connect] IG business account: %s", igID)
			h.savePlatformAccount("instagram", igID, page.AccessToken, &tokenExpiry, map[string]string{
				"ig_user_id": igID,
				"page_id":    page.ID,
			})
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

	log.Printf("[Auth/Connect] complete: page=%s(%s), ig=%s, threads=%s, user=%s",
		h.Tokens.PageName, h.Tokens.PageID, h.Tokens.IGUserID, h.Tokens.ThreadsUID, h.Tokens.UserName)

	c.JSON(http.StatusOK, gin.H{
		"status":       "connected",
		"page":         h.Tokens.PageName,
		"page_id":      h.Tokens.PageID,
		"ig_user":      h.Tokens.IGUserID,
		"threads":      h.Tokens.ThreadsUID,
		"user_name":    h.Tokens.UserName,
		"user_picture": h.Tokens.UserPicture,
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
	c.JSON(http.StatusOK, gin.H{
		"connected":    connected,
		"page_name":    h.Tokens.PageName,
		"page_id":      h.Tokens.PageID,
		"ig_user":      h.Tokens.IGUserID,
		"threads":      h.Tokens.ThreadsUID,
		"user_name":    h.Tokens.UserName,
		"user_picture": h.Tokens.UserPicture,
	})
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
