package iamsdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// HTTPJTIChecker returns a JTIChecker that calls the IAM server's
// POST <iamBase>/api/token/check endpoint to determine whether a
// given JWT ID has been revoked (e.g. via logout).
//
// iamBase should be the IAM server root (e.g. "http://iam.example:8090"),
// with or without trailing slash. If client is nil a default client with
// a 5s timeout is used.
//
// The endpoint is expected to return:
//
//	{"code":0,"message":"success","data":{"revoked":bool}}
func HTTPJTIChecker(iamBase string, client *http.Client) JTIChecker {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	url := strings.TrimRight(iamBase, "/") + "/api/token/check"

	return func(ctx context.Context, jti string) (bool, error) {
		payload, err := json.Marshal(map[string]string{"jti": jti})
		if err != nil {
			return false, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			return false, err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return false, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return false, fmt.Errorf("iamsdk: token/check status %d", resp.StatusCode)
		}

		var body struct {
			Code int `json:"code"`
			Data struct {
				Revoked bool `json:"revoked"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return false, err
		}
		return body.Data.Revoked, nil
	}
}

// IAMBaseFromJWKS derives an IAM base URL from a JWKS URL by stripping
// the "/.well-known/jwks.json" suffix. Returns jwksURL unchanged if the
// suffix is absent.
func IAMBaseFromJWKS(jwksURL string) string {
	const suffix = "/.well-known/jwks.json"
	if strings.HasSuffix(jwksURL, suffix) {
		return strings.TrimSuffix(jwksURL, suffix)
	}
	return jwksURL
}
