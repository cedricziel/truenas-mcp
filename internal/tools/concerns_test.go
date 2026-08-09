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
				// be satisfied, so Resolve would reject every call. Accepted
				// covers both roles an argument can be declared under -- Args
				// for a positional identifier, Filters for a query filter --
				// since Resolve itself accepts either.
				accepts := map[string]bool{}
				for _, a := range op.Args {
					accepts[a] = true
				}
				for _, f := range op.Filters {
					accepts[f.Arg] = true
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

// The correlation between a container's tag and an image's digest, and the
// choice between this op and outdated_images, have to live in the Summary
// itself -- that is the only thing a model reads before picking an op.
func TestAppsExposesImageDigests(t *testing.T) {
	images := findOp(Apps(), "images")
	if images == nil {
		t.Fatal(`apps concern must expose an "images" op resolving what a container actually runs`)
	}
	if images.Method != "app.image.query" {
		t.Errorf("images op method = %q, want app.image.query", images.Method)
	}
	if len(images.Required) != 0 {
		t.Errorf("images op requires %v, but app.image.query takes no name filter", images.Required)
	}

	for _, term := range []string{"repo_tags", "update_available", "outdated_images"} {
		if !strings.Contains(images.Summary, term) {
			t.Errorf("images op summary must mention %q so the correlation is discoverable, got: %s", term, images.Summary)
		}
	}

	want := map[string]bool{"id": true, "repo_tags": true, "repo_digests": true, "update_available": true}
	if len(images.Project) != len(want) {
		t.Fatalf("images op projects %v, want exactly %d fields", images.Project, len(want))
	}
	for _, f := range images.Project {
		if !want[f] {
			t.Errorf("images op projects unexpected field %q", f)
		}
	}
}

// app.update replaces the whole config rather than patching it -- a caveat
// that lives on the middleware method, not on this read. A caller who reads
// the config here should learn the contract for feeding it back in the same
// place, rather than discovering it only after describe_method or a bad call.
func TestAppsConfigSummaryPointsToUpdateContract(t *testing.T) {
	config := findOp(Apps(), "config")
	if config == nil {
		t.Fatal("apps concern must expose a config op")
	}
	if !strings.Contains(config.Summary, "app.update") {
		t.Errorf("config op summary must mention app.update, so reading the config also teaches "+
			"its update contract, got: %s", config.Summary)
	}
	// Naming the method without stating the contract would just send the caller
	// to describe_method for the half that matters.
	if !strings.Contains(config.Summary, "values") {
		t.Errorf("config op summary must name the argument this object goes back as, got: %s", config.Summary)
	}
	if !strings.Contains(config.Summary, "full") || !strings.Contains(config.Summary, "patch") {
		t.Errorf("config op summary must say the object goes back in full rather than as a patch, got: %s",
			config.Summary)
	}
}

func findOp(c *Concern, name string) *Op {
	for i := range c.Ops {
		if c.Ops[i].Name == name {
			return &c.Ops[i]
		}
	}
	return nil
}

// Each of the 50 items app.query returns by default was a whole app.query
// object -- roughly 4KB apiece, almost all of it active_workloads -- which is
// what blew a caller's token budget. The projection names the fields that
// answer "what is installed, what state is it in, and does it need updating".
// The last of those is not optional: the concern describes itself as covering
// what needs updating, so a summary that cannot answer it forces a second
// full-object call to learn what the first one already knew.
func TestAppsListProjectsStateAndUpdateStatus(t *testing.T) {
	list := findOp(Apps(), "list")
	if list == nil {
		t.Fatal("apps concern must expose a list op")
	}

	want := map[string]bool{
		"name": true, "state": true, "version": true,
		"upgrade_available": true, "image_updates_available": true,
	}
	if len(list.Project) != len(want) {
		t.Fatalf("list op projects %v, want exactly %d fields", list.Project, len(want))
	}
	for _, f := range list.Project {
		if !want[f] {
			t.Errorf("list op projects unexpected field %q", f)
		}
	}
}
