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
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type resourceEventTarget struct {
	srv         *httptest.Server
	mu          sync.Mutex
	connections map[string]*websocket.Conn
	changes     chan string
	refusal     string
}

func newResourceEventTarget(t *testing.T) *resourceEventTarget {
	t.Helper()
	target := &resourceEventTarget{changes: make(chan string, 32), connections: make(map[string]*websocket.Conn)}
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
			if conn.ReadJSON(&req) != nil {
				return
			}
			var result any = true
			switch req.Method {
			case "system.version":
				result = "TrueNAS-25.04.0"
			}
			response := map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}
			target.mu.Lock()
			if req.Method == "auth.login_with_api_key" {
				target.connections[sourceFromParams(req.Params)] = conn
			}
			if req.Method == "core.subscribe" && target.refusal != "" {
				delete(response, "result")
				response["error"] = map[string]any{"code": -32001, "message": target.refusal}
			}
			err = conn.WriteJSON(response)
			refused := req.Method == "core.subscribe" && target.refusal != ""
			target.mu.Unlock()
			if err != nil {
				return
			}
			if !refused && (req.Method == "core.subscribe" || req.Method == "core.unsubscribe") {
				target.changes <- req.Method + ":" + sourceFromParams(req.Params)
			}
		}
	}))
	t.Cleanup(target.srv.Close)
	return target
}

func (target *resourceEventTarget) emit(t *testing.T, source string, fields any) {
	target.emitFor(t, "caller-key", source, fields)
}

func (target *resourceEventTarget) emitFor(t *testing.T, credential, source string, fields any) {
	t.Helper()
	target.mu.Lock()
	defer target.mu.Unlock()
	if err := target.connections[credential].WriteJSON(map[string]any{"jsonrpc": "2.0", "method": "collection_update", "params": map[string]any{"collection": source, "fields": fields}}); err != nil {
		t.Fatal(err)
	}
}

func (target *resourceEventTarget) change(t *testing.T, want string) {
	t.Helper()
	select {
	case got := <-target.changes:
		if got != want {
			t.Fatalf("middleware change = %q, want %q", got, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("middleware never reached %s", want)
	}
}

func resourceSubscriptionServer(t *testing.T, target *resourceEventTarget) *mcp.Server {
	return resourceSubscriptionServerFor(t, target, "caller-key")
}

func resourceSubscriptionServerFor(t *testing.T, target *resourceEventTarget, credential string) *mcp.Server {
	t.Helper()
	manager := NewSessionManager(strings.Replace(target.srv.URL, "http://", "ws://", 1), false)
	sess, err := manager.Open(context.Background(), credential)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	subscriptions := newResourceSubscriptions(func(context.Context) (*Session, error) { return sess, nil })
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, &mcp.ServerOptions{SubscribeHandler: subscriptions.subscribe, UnsubscribeHandler: subscriptions.unsubscribe})
	subscriptions.setServer(srv)
	registerResources(srv, func(context.Context) (*Session, error) { return sess, nil })
	return srv
}

func resourceProtocolSession(t *testing.T, srv *mcp.Server, version string) *rawSession {
	t.Helper()
	client := newRawSession(t, srv)
	_, err := client.call("initialize", map[string]any{"protocolVersion": version, "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test", "version": "0"}})
	if err != nil {
		t.Fatal(err)
	}
	resourceWrite(t, client, "notifications/initialized", nil, nil)
	return client
}

func resourceWrite(t *testing.T, s *rawSession, method string, id any, params any) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	rpcID, err := jsonrpc.MakeID(id)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.conn.Write(ctx, &jsonrpc.Request{ID: rpcID, Method: method, Params: raw}); err != nil {
		t.Fatal(err)
	}
}

func resourceNotification(t *testing.T, s *rawSession, method string) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	msg, err := s.conn.Read(ctx)
	if err != nil {
		t.Fatalf("waiting for %s: %v", method, err)
	}
	req, ok := msg.(*jsonrpc.Request)
	if !ok {
		t.Fatalf("wanted %s notification, got %T: %v", method, msg, msg)
	}
	if req.Method != method {
		t.Fatalf("notification = %s, want %s", req.Method, method)
	}
	var params map[string]any
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatal(err)
	}
	return params
}

