package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequireCredentialRejectsUnauthenticated(t *testing.T) {
	reached := false
	h := RequireCredential(nil, "", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		reached = true
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if reached {
		t.Fatal("an unauthenticated request must not reach the MCP handler")
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Error("a 401 should tell the client how to authenticate")
	}
}

func TestRequireCredentialAllowsAuthenticated(t *testing.T) {
	reached := false
	h := RequireCredential(nil, "", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer 1-key")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !reached {
		t.Fatal("an authenticated request must reach the MCP handler")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// When OAuth is enabled, the challenge names the protected resource
// metadata URL so an OAuth-capable client can discover how to obtain a
// credential, per the mcp-transport spec's "Missing credential" scenario.
func TestRequireCredentialChallengeNamesResourceMetadataWhenOAuthEnabled(t *testing.T) {
	const metadataURL = "https://truenas-mcp.example.com/.well-known/oauth-protected-resource"
	h := RequireCredential(nil, metadataURL, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))

	challenge := rec.Header().Get("WWW-Authenticate")
	if !strings.Contains(challenge, metadataURL) {
		t.Fatalf("WWW-Authenticate = %q, want it to name %q", challenge, metadataURL)
	}
}

func TestRequireCredentialChallengeOmitsResourceMetadataWhenOAuthDisabled(t *testing.T) {
	h := RequireCredential(nil, "", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))

	challenge := rec.Header().Get("WWW-Authenticate")
	if strings.Contains(challenge, "resource_metadata") {
		t.Fatalf("WWW-Authenticate = %q, must not name resource_metadata when OAuth is disabled", challenge)
	}
}
