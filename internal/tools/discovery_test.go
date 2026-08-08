package tools

import (
	"strings"
	"testing"
)

func read(name string) MethodInfo {
	return MethodInfo{Name: name, Roles: []string{"SOMETHING_READ", "READONLY_ADMIN"}}
}

func write(name string) MethodInfo {
	return MethodInfo{Name: name, Roles: []string{"SOMETHING_WRITE"}}
}

func unroled(name string) MethodInfo {
	return MethodInfo{Name: name}
}

// Discoverability is decided by the target's own RBAC metadata rather than by
// guessing from a method's name. A method is readable exactly when it grants
// READONLY_ADMIN, which is the middleware's own answer and tracks API versions
// without a code change here.
func TestReadOnlyMethodsAreDiscoverable(t *testing.T) {
	for _, name := range []string{
		"pool.query", "disk.temperatures", "filesystem.stat",
		"pool.dataset.details", "vm.get_display_devices",
	} {
		if err := CheckDiscoverable(read(name), false); err != nil {
			t.Errorf("%s grants READONLY_ADMIN and should be discoverable: %v", name, err)
		}
	}
}

func TestMutatingMethodsNeedWritesEnabled(t *testing.T) {
	m := write("app.upgrade")

	if err := CheckDiscoverable(m, false); err == nil {
		t.Fatal("a mutating method must be refused with writes disabled")
	} else if !strings.Contains(err.Error(), "write") {
		t.Errorf("the error should identify the write tier: %v", err)
	}
	if err := CheckDiscoverable(m, true); err != nil {
		t.Errorf("should be reachable with writes enabled: %v", err)
	}
}

// A method that declares no roles has no privilege check at all. On this
// target those are mostly session and protocol plumbing -- login, subscribe,
// abort, bulk execution -- not harmless reads. Treating "no roles" as "no
// risk" would be exactly backwards, so the default is refusal and the few
// genuinely harmless ones are allowed individually.
func TestUnroledMethodsAreRefusedByDefault(t *testing.T) {
	for _, name := range []string{"core.set_options", "core.subscribe", "core.job_abort"} {
		if err := CheckDiscoverable(unroled(name), true); err == nil {
			t.Errorf("%s declares no roles and must not be discoverable", name)
		}
	}
}

// core.bulk invokes arbitrary methods. Reachable, it would bypass the denylist
// and every other gate in this server, so it is refused permanently.
func TestBulkExecutionIsPermanentlyRefused(t *testing.T) {
	for _, m := range []MethodInfo{unroled("core.bulk"), read("core.bulk"), write("core.bulk")} {
		if err := CheckDiscoverable(m, true); err == nil {
			t.Fatal("core.bulk must never be reachable: it would bypass every gate")
		}
	}
}

// Authentication is the server's to manage. A caller driving it could mint a
// token that outlives the session and is invisible in the API keys UI.
func TestAuthNamespaceIsRefused(t *testing.T) {
	for _, name := range []string{
		"auth.generate_token", "auth.login_with_api_key",
		"auth.logout", "auth.terminate_other_sessions",
	} {
		if err := CheckDiscoverable(read(name), true); err == nil {
			t.Errorf("%s must not be reachable through discovery", name)
		}
	}
}

// The permanent denylist outranks the roles metadata under every configuration.
func TestDenylistOutranksRoles(t *testing.T) {
	for _, name := range []string{"pool.export", "disk.wipe", "pool.dataset.delete"} {
		if err := CheckDiscoverable(read(name), true); err == nil {
			t.Errorf("%s must be refused even if it claimed to be readable", name)
		}
	}
}

func TestRefusalDoesNotLeakMethodDetail(t *testing.T) {
	err := CheckDiscoverable(unroled("core.set_options"), false)
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if strings.Contains(strings.ToLower(err.Error()), "exists") {
		t.Errorf("refusal should not reveal whether the method exists: %v", err)
	}
}

func TestSummaryFlattensASingleObjectParameter(t *testing.T) {
	params := []SchemaParam{{
		Name: "smb_create", Type: "object", Required: true,
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
