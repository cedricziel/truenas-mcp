package oauth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// maxRegisterBodyBytes bounds how much of a registration request this
// server will read. Registration is intentionally unauthenticated (RFC
// 7591 is an open endpoint), so unlike every other body-reading path here
// -- which goes through r.ParseForm(), capped by the stdlib at 10MB -- this
// one needs its own explicit limit rather than relying on a credential
// check to have already turned away an abusive caller.
const maxRegisterBodyBytes = 1 << 20 // 1MB

// registerRequest is the subset of RFC 7591's client metadata this server
// cares about. Every registered client is treated as public (no
// client_secret) with PKCE required, matching how MCP clients register
// today -- see design.md's "no confidential-client support" non-goal.
type registerRequest struct {
	RedirectURIs []string `json:"redirect_uris"`
	ClientName   string   `json:"client_name,omitempty"`
}

type registerResponse struct {
	ClientID                string   `json:"client_id"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	RedirectURIs            []string `json:"redirect_uris"`
	ClientName              string   `json:"client_name,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
}

func (h *Handler) handleRegister(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRegisterBodyBytes)

	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "request body must be valid JSON")
		return
	}

	if len(req.RedirectURIs) == 0 {
		writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uris must name at least one URI")
		return
	}
	for _, u := range req.RedirectURIs {
		if err := validateRedirectURIScheme(u); err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", err.Error())
			return
		}
	}

	now := time.Now()
	meta := ClientMetadata{
		RedirectURIs: req.RedirectURIs,
		ClientName:   req.ClientName,
		CreatedAt:    now.Unix(),
	}

	clientID, err := EncodeClientID(h.cfg.Keys, meta)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "could not register client")
		return
	}

	grantTypes := []string{"authorization_code"}
	if h.cfg.RefreshTokenTTL > 0 {
		grantTypes = append(grantTypes, "refresh_token")
	}

	writeJSON(w, http.StatusCreated, registerResponse{
		ClientID:                clientID,
		ClientIDIssuedAt:        now.Unix(),
		RedirectURIs:            meta.RedirectURIs,
		ClientName:              meta.ClientName,
		TokenEndpointAuthMethod: "none",
		GrantTypes:              grantTypes,
		ResponseTypes:           []string{"code"},
	})
}

// validateRedirectURIScheme rejects anything but an https redirect_uri, or
// http restricted to a loopback address (the OAuth 2.1 / RFC 8252
// allowance for a native or CLI client that runs its own local callback
// listener). Redirect_uri is rendered back to the resource owner on the
// consent screen and, on success, is where their authorization code is
// delivered -- accepting an arbitrary scheme here (javascript:, data:, or
// simply an unencrypted remote http endpoint) would widen that into an
// open-redirect and credential-phishing surface no exact-match check can
// close, since the match would just be matching against the attacker's own
// registered value.
func validateRedirectURIScheme(raw string) error {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || !parsed.IsAbs() {
		return fmt.Errorf("redirect_uris must be absolute URIs, got: %s", raw)
	}

	switch parsed.Scheme {
	case "https":
		return nil
	case "http":
		switch parsed.Hostname() {
		case "127.0.0.1", "::1", "localhost":
			return nil
		}
		return fmt.Errorf("http redirect_uri is only allowed for a loopback address (127.0.0.1, ::1, localhost), got: %s", raw)
	default:
		return fmt.Errorf("redirect_uri scheme must be https (or http for a loopback address), got: %s", raw)
	}
}
