package oauth

import (
	"context"
	"net/http"
	"time"
)

// CredentialValidator checks a TrueNAS API key against the target before
// the authorization endpoint issues a code bound to it -- the same check
// every other credential path in this server performs (see
// SessionManager.Open in internal/server/session.go), just run one step
// earlier so a bad key is caught before any client attempts to use it.
type CredentialValidator interface {
	Validate(ctx context.Context, apiKey string) error
}

// Config is what the authorization server needs to seal and verify its own
// values and to check a resource owner's credential. It holds no client or
// token storage -- see the package doc.
type Config struct {
	// Issuer is the externally-reachable base URL this server is known by,
	// e.g. "https://truenas-mcp.example.com". Every endpoint path in
	// discovery metadata is issued relative to it.
	Issuer string

	// Keys seals and verifies every client_id, authorization code, access
	// token, and refresh token this handler issues.
	Keys Keys

	// AccessTokenTTL and RefreshTokenTTL bound how long an issued token is
	// valid. RefreshTokenTTL <= 0 disables refresh token issuance: the
	// refresh_token grant is refused and no refresh token is minted on any
	// other grant.
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration

	// Validator checks a resource owner's TrueNAS credential at the
	// authorization endpoint before a code is issued for it.
	Validator CredentialValidator
}

// Handler serves the OAuth 2.1 authorization server: discovery metadata,
// Dynamic Client Registration, the authorization/consent endpoint, and the
// token endpoint.
type Handler struct {
	cfg   Config
	codes *ReplayCache
}

// NewHandler builds a Handler from cfg.
func NewHandler(cfg Config) *Handler {
	return &Handler{cfg: cfg, codes: NewReplayCache()}
}

// Mount registers every OAuth route on mux, so it can be served alongside
// the existing MCP handler on the same listener.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET "+ProtectedResourceMetadataPath, h.handleProtectedResourceMetadata)
	mux.HandleFunc("GET "+AuthorizationServerMetadataPath, h.handleAuthorizationServerMetadata)
	mux.HandleFunc("POST "+RegisterPath, h.handleRegister)
	mux.HandleFunc("GET "+AuthorizePath, h.handleAuthorizeGet)
	mux.HandleFunc("POST "+AuthorizePath, h.handleAuthorizePost)
	mux.HandleFunc("POST "+TokenPath, h.handleToken)
}
