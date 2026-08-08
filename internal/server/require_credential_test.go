package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireCredentialRejectsUnauthenticated(t *testing.T) {
	reached := false
	h := RequireCredential(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		reached = true
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if reached {
		t.Fatal("an unauthenticated request must not reach the MCP handler")
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Error("a 401 should tell the client how to authenticate")
	}
}

func TestRequireCredentialAllowsAuthenticated(t *testing.T) {
	reached := false
	h := RequireCredential(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer 1-key")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !reached {
		t.Fatal("an authenticated request must reach the MCP handler")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
