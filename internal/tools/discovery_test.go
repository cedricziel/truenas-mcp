package tools

import (
	"strings"
	"testing"
)

// Discovery is bounded by an allowlist rather than a denylist. An allowlist
// fails closed; a denylist maintained against an API that grows every release
// fails open, and a method added next version would be callable on day one.
func TestAllowlistedMethodsAreReachable(t *testing.T) {
	for _, method := range []string{
		"pool.query",
		"app.query",
		"smart.test.results",
		"reporting.netdata_get_data",
		"certificate.query",
	} {
		if err := CheckDiscoverable(method, false); err != nil {
			t.Errorf("%s should be discoverable: %v", method, err)
		}
	}
}

func TestNonAllowlistedMethodIsRefused(t *testing.T) {
	err := CheckDiscoverable("pool.dataset.create", false)
	if err == nil {
		t.Fatal("a mutating method must not be reachable with writes disabled")
	}
}

// A method invented after this allowlist was written must not be reachable
// just because nobody thought to deny it.
func TestUnknownFutureMethodIsRefused(t *testing.T) {
	if err := CheckDiscoverable("newthing.invented_next_release", false); err == nil {
		t.Fatal("a method matching no allowlist pattern must be refused")
	}
	if err := CheckDiscoverable("newthing.invented_next_release", true); err == nil {
		t.Fatal("enabling writes must not open the whole API")
	}
}

// Query-shaped methods are recognised by pattern, so renames and additions
// within an allowed namespace keep working across API versions.
func TestPatternsSurviveNewMethodsInAllowedShapes(t *testing.T) {
	for _, method := range []string{
		"somethingnew.query",
		"anothething.get_instance",
		"whatever.config",
	} {
		if err := CheckDiscoverable(method, false); err != nil {
			t.Errorf("%s matches an allowed read shape and should be reachable: %v", method, err)
		}
	}
}

// Discovery honours the same gating as the write tier.
func TestMutatingMethodNeedsWritesEnabled(t *testing.T) {
	if err := CheckDiscoverable("app.upgrade", false); err == nil {
		t.Fatal("a mutating method must be refused with writes disabled")
	} else if !strings.Contains(err.Error(), "write") {
		t.Errorf("the error should identify the write tier as the reason: %v", err)
	}

	if err := CheckDiscoverable("app.upgrade", true); err != nil {
		t.Errorf("app.upgrade should be reachable with writes enabled: %v", err)
	}
}

// The permanent denylist outranks the allowlist under every configuration.
func TestDenylistOutranksDiscovery(t *testing.T) {
	for _, method := range []string{"pool.export", "disk.wipe", "pool.dataset.delete"} {
		if err := CheckDiscoverable(method, true); err == nil {
			t.Errorf("%s must be refused even with writes enabled", method)
		}
	}
}

// A refusal must not disclose what it is hiding.
func TestRefusalDoesNotLeakMethodDetail(t *testing.T) {
	err := CheckDiscoverable("newthing.invented_next_release", false)
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if strings.Contains(strings.ToLower(err.Error()), "exists") {
		t.Errorf("refusal should not reveal whether the method exists: %v", err)
	}
}

// Write shapes must be checked before read shapes. Several mutating methods
// end in something that also reads as a read suffix -- app.pull_images ends in
// "_images" -- and matching reads first would let them through the gate.
func TestWriteShapesOutrankReadShapes(t *testing.T) {
	for _, method := range []string{
		"app.pull_images",
		"catalog.sync",
		"pool.scrub",
	} {
		if err := CheckDiscoverable(method, false); err == nil {
			t.Errorf("%s mutates and must be refused with writes disabled", method)
		}
		if err := CheckDiscoverable(method, true); err != nil {
			t.Errorf("%s should be reachable with writes enabled: %v", method, err)
		}
	}
}

// Middleware methods overwhelmingly take a single object parameter with the
// real arguments nested inside it. Summarising only the top level therefore
// achieves nothing for exactly the methods that need it -- sharing.smb.create
// is one top-level param wrapping a ~31,000-character structure.
func TestSummaryFlattensASingleObjectParameter(t *testing.T) {
	params := []SchemaParam{{
		Name:     "smb_create",
		Type:     "object",
		Required: true,
		Properties: []SchemaParam{
			{Name: "path", Required: true},
			{Name: "name", Required: true},
			{Name: "purpose", Default: "DEFAULT_SHARE"},
			{Name: "comment", Default: ""},
			{Name: "ro", Default: false},
		},
	}}

	got := SummarizeSchema(params, false)
	if len(got.Params) != 2 {
		t.Fatalf("expected the two required nested fields, got %d: %+v", len(got.Params), got.Params)
	}
	if got.Omitted != 3 {
		t.Errorf("omitted = %d, want 3", got.Omitted)
	}
}

func TestFullSchemaKeepsEverything(t *testing.T) {
	params := []SchemaParam{{
		Name: "wrapper", Type: "object", Required: true,
		Properties: []SchemaParam{
			{Name: "a", Required: true},
			{Name: "b", Default: 1},
		},
	}}

	got := SummarizeSchema(params, true)
	if len(got.Params) != 2 {
		t.Fatalf("full=true must keep every field, got %+v", got.Params)
	}
	if got.Omitted != 0 {
		t.Errorf("full=true omits nothing, got omitted=%d", got.Omitted)
	}
}
