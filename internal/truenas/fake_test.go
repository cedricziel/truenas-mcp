package truenas

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
)

// fakeMiddleware is a stand-in for the TrueNAS middleware's JSON-RPC endpoint.
// It exists so the connection layer can be tested without a live appliance;
// behaviour verified here is re-verified against a real box in the integration
// suite.
type fakeMiddleware struct {
	srv *httptest.Server

	mu sync.Mutex
	// handlers maps a method name to a response function. Returning a non-nil
	// error makes the fake reply with a JSON-RPC error object.
	handlers map[string]func(params json.RawMessage) (any, *rpcError)
	// dropAfter closes the connection abruptly after this many requests,
	// simulating a mid-flight interruption. Zero disables it.
	dropAfter int
	requests  []rpcRequest
	conn      *websocket.Conn
	writeMu   sync.Mutex
}

func newFakeMiddleware(t *testing.T) *fakeMiddleware {
	t.Helper()

	f := &fakeMiddleware{handlers: map[string]func(json.RawMessage) (any, *rpcError){}}

	upgrader := websocket.Upgrader{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		f.mu.Lock()
		f.conn = conn
		f.mu.Unlock()
		defer func() { _ = conn.Close() }()

		for n := 1; ; n++ {
			var req rpcRequest
			if err := conn.ReadJSON(&req); err != nil {
				return
			}

			f.mu.Lock()
			h, ok := f.handlers[req.Method]
			drop := f.dropAfter > 0 && n >= f.dropAfter
			f.requests = append(f.requests, req)
			f.mu.Unlock()

			if drop {
				return // close without replying
			}

			resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
			switch {
			case !ok:
				resp.Error = &rpcError{Code: -32601, Message: "method not found: " + req.Method}
			default:
				result, rerr := h(req.Params)
				if rerr != nil {
					resp.Error = rerr
				} else {
					b, _ := json.Marshal(result)
					resp.Result = b
				}
			}
			f.writeMu.Lock()
			err := conn.WriteJSON(resp)
			f.writeMu.Unlock()
			if err != nil {
				return
			}
		}
	}))
	t.Cleanup(f.srv.Close)

	// Sensible default: a login that accepts one known key.
	f.handle("auth.login_with_api_key", func(params json.RawMessage) (any, *rpcError) {
		var args []string
		_ = json.Unmarshal(params, &args)
		if len(args) == 1 && args[0] == validAPIKey {
			return true, nil
		}
		return false, nil
	})

	return f
}

const validAPIKey = "1-validkey"

func (f *fakeMiddleware) handle(method string, h func(json.RawMessage) (any, *rpcError)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[method] = h
}

func (f *fakeMiddleware) setDropAfter(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dropAfter = n
}

// emit sends an event for the given collection, wrapped the way the real
// middleware wraps it: every event-source notification arrives as a
// "collection_update" notification with the actual collection name nested in
// params.collection and the payload in params.fields, never as a top-level
// method matching the collection name itself.
func (f *fakeMiddleware) emit(collection string, fields any) error {
	f.mu.Lock()
	conn := f.conn
	f.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("no client connection")
	}

	f.writeMu.Lock()
	defer f.writeMu.Unlock()
	return conn.WriteJSON(map[string]any{
		"jsonrpc": "2.0",
		"method":  "collection_update",
		"params": map[string]any{
			"collection": collection,
			"fields":     fields,
		},
	})
}

func (f *fakeMiddleware) requestCount(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()

	count := 0
	for _, req := range f.requests {
		if req.Method == method {
			count++
		}
	}
	return count
}

// URL is the ws:// endpoint clients should dial.
func (f *fakeMiddleware) URL() string {
	return strings.Replace(f.srv.URL, "http://", "ws://", 1)
}
