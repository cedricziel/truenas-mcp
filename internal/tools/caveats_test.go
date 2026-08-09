package tools

import (
	"strings"
	"testing"
)

// app.update's own description on the target is "Update `app_name` app with
// new configuration" -- nothing there says the configuration must be
// complete, and a caller who sends only the fields they meant to change loses
// the rest. The caveat exists to say what the description does not.
func TestMethodCaveatDescribesAppUpdateAsReplaceNotPatch(t *testing.T) {
	caveat := MethodCaveat("app.update")
	if caveat == "" {
		t.Fatal("app.update must carry a caveat: its description does not say the config must be complete")
	}

	lower := strings.ToLower(caveat)
	if !strings.Contains(lower, "patch") {
		t.Errorf("caveat must say this replaces rather than patches the config, got: %s", caveat)
	}
	if !strings.Contains(lower, "whole") && !strings.Contains(lower, "full") && !strings.Contains(lower, "complete") {
		t.Errorf("caveat must say the complete config is required, got: %s", caveat)
	}

	// A warning alone leaves the caller to work out what to do instead, which
	// is the state they were already in. These two name the recovery: where the
	// complete object comes from, and which key it goes back under.
	if !strings.Contains(lower, `apps(op="config"`) {
		t.Errorf("caveat must point at the read that produces the complete config, got: %s", caveat)
	}
	if !strings.Contains(lower, "values") {
		t.Errorf("caveat must name the argument the config is passed back as, got: %s", caveat)
	}
}

// The table is deliberately short: most methods carry no caveat at all, and
// that must come back as "", not a zero-value struct or a placeholder string.
func TestMethodCaveatIsEmptyForUncaveatedMethods(t *testing.T) {
	if c := MethodCaveat("pool.query"); c != "" {
		t.Errorf("pool.query has no established caveat, got: %q", c)
	}
}
