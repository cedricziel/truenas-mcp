package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// fakeValidator accepts exactly one TrueNAS API key, standing in for
// SessionManager.Open in these package-local tests -- see
// internal/server/mcp.go for the real adapter.
type fakeValidator struct {
	acceptedKey string
}

func (v fakeValidator) Validate(_ context.Context, apiKey string) error {
	if apiKey != v.acceptedKey {
		return errors.New("invalid credential")
	}
	return nil
}

func newTestHandler(t *testing.T, validator CredentialValidator, refreshTTL time.Duration) *Handler {
	t.Helper()
	return NewHandler(Config{
		Issuer:          "https://truenas-mcp.example.com",
		Keys:            testKeys(t),
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: refreshTTL,
		Validator:       validator,
	})
}

func newTestMux(t *testing.T, validator CredentialValidator, refreshTTL time.Duration) (*Handler, *http.ServeMux) {
	t.Helper()
	h := newTestHandler(t, validator, refreshTTL)
	mux := http.NewServeMux()
	h.Mount(mux)
	return h, mux
}

func TestProtectedResourceMetadata(t *testing.T) {
	_, mux := newTestMux(t, fakeValidator{}, time.Hour)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ProtectedResourceMetadataPath, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var body protectedResourceMetadata
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body.Resource != "https://truenas-mcp.example.com/mcp" {
		t.Errorf("resource = %q", body.Resource)
	}
	if len(body.AuthorizationServers) != 1 || body.AuthorizationServers[0] != "https://truenas-mcp.example.com" {
		t.Errorf("authorization_servers = %v", body.AuthorizationServers)
	}
}

func TestAuthorizationServerMetadata(t *testing.T) {
	_, mux := newTestMux(t, fakeValidator{}, time.Hour)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, AuthorizationServerMetadataPath, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var body authorizationServerMetadata
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body.AuthorizationEndpoint != "https://truenas-mcp.example.com"+AuthorizePath {
		t.Errorf("authorization_endpoint = %q", body.AuthorizationEndpoint)
	}
	if body.TokenEndpoint != "https://truenas-mcp.example.com"+TokenPath {
		t.Errorf("token_endpoint = %q", body.TokenEndpoint)
	}
	if body.RegistrationEndpoint != "https://truenas-mcp.example.com"+RegisterPath {
		t.Errorf("registration_endpoint = %q", body.RegistrationEndpoint)
	}
	if len(body.CodeChallengeMethodsSupported) != 1 || body.CodeChallengeMethodsSupported[0] != "S256" {
		t.Errorf("code_challenge_methods_supported = %v", body.CodeChallengeMethodsSupported)
	}
	if len(body.TokenEndpointAuthMethodsSupported) != 1 || body.TokenEndpointAuthMethodsSupported[0] != "none" {
		t.Errorf("token_endpoint_auth_methods_supported = %v", body.TokenEndpointAuthMethodsSupported)
	}
}

func TestAuthorizationServerMetadataOmitsRefreshWhenDisabled(t *testing.T) {
	_, mux := newTestMux(t, fakeValidator{}, 0)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, AuthorizationServerMetadataPath, nil))

	var body authorizationServerMetadata
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	for _, g := range body.GrantTypesSupported {
		if g == "refresh_token" {
			t.Fatalf("grant_types_supported must not advertise refresh_token when disabled: %v", body.GrantTypesSupported)
		}
	}
}

func TestRegisterValidClient(t *testing.T) {
	_, mux := newTestMux(t, fakeValidator{}, time.Hour)

	body := strings.NewReader(`{"redirect_uris":["https://claude.ai/api/mcp/callback"],"client_name":"Claude"}`)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, RegisterPath, body))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body)
	}
	var resp registerResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.ClientID == "" {
		t.Fatal("expected a non-empty client_id")
	}
	if resp.TokenEndpointAuthMethod != "none" {
		t.Errorf("token_endpoint_auth_method = %q, want none", resp.TokenEndpointAuthMethod)
	}
}

func TestRegisterRejectsMissingRedirectURI(t *testing.T) {
	_, mux := newTestMux(t, fakeValidator{}, time.Hour)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, RegisterPath, strings.NewReader(`{}`)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
	var body oauthError
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body.Error != "invalid_redirect_uri" {
		t.Errorf("error = %q, want invalid_redirect_uri", body.Error)
	}
}

func TestRegisterRejectsMalformedRedirectURI(t *testing.T) {
	_, mux := newTestMux(t, fakeValidator{}, time.Hour)

	body := strings.NewReader(`{"redirect_uris":["not-a-url"]}`)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, RegisterPath, body))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
}

// registerClient is a test helper driving the registration endpoint the
// same way a real client would, rather than calling EncodeClientID
// directly, so these tests exercise the actual HTTP contract.
func registerClient(t *testing.T, mux *http.ServeMux, redirectURI string) string {
	t.Helper()
	body := strings.NewReader(`{"redirect_uris":["` + redirectURI + `"],"client_name":"Test Client"}`)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, RegisterPath, body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("registering client: status = %d: %s", rec.Code, rec.Body)
	}
	var resp registerResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding registration response: %v", err)
	}
	return resp.ClientID
}

