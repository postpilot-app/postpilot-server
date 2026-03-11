package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const metaGraphBaseURL = "https://graph.facebook.com/v22.0"

// MetaClient is a shared HTTP client for Meta Graph API (IG, FB, Threads)
type MetaClient struct {
	httpClient *http.Client
}

func NewMetaClient() *MetaClient {
	return &MetaClient{
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// graphGet performs a GET request to Meta Graph API
func (m *MetaClient) graphGet(ctx context.Context, path string, params url.Values) (map[string]interface{}, error) {
	reqURL := fmt.Sprintf("%s%s?%s", metaGraphBaseURL, path, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	return m.doJSON(req)
}

// GraphDelete performs a DELETE request to Meta Graph API
func (m *MetaClient) GraphDelete(ctx context.Context, path string, params url.Values) (map[string]interface{}, error) {
	reqURL := fmt.Sprintf("%s%s?%s", metaGraphBaseURL, path, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, reqURL, nil)
	if err != nil {
		return nil, err
	}
	return m.doJSON(req)
}

// graphPost performs a POST request to Meta Graph API
func (m *MetaClient) graphPost(ctx context.Context, path string, params url.Values) (map[string]interface{}, error) {
	reqURL := fmt.Sprintf("%s%s", metaGraphBaseURL, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return m.doJSON(req)
}

func (m *MetaClient) doJSON(req *http.Request) (map[string]interface{}, error) {
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response (status %d): %s", resp.StatusCode, string(body))
	}

	// Check for Graph API error
	if errObj, ok := result["error"]; ok {
		errMap, _ := errObj.(map[string]interface{})
		msg, _ := errMap["message"].(string)
		code, _ := errMap["code"].(float64)
		return nil, fmt.Errorf("graph API error %d: %s", int(code), msg)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return result, nil
}

// ExchangeToken exchanges short-lived token for long-lived token
func (m *MetaClient) ExchangeToken(ctx context.Context, appID, appSecret, shortToken string) (string, int64, error) {
	params := url.Values{
		"grant_type":        {"fb_exchange_token"},
		"client_id":         {appID},
		"client_secret":     {appSecret},
		"fb_exchange_token": {shortToken},
	}
	result, err := m.graphGet(ctx, "/oauth/access_token", params)
	if err != nil {
		return "", 0, fmt.Errorf("exchange token: %w", err)
	}

	token, _ := result["access_token"].(string)
	expiresIn, _ := result["expires_in"].(float64)
	if token == "" {
		return "", 0, fmt.Errorf("no access_token in response")
	}

	return token, int64(expiresIn), nil
}

// GetUserPages returns Facebook Pages the user manages
func (m *MetaClient) GetUserPages(ctx context.Context, userToken string) ([]PageInfo, error) {
	params := url.Values{
		"access_token": {userToken},
		"fields":       {"id,name,access_token"},
	}
	result, err := m.graphGet(ctx, "/me/accounts", params)
	if err != nil {
		return nil, err
	}

	log.Printf("[Meta] /me/accounts raw response: %v", result)
	data, _ := result["data"].([]interface{})
	var pages []PageInfo
	for _, item := range data {
		p, _ := item.(map[string]interface{})
		pages = append(pages, PageInfo{
			ID:          fmt.Sprintf("%v", p["id"]),
			Name:        fmt.Sprintf("%v", p["name"]),
			AccessToken: fmt.Sprintf("%v", p["access_token"]),
		})
	}
	return pages, nil
}

// GetInstagramBusinessAccount returns the IG Business Account ID linked to a Page
func (m *MetaClient) GetInstagramBusinessAccount(ctx context.Context, pageID, pageToken string) (string, error) {
	params := url.Values{
		"access_token": {pageToken},
		"fields":       {"instagram_business_account"},
	}
	result, err := m.graphGet(ctx, "/"+pageID, params)
	if err != nil {
		return "", err
	}

	igAccount, _ := result["instagram_business_account"].(map[string]interface{})
	igID, _ := igAccount["id"].(string)
	if igID == "" {
		return "", fmt.Errorf("no instagram_business_account linked to page %s", pageID)
	}
	return igID, nil
}

// GetThreadsUserID returns the user's Threads user ID
func (m *MetaClient) GetThreadsUserID(ctx context.Context, userToken string) (string, error) {
	params := url.Values{
		"access_token": {userToken},
		"fields":       {"id"},
	}
	result, err := m.graphGet(ctx, "/me", params)
	if err != nil {
		return "", err
	}
	id, _ := result["id"].(string)
	if id == "" {
		return "", fmt.Errorf("no user ID in response")
	}
	return id, nil
}

// GetUserProfile returns non-sensitive user info (name, picture, etc.)
func (m *MetaClient) GetUserProfile(ctx context.Context, userToken string) (*UserProfile, error) {
	params := url.Values{
		"access_token": {userToken},
		"fields":       {"id,name,picture.width(200).height(200)"},
	}
	result, err := m.graphGet(ctx, "/me", params)
	if err != nil {
		return nil, err
	}
	profile := &UserProfile{
		ID:   fmt.Sprintf("%v", result["id"]),
		Name: fmt.Sprintf("%v", result["name"]),
	}
	// picture 结构: {"data": {"url": "...", "is_silhouette": false}}
	if pic, ok := result["picture"].(map[string]interface{}); ok {
		if data, ok := pic["data"].(map[string]interface{}); ok {
			profile.PictureURL, _ = data["url"].(string)
		}
	}
	return profile, nil
}

// UserProfile represents non-sensitive Facebook user info
type UserProfile struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PictureURL string `json:"picture_url"`
}

// GetTokenPermissions returns the list of granted permissions for a token
func (m *MetaClient) GetTokenPermissions(ctx context.Context, token string) ([]string, error) {
	params := url.Values{"access_token": {token}}
	data, err := m.graphGet(ctx, "/me/permissions", params)
	if err != nil {
		return nil, err
	}
	permsData, ok := data["data"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected permissions response")
	}
	var granted []string
	for _, p := range permsData {
		pm, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		if pm["status"] == "granted" {
			if name, ok := pm["permission"].(string); ok {
				granted = append(granted, name)
			}
		}
	}
	return granted, nil
}

// PageInfo represents a Facebook Page
type PageInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	AccessToken string `json:"access_token"`
}
