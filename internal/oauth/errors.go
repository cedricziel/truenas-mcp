package oauth

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// oauthError is the RFC 6749 §5.2 / RFC 7591 §3.2.2 error body shape, used
// by every JSON-speaking endpoint (registration, token). The browser-facing
// authorization endpoint uses writeOAuthProblem instead -- see authorize.go.
type oauthError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(oauthError{Error: code, ErrorDescription: description})
}

// writeOAuthProblem reports a request the authorization endpoint refuses
// before it can trust redirect_uri enough to redirect the error there --
// an unrecognized client_id, an unregistered redirect_uri, or a missing
// PKCE challenge. Plain text, since the caller here is a browser navigating
// directly rather than an OAuth client parsing JSON.
func writeOAuthProblem(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintln(w, message)
}
