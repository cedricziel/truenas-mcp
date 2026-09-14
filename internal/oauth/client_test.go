package oauth

import "testing"

func TestClientIDRoundTrip(t *testing.T) {
	keys := testKeys(t)
	want := ClientMetadata{
		RedirectURIs: []string{"https://claude.ai/api/mcp/callback"},
		ClientName:   "Claude",
		CreatedAt:    1700000000,
	}

	id, err := EncodeClientID(keys, want)
	if err != nil {
		t.Fatalf("EncodeClientID: %v", err)
	}
	if got := len(id); got == 0 {
		t.Fatal("EncodeClientID returned an empty id")
	}

	got, err := DecodeClientID(keys, id)
	if err != nil {
		t.Fatalf("DecodeClientID: %v", err)
	}
	if len(got.RedirectURIs) != 1 || got.RedirectURIs[0] != want.RedirectURIs[0] {
		t.Fatalf("RedirectURIs = %v, want %v", got.RedirectURIs, want.RedirectURIs)
	}
	if got.ClientName != want.ClientName || got.CreatedAt != want.CreatedAt {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestDecodeClientIDRejectsTamperedRedirectURIs(t *testing.T) {
	keys := testKeys(t)
	id, err := EncodeClientID(keys, ClientMetadata{
		RedirectURIs: []string{"https://attacker.example/callback"},
	})
	if err != nil {
		t.Fatalf("EncodeClientID: %v", err)
	}

	// Widening the registered redirect URI set without the HMAC noticing
	// would let a client escalate where an authorization code is delivered.
	tampered := id[:len(id)-4] + "AAAA"
	if _, err := DecodeClientID(keys, tampered); err != ErrInvalidToken {
		t.Fatalf("DecodeClientID(tampered) = %v, want ErrInvalidToken", err)
	}
}

func TestDecodeClientIDRejectsForeignID(t *testing.T) {
	keys := testKeys(t)
	if _, err := DecodeClientID(keys, "not-a-client-id"); err != ErrInvalidToken {
		t.Fatalf("DecodeClientID(garbage) = %v, want ErrInvalidToken", err)
	}
}

func TestValidateRedirectURI(t *testing.T) {
	meta := ClientMetadata{RedirectURIs: []string{"https://claude.ai/api/mcp/callback"}}

	if err := meta.ValidateRedirectURI("https://claude.ai/api/mcp/callback"); err != nil {
		t.Fatalf("registered URI should validate, got: %v", err)
	}
	if err := meta.ValidateRedirectURI("https://claude.ai/api/mcp/callback/extra"); err != ErrRedirectURINotRegistered {
		t.Fatalf("unregistered URI should be rejected, got: %v", err)
	}
	if err := meta.ValidateRedirectURI("https://evil.example/callback"); err != ErrRedirectURINotRegistered {
		t.Fatalf("foreign URI should be rejected, got: %v", err)
	}
}
