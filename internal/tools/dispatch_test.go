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

// A filter is a way of reaching the target, not a response-shaping knob, so
// Resolve has to accept it exactly where it accepts a positional identifier:
// when the operation declared it.
func TestDeclaredFilterArgumentIsAccepted(t *testing.T) {
	c := &Concern{
		Name: "catalog",
		Ops: []Op{
			{
				Name:    "list",
				Method:  "app.available",
				Filters: []Filter{{Arg: "category", Field: "categories", Operator: "rin"}},
			},
		},
	}
	if _, err := c.Resolve("list", Args{"category": "media"}); err != nil {
		t.Fatalf("an argument declared as a filter must be accepted: %v", err)
	}
}

// An op that never declared the argument -- as a filter or otherwise -- must
// still refuse it and name what it does accept, the same contract Resolve
// already keeps for positional arguments.
func TestUndeclaredFilterArgumentIsStillRefused(t *testing.T) {
	c := &Concern{
		Name: "catalog",
		Ops: []Op{
			{Name: "categories", Method: "app.categories"},
		},
	}
	_, err := c.Resolve("categories", Args{"category": "media"})
	if err == nil {
		t.Fatal("an argument the operation does not declare must be refused")
	}
	if !strings.Contains(err.Error(), "category") {
		t.Errorf("error must name the offending argument: %v", err)
	}
	if !strings.Contains(err.Error(), "categories") {
		t.Errorf("error must name the operation: %v", err)
	}
}

// The same argument name can mean different things on different operations:
// positional on one, a query filter on another. Each op's own declaration
// governs its own validation, and neither leaks into the other.
func TestSameArgumentNamePositionalOnOneOpAndFilterOnAnother(t *testing.T) {
	c := &Concern{
		Name: "mixed",
		Ops: []Op{
			{
				Name: "show", Method: "app.get_instance",
				Args: []string{"name"}, Required: []string{"name"},
			},
			{
				Name:    "filtered_show",
				Method:  "app.available",
				Filters: []Filter{{Arg: "name", Field: "name", Operator: "="}},
			},
		},
	}
	if _, err := c.Resolve("show", Args{"name": "plex"}); err != nil {
		t.Fatalf("the positional declaration must still resolve: %v", err)
	}
	if _, err := c.Resolve("filtered_show", Args{"name": "plex"}); err != nil {
		t.Fatalf("the filter declaration must still resolve: %v", err)
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
