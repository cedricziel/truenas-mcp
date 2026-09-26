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
	handlers  map[string]func(params json.RawMessage) any
	failures  map[string]fakeFailure
	calls     []capturedCall
	download  fakeDownload
}

// fakeDownload is what the fake serves on the one URL its core.download
// hands out.
type fakeDownload struct {
	status int
	body   string
}

const fakeDownloadPath = "/_download/9"

// serveDownload makes core.download succeed and the URL it returns answer
// with status and body, the way the middleware streams a job's output.
func (f *fakeTarget) serveDownload(status int, body string) {
	f.mu.Lock()
	f.download = fakeDownload{status: status, body: body}
	f.mu.Unlock()
	f.respond("core.download", []any{9, fakeDownloadPath + "?auth_token=one-time"})
}

// fakeFailure is a JSON-RPC error the fake target returns for a method, so a
// test can exercise how the server reports a refusal the target itself made.
type fakeFailure struct {
	errname string
	reason  string
}

// capturedCall is one request the fake target received, kept so a test can
// assert what the server actually sent -- a filter's field, operator, and
// value -- rather than only what came back.
type capturedCall struct {
	method string
	params json.RawMessage
}

const fakeTargetAPIKey = "1-validkey"

func newFakeTarget(t *testing.T) *fakeTarget {
	t.Helper()

	f := &fakeTarget{}
	upgrader := websocket.Upgrader{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == fakeDownloadPath {
			f.mu.Lock()
			d := f.download
			f.mu.Unlock()
			w.WriteHeader(d.status)
			_, _ = w.Write([]byte(d.body))
			return
		}

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
			f.recordCall(req.Method, req.Params)

			resp := struct {
				JSONRPC string          `json:"jsonrpc"`
				ID      uint64          `json:"id"`
				Result  json.RawMessage `json:"result,omitempty"`
				Error   *struct {
					Code    int            `json:"code"`
					Message string         `json:"message"`
					Data    map[string]any `json:"data"`
				} `json:"error,omitempty"`
			}{JSONRPC: "2.0", ID: req.ID}

			if failure, ok := f.failure(req.Method); ok {
				resp.Error = &struct {
					Code    int            `json:"code"`
					Message string         `json:"message"`
					Data    map[string]any `json:"data"`
				}{
					Code:    -32001,
					Message: "Method call error",
					Data:    map[string]any{"errname": failure.errname, "reason": failure.reason},
				}
			} else if handler, ok := f.handler(req.Method); ok {
				resp.Result, _ = json.Marshal(handler(req.Params))
			} else if custom, ok := f.customResponse(req.Method); ok {
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

// respondFunc registers a handler computing the result from the call's own
// params, for tests that must prove *what* was sent rather than just that
// something was -- a category filter narrowing the response the fake target
// hands back, say, rather than always the same canned list regardless of
// what the server asked for.
func (f *fakeTarget) respondFunc(method string, fn func(params json.RawMessage) any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.handlers == nil {
		f.handlers = map[string]func(params json.RawMessage) any{}
	}
	f.handlers[method] = fn
}

// fail makes the target answer method with a failed-call error, the way the
// middleware does: errname EACCES is a call the caller's key is not
// privileged for; see internal/truenas/errors.go.
func (f *fakeTarget) fail(method, errname, reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failures == nil {
		f.failures = map[string]fakeFailure{}
	}
	f.failures[method] = fakeFailure{errname: errname, reason: reason}
}

func (f *fakeTarget) failure(method string) (fakeFailure, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	failure, ok := f.failures[method]
	return failure, ok
}

func (f *fakeTarget) handler(method string) (func(params json.RawMessage) any, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn, ok := f.handlers[method]
	return fn, ok
}

func (f *fakeTarget) recordCall(method string, params json.RawMessage) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, capturedCall{method: method, params: params})
}

// lastParams returns the params of the most recent call to method, so a test
// can inspect exactly what filter triple the server sent.
func (f *fakeTarget) lastParams(method string) (json.RawMessage, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.calls) - 1; i >= 0; i-- {
		if f.calls[i].method == method {
			return f.calls[i].params, true
		}
	}
	return nil, false
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
