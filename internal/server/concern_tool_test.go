package server

import (
	"encoding/json"
	"testing"

	"github.com/cedricziel/truenas-mcp/internal/tools"
)

// Every argument a concern declares must be carried by the shared dispatch
// input. An op that names an argument the schema cannot express is rejected by
// the client's own validation before it ever reaches the server, so this
// mismatch is invisible until someone tries the tool.
func TestDispatchInputCarriesEveryDeclaredArgument(t *testing.T) {
	// Every field populated, so omitempty does not hide any of them.
	full := DispatchInput{Op: "x", ID: "x", Name: "x", Pool: "x", Dataset: "x", Path: "x", Limit: 1}
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
		}
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
