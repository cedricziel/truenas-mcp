package tools

import (
	"strings"
	"testing"
)

func TestWriteOpsAreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, w := range WriteOps() {
		if seen[w.Name] {
			t.Errorf("duplicate write tool %q", w.Name)
		}
		seen[w.Name] = true

		if w.Description == "" {
			t.Errorf("%s needs a description", w.Name)
		}
		if w.Method == "" {
			t.Errorf("%s has no middleware method", w.Name)
		}
		// A mutation must resolve to one concrete object before it runs, so
		// consent is given against a specific thing rather than a name.
		if w.TargetArg == "" {
			t.Errorf("%s must name the object it acts on", w.Name)
		}
	}
}

// Rollback is what makes the rest of the app write surface defensible, so it
// must ship whenever any of them does.
func TestRollbackShipsWithAppWrites(t *testing.T) {
	names := map[string]bool{}
	for _, w := range AppWrites() {
		names[w.Name] = true
	}
	if !names[RollbackToolName] {
		t.Fatalf("%s must be present whenever app mutations are exposed", RollbackToolName)
	}
}

// v1 excludes anything whose blast radius is data rather than a service.
func TestV1ExcludesDataDestruction(t *testing.T) {
	for _, w := range WriteOps() {
		for _, forbidden := range []string{"delete", "destroy", "wipe", "export"} {
			if strings.Contains(w.Name, forbidden) {
				t.Errorf("%s is out of scope for v1: its blast radius is data", w.Name)
			}
		}
		if err := CheckDenied(w.Method, nil); err != nil {
			t.Errorf("%s maps to a denied method: %v", w.Name, err)
		}
	}
}

func TestDestructiveOpsAreMarked(t *testing.T) {
	byName := map[string]WriteOp{}
	for _, w := range WriteOps() {
		byName[w.Name] = w
	}

	// Interrupting a service counts as destructive: clients use the hint to
	// decide whether to prompt, and a silent restart of someone's media server
	// is not something to do unannounced.
	for _, name := range []string{"app_stop", "app_redeploy", "app_pull_images", "app_upgrade"} {
		if !byName[name].Destructive {
			t.Errorf("%s interrupts a service and must be marked destructive", name)
		}
	}

	// Additive operations must not be, or every call prompts and users stop
	// reading the prompts.
	for _, name := range []string{"app_start", "create_snapshot"} {
		if byName[name].Destructive {
			t.Errorf("%s is additive and must not be marked destructive", name)
		}
	}
}

func TestPullImagesCarriesRedeployOption(t *testing.T) {
	for _, w := range AppWrites() {
		if w.Name != "app_pull_images" {
			continue
		}
		for _, opt := range w.Options {
			if opt == "redeploy" {
				return
			}
		}
		t.Fatal("app_pull_images must expose the redeploy option")
	}
	t.Fatal("app_pull_images is missing")
}