func TestResourceSubscriptionsLegacyShareUpstreamAndReleaseOnDisconnect(t *testing.T) {
	target := newResourceEventTarget(t)
	srv := resourceSubscriptionServer(t, target)
	first := resourceProtocolSession(t, srv, "2025-11-25")
	second := resourceProtocolSession(t, srv, "2025-11-25")
	_, err := first.call("resources/subscribe", map[string]any{"uri": uriPools})
	if err != nil {
		t.Fatal(err)
	}
	target.change(t, "core.subscribe:pool.query")
	_, err = second.call("resources/subscribe", map[string]any{"uri": uriPools})
	if err != nil {
		t.Fatal(err)
	}
	target.emit(t, "pool.query", map[string]any{"id": 1})
	if got := resourceNotification(t, first, "notifications/resources/updated")["uri"]; got != uriPools {
		t.Fatalf("uri = %v", got)
	}
	if got := resourceNotification(t, second, "notifications/resources/updated")["uri"]; got != uriPools {
		t.Fatalf("uri = %v", got)
	}
	_, err = first.call("resources/unsubscribe", map[string]any{"uri": uriPools})
	if err != nil {
		t.Fatal(err)
	}
	target.emit(t, "pool.query", map[string]any{"id": 2})
	resourceNotification(t, second, "notifications/resources/updated")
	if err := second.conn.Close(); err != nil {
		t.Fatal(err)
	}
	target.change(t, "core.unsubscribe:pool.query")
}

func TestResourceSubscriptionsModernJobChangesCarrySubscriptionIDAndCancel(t *testing.T) {
	target := newResourceEventTarget(t)
	s := resourceProtocolSession(t, resourceSubscriptionServer(t, target), "2026-07-28")
	resourceWrite(t, s, "subscriptions/listen", "watch", map[string]any{"notifications": map[string]any{"resourceSubscriptions": []string{"truenas://job/7", "truenas://job/8"}}, "_meta": metaFor(false)})
	ack := resourceNotification(t, s, "notifications/subscriptions/acknowledged")
	if ack["_meta"].(map[string]any)["io.modelcontextprotocol/subscriptionId"] != "watch" {
		t.Fatalf("ack metadata = %v", ack)
	}
	target.change(t, "core.subscribe:core.get_jobs")
	target.emit(t, "core.get_jobs", map[string]any{"id": 8, "state": "RUNNING"})
	got := resourceNotification(t, s, "notifications/resources/updated")
	if got["uri"] != "truenas://job/8" {
		t.Fatalf("cross-job notification: %v", got)
	}
	if got["_meta"].(map[string]any)["io.modelcontextprotocol/subscriptionId"] != "watch" {
		t.Fatalf("notification metadata = %v", got)
	}
	resourceWrite(t, s, "notifications/cancelled", nil, map[string]any{"requestId": "watch", "_meta": metaFor(false)})
	target.change(t, "core.unsubscribe:core.get_jobs")
}

func TestResourceSubscriptionsRejectUnsupportedURIsAndMiddlewareRefusal(t *testing.T) {
	target := newResourceEventTarget(t)
	s := resourceProtocolSession(t, resourceSubscriptionServer(t, target), "2025-11-25")
	for _, uri := range []string{uriHealth, uriDocFilter, "truenas://job/7trailing", "truenas://job/-1", "truenas://unknown"} {
		_, err := s.call("resources/subscribe", map[string]any{"uri": uri})
		if err == nil {
			t.Fatalf("subscribed to unsupported URI %s", uri)
		}
	}
	target.mu.Lock()
	target.refusal = "permission denied"
	target.mu.Unlock()
	_, err := s.call("resources/subscribe", map[string]any{"uri": uriAlerts})
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("subscribe refusal = %v", err)
	}
}

func TestResourceSubscriptionsDoNotNotifyOtherCredentials(t *testing.T) {
	target := newResourceEventTarget(t)
	first := resourceProtocolSession(t, resourceSubscriptionServerFor(t, target, "first-key"), "2025-11-25")
	second := resourceProtocolSession(t, resourceSubscriptionServerFor(t, target, "second-key"), "2025-11-25")
	if _, err := first.call("resources/subscribe", map[string]any{"uri": uriPools}); err != nil {
		t.Fatal(err)
	}
	target.change(t, "core.subscribe:pool.query")
	if _, err := second.call("resources/subscribe", map[string]any{"uri": uriPools}); err != nil {
		t.Fatal(err)
	}
	target.change(t, "core.subscribe:pool.query")
	if _, err := second.call("resources/subscribe", map[string]any{"uri": uriApps}); err != nil {
		t.Fatal(err)
	}
	target.change(t, "core.subscribe:app.query")
	target.emitFor(t, "first-key", "pool.query", map[string]any{"id": 1})
	resourceNotification(t, first, "notifications/resources/updated")
	target.emitFor(t, "second-key", "app.query", map[string]any{"id": "paperless"})
	if got := resourceNotification(t, second, "notifications/resources/updated")["uri"]; got != uriApps {
		t.Fatalf("another credential leaked a notification: %v", got)
	}
}

