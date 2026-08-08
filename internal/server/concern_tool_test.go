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
