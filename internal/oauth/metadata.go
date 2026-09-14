package oauth

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Well-known and endpoint paths. Exported so the caller mounting this
// handler (and its tests) can reference them without duplicating strings.
const (
	ProtectedResourceMetadataPath   = "/.well-known/oauth-protected-resource"
	AuthorizationServerMetadataPath = "/.well-known/oauth-authorization-server"
	RegisterPath                    = "/register"
	AuthorizePath                   = "/authorize"
	TokenPath                       = "/token"
)

// protectedResourceMetadata is RFC 9728's document, naming this server's own
// authorization server for the MCP resource.
type protectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
}

// authorizationServerMetadata is RFC 8414's document.
type authorizationServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
}

func (h *Handler) issuerBase() string {
	return strings.TrimSuffix(h.cfg.Issuer, "/")
}

// ResourceURL is the MCP endpoint's resource identifier: what protected
// resource metadata names, and what the MCP transport's own
// WWW-Authenticate challenge points a client at to discover it.
func (h *Handler) ResourceURL() string {
	return h.issuerBase() + "/mcp"
}

// ProtectedResourceMetadataURL is the absolute discovery URL a
// WWW-Authenticate challenge names.
func (h *Handler) ProtectedResourceMetadataURL() string {
	return h.issuerBase() + ProtectedResourceMetadataPath
}

func (h *Handler) handleProtectedResourceMetadata(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, protectedResourceMetadata{
		Resource:             h.ResourceURL(),
		AuthorizationServers: []string{h.issuerBase()},
	})
}

func (h *Handler) handleAuthorizationServerMetadata(w http.ResponseWriter, _ *http.Request) {
	base := h.issuerBase()

	grantTypes := []string{"authorization_code"}
	if h.cfg.RefreshTokenTTL > 0 {
		grantTypes = append(grantTypes, "refresh_token")
	}

	writeJSON(w, http.StatusOK, authorizationServerMetadata{
		Issuer:                            base,
		AuthorizationEndpoint:             base + AuthorizePath,
		TokenEndpoint:                     base + TokenPath,
		RegistrationEndpoint:              base + RegisterPath,
		ResponseTypesSupported:            []string{"code"},
		GrantTypesSupported:               grantTypes,
		CodeChallengeMethodsSupported:     []string{"S256"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
