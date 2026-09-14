package oauth

import (
	"strings"
	"time"
)

const (
	accessTokenPurpose  = "tnmcp_at_v1"
	refreshTokenPurpose = "tnmcp_rt_v1"
)

// AccessTokenPrefix and RefreshTokenPrefix distinguish this server's own
// OAuth tokens from a raw TrueNAS API key on the same bearer-credential
// path -- see CredentialFromRequest in internal/server/session.go.
const (
	AccessTokenPrefix  = "tnmcp_at_"
	RefreshTokenPrefix = "tnmcp_rt_"
)

// TokenPayload is what an access or refresh token is sealed around.
type TokenPayload struct {
	Credential Credential `json:"credential"`
	ExpiresAt  int64      `json:"expires_at"`
}

func encodeToken(keys Keys, purpose, prefix string, cred Credential, ttl time.Duration, now time.Time) (string, error) {
	token, err := seal(keys, purpose, TokenPayload{
		Credential: cred,
		ExpiresAt:  now.Add(ttl).Unix(),
	})
	if err != nil {
		return "", err
	}
	return prefix + token, nil
}

func decodeToken(keys Keys, purpose, prefix, token string, now time.Time) (TokenPayload, error) {
	var payload TokenPayload

	body, ok := strings.CutPrefix(token, prefix)
	if !ok {
		return payload, ErrInvalidToken
	}
	if err := open(keys, purpose, body, &payload); err != nil {
		return payload, err
	}
	if now.Unix() >= payload.ExpiresAt {
		return payload, ErrInvalidToken
	}
	return payload, nil
}

// EncodeAccessToken seals cred into an access token valid for ttl.
func EncodeAccessToken(keys Keys, cred Credential, ttl time.Duration, now time.Time) (string, error) {
	return encodeToken(keys, accessTokenPurpose, AccessTokenPrefix, cred, ttl, now)
}

// DecodeAccessToken reverses EncodeAccessToken and rejects a token past its
// expiry.
func DecodeAccessToken(keys Keys, token string, now time.Time) (TokenPayload, error) {
	return decodeToken(keys, accessTokenPurpose, AccessTokenPrefix, token, now)
}

// EncodeRefreshToken seals cred into a refresh token valid for ttl.
func EncodeRefreshToken(keys Keys, cred Credential, ttl time.Duration, now time.Time) (string, error) {
	return encodeToken(keys, refreshTokenPurpose, RefreshTokenPrefix, cred, ttl, now)
}

// DecodeRefreshToken reverses EncodeRefreshToken and rejects a token past
// its expiry.
func DecodeRefreshToken(keys Keys, token string, now time.Time) (TokenPayload, error) {
	return decodeToken(keys, refreshTokenPurpose, RefreshTokenPrefix, token, now)
}

// IsAccessToken reports whether token has the shape of a value this package
// issues as an access token (regardless of whether it is still valid),
// distinguishing it from a raw TrueNAS API key on the same bearer header.
func IsAccessToken(token string) bool {
	return strings.HasPrefix(token, AccessTokenPrefix)
}
