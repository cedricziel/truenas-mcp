package server

import (
	"encoding/json"
	"testing"
)

// Each of the 50 items a list result carries was a whole middleware object,
// which is what actually blew a caller's token budget -- the existing
// count/total/truncated accounting already caps how many items come back.
// shape must also be able to reduce each item to the fields an op declares,
// and must say so, the same way it already says a list was cut short.
func TestShapeProjectsListItemsWhenOpDeclaresFields(t *testing.T) {
	raw := json.RawMessage(`[{"name":"signaldb","state":"RUNNING","version":"1.0","id":"internal-id"}]`)
	out := shape("list", raw, 0, []string{"name", "state", "version"}, false)

	items, ok := out.Result.([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected one item, got %#v", out.Result)
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("expected item to be an object, got %T", items[0])
	}
	if len(item) != 3 {
		t.Errorf("projected item has %d fields, want 3: %v", len(item), item)
	}
	if _, ok := item["id"]; ok {
		t.Error("projected item still carries id, which was not in the declared projection")
	}
	if !out.Projected {
		t.Error("Projected must be true when fields were dropped from the result")
	}
}

// full is the caller's escape hatch: it must bypass projection entirely, not
// just widen it.
func TestShapeFullBypassesProjection(t *testing.T) {
	raw := json.RawMessage(`[{"name":"signaldb","state":"RUNNING","version":"1.0","id":"internal-id"}]`)
	out := shape("list", raw, 0, []string{"name", "state", "version"}, true)

	items := out.Result.([]any)
	item := items[0].(map[string]any)
	if len(item) != 4 {
		t.Errorf("full=true must bypass projection entirely, got %v", item)
	}
	if out.Projected {
		t.Error("Projected must be false when full=true bypassed projection")
	}
}

// Ops that declare no projection must behave exactly as before -- projection
// is opt-in per op, not a blanket behavior change.
func TestShapeWithoutDeclaredProjectionIsUnchanged(t *testing.T) {
	raw := json.RawMessage(`[{"name":"tank","used":123}]`)
	out := shape("list_pools", raw, 0, nil, false)

	items := out.Result.([]any)
	item := items[0].(map[string]any)
	if len(item) != 2 {
		t.Errorf("an op without a declared projection must return every field, got %v", item)
	}
	if out.Projected {
		t.Error("Projected must be false when the op declares no projection")
	}
}

// A truncated object should no more read as complete than a truncated list
// does -- the two signals are independent and must not interfere.
func TestShapeProjectionDoesNotAffectTruncationAccounting(t *testing.T) {
	items := make([]map[string]any, 3)
	for i := range items {
		items[i] = map[string]any{"name": "a", "state": "RUNNING", "version": "1", "id": i}
	}
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}

	out := shape("list", raw, 2, []string{"name"}, false)
	if !out.Truncated || out.Total != 3 || out.Count != 2 {
		t.Fatalf("truncation accounting must be unaffected by projection: %+v", out)
	}
	if !out.Projected {
		t.Error("Projected must still report true alongside Truncated")
	}
}

// Projection only ever applies to list-shaped results; a single object, such
// as from a get_instance-style op, must pass through untouched even if a
// projection happened to be declared for it.
func TestShapeSingleObjectResultIsNeverProjected(t *testing.T) {
	raw := json.RawMessage(`{"name":"signaldb","state":"RUNNING","version":"1.0","id":"internal-id"}`)
	out := shape("show", raw, 0, []string{"name", "state", "version"}, false)

	obj, ok := out.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected a single object, got %T", out.Result)
	}
	if len(obj) != 4 {
		t.Errorf("single-object results must never be projected, got %v", obj)
	}
	if out.Projected {
		t.Error("Projected must be false for a single-object result")
	}
}

// Projected has to mean fields were dropped, not that a projection was
// configured -- it sits next to Truncated, which reports what happened rather
// than what was asked for, and a caller reading one the way it reads the other
// would be told an answer is partial when it is whole.
func TestShapeReportsProjectedOnlyWhenFieldsWereActuallyDropped(t *testing.T) {
	fields := []string{"name", "state"}

	for _, tc := range []struct {
		name string
		raw  string
		want bool
	}{
		{"object with extra fields loses them", `[{"name":"a","state":"UP","extra":1}]`, true},
		{"object already minimal loses nothing", `[{"name":"a","state":"UP"}]`, false},
		{"empty list has nothing to drop", `[]`, false},
		{"scalar items pass through untouched", `["a","b"]`, false},
		{"one object among scalars still counts", `["a",{"name":"b","state":"UP","extra":1}]`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := shape("list", json.RawMessage(tc.raw), 0, fields, false)
			if out.Projected != tc.want {
				t.Errorf("Projected = %v, want %v for %s", out.Projected, tc.want, tc.raw)
			}
		})
	}
}
