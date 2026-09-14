package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cedricziel/truenas-mcp/internal/oauth"
)

func TestMountOAuthNoOpWhenIssuerEmpty(t *testing.T) {
	mux := http.NewServeMux()
	keys, resourceMetadataURL := MountOAuth(mux, nil, "", [32]byte{}, time.Hour, time.Hour)

	if keys != nil {
		t.Fatal("expected no keys when issuer is empty")
	}
	if resourceMetadataURL != "" {
		t.Fatalf("expected no resource metadata URL, got %q", resourceMetadataURL)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, oauth.ProtectedResourceMetadataPath, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("discovery route should not be mounted when OAuth is disabled, got status %d", rec.Code)
	}
}

func TestMountOAuthRegistersRoutes(t *testing.T) {
	target := newFakeTarget(t)
	sessions := NewSessionManager(target.URL(), false)
	t.Cleanup(sessions.CloseAll)

	mux := http.NewServeMux()
	keys, resourceMetadataURL := MountOAuth(mux, sessions, "https://truenas-mcp.example.com",
		[32]byte{1, 2, 3}, time.Hour, time.Hour)

	if keys == nil {
		t.Fatal("expected non-nil keys when issuer is set")
	}
	if resourceMetadataURL != "https://truenas-mcp.example.com"+oauth.ProtectedResourceMetadataPath {
		t.Fatalf("resourceMetadataURL = %q", resourceMetadataURL)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, oauth.ProtectedResourceMetadataPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("discovery route should be mounted, got status %d: %s", rec.Code, rec.Body)
	}
}

// TestOAuthEndToEndAuthenticatesMCPRequest drives the full flow this
// capability exists for: register a client, obtain consent with a TrueNAS
// credential the fake target accepts, exchange the code for an access
// token with PKCE, and use that token -- not the raw API key -- to
// authenticate a real MCP request.
func TestOAuthEndToEndAuthenticatesMCPRequest(t *testing.T) {
	target := newFakeTarget(t)
	sessions := NewSessionManager(target.URL(), false)
	t.Cleanup(sessions.CloseAll)

	mux := http.NewServeMux()
	oauthKeys, resourceMetadataURL := MountOAuth(mux, sessions, "https://truenas-mcp.example.com",
		[32]byte{1, 2, 3}, time.Hour, time.Hour)

	mux.Handle("/mcp", NewMCPHandler(MCPConfig{
		Version:                      "test",
		Target:                       "nas.local",
		Sessions:                     sessions,
		OAuthKeys:                    oauthKeys,
		ProtectedResourceMetadataURL: resourceMetadataURL,
	}))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	const redirectURI = "https://claude.ai/api/mcp/callback"

	// 1. Dynamic Client Registration.
	regRec := httptest.NewRecorder()
	mux.ServeHTTP(regRec, httptest.NewRequest(http.MethodPost, oauth.RegisterPath,
		strings.NewReader(`{"redirect_uris":["`+redirectURI+`"]}`)))
	if regRec.Code != http.StatusCreated {
		t.Fatalf("register: status = %d: %s", regRec.Code, regRec.Body)
	}
	var reg struct {
		ClientID string `json:"client_id"`
	}
	if err := json.NewDecoder(regRec.Body).Decode(&reg); err != nil {
		t.Fatalf("decoding registration response: %v", err)
	}

	// 2. Authorization with PKCE, using the credential the fake target
	// actually accepts (see fakeTargetAPIKey in session_provider_test.go).
	verifier := "test-code-verifier-that-is-long-enough-1234567890"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	form := url.Values{
		"client_id":             {reg.ClientID},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"api_key":               {fakeTargetAPIKey},
	}
	authReq := httptest.NewRequest(http.MethodPost, oauth.AuthorizePath, strings.NewReader(form.Encode()))
	authReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	authRec := httptest.NewRecorder()
	mux.ServeHTTP(authRec, authReq)
	if authRec.Code != http.StatusFound {
		t.Fatalf("authorize: status = %d: %s", authRec.Code, authRec.Body)
	}
	loc, err := url.Parse(authRec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parsing Location: %v", err)
	}
	code := loc.Query().Get("code")
	if code == "" {
		t.Fatal("expected an authorization code in the redirect")
	}

	// 3. Token exchange.
	tokenForm := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {redirectURI},
	}
	tokenReq := httptest.NewRequest(http.MethodPost, oauth.TokenPath, strings.NewReader(tokenForm.Encode()))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenRec := httptest.NewRecorder()
	mux.ServeHTTP(tokenRec, tokenReq)
	if tokenRec.Code != http.StatusOK {
		t.Fatalf("token: status = %d: %s", tokenRec.Code, tokenRec.Body)
	}
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(tokenRec.Body).Decode(&tok); err != nil {
		t.Fatalf("decoding token response: %v", err)
	}
	if tok.AccessToken == "" {
		t.Fatal("expected a non-empty access_token")
	}

	// 4. The access token authenticates an MCP request, the same as the raw
	// API key would.
	httpReq, err := http.NewRequest(http.MethodPost, srv.URL+"/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`))
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	httpReq.Header.Set("Authorization", "Bearer "+tok.AccessToken)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("mcp request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mcp initialize status = %d, want 200", resp.StatusCode)
	}

	// 5. The refresh token still works after the access token was already
	// used, confirming refresh does not depend on the exchanged code.
	refreshForm := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {tok.RefreshToken},
	}
	refreshReq := httptest.NewRequest(http.MethodPost, oauth.TokenPath, strings.NewReader(refreshForm.Encode()))
	refreshReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	refreshRec := httptest.NewRecorder()
	mux.ServeHTTP(refreshRec, refreshReq)
	if refreshRec.Code != http.StatusOK {
		t.Fatalf("refresh: status = %d: %s", refreshRec.Code, refreshRec.Body)
	}
}

// A raw invalid credential submitted through the consent form never reaches
// a client: SessionCredentialValidator rejects it against the real fake
// target before any code is issued.
func TestSessionCredentialValidatorRejectsBadKey(t *testing.T) {
	target := newFakeTarget(t)
	sessions := NewSessionManager(target.URL(), false)
	t.Cleanup(sessions.CloseAll)

	v := SessionCredentialValidator{Sessions: sessions}
	if err := v.Validate(context.Background(), "1-wrongkey"); err == nil {
		t.Fatal("expected an error for a key the target rejects")
	}
	if err := v.Validate(context.Background(), fakeTargetAPIKey); err != nil {
		t.Fatalf("expected the target's own accepted key to validate: %v", err)
	}
}
