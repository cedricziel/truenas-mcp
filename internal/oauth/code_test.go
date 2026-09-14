package oauth

import (
	"testing"
	"time"
)

func TestCodeRoundTrip(t *testing.T) {
	keys := testKeys(t)
	now := time.Unix(1700000000, 0)

	want := CodePayload{
		Credential:    Credential{Username: "admin", APIKey: "1-secret"},
		ClientID:      "tnmcp_client_abc",
		RedirectURI:   "https://claude.ai/api/mcp/callback",
		CodeChallenge: "challenge",
	}

	code, err := EncodeCode(keys, want, now)
	if err != nil {
		t.Fatalf("EncodeCode: %v", err)
	}

	got, err := DecodeCode(keys, code, now)
	if err != nil {
		t.Fatalf("DecodeCode: %v", err)
	}
	if got.Credential != want.Credential || got.ClientID != want.ClientID ||
		got.RedirectURI != want.RedirectURI || got.CodeChallenge != want.CodeChallenge {
		t.Fatalf("got %+v, want fields from %+v", got, want)
	}
}

func TestCodeExpires(t *testing.T) {
	keys := testKeys(t)
	issuedAt := time.Unix(1700000000, 0)

	code, err := EncodeCode(keys, CodePayload{Credential: Credential{APIKey: "1-secret"}}, issuedAt)
	if err != nil {
		t.Fatalf("EncodeCode: %v", err)
	}

	if _, err := DecodeCode(keys, code, issuedAt.Add(CodeTTL+time.Second)); err != ErrInvalidToken {
		t.Fatalf("DecodeCode(expired) = %v, want ErrInvalidToken", err)
	}
	if _, err := DecodeCode(keys, code, issuedAt.Add(CodeTTL-time.Second)); err != nil {
		t.Fatalf("DecodeCode(not yet expired) = %v, want nil", err)
	}
}

func TestDecodeCodeRejectsForeignValue(t *testing.T) {
	keys := testKeys(t)
	if _, err := DecodeCode(keys, "not-a-code", time.Now()); err != ErrInvalidToken {
		t.Fatalf("DecodeCode(garbage) = %v, want ErrInvalidToken", err)
	}
}
