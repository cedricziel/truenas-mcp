package oauth

import (
	"testing"
	"time"
)

func TestAccessTokenRoundTrip(t *testing.T) {
	keys := testKeys(t)
	now := time.Unix(1700000000, 0)
	cred := Credential{Username: "admin", APIKey: "1-secret"}

	token, err := EncodeAccessToken(keys, cred, time.Hour, now)
	if err != nil {
		t.Fatalf("EncodeAccessToken: %v", err)
	}
	if !IsAccessToken(token) {
		t.Fatal("IsAccessToken should recognize a token it just issued")
	}

	got, err := DecodeAccessToken(keys, token, now)
	if err != nil {
		t.Fatalf("DecodeAccessToken: %v", err)
	}
	if got.Credential != cred {
		t.Fatalf("Credential = %+v, want %+v", got.Credential, cred)
	}
}

func TestAccessTokenExpires(t *testing.T) {
	keys := testKeys(t)
	now := time.Unix(1700000000, 0)

	token, err := EncodeAccessToken(keys, Credential{APIKey: "1-secret"}, time.Hour, now)
	if err != nil {
		t.Fatalf("EncodeAccessToken: %v", err)
	}

	if _, err := DecodeAccessToken(keys, token, now.Add(time.Hour+time.Second)); err != ErrInvalidToken {
		t.Fatalf("DecodeAccessToken(expired) = %v, want ErrInvalidToken", err)
	}
}

func TestRefreshTokenRoundTrip(t *testing.T) {
	keys := testKeys(t)
	now := time.Unix(1700000000, 0)
	cred := Credential{APIKey: "1-secret"}

	token, err := EncodeRefreshToken(keys, cred, 30*24*time.Hour, now)
	if err != nil {
		t.Fatalf("EncodeRefreshToken: %v", err)
	}

	got, err := DecodeRefreshToken(keys, token, now)
	if err != nil {
		t.Fatalf("DecodeRefreshToken: %v", err)
	}
	if got.Credential != cred {
		t.Fatalf("Credential = %+v, want %+v", got.Credential, cred)
	}
}

func TestRefreshTokenExpires(t *testing.T) {
	keys := testKeys(t)
	now := time.Unix(1700000000, 0)

	token, err := EncodeRefreshToken(keys, Credential{APIKey: "1-secret"}, time.Hour, now)
	if err != nil {
		t.Fatalf("EncodeRefreshToken: %v", err)
	}

	if _, err := DecodeRefreshToken(keys, token, now.Add(time.Hour+time.Second)); err != ErrInvalidToken {
		t.Fatalf("DecodeRefreshToken(expired) = %v, want ErrInvalidToken", err)
	}
}

func TestAccessAndRefreshTokensAreNotInterchangeable(t *testing.T) {
	keys := testKeys(t)
	now := time.Unix(1700000000, 0)
	cred := Credential{APIKey: "1-secret"}

	access, err := EncodeAccessToken(keys, cred, time.Hour, now)
	if err != nil {
		t.Fatalf("EncodeAccessToken: %v", err)
	}
	refresh, err := EncodeRefreshToken(keys, cred, time.Hour, now)
	if err != nil {
		t.Fatalf("EncodeRefreshToken: %v", err)
	}

	// The prefixes alone would make an access token look like a refresh
	// token to DecodeRefreshToken's CutPrefix check; the AEAD purpose
	// binding is what actually rejects the swap.
	forged := RefreshTokenPrefix + access[len(AccessTokenPrefix):]
	if _, err := DecodeRefreshToken(keys, forged, now); err != ErrInvalidToken {
		t.Fatalf("DecodeRefreshToken(access token body) = %v, want ErrInvalidToken", err)
	}

	forgedAccess := AccessTokenPrefix + refresh[len(RefreshTokenPrefix):]
	if _, err := DecodeAccessToken(keys, forgedAccess, now); err != ErrInvalidToken {
		t.Fatalf("DecodeAccessToken(refresh token body) = %v, want ErrInvalidToken", err)
	}
}

func TestIsAccessTokenDoesNotMatchRawKeyOrRefreshToken(t *testing.T) {
	if IsAccessToken("1-someRawTrueNASApiKey") {
		t.Fatal("a raw TrueNAS API key must not look like an access token")
	}
	if IsAccessToken(RefreshTokenPrefix + "abc") {
		t.Fatal("a refresh token must not look like an access token")
	}
}
