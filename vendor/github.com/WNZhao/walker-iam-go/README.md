# walker-iam-go

Go SDK for Walker IAM — JWKS fetching, RS256 token verification, and Gin middleware.

## Install

```bash
go get github.com/WNZhao/walker-iam-go
```

## Quick start

```go
import iamsdk "github.com/WNZhao/walker-iam-go"

v, err := iamsdk.NewVerifier("https://iam-api.walker-learn.xyz/.well-known/jwks.json")
if err != nil {
    log.Fatal(err)
}

r := gin.New()
r.Use(v.GinAuth())
r.GET("/admin", v.RequireAdmin(), func(c *gin.Context) {
    claims, _ := iamsdk.ClaimsFromContext(c)
    c.JSON(200, gin.H{"user": claims.Username, "tenant": claims.TenantCode})
})
```

## Options

| Option | Default | Purpose |
|---|---|---|
| `WithJWKSTTL(d)` | `1h` | JWKS cache TTL |
| `WithHTTPClient(c)` | `http.DefaultClient` w/ 10s timeout | Custom HTTP client |
| `WithIssuer(iss)` | `"walker-iam"` | Enforce `iss` claim (empty disables) |
| `WithJTIChecker(fn)` | nil | Optional JTI blacklist check (Redis / HTTP) |
| `WithLogger(l)` | no-op | Inject logger |
| `WithLazyInit(true)` | `false` | Don't fail at startup if IAM is unreachable; retry on each request |

## Claims

```go
type IAMClaims struct {
    UserID     uint   `json:"user_id"`
    Username   string `json:"username"`
    TenantCode string `json:"tenant_code"`
    TenantID   uint   `json:"tenant_id"`
    Role       string `json:"role"`
    AppCode    string `json:"app_code,omitempty"`
    jwt.RegisteredClaims
}
```

`IsAdmin()` matches `admin`, `super_admin`, `platform_admin`.

## Context helpers

```go
claims, ok := iamsdk.ClaimsFromContext(c)
uid := iamsdk.UserIDFromContext(c)
tc  := iamsdk.TenantCodeFromContext(c)
role := iamsdk.RoleFromContext(c)
```

## License

MIT
