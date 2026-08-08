package tools

import "testing"

func TestSafeUnroledReadsAreDiscoverable(t *testing.T) {
	for _, name := range []string{"system.version", "core.get_jobs", "core.ping"} {
		if err := CheckDiscoverable(MethodInfo{Name: name}, false); err != nil {
			t.Errorf("%s is an established harmless read and should be reachable: %v", name, err)
		}
	}
}

// The allowlist must not become a hole. Everything dangerous that happens to be
// unroled stays refused.
func TestDangerousUnroledMethodsStayRefused(t *testing.T) {
	for _, name := range []string{
		"auth.generate_token", "auth.login_with_api_key", "auth.logout",
		"core.bulk", "core.subscribe", "core.unsubscribe",
		"core.job_abort", "core.set_options", "core.job_wait",
		"core.download", "core.resize_shell",
	} {
		if err := CheckDiscoverable(MethodInfo{Name: name}, true); err == nil {
			t.Errorf("%s must stay refused even with writes enabled", name)
		}
	}
}

// Every entry carries a justification, so the list cannot grow by accident.
func TestEverySafeUnroledEntryIsJustified(t *testing.T) {
	for name, why := range safeUnroledReads {
		if why == "" {
			t.Errorf("%s has no justification recorded", name)
		}
	}
}