func TestResourceSubscriptionsAlertChangesInvalidateAlerts(t *testing.T) {
	target := newResourceEventTarget(t)
	s := resourceProtocolSession(t, resourceSubscriptionServer(t, target), "2025-11-25")
	if _, err := s.call("resources/subscribe", map[string]any{"uri": uriAlerts}); err != nil {
		t.Fatal(err)
	}
	target.change(t, "core.subscribe:alert.list")
	target.emit(t, "alert.list", nil)
	if got := resourceNotification(t, s, "notifications/resources/updated")["uri"]; got != uriAlerts {
		t.Fatalf("alert notification URI = %v", got)
	}
	if _, err := s.call("resources/unsubscribe", map[string]any{"uri": uriAlerts}); err != nil {
		t.Fatal(err)
	}
	target.change(t, "core.unsubscribe:alert.list")
}

func (target *resourceEventTarget) interrupt(t *testing.T) {
	t.Helper()
	target.mu.Lock()
	defer target.mu.Unlock()
	if err := target.connections["caller-key"].Close(); err != nil {
		t.Fatal(err)
	}
}

func TestResourceSubscriptionsModernUpstreamFailureEndsListenWithError(t *testing.T) {
	target := newResourceEventTarget(t)
	s := resourceProtocolSession(t, resourceSubscriptionServer(t, target), "2026-07-28")
	resourceWrite(t, s, "subscriptions/listen", "watch", map[string]any{"notifications": map[string]any{"resourceSubscriptions": []string{uriPools}}, "_meta": metaFor(false)})
	resourceNotification(t, s, "notifications/subscriptions/acknowledged")
	target.change(t, "core.subscribe:pool.query")
	target.interrupt(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	msg, err := s.conn.Read(ctx)
	if err != nil {
		t.Fatalf("listen did not report upstream failure: %v", err)
	}
	response, ok := msg.(*jsonrpc.Response)
	if !ok || response.Error == nil || !strings.Contains(response.Error.Error(), "interrupted") {
		t.Fatalf("upstream failure response = %#v", msg)
	}
}

func TestResourceSubscriptionsLegacyUpstreamFailureClosesSession(t *testing.T) {
	target := newResourceEventTarget(t)
	s := resourceProtocolSession(t, resourceSubscriptionServer(t, target), "2025-11-25")
	if _, err := s.call("resources/subscribe", map[string]any{"uri": uriPools}); err != nil {
		t.Fatal(err)
	}
	target.change(t, "core.subscribe:pool.query")
	target.interrupt(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := s.conn.Read(ctx)
	if err == nil || ctx.Err() != nil {
		t.Fatalf("MCP session did not close after upstream failure: %v", err)
	}
}

func TestResourceSubscriptionsRejectOverlappingListenWithoutReplacingExisting(t *testing.T) {
	target := newResourceEventTarget(t)
	s := resourceProtocolSession(t, resourceSubscriptionServer(t, target), "2026-07-28")
	resourceWrite(t, s, "subscriptions/listen", "watch", map[string]any{"notifications": map[string]any{"resourceSubscriptions": []string{uriPools}}, "_meta": metaFor(false)})
	resourceNotification(t, s, "notifications/subscriptions/acknowledged")
	target.change(t, "core.subscribe:pool.query")
	_, err := s.call("subscriptions/listen", map[string]any{"notifications": map[string]any{"resourceSubscriptions": []string{uriApps, uriPools}}, "_meta": metaFor(false)})
	if err == nil || !strings.Contains(err.Error(), "active listen") {
		t.Fatalf("overlapping listen error = %v", err)
	}
	target.emit(t, "pool.query", map[string]any{"id": 1})
	got := resourceNotification(t, s, "notifications/resources/updated")
	if got["_meta"].(map[string]any)["io.modelcontextprotocol/subscriptionId"] != "watch" {
		t.Fatalf("overlap replaced original listener: %v", got)
	}
	resourceWrite(t, s, "notifications/cancelled", nil, map[string]any{"requestId": "watch", "_meta": metaFor(false)})
	target.change(t, "core.unsubscribe:pool.query")
}

func TestResourceSubscriptionsRejectDuplicateURIsWithinListen(t *testing.T) {
	target := newResourceEventTarget(t)
	s := resourceProtocolSession(t, resourceSubscriptionServer(t, target), "2026-07-28")
	_, err := s.call("subscriptions/listen", map[string]any{"notifications": map[string]any{"resourceSubscriptions": []string{uriPools, uriPools}}, "_meta": metaFor(false)})
	if err == nil || !strings.Contains(err.Error(), "active listen") {
		t.Fatalf("duplicate listen error = %v", err)
	}
}

func TestResourceSubscriptionsStartedSetupTimeoutCannotPublishLiveStream(t *testing.T) {
	streamCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	resume := make(chan struct{})
	timer := time.AfterFunc(0, func() { close(started); <-resume; cancel() })
	<-started
	defer close(resume)
	err := finishResourceSubscriptionSetup(context.Background(), streamCtx, cancel, timer, func() bool { return true })
	if err != context.DeadlineExceeded || streamCtx.Err() != context.Canceled {
		t.Fatalf("started timeout accepted stream: error=%v stream=%v", err, streamCtx.Err())
	}
}

func TestResourceSubscriptionsModernLegacyMethodsCannotReplaceActiveListen(t *testing.T) {
	target := newResourceEventTarget(t)
	s := resourceProtocolSession(t, resourceSubscriptionServer(t, target), "2026-07-28")
	resourceWrite(t, s, "subscriptions/listen", "watch", map[string]any{"notifications": map[string]any{"resourceSubscriptions": []string{uriPools}}, "_meta": metaFor(false)})
	resourceNotification(t, s, "notifications/subscriptions/acknowledged")
	target.change(t, "core.subscribe:pool.query")
	if _, err := s.call("resources/subscribe", map[string]any{"uri": uriPools, "_meta": metaFor(false)}); err == nil {
		t.Fatal("modern session accepted legacy subscribe")
	}
	if _, err := s.call("resources/unsubscribe", map[string]any{"uri": uriPools, "_meta": metaFor(false)}); err == nil {
		t.Fatal("modern session accepted legacy unsubscribe")
	}
	target.emit(t, "pool.query", map[string]any{"id": 1})
	got := resourceNotification(t, s, "notifications/resources/updated")
	if got["_meta"].(map[string]any)["io.modelcontextprotocol/subscriptionId"] != "watch" {
		t.Fatalf("legacy methods replaced active listen: %v", got)
	}
	resourceWrite(t, s, "notifications/cancelled", nil, map[string]any{"requestId": "watch", "_meta": metaFor(false)})
	target.change(t, "core.unsubscribe:pool.query")
}

func TestResourceSubscriptionsTargetSourceTerminationEndsListenWithReason(t *testing.T) {
	target := newResourceEventTarget(t)
	s := resourceProtocolSession(t, resourceSubscriptionServer(t, target), "2026-07-28")
	resourceWrite(t, s, "subscriptions/listen", "watch", map[string]any{"notifications": map[string]any{"resourceSubscriptions": []string{uriPools}}, "_meta": metaFor(false)})
	resourceNotification(t, s, "notifications/subscriptions/acknowledged")
	target.change(t, "core.subscribe:pool.query")
	target.mu.Lock()
	err := target.connections["caller-key"].WriteJSON(map[string]any{"jsonrpc": "2.0", "method": "notify_unsubscribed", "params": map[string]any{"collection": "pool.query", "error": map[string]any{"error": 5, "reason": "source failed"}}})
	target.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	msg, err := s.conn.Read(ctx)
	if err != nil {
		t.Fatalf("listen did not report terminal source failure: %v", err)
	}
	response, ok := msg.(*jsonrpc.Response)
	if !ok || response.Error == nil || !strings.Contains(response.Error.Error(), "source failed") {
		t.Fatalf("terminal source failure response = %#v", msg)
	}
}