const testRedirectURI = "https://claude.ai/api/mcp/callback"

// pkcePair returns a PKCE code verifier and its S256 challenge.
func pkcePair() (verifier, challenge string) {
	verifier = "test-code-verifier-that-is-long-enough-1234567890"
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge
}

func TestAuthorizeGetValidRequest(t *testing.T) {
	_, mux := newTestMux(t, fakeValidator{}, time.Hour)
	clientID := registerClient(t, mux, testRedirectURI)
	_, challenge := pkcePair()

	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {testRedirectURI},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {"xyz"},
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, AuthorizePath+"?"+q.Encode(), nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "api_key") {
		t.Fatal("expected the consent form to include an api_key field")
	}
}

func TestAuthorizeGetRejectsUnregisteredRedirectURI(t *testing.T) {
	_, mux := newTestMux(t, fakeValidator{}, time.Hour)
	clientID := registerClient(t, mux, testRedirectURI)
	_, challenge := pkcePair()

	q := url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {"https://evil.example/callback"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, AuthorizePath+"?"+q.Encode(), nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
}

func TestAuthorizeGetRejectsMissingPKCE(t *testing.T) {
	_, mux := newTestMux(t, fakeValidator{}, time.Hour)
	clientID := registerClient(t, mux, testRedirectURI)

	q := url.Values{
		"client_id":    {clientID},
		"redirect_uri": {testRedirectURI},
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, AuthorizePath+"?"+q.Encode(), nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
}

func doAuthorizePost(t *testing.T, mux *http.ServeMux, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, AuthorizePath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestAuthorizePostAcceptsValidCredential(t *testing.T) {
	_, mux := newTestMux(t, fakeValidator{acceptedKey: "1-goodkey"}, time.Hour)
	clientID := registerClient(t, mux, testRedirectURI)
	_, challenge := pkcePair()

	rec := doAuthorizePost(t, mux, url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {testRedirectURI},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {"xyz"},
		"username":              {"admin"},
		"api_key":               {"1-goodkey"},
	})

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302: %s", rec.Code, rec.Body)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parsing Location: %v", err)
	}
	if loc.Query().Get("state") != "xyz" {
		t.Errorf("state = %q, want xyz", loc.Query().Get("state"))
	}
	if loc.Query().Get("code") == "" {
		t.Fatal("expected a code in the redirect")
	}
}

func TestAuthorizePostRejectsInvalidCredentialWithoutRedirecting(t *testing.T) {
	_, mux := newTestMux(t, fakeValidator{acceptedKey: "1-goodkey"}, time.Hour)
	clientID := registerClient(t, mux, testRedirectURI)
	_, challenge := pkcePair()

	rec := doAuthorizePost(t, mux, url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {testRedirectURI},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"api_key":               {"1-wrongkey"},
	})

	if rec.Code == http.StatusFound {
		t.Fatal("a rejected credential must not redirect")
	}
	if strings.Contains(rec.Body.String(), "1-wrongkey") {
		t.Fatal("the submitted key must never be echoed back")
	}
}

// authorizeAndExchange drives the full authorize -> token exchange with a
// valid credential, returning the token response.
func authorizeAndExchange(t *testing.T, mux *http.ServeMux, apiKey, verifier, challenge, clientID string) tokenResponse {
	t.Helper()

	authRec := doAuthorizePost(t, mux, url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {testRedirectURI},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"api_key":               {apiKey},
	})
	if authRec.Code != http.StatusFound {
		t.Fatalf("authorize: status = %d: %s", authRec.Code, authRec.Body)
	}
	loc, err := url.Parse(authRec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parsing Location: %v", err)
	}
	code := loc.Query().Get("code")

	tokenRec := doTokenPost(t, mux, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {testRedirectURI},
	})
	if tokenRec.Code != http.StatusOK {
		t.Fatalf("token: status = %d: %s", tokenRec.Code, tokenRec.Body)
	}

	var resp tokenResponse
	if err := json.NewDecoder(tokenRec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding token response: %v", err)
	}
	return resp
}

