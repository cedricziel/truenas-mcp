package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type logTarget struct {
	srv *httptest.Server

	mu            sync.Mutex
	containers    map[string]any
	events        map[string][]map[string]string
	refusals      map[string]string
	subscriptions map[string]bool
}

func newLogTarget(t *testing.T) *logTarget {
	t.Helper()

	target := &logTarget{
		containers:    map[string]any{"container-1": map[string]string{"service_name": "app"}},
		events:        map[string][]map[string]string{},
		refusals:      map[string]string{},
		subscriptions: map[string]bool{},
	}
	upgrader := websocket.Upgrader{}
	target.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
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

			result, refusal, source := target.handle(req.Method, req.Params)
			response := map[string]any{"jsonrpc": "2.0", "id": req.ID}
			if refusal != "" {
				response["error"] = map[string]any{"code": -32001, "message": refusal}
			} else {
				response["result"] = result
			}
			if err := conn.WriteJSON(response); err != nil {
				return
			}
			if source == "" || refusal != "" {
				continue
			}
			for _, event := range target.eventsFor(source) {
				// Every event-source notification arrives wrapped in a fixed
				// "collection_update" envelope: the top-level method is
				// never the collection name itself, which instead lives in
				// params.collection, alongside the payload in params.fields.
				if err := conn.WriteJSON(map[string]any{
					"jsonrpc": "2.0",
					"method":  "collection_update",
					"params":  map[string]any{"collection": source, "fields": event},
				}); err != nil {
					return
				}
			}
		}
	}))
	t.Cleanup(target.srv.Close)
	return target
}

func (t *logTarget) handle(method string, params json.RawMessage) (any, string, string) {
	switch method {
	case "auth.login_with_api_key":
		return true, "", ""
	case "system.version":
		return "TrueNAS-25.04.0", "", ""
	case "app.container_ids":
		t.mu.Lock()
		containers := make(map[string]any, len(t.containers))
		for id, details := range t.containers {
			containers[id] = details
		}
		t.mu.Unlock()
		return containers, "", ""
	case "core.subscribe":
		source := sourceFromParams(params)
		t.mu.Lock()
		refusal := t.refusals[source]
		if refusal == "" {
			t.subscriptions[source] = true
		}
		t.mu.Unlock()
		return true, refusal, source
	case "core.unsubscribe":
		source := sourceFromParams(params)
		t.mu.Lock()
		delete(t.subscriptions, source)
		t.mu.Unlock()
		return true, "", ""
	default:
		return nil, "unexpected method " + method, ""
	}
}

func sourceFromParams(params json.RawMessage) string {
	var values []string
	_ = json.Unmarshal(params, &values)
	if len(values) == 1 {
		return values[0]
	}
	return ""
}

func (t *logTarget) eventsFor(source string) []map[string]string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.events[source]
}

func (t *logTarget) activeSubscriptions() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.subscriptions)
}

func (t *logTarget) URL() string {
	return strings.Replace(t.srv.URL, "http://", "ws://", 1)
}

func logSource(app, container string, lines int) string {
	args, _ := json.Marshal(map[string]any{"app_name": app, "container_id": container, "tail_lines": lines})
	return "app.container_log_follow:" + string(args)
}

func logClient(t *testing.T, target *logTarget) *mcp.ClientSession {
	t.Helper()
	sessions := NewSessionManager(target.URL(), false)
	t.Cleanup(sessions.CloseAll)
	session, err := sessions.Open(context.Background(), fakeTargetAPIKey)
	if err != nil {
		t.Fatalf("open session against log target: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return discoveryClient(t, func(context.Context) (*Session, error) { return session, nil }, false)
}

func TestAppsLogsReturnsTimestampedEntriesInOrder(t *testing.T) {
	target := newLogTarget(t)
	source := logSource("paperless", "container-1", 2)
	target.events[source] = []map[string]string{
		{"timestamp": "2026-08-09T10:00:00+02:00", "data": "first"},
		{"timestamp": "2026-08-09T10:00:01+02:00", "data": "second"},
	}

	res, err := logClient(t, target).CallTool(context.Background(), &mcp.CallToolParams{
		Name: "apps", Arguments: map[string]any{"op": "logs", "name": "paperless", "limit": 2},
	})
	if err != nil {
		t.Fatalf("call apps logs: %v", err)
	}
	if res.IsError {
		t.Fatalf("apps logs returned an error: %v", res.Content)
	}

	out := res.StructuredContent.(map[string]any)
	entries := out["result"].([]any)
	if len(entries) != 2 {
		t.Fatalf("entries = %#v, want two", entries)
	}
	first := entries[0].(map[string]any)
	second := entries[1].(map[string]any)
	if first["timestamp"] != "2026-08-09T10:00:00+02:00" || first["data"] != "first" {
		t.Errorf("first entry = %#v", first)
	}
	if second["timestamp"] != "2026-08-09T10:00:01+02:00" || second["data"] != "second" {
		t.Errorf("second entry = %#v", second)
	}
	waitForSubscriptions(t, target, 0)
}

func TestAppsLogsReturnsStoppedAppRefusalAndReleasesSubscription(t *testing.T) {
	target := newLogTarget(t)
	source := logSource("stopped", "container-1", defaultLimit)
	target.refusals[source] = "app is stopped"

	res, err := logClient(t, target).CallTool(context.Background(), &mcp.CallToolParams{
		Name: "apps", Arguments: map[string]any{"op": "logs", "name": "stopped"},
	})
	if err != nil {
		t.Fatalf("call apps logs: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "app is stopped") {
		t.Fatalf("result = %#v, want target refusal", res)
	}
	waitForSubscriptions(t, target, 0)
}

func TestAppsLogsRequiresContainerWhenAppHasMultiple(t *testing.T) {
	target := newLogTarget(t)
	target.containers = map[string]any{"api": map[string]string{}, "worker": map[string]string{}}

	res, err := logClient(t, target).CallTool(context.Background(), &mcp.CallToolParams{
		Name: "apps", Arguments: map[string]any{"op": "logs", "name": "paperless"},
	})
	if err != nil {
		t.Fatalf("call apps logs: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "api") ||
		!strings.Contains(res.Content[0].(*mcp.TextContent).Text, "worker") {
		t.Fatalf("result = %#v, want a multi-container error naming candidates", res)
	}
	waitForSubscriptions(t, target, 0)
}

func waitForSubscriptions(t *testing.T, target *logTarget, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if target.activeSubscriptions() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("active subscriptions = %d, want %d", target.activeSubscriptions(), want)
}
