package iamsdk

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

type Verifier struct {
	opts verifierOpts
	jwks *jwksCache
}

func NewVerifier(jwksURL string, opts ...Option) (*Verifier, error) {
	o := defaultOpts()
	for _, opt := range opts {
		opt(&o)
	}
	v := &Verifier{
		opts: o,
		jwks: newJWKSCache(jwksURL, o.httpClient, o.jwksTTL),
	}
	if err := v.jwks.refresh(context.Background()); err != nil {
		if o.lazyInit {
			o.logger.Warnf("iamsdk: initial JWKS fetch failed (lazy mode, will retry on request): %v", err)
			return v, nil
		}
		return nil, err
	}
	return v, nil
}

func (v *Verifier) RefreshJWKS(ctx context.Context) error {
	return v.jwks.refresh(ctx)
}

func (v *Verifier) ParseToken(ctx context.Context, tokenStr string) (*IAMClaims, error) {
	claims := &IAMClaims{}
	_, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("%w: unexpected signing method %v", ErrInvalidToken, t.Header["alg"])
		}
		kid, _ := t.Header["kid"].(string)
		return v.keyForKid(ctx, kid)
	})
	if err != nil {
		switch {
		case errors.Is(err, jwt.ErrTokenExpired):
			return nil, ErrTokenExpired
		default:
			return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
		}
	}
	if v.opts.issuer != "" && claims.Issuer != v.opts.issuer {
		return nil, ErrInvalidIssuer
	}
	if v.opts.jtiChecker != nil && claims.ID != "" {
		revoked, cerr := v.opts.jtiChecker(ctx, claims.ID)
		if cerr != nil {
			v.opts.logger.Warnf("iamsdk: jti check failed: %v", cerr)
		} else if revoked {
			return nil, ErrTokenRevoked
		}
	}
	return claims, nil
}

func (v *Verifier) keyForKid(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	if kid == "" {
		if k, ok := v.jwks.getSingle(); ok {
			return k, nil
		}
		// No keys loaded yet (lazy init failure); try once.
		if v.jwks.isEmpty() {
			if err := v.jwks.refresh(ctx); err != nil {
				return nil, err
			}
			if k, ok := v.jwks.getSingle(); ok {
				return k, nil
			}
		}
		return nil, fmt.Errorf("%w: missing kid header", ErrInvalidToken)
	}
	return v.jwks.get(ctx, kid)
}
