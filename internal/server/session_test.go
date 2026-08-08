package server

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

// Callers supply their own TrueNAS API key. The server holds none, so what a
// session can do is bounded by that user's own privileges rather than by a
// shared server-wide credential. See design D11b.
func TestCredentialFromAuthorizationBearer(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "/mcp", nil)
	r.Header.Set("Authorization", "Bearer 1-somekey")

	key, err := CredentialFromRequest(r)
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

	key, err := CredentialFromRequest(r)
	if err != nil {
		t.Fatalf("dedicated header should be accepted: %v", err)
	}
	if key != "1-otherkey" {
		t.Fatalf("key = %q", key)
	}
}

func TestCredentialMissing(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "/mcp", nil)

	_, err := CredentialFromRequest(r)
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

	if _, err := CredentialFromRequest(r); !errors.Is(err, ErrNoCredential) {
		t.Fatalf("a non-bearer scheme must not be treated as a credential, got %v", err)
	}
}

func TestCredentialRejectsEmptyBearer(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "/mcp", nil)
	r.Header.Set("Authorization", "Bearer    ")

	if _, err := CredentialFromRequest(r); !errors.Is(err, ErrNoCredential) {
		t.Fatalf("an empty bearer value must not count as a credential, got %v", err)
	}
}

// A misconfigured client sends its key somewhere visible; the error must not
// echo it back into logs.
func TestCredentialErrorNeverEchoesTheKey(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "/mcp", nil)
	r.Header.Set("Authorization", "Basic 1-secretkeyvalue")

	_, err := CredentialFromRequest(r)
	if err != nil && strings.Contains(err.Error(), "1-secretkeyvalue") {
		t.Fatalf("error disclosed the credential: %v", err)
	}
}