func doTokenPost(t *testing.T, mux *http.ServeMux, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, TokenPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestTokenExchangeIssuesAccessAndRefreshToken(t *testing.T) {
	_, mux := newTestMux(t, fakeValidator{acceptedKey: "1-goodkey"}, time.Hour)
	clientID := registerClient(t, mux, testRedirectURI)
	verifier, challenge := pkcePair()

	resp := authorizeAndExchange(t, mux, "1-goodkey", verifier, challenge, clientID)

	if !IsAccessToken(resp.AccessToken) {
		t.Fatalf("access_token does not look like an access token: %q", resp.AccessToken)
	}
	if resp.TokenType != "Bearer" {
		t.Errorf("token_type = %q, want Bearer", resp.TokenType)
	}
	if resp.RefreshToken == "" {
		t.Fatal("expected a refresh_token when refresh tokens are enabled")
	}
}

func TestTokenExchangeOmitsRefreshTokenWhenDisabled(t *testing.T) {
	_, mux := newTestMux(t, fakeValidator{acceptedKey: "1-goodkey"}, 0)
	clientID := registerClient(t, mux, testRedirectURI)
	verifier, challenge := pkcePair()

	resp := authorizeAndExchange(t, mux, "1-goodkey", verifier, challenge, clientID)

	if resp.RefreshToken != "" {
		t.Fatalf("expected no refresh_token when disabled, got %q", resp.RefreshToken)
	}
}

func TestTokenExchangeRejectsMismatchedVerifier(t *testing.T) {
	_, mux := newTestMux(t, fakeValidator{acceptedKey: "1-goodkey"}, time.Hour)
	clientID := registerClient(t, mux, testRedirectURI)
	_, challenge := pkcePair()

	authRec := doAuthorizePost(t, mux, url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {testRedirectURI},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"api_key":               {"1-goodkey"},
	})
	loc, _ := url.Parse(authRec.Header().Get("Location"))
	code := loc.Query().Get("code")

	rec := doTokenPost(t, mux, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {"the-wrong-verifier"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
}

func TestTokenExchangeRejectsReplayedCode(t *testing.T) {
	_, mux := newTestMux(t, fakeValidator{acceptedKey: "1-goodkey"}, time.Hour)
	clientID := registerClient(t, mux, testRedirectURI)
	verifier, challenge := pkcePair()

	authRec := doAuthorizePost(t, mux, url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {testRedirectURI},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"api_key":               {"1-goodkey"},
	})
	loc, _ := url.Parse(authRec.Header().Get("Location"))
	code := loc.Query().Get("code")

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {verifier},
	}
	first := doTokenPost(t, mux, form)
	if first.Code != http.StatusOK {
		t.Fatalf("first exchange: status = %d: %s", first.Code, first.Body)
	}

	second := doTokenPost(t, mux, form)
	if second.Code != http.StatusBadRequest {
		t.Fatalf("second exchange: status = %d, want 400: %s", second.Code, second.Body)
	}
}

func TestTokenExchangeRejectsExpiredCode(t *testing.T) {
	keys := testKeys(t)
	h := NewHandler(Config{
		Issuer:          "https://truenas-mcp.example.com",
		Keys:            keys,
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: time.Hour,
		Validator:       fakeValidator{acceptedKey: "1-goodkey"},
	})
	mux := http.NewServeMux()
	h.Mount(mux)

	expired, err := EncodeCode(keys, CodePayload{
		Credential:    Credential{APIKey: "1-goodkey"},
		ClientID:      "whatever",
		RedirectURI:   testRedirectURI,
		CodeChallenge: "challenge",
	}, time.Now().Add(-2*CodeTTL))
	if err != nil {
		t.Fatalf("EncodeCode: %v", err)
	}

	rec := doTokenPost(t, mux, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {expired},
		"code_verifier": {"anything"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
}

func TestRefreshTokenGrantIssuesNewAccessToken(t *testing.T) {
	_, mux := newTestMux(t, fakeValidator{acceptedKey: "1-goodkey"}, time.Hour)
	clientID := registerClient(t, mux, testRedirectURI)
	verifier, challenge := pkcePair()

	first := authorizeAndExchange(t, mux, "1-goodkey", verifier, challenge, clientID)

	rec := doTokenPost(t, mux, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {first.RefreshToken},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var second tokenResponse
	if err := json.NewDecoder(rec.Body).Decode(&second); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if !IsAccessToken(second.AccessToken) {
		t.Fatalf("refreshed access_token does not look like an access token: %q", second.AccessToken)
	}
}

func TestRefreshTokenGrantDisabled(t *testing.T) {
	_, mux := newTestMux(t, fakeValidator{acceptedKey: "1-goodkey"}, 0)

	rec := doTokenPost(t, mux, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {"tnmcp_rt_whatever"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
	var body oauthError
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body.Error != "unsupported_grant_type" {
		t.Errorf("error = %q, want unsupported_grant_type", body.Error)
	}
}

func TestRefreshTokenGrantRejectsExpired(t *testing.T) {
	keys := testKeys(t)
	h := NewHandler(Config{
		Issuer:          "https://truenas-mcp.example.com",
		Keys:            keys,
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: time.Hour,
		Validator:       fakeValidator{acceptedKey: "1-goodkey"},
	})
	mux := http.NewServeMux()
	h.Mount(mux)

	expired, err := EncodeRefreshToken(keys, Credential{APIKey: "1-goodkey"}, time.Hour, time.Now().Add(-2*time.Hour))
	if err != nil {
		t.Fatalf("EncodeRefreshToken: %v", err)
	}

	rec := doTokenPost(t, mux, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {expired},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
}

func TestTokenEndpointRejectsUnsupportedGrant(t *testing.T) {
	_, mux := newTestMux(t, fakeValidator{}, time.Hour)

	rec := doTokenPost(t, mux, url.Values{"grant_type": {"client_credentials"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
}
