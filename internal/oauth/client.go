package oauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ClientIDPrefix distinguishes a client_id this server issued from anything
// else a caller might send in that field.
const ClientIDPrefix = "tnmcp_client_"

// ClientMetadata is what Dynamic Client Registration records about a client.
// It is not a secret -- redirect URIs and a display name are not sensitive --
// so it only needs to be tamper-evident, not confidential.
type ClientMetadata struct {
	RedirectURIs []string `json:"redirect_uris"`
	ClientName   string   `json:"client_name,omitempty"`
	CreatedAt    int64    `json:"created_at"`
}

// EncodeClientID signs meta into a self-contained client_id. Registration
// has nothing else to persist: the client_id itself is the only record of
// what was registered, verified again on every authorization request.
func EncodeClientID(keys Keys, meta ClientMetadata) (string, error) {
	payload, err := json.Marshal(meta)
	if err != nil {
		return "", fmt.Errorf("marshaling client metadata: %w", err)
	}

	sig := hmac.New(sha256.New, keys.hmac[:])
	sig.Write(payload)

	return ClientIDPrefix +
		base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(sig.Sum(nil)), nil
}

// DecodeClientID verifies and decodes a client_id previously issued by
// EncodeClientID.
func DecodeClientID(keys Keys, clientID string) (ClientMetadata, error) {
	var meta ClientMetadata

	body, ok := strings.CutPrefix(clientID, ClientIDPrefix)
	if !ok {
		return meta, ErrInvalidToken
	}

	payloadPart, sigPart, ok := strings.Cut(body, ".")
	if !ok {
		return meta, ErrInvalidToken
	}

	payload, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		return meta, ErrInvalidToken
	}
	wantSig, err := base64.RawURLEncoding.DecodeString(sigPart)
	if err != nil {
		return meta, ErrInvalidToken
	}

	mac := hmac.New(sha256.New, keys.hmac[:])
	mac.Write(payload)
	if subtle.ConstantTimeCompare(mac.Sum(nil), wantSig) != 1 {
		return meta, ErrInvalidToken
	}

	if err := json.Unmarshal(payload, &meta); err != nil {
		return meta, ErrInvalidToken
	}
	return meta, nil
}

// ErrRedirectURINotRegistered means a redirect_uri did not exactly match one
// supplied at registration. OAuth 2.1 requires an exact match -- no prefix or
// wildcard matching -- to prevent an authorization code or token being
// redirected somewhere the client never registered.
var ErrRedirectURINotRegistered = errors.New("redirect_uri is not registered for this client")

// ValidateRedirectURI checks redirectURI against meta's registered set.
func (meta ClientMetadata) ValidateRedirectURI(redirectURI string) error {
	for _, u := range meta.RedirectURIs {
		if u == redirectURI {
			return nil
		}
	}
	return ErrRedirectURINotRegistered
}
