package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/postpilot-dev/postpilot-server/internal/config"
	"github.com/postpilot-dev/postpilot-server/internal/service/platform"
)

type AuthHandler struct {
	cfg    *config.Config
	client *platform.MetaClient
	// 自用模式下，Token 存内存即可; 多用户模式后存 DB
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
}

func NewAuthHandler(cfg *config.Config, client *platform.MetaClient) *AuthHandler {
	return &AuthHandler{
		cfg:    cfg,
		client: client,
		Tokens: &MetaTokens{},
	}
}

// MetaLogin redirects to Meta OAuth
// GET /api/v1/auth/meta/login
func (h *AuthHandler) MetaLogin(c *gin.Context) {
	// Instagram + Facebook + Threads 权限
	scopes := "instagram_basic,instagram_content_publish,pages_show_list,pages_read_engagement,pages_manage_posts,threads_basic,threads_content_publish"

	authURL := fmt.Sprintf(
		"https://www.facebook.com/v22.0/dialog/oauth?client_id=%s&redirect_uri=%s&scope=%s&response_type=code",
		h.cfg.Meta.AppID,
		url.QueryEscape(h.cfg.Meta.RedirectURI),
		scopes,
	)

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

	// Step 3: Get user's Facebook Pages
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

	// Step 4: Get Instagram Business Account
	igID, err := h.client.GetInstagramBusinessAccount(ctx, page.ID, page.AccessToken)
	if err != nil {
		log.Printf("[Auth] no IG business account: %v", err)
	} else {
		h.Tokens.IGUserID = igID
		log.Printf("[Auth] IG business account: %s", igID)
	}

	// Step 5: Get Threads User ID (same as IG user in most cases)
	threadsUID, err := h.client.GetThreadsUserID(ctx, longToken)
	if err != nil {
		log.Printf("[Auth] no Threads user: %v", err)
	} else {
		h.Tokens.ThreadsUID = threadsUID
		log.Printf("[Auth] Threads user: %s", threadsUID)
	}

	log.Printf("[Auth] Meta OAuth complete: page=%s(%s), ig=%s, threads=%s",
		page.Name, page.ID, h.Tokens.IGUserID, h.Tokens.ThreadsUID)

	c.JSON(http.StatusOK, gin.H{
		"status":   "connected",
		"page":     page.Name,
		"page_id":  page.ID,
		"ig_user":  h.Tokens.IGUserID,
		"threads":  h.Tokens.ThreadsUID,
	})
}

// MetaStatus returns current connection status
// GET /api/v1/auth/meta/status
func (h *AuthHandler) MetaStatus(c *gin.Context) {
	connected := h.Tokens.PageToken != ""
	c.JSON(http.StatusOK, gin.H{
		"connected": connected,
		"page_name": h.Tokens.PageName,
		"page_id":   h.Tokens.PageID,
		"ig_user":   h.Tokens.IGUserID,
		"threads":   h.Tokens.ThreadsUID,
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
