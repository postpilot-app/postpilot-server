package iamsdk

import "errors"

var (
	ErrNoToken       = errors.New("iamsdk: no token provided")
	ErrMalformedAuth = errors.New("iamsdk: malformed Authorization header")
	ErrInvalidToken  = errors.New("iamsdk: invalid token")
	ErrTokenExpired  = errors.New("iamsdk: token expired")
	ErrInvalidIssuer = errors.New("iamsdk: invalid issuer")
	ErrKidNotFound   = errors.New("iamsdk: kid not found in JWKS")
	ErrJWKSFetch     = errors.New("iamsdk: failed to fetch JWKS")
	ErrTokenRevoked  = errors.New("iamsdk: token revoked")
)
