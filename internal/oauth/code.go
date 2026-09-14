package oauth

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

const codePurpose = "tnmcp_code_v1"

// CodePrefix distinguishes an authorization code this server issued from
// anything else a caller might send to the token endpoint.
const CodePrefix = "tnmcp_code_"

// CodeTTL is how long an authorization code is valid for. Short by design:
// it exists only for the brief window between the resource owner's consent
// and the client's token exchange, and single-use enforcement (ReplayCache)
// only needs to cover that same window.
const CodeTTL = 60 * time.Second

// CodePayload is what an authorization code is sealed around: the resource
// owner's credential, which client and redirect it was issued to, and the
// PKCE challenge the eventual token exchange must satisfy.
type CodePayload struct {
	Credential    Credential `json:"credential"`
	ClientID      string     `json:"client_id"`
	RedirectURI   string     `json:"redirect_uri"`
	CodeChallenge string     `json:"code_challenge"`
	ExpiresAt     int64      `json:"expires_at"`
}

// EncodeCode seals payload into an authorization code, stamping ExpiresAt
// CodeTTL past now.
func EncodeCode(keys Keys, payload CodePayload, now time.Time) (string, error) {
	payload.ExpiresAt = now.Add(CodeTTL).Unix()
	token, err := seal(keys, codePurpose, payload)
	if err != nil {
		return "", err
	}
	return CodePrefix + token, nil
}

// DecodeCode reverses EncodeCode and rejects a code past its expiry.
func DecodeCode(keys Keys, code string, now time.Time) (CodePayload, error) {
	var payload CodePayload

	body, ok := strings.CutPrefix(code, CodePrefix)
	if !ok {
		return payload, ErrInvalidToken
	}
	if err := open(keys, codePurpose, body, &payload); err != nil {
		return payload, err
	}
	if now.Unix() >= payload.ExpiresAt {
		return payload, ErrInvalidToken
	}
	return payload, nil
}

// CodeReplayID returns the identifier a ReplayCache should track for code,
// so a code redeems exactly once regardless of what it decodes to.
func CodeReplayID(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}
