// Package oauth implements the OAuth 2.1 authorization server role the HTTP
// transport plays so clients that only speak OAuth -- Claude.ai's connector
// flow among them -- can register, obtain a resource owner's consent, and
// authenticate, without TrueNAS itself being an OAuth provider.
//
// Every registered client, authorization code, access token, and refresh
// token is self-contained: a value this package can verify and decrypt using
// one symmetric master key, rather than a row looked up in a database. The
// server holds no persistent store for any of it -- see design.md in
// openspec/changes/oauth-dcr-authorization for the rationale. Losing or
// rotating the master key invalidates everything sealed under it; it never
// affects the TrueNAS credentials those values wrap.
package oauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Keys holds the subkeys derived from the operator's one configured (or
// generated) master key. AEAD sealing and HMAC signing use separate subkeys
// so the two primitives never share key material.
type Keys struct {
	aead [32]byte
	hmac [32]byte
}

// DeriveKeys splits master into independent AEAD and HMAC subkeys.
func DeriveKeys(master [32]byte) Keys {
	return Keys{
		aead: labelKey(master, "truenas-mcp-oauth-aead-v1"),
		hmac: labelKey(master, "truenas-mcp-oauth-hmac-v1"),
	}
}

func labelKey(master [32]byte, label string) [32]byte {
	m := hmac.New(sha256.New, master[:])
	m.Write([]byte(label))
	var out [32]byte
	copy(out[:], m.Sum(nil))
	return out
}

// ParseMasterKey decodes an operator-supplied master key: 64 hex characters
// encoding 32 bytes.
func ParseMasterKey(hexKey string) ([32]byte, error) {
	var key [32]byte
	if len(hexKey) != 64 {
		return key, fmt.Errorf(
			"must be 64 hex characters (32 bytes), for example the output of `openssl rand -hex 32`; got %d characters",
			len(hexKey))
	}
	b, err := hex.DecodeString(hexKey)
	if err != nil {
		return key, fmt.Errorf("must be valid hex: %w", err)
	}
	copy(key[:], b)
	return key, nil
}

// GenerateMasterKey returns a fresh random master key, for use when the
// operator has not configured one. Whatever this key seals becomes
// unverifiable the moment the process holding it exits.
func GenerateMasterKey() [32]byte {
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		// crypto/rand failing means the platform has no secure randomness
		// source; nothing downstream of this key could be trusted, so this
		// must not silently fall through to a predictable key.
		panic("truenas-mcp: crypto/rand unavailable: " + err.Error())
	}
	return key
}

// ResolveMasterKey parses hexKey if the operator set one, otherwise
// generates a fresh key. The second return reports whether a key was
// generated, which callers use to warn that restarting will invalidate
// every outstanding OAuth registration, code, and token.
func ResolveMasterKey(hexKey string) (key [32]byte, generated bool, err error) {
	if hexKey == "" {
		return GenerateMasterKey(), true, nil
	}
	key, err = ParseMasterKey(hexKey)
	return key, false, err
}
