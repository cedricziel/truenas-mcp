package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthReportsServingWhenReady(t *testing.T) {
	h := NewHealth("test-build")
	h.SetServing(true)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("health body must be JSON: %v", err)
	}
	if body.Status != "healthy" {
		t.Errorf("status = %q, want %q", body.Status, "healthy")
	}
	if body.Version != "test-build" {
		t.Errorf("version = %q, want the build identifier", body.Version)
	}
}

// The apps platform reads this to decide whether the app is up, so a server
// that cannot serve must not report healthy.
func TestHealthReportsUnhealthyBeforeServing(t *testing.T) {
	h := NewHealth("test-build")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	var body struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("health body must be JSON: %v", err)
	}
	if body.Status == "healthy" {
		t.Error("must not report healthy before serving")
	}
	if body.Reason == "" {
		t.Error("an unhealthy response must carry a reason")
	}
}

func TestHealthReasonIsReportable(t *testing.T) {
	h := NewHealth("test-build")
	h.SetServing(true)
	h.SetUnhealthy("target unreachable")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Reason != "target unreachable" {
		t.Errorf("reason = %q, want the reason that was set", body.Reason)
	}
}
