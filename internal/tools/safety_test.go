package tools

import (
	"strings"
	"testing"
)

// Some operations are unrecoverable and no configuration may reach them. If a
// user needs these, the TrueNAS web interface is the correct place.
func TestUnrecoverableMethodsAreDenied(t *testing.T) {
	for _, method := range []string{
		"pool.export",
		"disk.wipe",
		"pool.dataset.delete",
		"boot.detach",
	} {
		if err := CheckDenied(method, nil); err == nil {
			t.Errorf("%s must be denied", method)
		}
	}
}

func TestOrdinaryMethodsAreAllowed(t *testing.T) {
	for _, method := range []string{
		"pool.query",
		"app.pull_images",
		"app.start",
		"pool.snapshot.create",
	} {
		if err := CheckDenied(method, nil); err != nil {
			t.Errorf("%s should be allowed: %v", method, err)
		}
	}
}

// The denylist constrains argument values, not only method names: deleting an
// app is recoverable, deleting it along with its volumes is not, and those are
// the same method.
func TestDeniedArgumentCombination(t *testing.T) {
	err := CheckDenied("app.delete", map[string]any{"remove_ixvolumes": true})
	if err == nil {
		t.Fatal("removing an app's volumes destroys data and must be denied")
	}
	if !strings.Contains(err.Error(), "remove_ixvolumes") {
		t.Errorf("error must name the offending argument: %v", err)
	}
}

func TestSameMethodAllowedInItsSafeForm(t *testing.T) {
	if err := CheckDenied("app.delete", map[string]any{"remove_images": true}); err != nil {
		t.Errorf("deleting an app without its volumes is recoverable: %v", err)
	}
	if err := CheckDenied("app.delete", nil); err != nil {
		t.Errorf("deleting an app with no options is recoverable: %v", err)
	}
}

func TestDeniedArgumentOnlyWhenTrue(t *testing.T) {
	if err := CheckDenied("app.delete", map[string]any{"remove_ixvolumes": false}); err != nil {
		t.Errorf("explicitly declining volume removal is not destruction: %v", err)
	}
}

// A refusal should say where to go, since the operation is legitimate --
// it is just not something this server will do on a model's say-so.
func TestDenialExplainsWhereToGo(t *testing.T) {
	err := CheckDenied("pool.export", nil)
	if err == nil {
		t.Fatal("expected a denial")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "web interface") {
		t.Errorf("denial should direct the user elsewhere: %v", err)
	}
}

func TestRecursiveDatasetDeletionIsDenied(t *testing.T) {
	// Dataset deletion is denied outright, so recursion is moot -- but the
	// argument-level rule must hold if the method is ever permitted.
	if err := CheckDenied("pool.dataset.delete", map[string]any{"recursive": true}); err == nil {
		t.Error("recursive dataset deletion must be denied")
	}
}
