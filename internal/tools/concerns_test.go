package tools

import (
	"strings"
	"testing"
)

func TestConcernsAreWellFormed(t *testing.T) {
	for _, c := range ReadConcerns() {
		t.Run(c.Name, func(t *testing.T) {
			if c.Description == "" {
				t.Error("concern needs a description")
			}
			if len(c.Ops) == 0 {
				t.Fatal("concern needs at least one operation")
			}

			seen := map[string]bool{}
			for _, op := range c.Ops {
				if seen[op.Name] {
					t.Errorf("duplicate operation %q", op.Name)
				}
				seen[op.Name] = true

				if op.Summary == "" {
					t.Errorf("operation %q needs a summary: it is what a model selects on", op.Name)
				}
				if op.Method == "" {
					t.Errorf("operation %q has no middleware method", op.Name)
				}

				// A required argument the operation does not accept can never
				// be satisfied, so Resolve would reject every call.
				accepts := map[string]bool{}
				for _, a := range op.Args {
					accepts[a] = true
				}
				for _, r := range op.Required {
					if !accepts[r] {
						t.Errorf("operation %q requires %q but does not accept it", op.Name, r)
					}
				}
			}
		})
	}
}

// The read surface must stay navigable. The original budget was four concerns,
// raised to eight as coverage grew: what protects tool-selection accuracy is
// names mapping to distinct domains, not a small count. "Is my backup running"
// and "what VMs exist" are not questions a model confuses, whereas folding them
// into one tool with a thirty-value op enum would hurt exactly the accuracy the
// budget exists to defend.
func TestReadSurfaceStaysNavigable(t *testing.T) {
	if n := len(ReadConcerns()); n > 8 {
		t.Fatalf("%d read concerns; beyond 8 the list stops being scannable", n)
	}
}

// Concern names must be distinct domains, since that is what makes a larger
// surface navigable rather than confusing.
func TestConcernNamesAreDistinct(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range ReadConcerns() {
		if seen[c.Name] {
			t.Errorf("duplicate concern name %q", c.Name)
		}
		seen[c.Name] = true
		if c.Title == "" {
			t.Errorf("concern %q has no title", c.Name)
		}
	}
}

// Past roughly ten operations with divergent arguments a dispatch tool stops
// paying for itself and should be split.
func TestOpEnumsStayNarrow(t *testing.T) {
	for _, c := range ReadConcerns() {
		if n := len(c.Ops); n > 10 {
			t.Errorf("concern %q has %d operations; beyond ~10 dispatch stops paying", c.Name, n)
		}
	}
}

// Every operation in a read concern must map to a non-mutating method. This is
// asserted structurally here and against the live target's own RBAC metadata in
// the integration suite.
func TestReadOpsUseNoObviouslyMutatingMethod(t *testing.T) {
	mutating := []string{
		".create", ".update", ".delete", ".start", ".stop", ".restart",
		".upgrade", ".rollback", ".pull_images", ".redeploy", ".export", ".wipe",
	}
	for _, c := range ReadConcerns() {
		for _, op := range c.Ops {
			for _, m := range mutating {
				if strings.HasSuffix(op.Method, m) {
					t.Errorf("read concern %q exposes mutating method %q via op %q",
						c.Name, op.Method, op.Name)
				}
			}
		}
	}
}

// The driving workflow needs these reads to decide whether to act at all.
func TestAppsExposesLifecycleReads(t *testing.T) {
	names := map[string]bool{}
	for _, n := range Apps().OpNames() {
		names[n] = true
	}
	for _, required := range []string{"outdated_images", "upgrade_summary", "rollback_versions"} {
		if !names[required] {
			t.Errorf("apps concern must expose %q so a caller can decide before mutating", required)
		}
	}
}
