package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gorilla/websocket"
)

// fakeTarget is a minimal stand-in for the TrueNAS middleware: just enough
// for SessionManager.Open to succeed, a login that accepts one key and a
// version response that clears CheckVersion's floor.
//
// It exists here rather than reusing internal/truenas/fake_test.go's fuller
// fake because Go does not let one package's test files import another's.
// NewSessionProvider is exercised at this package's level, against a real
// *SessionManager and a real *truenas.Client, so it needs something to
// actually dial.
type fakeTarget struct {
	srv   *httptest.Server
	conns atomic.Int32

	mu        sync.Mutex
	responses map[string]json.RawMessage
}

const fakeTargetAPIKey = "1-validkey"

func newFakeTarget(t *testing.T) *fakeTarget {
	t.Helper()

	f := &fakeTarget{}
	upgrader := websocket.Upgrader{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		f.conns.Add(1)
		defer func() { _ = conn.Close() }()

		for {
			var req struct {
				ID     uint64          `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if err := conn.ReadJSON(&req); err != nil {
				return
			}

			resp := struct {
				JSONRPC string          `json:"jsonrpc"`
				ID      uint64          `json:"id"`
				Result  json.RawMessage `json:"result,omitempty"`
			}{JSONRPC: "2.0", ID: req.ID}

			if custom, ok := f.customResponse(req.Method); ok {
				resp.Result = custom
			} else {
				switch req.Method {
				case "auth.login_with_api_key":
					var args []string
					_ = json.Unmarshal(req.Params, &args)
					ok := len(args) == 1 && args[0] == fakeTargetAPIKey
					resp.Result, _ = json.Marshal(ok)
				case "system.version":
					resp.Result, _ = json.Marshal("TrueNAS-25.04.0")
				}
			}
			if err := conn.WriteJSON(resp); err != nil {
				return
			}
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// respond registers a canned result for a method beyond the built-in
// auth.login_with_api_key and system.version handling every session needs.
// Tests that exercise more of the middleware surface -- core.get_methods, a
// job-starting call -- add just the method they need rather than growing
// their own websocket fake.
func (f *fakeTarget) respond(method string, result any) {
	raw, err := json.Marshal(result)
	if err != nil {
		panic(err) // test setup only
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.responses == nil {
		f.responses = map[string]json.RawMessage{}
	}
	f.responses[method] = raw
}

func (f *fakeTarget) customResponse(method string) (json.RawMessage, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, ok := f.responses[method]
	return raw, ok
}

// URL is the ws:// endpoint SessionManager should dial.
func (f *fakeTarget) URL() string {
	return strings.Replace(f.srv.URL, "http://", "ws://", 1)
}

// connections is how many distinct sockets the target has accepted, so tests
// can assert whether a reconnect happened.
func (f *fakeTarget) connections() int32 {
	return f.conns.Load()
}

// The target rate-limits authentication, so a healthy session must be reused
// rather than reopened on every call -- see the reasoning in
// NewSessionProvider.
func TestSessionProviderOpensLazilyAndReusesWhileAlive(t *testing.T) {
	target := newFakeTarget(t)
	sessions := NewSessionManager(target.URL(), false)
	t.Cleanup(sessions.CloseAll)

	provider := NewSessionProvider(sessions, "test", fakeTargetAPIKey)

	if target.connections() != 0 {
		t.Fatal("NewSessionProvider must not open a connection before first use")
	}

	first, err := provider(context.Background())
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if target.connections() != 1 {
		t.Fatalf("expected exactly one connection after the first call, got %d", target.connections())
	}

	second, err := provider(context.Background())
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if second != first {
		t.Fatal("a healthy session must be reused, not reopened")
	}
	if target.connections() != 1 {
		t.Fatalf("a healthy session must not reconnect, got %d connections", target.connections())
	}
}

// The target restarts, networks blip, and middleware connections are
// long-lived -- the provider must re-establish rather than handing back a
// dead socket.
func TestSessionProviderReconnectsWhenDead(t *testing.T) {
	target := newFakeTarget(t)
	sessions := NewSessionManager(target.URL(), false)
	t.Cleanup(sessions.CloseAll)

	provider := NewSessionProvider(sessions, "test", fakeTargetAPIKey)

	first, err := provider(context.Background())
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Simulate the target going away mid-session: closing the client locally
	// makes Alive() report false immediately, the same state a dropped
	// connection eventually settles into.
	_ = first.Client().Close()

	second, err := provider(context.Background())
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if second == first {
		t.Fatal("a dead session must be replaced, not reused")
	}
	if !second.Client().Alive() {
		t.Fatal("the replacement session must be alive")
	}
	if target.connections() != 2 {
		t.Fatalf("expected a second connection after the first died, got %d", target.connections())
	}
}
