package oauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"time"
)

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

func (h *Handler) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "request body must be application/x-www-form-urlencoded")
		return
	}

	switch r.PostFormValue("grant_type") {
	case "authorization_code":
		h.handleAuthorizationCodeGrant(w, r)
	case "refresh_token":
		h.handleRefreshTokenGrant(w, r)
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type",
			`grant_type must be "authorization_code" or "refresh_token"`)
	}
}

func (h *Handler) handleAuthorizationCodeGrant(w http.ResponseWriter, r *http.Request) {
	clientID := r.PostFormValue("client_id")
	code := r.PostFormValue("code")
	verifier := r.PostFormValue("code_verifier")
	redirectURI := r.PostFormValue("redirect_uri")

	// redirect_uri is mandatory at the authorization endpoint that produced
	// any code this server issues (see parseAuthorizeRequest), so per RFC
	// 6749 §4.1.3 / OAuth 2.1 §4.1.3 it is required here too and always
	// checked -- never skipped just because a client omitted it. client_id
	// is required for the same reason every registered client here is
	// public (no client_secret, token_endpoint_auth_method "none") -- RFC
	// 6749 §3.2.1 requires it on this grant for exactly that client type.
	if code == "" || verifier == "" || redirectURI == "" || clientID == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "client_id, code, code_verifier, and redirect_uri are required")
		return
	}

	now := time.Now()
	payload, err := DecodeCode(h.cfg.Keys, code, now)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "authorization code is invalid or expired")
		return
	}

	if subtle.ConstantTimeCompare([]byte(clientID), []byte(payload.ClientID)) != 1 {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "authorization code was not issued to this client")
		return
	}

	if redirectURI != payload.RedirectURI {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri does not match the authorization request")
		return
	}

	if !verifyPKCE(payload.CodeChallenge, verifier) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "code_verifier does not match code_challenge")
		return
	}

	// Checked last, after every other reason to reject this code, so a
	// client that retries a request for an unrelated reason (its own
	// network hiccup, say) doesn't burn its one shot at redemption on a
	// request that was going to fail anyway.
	if !h.codes.Claim(CodeReplayID(code), CodeTTL) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "authorization code has already been used")
		return
	}

	h.issueTokens(w, payload.Credential, now)
}

func (h *Handler) handleRefreshTokenGrant(w http.ResponseWriter, r *http.Request) {
	if h.cfg.RefreshTokenTTL <= 0 {
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "refresh tokens are disabled on this server")
		return
	}

	refreshToken := r.PostFormValue("refresh_token")
	if refreshToken == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "refresh_token is required")
		return
	}

	now := time.Now()
	payload, err := DecodeRefreshToken(h.cfg.Keys, refreshToken, now)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "refresh token is invalid or expired")
		return
	}

	h.issueTokens(w, payload.Credential, now)
}

func (h *Handler) issueTokens(w http.ResponseWriter, cred Credential, now time.Time) {
	access, err := EncodeAccessToken(h.cfg.Keys, cred, h.cfg.AccessTokenTTL, now)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "could not issue an access token")
		return
	}

	resp := tokenResponse{
		AccessToken: access,
		TokenType:   "Bearer",
		ExpiresIn:   int64(h.cfg.AccessTokenTTL.Seconds()),
	}

	if h.cfg.RefreshTokenTTL > 0 {
		refresh, err := EncodeRefreshToken(h.cfg.Keys, cred, h.cfg.RefreshTokenTTL, now)
		if err != nil {
			writeOAuthError(w, http.StatusInternalServerError, "server_error", "could not issue a refresh token")
			return
		}
		resp.RefreshToken = refresh
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, resp)
}

// verifyPKCE checks verifier against the S256 code_challenge recorded at
// authorization time, per RFC 7636.
func verifyPKCE(challenge, verifier string) bool {
	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(computed), []byte(challenge)) == 1
}
