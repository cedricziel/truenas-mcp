package server

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cedricziel/truenas-mcp/internal/oauth"
)

// Callers supply their own TrueNAS API key. The server holds none, so what a
// session can do is bounded by that user's own privileges rather than by a
// shared server-wide credential. See design D11b.
func TestCredentialFromAuthorizationBearer(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "/mcp", nil)
	r.Header.Set("Authorization", "Bearer 1-somekey")

	key, err := CredentialFromRequest(r, nil)
	if err != nil {
		t.Fatalf("bearer credential should be accepted: %v", err)
	}
	if key != "1-somekey" {
		t.Fatalf("key = %q", key)
	}
}

// Not every MCP client can set an Authorization header, so a dedicated header
// is accepted too.
func TestCredentialFromDedicatedHeader(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "/mcp", nil)
	r.Header.Set("X-TrueNAS-API-Key", "1-otherkey")

	key, err := CredentialFromRequest(r, nil)
	if err != nil {
		t.Fatalf("dedicated header should be accepted: %v", err)
	}
	if key != "1-otherkey" {
		t.Fatalf("key = %q", key)
	}
}

func TestCredentialMissing(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "/mcp", nil)

	_, err := CredentialFromRequest(r, nil)
	if !errors.Is(err, ErrNoCredential) {
		t.Fatalf("want ErrNoCredential, got %v", err)
	}
	// The error is what a user sees when their client is misconfigured, so it
	// has to say what to supply.
	if !strings.Contains(err.Error(), "API key") {
		t.Errorf("error should explain what is missing, got: %v", err)
	}
}

func TestCredentialRejectsNonBearerScheme(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "/mcp", nil)
	r.Header.Set("Authorization", "Basic dXNlcjpwYXNz")

	if _, err := CredentialFromRequest(r, nil); !errors.Is(err, ErrNoCredential) {
		t.Fatalf("a non-bearer scheme must not be treated as a credential, got %v", err)
	}
}

func TestCredentialRejectsEmptyBearer(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "/mcp", nil)
	r.Header.Set("Authorization", "Bearer    ")

	if _, err := CredentialFromRequest(r, nil); !errors.Is(err, ErrNoCredential) {
		t.Fatalf("an empty bearer value must not count as a credential, got %v", err)
	}
}

// A misconfigured client sends its key somewhere visible; the error must not
// echo it back into logs.
func TestCredentialErrorNeverEchoesTheKey(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "/mcp", nil)
	r.Header.Set("Authorization", "Basic 1-secretkeyvalue")

	_, err := CredentialFromRequest(r, nil)
	if err != nil && strings.Contains(err.Error(), "1-secretkeyvalue") {
		t.Fatalf("error disclosed the credential: %v", err)
	}
}

// An OAuth access token authenticates exactly like the raw TrueNAS API key
// it was bound to at authorization time -- see the mcp-transport spec's
// "Valid OAuth access token" scenario.
func TestCredentialFromOAuthAccessToken(t *testing.T) {
	keys := oauth.DeriveKeys(oauth.GenerateMasterKey())
	token, err := oauth.EncodeAccessToken(keys, oauth.Credential{APIKey: "1-oauthbackedkey"}, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("EncodeAccessToken: %v", err)
	}

	r, _ := http.NewRequest(http.MethodPost, "/mcp", nil)
	r.Header.Set("Authorization", "Bearer "+token)

	key, err := CredentialFromRequest(r, &keys)
	if err != nil {
		t.Fatalf("valid access token should be accepted: %v", err)
	}
	if key != "1-oauthbackedkey" {
		t.Fatalf("key = %q, want the underlying TrueNAS API key", key)
	}
}

// Without OAuth enabled (oauthKeys nil), a token that merely looks like an
// access token is still treated as a raw API key -- exactly the behavior
// before this capability existed.
func TestCredentialFromOAuthAccessTokenIgnoredWhenOAuthDisabled(t *testing.T) {
	keys := oauth.DeriveKeys(oauth.GenerateMasterKey())
	token, err := oauth.EncodeAccessToken(keys, oauth.Credential{APIKey: "1-oauthbackedkey"}, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("EncodeAccessToken: %v", err)
	}

	r, _ := http.NewRequest(http.MethodPost, "/mcp", nil)
	r.Header.Set("Authorization", "Bearer "+token)

	key, err := CredentialFromRequest(r, nil)
	if err != nil {
		t.Fatalf("should fall back to treating it as a raw key: %v", err)
	}
	if key != token {
		t.Fatalf("key = %q, want the raw bearer value unchanged", key)
	}
}

func TestCredentialFromExpiredOAuthAccessToken(t *testing.T) {
	keys := oauth.DeriveKeys(oauth.GenerateMasterKey())
	token, err := oauth.EncodeAccessToken(keys, oauth.Credential{APIKey: "1-oauthbackedkey"}, time.Hour, time.Now().Add(-2*time.Hour))
	if err != nil {
		t.Fatalf("EncodeAccessToken: %v", err)
	}

	r, _ := http.NewRequest(http.MethodPost, "/mcp", nil)
	r.Header.Set("Authorization", "Bearer "+token)

	if _, err := CredentialFromRequest(r, &keys); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("want ErrInvalidCredential, got %v", err)
	}
}

func TestCredentialFromTamperedOAuthAccessToken(t *testing.T) {
	keys := oauth.DeriveKeys(oauth.GenerateMasterKey())
	token, err := oauth.EncodeAccessToken(keys, oauth.Credential{APIKey: "1-oauthbackedkey"}, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("EncodeAccessToken: %v", err)
	}

	r, _ := http.NewRequest(http.MethodPost, "/mcp", nil)
	r.Header.Set("Authorization", "Bearer "+token+"tampered")

	if _, err := CredentialFromRequest(r, &keys); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("want ErrInvalidCredential, got %v", err)
	}
}
