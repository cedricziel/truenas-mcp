package server

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/cedricziel/truenas-mcp/internal/tools"
)

// Every argument a concern declares must be carried by the shared dispatch
// input. An op that names an argument the schema cannot express is rejected by
// the client's own validation before it ever reaches the server, so this
// mismatch is invisible until someone tries the tool.
func TestDispatchInputCarriesEveryDeclaredArgument(t *testing.T) {
	// Every field populated, so omitempty does not hide any of them.
	full := DispatchInput{Op: "x", ID: "x", Name: "x", Pool: "x", Dataset: "x", Path: "x", Category: "x", Limit: 1}
	raw, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("decode: %v", err)
	}

	for _, c := range tools.ReadConcerns() {
		for _, op := range c.Ops {
			for _, arg := range op.Args {
				if _, ok := fields[arg]; !ok {
					t.Errorf("%s.%s declares argument %q, which DispatchInput cannot carry",
						c.Name, op.Name, arg)
				}
			}
			// A filter is just as much a way of reaching the target as a
			// positional argument, so it needs the same coverage: a Filter
			// whose Arg names a field DispatchInput does not have would fail
			// silently, since middlewareParams would just never find a value
			// for it.
			for _, f := range op.Filters {
				if _, ok := fields[f.Arg]; !ok {
					t.Errorf("%s.%s declares filter argument %q, which DispatchInput cannot carry",
						c.Name, op.Name, f.Arg)
				}
			}
		}
	}
}

// A declared filter must become a middleware filter triple under the operator
// the operation declared, not a hardcoded one -- this is the whole point of
// moving pool/dataset off the dispatcher's own chain and onto Op.Filters.
func TestMiddlewareParamsBuildsDeclaredFilter(t *testing.T) {
	op := tools.Op{
		Name:    "list_datasets",
		Method:  "pool.dataset.query",
		Filters: []tools.Filter{{Arg: "pool", Field: "pool", Operator: "="}},
	}
	in := DispatchInput{Op: "list_datasets", Pool: "tank"}

	got := middlewareParams(&op, in)
	want := []any{[]any{[]any{"pool", "=", "tank"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("middlewareParams = %#v, want %#v", got, want)
	}
}

// A declared membership operator must reach the target as such. Using "pool"
// with "rin" here is deliberately hypothetical -- no real concern declares
// pool that way -- because the point is that middlewareParams emits whatever
// operator the operation declared, not that it special-cases any one field.
func TestMiddlewareParamsEmitsDeclaredOperatorRatherThanEquality(t *testing.T) {
	op := tools.Op{
		Name:    "hypothetical",
		Method:  "some.query",
		Filters: []tools.Filter{{Arg: "pool", Field: "pool", Operator: "rin"}},
	}
	in := DispatchInput{Op: "hypothetical", Pool: "tank"}

	got := middlewareParams(&op, in)
	want := []any{[]any{[]any{"pool", "rin", "tank"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("middlewareParams = %#v, want %#v -- a declared membership filter "+
			"must not be silently emitted as equality", got, want)
	}
}

// An operation that takes an identifier positionally is fetching one record;
// a filter alongside it would be meaningless. Positional resolution has to
// win rather than the two being merged.
func TestMiddlewareParamsPositionalWinsOverDeclaredFilter(t *testing.T) {
	op := tools.Op{
		Name:    "show",
		Method:  "app.get_instance",
		Args:    []string{"name"},
		Filters: []tools.Filter{{Arg: "name", Field: "name", Operator: "="}},
	}
	in := DispatchInput{Op: "show", Name: "plex"}

	got := middlewareParams(&op, in)
	want := []any{"plex"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("middlewareParams = %#v, want %#v -- positional resolution must win "+
			"when an operation declares both", got, want)
	}
}

// TestMiddlewareParamsEmitsDeclaredOptionsWithCallerLimit proves an
// Options-declaring op sends a single object parameter -- not an empty
// parameter list, which is the original audit_log defect -- carrying its
// declared parts plus the caller's own limit merged into
// query-options.limit. audit.query needs this bound present before the
// target accepts the call at all, so it has to travel upstream rather than
// being applied to the response the way shape() applies limit everywhere
// else.
func TestMiddlewareParamsEmitsDeclaredOptionsWithCallerLimit(t *testing.T) {
	op := tools.Op{
		Name:   "audit_log",
		Method: "audit.query",
		Options: map[string]any{
			"query-options": map[string]any{
				"order_by": []string{"-message_timestamp"},
			},
		},
	}
	in := DispatchInput{Op: "audit_log", Limit: 7}

	got := middlewareParams(&op, in)
	want := []any{map[string]any{
		"query-options": map[string]any{
			"order_by": []string{"-message_timestamp"},
			"limit":    7,
		},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("middlewareParams = %#v, want %#v", got, want)
	}

	// The op's own declaration must come back untouched: Options lives on a
	// Concern built once at startup and reused for every call this
	// operation ever serves, so merging the limit in place would leak one
	// caller's limit into the next caller's request.
	orig := op.Options["query-options"].(map[string]any)
	if _, ok := orig["limit"]; ok {
		t.Error("middlewareParams must not mutate the op's declared Options in place")
	}
}

// TestMiddlewareParamsEmitsDefaultLimitWhenCallerSuppliesNone proves the
// limit merged into query-options is the same effective limit shape() would
// use elsewhere -- the caller's own limit when supplied, otherwise
// defaultLimit -- rather than some other default drifting out of sync with
// it.
func TestMiddlewareParamsEmitsDefaultLimitWhenCallerSuppliesNone(t *testing.T) {
	op := tools.Op{
		Name:    "audit_log",
		Method:  "audit.query",
		Options: map[string]any{"query-options": map[string]any{}},
	}
	in := DispatchInput{Op: "audit_log"}

	got := middlewareParams(&op, in)
	want := []any{map[string]any{
		"query-options": map[string]any{"limit": defaultLimit},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("middlewareParams = %#v, want %#v -- no caller limit must fall back to "+
			"defaultLimit, the same bound shape() uses", got, want)
	}
}

// TestLimitIsNeverRefused is the regression test for the defect this fix
// addresses: limit used to be forwarded into the tools.Args map like a
// narrowing argument (pool, dataset), which meant Concern.Resolve refused it
// for every op that did not separately declare "limit" in its Args slice.
// Only 3 of the 43 ops across every concern happened to declare it; the other
// 40 refused a parameter their own advertised schema offered.
//
// This walks DispatchInput.args() -- the actual mechanism the fix touches,
// which is why this lives here rather than in internal/tools: args() is
// unexported, and testing the real code beats re-implementing its mapping
// logic in a second place that could drift from it. It covers every op in
// every concern so a future op is protected automatically; nobody has to
// remember to add it here.
func TestLimitIsNeverRefused(t *testing.T) {
	for _, c := range tools.ReadConcerns() {
		for _, op := range c.Ops {
			t.Run(c.Name+"/"+op.Name, func(t *testing.T) {
				in := DispatchInput{Op: op.Name, Limit: 1}
				for _, req := range op.Required {
					switch req {
					case "id":
						in.ID = "placeholder"
					case "name":
						in.Name = "placeholder"
					case "pool":
						in.Pool = "placeholder"
					case "dataset":
						in.Dataset = "placeholder"
					case "path":
						in.Path = "placeholder"
					}
				}

				if _, err := c.Resolve(op.Name, in.args()); err != nil {
					t.Errorf("limit must never be refused, since it only bounds the response and "+
						"is never read as a middleware argument: %v", err)
				}
			})
		}
	}
}
