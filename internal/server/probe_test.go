package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The published image is distroless: no shell, no curl. A container health
// check therefore has to be the binary probing itself.
func TestProbeSucceedsAgainstHealthyServer(t *testing.T) {
	h := NewHealth("test")
	h.SetServing(true)
	srv := httptest.NewServer(h)
	defer srv.Close()

	if err := Probe(srv.URL); err != nil {
		t.Fatalf("probe against a healthy server should succeed, got: %v", err)
	}
}

func TestProbeFailsAgainstUnhealthyServer(t *testing.T) {
	h := NewHealth("test")
	h.SetUnhealthy("target unreachable")
	srv := httptest.NewServer(h)
	defer srv.Close()

	err := Probe(srv.URL)
	if err == nil {
		t.Fatal("probe against an unhealthy server should fail")
	}
}

func TestProbeFailsWhenNothingIsListening(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // nothing listening now

	if err := Probe(url); err == nil {
		t.Fatal("probe should fail when the server is not reachable")
	}
}

// The probe has to reach the port the server actually binds, including when
// the bind address omits a host.
func TestProbeURLFromListenAddr(t *testing.T) {
	for _, tc := range []struct{ listen, want string }{
		{":8080", "http://127.0.0.1:8080/healthz"},
		{"0.0.0.0:9000", "http://127.0.0.1:9000/healthz"},
		{"127.0.0.1:1234", "http://127.0.0.1:1234/healthz"},
	} {
		if got := ProbeURL(tc.listen); got != tc.want {
			t.Errorf("ProbeURL(%q) = %q, want %q", tc.listen, got, tc.want)
		}
	}
}
