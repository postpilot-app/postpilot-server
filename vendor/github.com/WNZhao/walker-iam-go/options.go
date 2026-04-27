package iamsdk

import (
	"context"
	"net/http"
	"time"
)

type Logger interface {
	Warnf(format string, args ...interface{})
}

type noopLogger struct{}

func (noopLogger) Warnf(string, ...interface{}) {}

type JTIChecker func(ctx context.Context, jti string) (revoked bool, err error)

type verifierOpts struct {
	jwksTTL    time.Duration
	httpClient *http.Client
	issuer     string
	jtiChecker JTIChecker
	logger     Logger
	lazyInit   bool
}

type Option func(*verifierOpts)

func WithJWKSTTL(d time.Duration) Option {
	return func(o *verifierOpts) { o.jwksTTL = d }
}

func WithHTTPClient(c *http.Client) Option {
	return func(o *verifierOpts) { o.httpClient = c }
}

func WithIssuer(iss string) Option {
	return func(o *verifierOpts) { o.issuer = iss }
}

func WithJTIChecker(fn JTIChecker) Option {
	return func(o *verifierOpts) { o.jtiChecker = fn }
}

func WithLogger(l Logger) Option {
	return func(o *verifierOpts) { o.logger = l }
}

// WithLazyInit lets NewVerifier succeed even if the initial JWKS fetch
// fails; in that case ParseToken will attempt to refresh on each call
// until JWKS becomes reachable. Useful when the IAM server may not be
// ready at consumer startup.
func WithLazyInit(lazy bool) Option {
	return func(o *verifierOpts) { o.lazyInit = lazy }
}

func defaultOpts() verifierOpts {
	return verifierOpts{
		jwksTTL:    1 * time.Hour,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		issuer:     "walker-iam",
		logger:     noopLogger{},
	}
}
