package oauth

import (
	"encoding/json"
	"net/http"
	"net/url"
	"time"
)

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
		parsed, err := url.ParseRequestURI(u)
		if err != nil || !parsed.IsAbs() {
			writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri",
				"redirect_uris must be absolute URIs, got: "+u)
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
