package tools

import (
	"strings"
	"testing"
)

func testConcern() *Concern {
	return &Concern{
		Name:        "storage",
		Description: "storage reads",
		Ops: []Op{
			{Name: "list_pools", Method: "pool.query", Summary: "list pools"},
			{Name: "show_pool", Method: "pool.get_instance", Summary: "show one pool",
				Args: []string{"id"}, Required: []string{"id"}},
			{Name: "list_datasets", Method: "pool.dataset.query", Summary: "list datasets", Args: []string{"limit"}},
		},
	}
}

func TestOpNamesAreEnumerated(t *testing.T) {
	got := testConcern().OpNames()
	want := []string{"list_pools", "show_pool", "list_datasets"}
	if len(got) != len(want) {
		t.Fatalf("OpNames() = %v, want %v", got, want)
	}
}

func TestUnknownOpIsRejectedAndListsValidOnes(t *testing.T) {
	_, err := testConcern().Resolve("list_poolz", Args{})
	if err == nil {
		t.Fatal("an unknown op must be rejected")
	}
	// A model that guessed wrong should be able to recover in one turn.
	for _, name := range []string{"list_pools", "show_pool"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error must list valid operations, missing %q: %v", name, err)
		}
	}
}

func TestMissingRequiredArgIsNamed(t *testing.T) {
	_, err := testConcern().Resolve("show_pool", Args{})
	if err == nil {
		t.Fatal("show_pool requires an id")
	}
	if !strings.Contains(err.Error(), "id") {
		t.Errorf("error must name the missing argument: %v", err)
	}
	if !strings.Contains(err.Error(), "show_pool") {
		t.Errorf("error must name the operation it applies to: %v", err)
	}
}

// The flat-superset argument shape means a model can pass an argument that
// belongs to a different operation. Silently ignoring it would produce a
// confidently wrong answer, so it is refused.
func TestIrrelevantArgIsRefused(t *testing.T) {
	_, err := testConcern().Resolve("list_pools", Args{"id": "tank"})
	if err == nil {
		t.Fatal("an argument that does not apply to the operation must be refused")
	}
	if !strings.Contains(err.Error(), "id") {
		t.Errorf("error must name the offending argument: %v", err)
	}
	if !strings.Contains(err.Error(), "list_pools") {
		t.Errorf("error must name the operation: %v", err)
	}
}

func TestValidCallResolves(t *testing.T) {
	op, err := testConcern().Resolve("show_pool", Args{"id": "tank"})
	if err != nil {
		t.Fatalf("valid call should resolve: %v", err)
	}
	if op.Method != "pool.get_instance" {
		t.Fatalf("resolved to %q", op.Method)
	}
}

func TestOptionalArgMayBeOmitted(t *testing.T) {
	if _, err := testConcern().Resolve("list_datasets", Args{}); err != nil {
		t.Fatalf("optional arguments may be omitted: %v", err)
	}
}

// The description is what a model reads to pick an operation, so every op has
// to contribute one.
func TestDescriptionListsEveryOp(t *testing.T) {
	desc := testConcern().ToolDescription()
	for _, name := range testConcern().OpNames() {
		if !strings.Contains(desc, name) {
			t.Errorf("tool description omits %q", name)
		}
	}
}
