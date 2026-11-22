package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type httpProtocolMessage struct {
	ID     any            `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
	Result map[string]any `json:"result"`
	Error  *jsonrpc.Error `json:"error"`
}

func httpProtocolServer(t *testing.T, target *resourceEventTarget) *httptest.Server {
	t.Helper()
	manager := NewSessionManager(strings.Replace(target.srv.URL, "http://", "ws://", 1), false)
	t.Cleanup(manager.CloseAll)
	server := httptest.NewServer(NewMCPHandler(MCPConfig{Version: "test", Sessions: manager}))
	t.Cleanup(server.Close)
	return server
}

func httpProtocolPost(t *testing.T, endpoint, method, headerVersion, metaVersion string, params map[string]any) *http.Response {
	t.Helper()
	if params == nil {
		params = map[string]any{}
	}
	if metaVersion != "" {
		params["_meta"] = map[string]any{
			"io.modelcontextprotocol/protocolVersion":    metaVersion,
			"io.modelcontextprotocol/clientCapabilities": map[string]any{},
		}
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "http-test", "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer caller-key")
	req.Header.Set("Mcp-Method", method)
	if headerVersion != "" {
		req.Header.Set("Mcp-Protocol-Version", headerVersion)
	}
	if name, ok := params["name"].(string); ok {
		req.Header.Set("Mcp-Name", name)
	}
	if uri, ok := params["uri"].(string); ok {
		req.Header.Set("Mcp-Name", uri)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func httpProtocolSSE(t *testing.T, scanner *bufio.Scanner) httpProtocolMessage {
	t.Helper()
	var data []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
		if line == "" && len(data) > 0 {
			var message httpProtocolMessage
			if err := json.Unmarshal([]byte(strings.Join(data, "\n")), &message); err != nil {
				t.Fatalf("decode SSE message: %v", err)
			}
			return message
		}
	}
	t.Fatalf("SSE stream ended before a message: %v", scanner.Err())
	return httpProtocolMessage{}
}

func httpProtocolResponse(t *testing.T, response *http.Response) httpProtocolMessage {
	t.Helper()
	if strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		scanner := bufio.NewScanner(response.Body)
		scanner.Buffer(make([]byte, 4096), 4<<20)
		return httpProtocolSSE(t, scanner)
	}
	var message httpProtocolMessage
	if err := json.NewDecoder(response.Body).Decode(&message); err != nil {
		t.Fatalf("decode HTTP %d response: %v", response.StatusCode, err)
	}
	return message
}

func TestHTTPModernDiscoveryAdvertisesCurrentProtocolWithoutASession(t *testing.T) {
	server := httpProtocolServer(t, newResourceEventTarget(t))
	response := httpProtocolPost(t, server.URL, "server/discover", "2026-07-28", "2026-07-28", nil)
	message := httpProtocolResponse(t, response)
	if response.StatusCode != http.StatusOK || message.Error != nil {
		t.Fatalf("modern discovery: HTTP %d, error %v", response.StatusCode, message.Error)
	}
	versions, _ := message.Result["supportedVersions"].([]any)
	if !slices.Contains(versions, any("2026-07-28")) {
		t.Fatalf("modern discovery omitted current protocol: %#v", message.Result)
	}
	if got := response.Header.Get("Mcp-Session-Id"); got != "" {
		t.Fatalf("modern response minted legacy session %q", got)
	}
}

func TestHTTPModernCallsDoNotRequireInitialize(t *testing.T) {
	server := httpProtocolServer(t, newResourceEventTarget(t))
	response := httpProtocolPost(t, server.URL, "tools/call", "2026-07-28", "2026-07-28", map[string]any{"name": "server_info", "arguments": map[string]any{}})
	message := httpProtocolResponse(t, response)
	if response.StatusCode != http.StatusOK || message.Error != nil || message.Result["resultType"] != "complete" {
		t.Fatalf("modern tool call: HTTP %d, message %#v", response.StatusCode, message)
	}
	if response.Header.Get("Mcp-Session-Id") != "" {
		t.Fatal("modern tool call created a legacy session")
	}
}

func TestHTTPModernResourceStreamsShareUpstreamAndCancelIndependently(t *testing.T) {
	target := newResourceEventTarget(t)
	server := httpProtocolServer(t, target)
	params := func() map[string]any {
		return map[string]any{"notifications": map[string]any{"resourceSubscriptions": []string{uriPools}}}
	}
	first := httpProtocolPost(t, server.URL, "subscriptions/listen", "2026-07-28", "2026-07-28", params())
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first modern listen: HTTP %d: %#v", first.StatusCode, httpProtocolResponse(t, first))
	}
	firstScanner := bufio.NewScanner(first.Body)
	if ack := httpProtocolSSE(t, firstScanner); ack.Method != "notifications/subscriptions/acknowledged" {
		t.Fatalf("first stream did not acknowledge: %#v", ack)
	}
	target.change(t, "core.subscribe:pool.query")
	second := httpProtocolPost(t, server.URL, "subscriptions/listen", "2026-07-28", "2026-07-28", params())
	if second.StatusCode != http.StatusOK {
		t.Fatalf("second modern listen: HTTP %d: %#v", second.StatusCode, httpProtocolResponse(t, second))
	}
	secondScanner := bufio.NewScanner(second.Body)
	if ack := httpProtocolSSE(t, secondScanner); ack.Method != "notifications/subscriptions/acknowledged" {
		t.Fatalf("second stream did not acknowledge: %#v", ack)
	}
	target.emit(t, "pool.query", map[string]any{"id": 1})
	firstEvent := httpProtocolSSE(t, firstScanner)
	secondEvent := httpProtocolSSE(t, secondScanner)
	if firstEvent.Params["uri"] != uriPools || secondEvent.Params["uri"] != uriPools || firstEvent.Params["_meta"].(map[string]any)["io.modelcontextprotocol/subscriptionId"] != "http-test" {
		t.Fatalf("modern events = %#v, %#v", firstEvent, secondEvent)
	}
	if err := first.Body.Close(); err != nil {
		t.Fatal(err)
	}
	target.emit(t, "pool.query", map[string]any{"id": 2})
	if event := httpProtocolSSE(t, secondScanner); event.Params["uri"] != uriPools {
		t.Fatalf("closing one stream broke another: %#v", event)
	}
	if err := second.Body.Close(); err != nil {
		t.Fatal(err)
	}
	target.change(t, "core.unsubscribe:pool.query")
}

func TestHTTPLegacyInitializeAndResourceSubscriptionStillWork(t *testing.T) {
	target := newResourceEventTarget(t)
	server := httpProtocolServer(t, target)
	updates := make(chan string, 1)
	client := mcp.NewClient(&mcp.Implementation{Name: "legacy-test", Version: "0"}, &mcp.ClientOptions{
		ResourceUpdatedHandler: func(_ context.Context, req *mcp.ResourceUpdatedNotificationRequest) { updates <- req.Params.URI },
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL, HTTPClient: &http.Client{Transport: httpProtocolCredentialTransport{}}}, &mcp.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	if session.InitializeResult().ProtocolVersion != "2025-11-25" {
		t.Fatalf("legacy initialized with %s", session.InitializeResult().ProtocolVersion)
	}
	if err := session.Subscribe(ctx, &mcp.SubscribeParams{URI: uriPools}); err != nil {
		t.Fatal(err)
	}
	target.change(t, "core.subscribe:pool.query")
	target.emit(t, "pool.query", map[string]any{"id": 1})
	select {
	case uri := <-updates:
		if uri != uriPools {
			t.Fatalf("legacy update = %s", uri)
		}
	case <-ctx.Done():
		t.Fatal("legacy subscription received no update")
	}
	if err := session.Unsubscribe(ctx, &mcp.UnsubscribeParams{URI: uriPools}); err != nil {
		t.Fatal(err)
	}
	target.change(t, "core.unsubscribe:pool.query")
}

func TestHTTPModernCacheableResultsStayPrivateAndImmediatelyStale(t *testing.T) {
	for _, method := range []string{"server/discover", "tools/list", "resources/list", "resources/templates/list", "prompts/list", "resources/read"} {
		t.Run(method, func(t *testing.T) {
			server := httpProtocolServer(t, newResourceEventTarget(t))
			params := map[string]any{}
			if method == "resources/read" {
				params["uri"] = uriDocFilter
			}
			response := httpProtocolPost(t, server.URL, method, "2026-07-28", "2026-07-28", params)
			message := httpProtocolResponse(t, response)
			if message.Error != nil || message.Result["cacheScope"] != "private" || message.Result["ttlMs"] != float64(0) {
				t.Fatalf("credential-bound response is cacheable across users or time: %#v", message)
			}
		})
	}
}

func TestHTTPModernLivePoolReadCannotBeSharedAcrossCredentials(t *testing.T) {
	target := newFakeTarget(t)
	target.respond("auth.login_with_api_key", true)
	target.respond("pool.query", []any{map[string]any{"name": "private-tank", "status": "ONLINE"}})
	manager := NewSessionManager(target.URL(), false)
	t.Cleanup(manager.CloseAll)
	server := httptest.NewServer(NewMCPHandler(MCPConfig{Sessions: manager}))
	t.Cleanup(server.Close)
	response := httpProtocolPost(t, server.URL, "resources/read", "2026-07-28", "2026-07-28", map[string]any{"uri": uriPools})
	message := httpProtocolResponse(t, response)
	if response.StatusCode != http.StatusOK || message.Error != nil || message.Result["cacheScope"] != "private" || message.Result["ttlMs"] != float64(0) {
		t.Fatalf("live pool response is cacheable across users or time: %#v", message)
	}
	contents := message.Result["contents"].([]any)
	if len(contents) != 1 || !strings.Contains(contents[0].(map[string]any)["text"].(string), "private-tank") {
		t.Fatalf("live pool response did not contain the target evidence: %#v", contents)
	}
}

func TestHTTPModernRejectsVersionHeaderMismatchAndHeaderlessDiscovery(t *testing.T) {
	for _, test := range []struct{ header, meta string }{{"2026-07-28", "2025-11-25"}, {"", "2026-07-28"}} {
		t.Run(test.header+test.meta, func(t *testing.T) {
			server := httpProtocolServer(t, newResourceEventTarget(t))
			response := httpProtocolPost(t, server.URL, "server/discover", test.header, test.meta, nil)
			message := httpProtocolResponse(t, response)
			if response.StatusCode != http.StatusBadRequest || message.Error == nil || message.Error.Code != mcp.CodeHeaderMismatch {
				t.Fatalf("mismatched version accepted: HTTP %d, %#v", response.StatusCode, message)
			}
		})
	}
}

type httpProtocolCredentialTransport struct{}

func (httpProtocolCredentialTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	copy := req.Clone(req.Context())
	copy.Header.Set("Authorization", "Bearer caller-key")
	return http.DefaultTransport.RoundTrip(copy)
}
