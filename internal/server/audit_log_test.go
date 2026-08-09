package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestAuditLogNeverSendsAnEmptyParameterList is the regression test for the
// original defect: system(op="audit_log") mapped to audit.query and the
// dispatcher sent it no parameters at all, which the target always rejected
// with -32602 Invalid params -- verified live against TrueNAS 26, and true
// of every params shape short of one carrying a query-options bound. The
// operation shipped broken and nothing caught it, so this pins the specific
// shape a fixed call must never regress to: params must be non-empty.
func TestAuditLogNeverSendsAnEmptyParameterList(t *testing.T) {
	target := newFakeTarget(t)
	target.respond("audit.query", []map[string]any{})
	client := discoveryClient(t, discoverySession(t, target), false)

	res, err := client.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "system",
		Arguments: map[string]any{"op": "audit_log"},
	})
	if err != nil {
		t.Fatalf("call system: %v", err)
	}
	if res.IsError {
		t.Fatalf("system returned an error: %v", res.Content)
	}

	raw, ok := target.lastParams("audit.query")
	if !ok {
		t.Fatal("audit.query was never called")
	}

	var params []any
	if err := json.Unmarshal(raw, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if len(params) == 0 {
		t.Fatal("audit.query must never be called with an empty parameter list -- the target " +
			"rejects that with -32602 Invalid params, which is exactly how this operation shipped broken")
	}
}

// TestAuditLogSendsOrderedBoundedQueryOptions proves the fix end to end
// through the registered system tool: the call succeeds, the target
// receives a single object parameter carrying both the descending
// message_timestamp order and a query-options.limit bound, and the response
// carries neither total nor truncated. Both would have to be fabricated: the
// server asked the target for only defaultLimit records out of however many
// actually exist, so it has literally seen a page, not the whole collection,
// and cannot state a real total or say whether the page was cut short of one.
func TestAuditLogSendsOrderedBoundedQueryOptions(t *testing.T) {
	target := newFakeTarget(t)
	target.respondFunc("audit.query", func(params json.RawMessage) any {
		var args []any
		_ = json.Unmarshal(params, &args)
		if len(args) != 1 {
			t.Errorf("expected exactly one object parameter, got %d: %s", len(args), params)
			return []map[string]any{}
		}
		obj, ok := args[0].(map[string]any)
		if !ok {
			t.Errorf("expected an object parameter, got %T", args[0])
			return []map[string]any{}
		}
		qo, ok := obj["query-options"].(map[string]any)
		if !ok {
			t.Errorf("expected query-options in the object parameter, got %#v", obj)
			return []map[string]any{}
		}
		orderBy, ok := qo["order_by"].([]any)
		if !ok || len(orderBy) != 1 || orderBy[0] != "-message_timestamp" {
			t.Errorf("expected order_by [-message_timestamp], got %#v", qo["order_by"])
		}
		if limit, _ := qo["limit"].(float64); limit != float64(defaultLimit) {
			t.Errorf("expected query-options.limit = %d (no limit was supplied by the caller), got %v",
				defaultLimit, qo["limit"])
		}
		// A handful of records, well under defaultLimit -- what matters here is
		// that the target, not the server, is the one that bounded the real
		// 1220-entry collection down to this.
		return []map[string]any{
			{"message": "one", "message_timestamp": 3},
			{"message": "two", "message_timestamp": 2},
		}
	})
	client := discoveryClient(t, discoverySession(t, target), false)

	res, err := client.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "system",
		Arguments: map[string]any{"op": "audit_log"},
	})
	if err != nil {
		t.Fatalf("call system: %v", err)
	}
	if res.IsError {
		t.Fatalf("system returned an error: %v", res.Content)
	}

	out, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured content, got %T", res.StructuredContent)
	}

	if _, ok := out["total"]; ok {
		t.Errorf("audit_log must not report total: the target applied the bound, so the " +
			"server has only seen a page and cannot know the real total")
	}
	if _, ok := out["truncated"]; ok {
		t.Errorf("audit_log must not report truncated: whether more records exist beyond " +
			"the page the target returned is exactly as unknowable as the total")
	}

	items, ok := out["result"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("expected 2 result items, got %#v", out["result"])
	}
}
