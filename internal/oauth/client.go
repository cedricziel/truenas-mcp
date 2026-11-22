package oauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ClientIDPrefix distinguishes a client_id this server issued from anything
// else a caller might send in that field.
const ClientIDPrefix = "tnmcp_client_"

// ClientMetadata describes a public OAuth client's display name and registered redirects.
type ClientMetadata struct {
	RedirectURIs      []string `json:"redirect_uris"`
	ClientName        string   `json:"client_name,omitempty"`
	CreatedAt         int64    `json:"created_at"`
	allowLoopbackPort bool
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

// ErrRedirectURINotRegistered means no registered redirect matches. CIMD clients
// may change only the port of an HTTP callback on 127.0.0.1 or [::1].
var ErrRedirectURINotRegistered = errors.New("redirect_uri is not registered for this client")

// ValidateRedirectURI checks registered redirects, allowing CIMD native callback ports.
func (meta ClientMetadata) ValidateRedirectURI(redirectURI string) error {
	for _, u := range meta.RedirectURIs {
		if u == redirectURI {
			return nil
		}
	}
	if meta.allowLoopbackPort {
		requested, ok := loopbackRedirectWithoutPort(redirectURI)
		if ok {
			for _, registered := range meta.RedirectURIs {
				if candidate, ok := loopbackRedirectWithoutPort(registered); ok && candidate == requested {
					return nil
				}
			}
		}
	}
	return ErrRedirectURINotRegistered
}

func loopbackRedirectWithoutPort(raw string) (string, bool) {
	if !strings.HasPrefix(raw, "http://") || validateRedirectURIScheme(raw) != nil {
		return "", false
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	host := parsed.Hostname()
	if host != "127.0.0.1" && host != "::1" {
		return "", false
	}
	if host == "::1" {
		host = "[::1]"
	}
	// Preserve the exact path and query spelling while removing only the native callback port.
	return "http://" + host + strings.TrimPrefix(raw, "http://"+parsed.Host), true
}
