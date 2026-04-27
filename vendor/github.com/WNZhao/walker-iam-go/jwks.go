package iamsdk

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"
)

type jwk struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type jwksDoc struct {
	Keys []jwk `json:"keys"`
}

type jwksCache struct {
	url        string
	httpClient *http.Client
	ttl        time.Duration

	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
}

func newJWKSCache(url string, client *http.Client, ttl time.Duration) *jwksCache {
	return &jwksCache{
		url:        url,
		httpClient: client,
		ttl:        ttl,
		keys:       map[string]*rsa.PublicKey{},
	}
}

func (j *jwksCache) get(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	j.mu.RLock()
	k, ok := j.keys[kid]
	stale := time.Since(j.fetchedAt) > j.ttl
	j.mu.RUnlock()

	if ok && !stale {
		return k, nil
	}

	if err := j.refresh(ctx); err != nil {
		if ok {
			return k, nil
		}
		return nil, err
	}

	j.mu.RLock()
	defer j.mu.RUnlock()
	k, ok = j.keys[kid]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrKidNotFound, kid)
	}
	return k, nil
}

func (j *jwksCache) isEmpty() bool {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return len(j.keys) == 0
}

func (j *jwksCache) getSingle() (*rsa.PublicKey, bool) {
	j.mu.RLock()
	defer j.mu.RUnlock()
	if len(j.keys) != 1 {
		return nil, false
	}
	for _, k := range j.keys {
		return k, true
	}
	return nil, false
}

func (j *jwksCache) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, j.url, nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrJWKSFetch, err)
	}
	resp, err := j.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrJWKSFetch, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", ErrJWKSFetch, resp.StatusCode)
	}
	var doc jwksDoc
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return fmt.Errorf("%w: %v", ErrJWKSFetch, err)
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range doc.Keys {
		if k.Kty != "RSA" {
			continue
		}
		pk, err := jwkToRSA(k)
		if err != nil {
			continue
		}
		keys[k.Kid] = pk
	}
	if len(keys) == 0 {
		return fmt.Errorf("%w: no valid RSA keys", ErrJWKSFetch)
	}
	j.mu.Lock()
	j.keys = keys
	j.fetchedAt = time.Now()
	j.mu.Unlock()
	return nil
}

func jwkToRSA(k jwk) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, err
	}
	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)
	return &rsa.PublicKey{N: n, E: int(e.Int64())}, nil
}
