package tools

import (
	"strings"
	"testing"
)

func TestConfigWritesAreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, w := range ConfigWrites() {
		if seen[w.Name] {
			t.Errorf("duplicate config write %q", w.Name)
		}
		seen[w.Name] = true

		for _, field := range []struct {
			name string
			val  string
		}{{"Title", w.Title}, {"Description", w.Description}, {"Method", w.Method}} {
			if field.val == "" {
				t.Errorf("%s: %s is empty", w.Name, field.name)
			}
		}
		if !w.NeedsID && !w.NeedsConfig {
			t.Errorf("%s takes neither an id nor a config; it cannot act on anything", w.Name)
		}
		if err := CheckDenied(w.Method, nil); err != nil {
			t.Errorf("%s maps to a denied method: %v", w.Name, err)
		}
	}
}

// Creating a share adds access; changing or removing one takes it away from
// whoever was using it.
func TestConfigWriteDestructiveFlagsMatchEffect(t *testing.T) {
	byName := map[string]ConfigWriteOp{}
	for _, w := range ConfigWrites() {
		byName[w.Name] = w
	}

	for _, n := range []string{"create_smb_share", "create_nfs_export"} {
		if byName[n].Destructive {
			t.Errorf("%s only adds access and must not be marked destructive", n)
		}
	}
	for _, n := range []string{
		"update_smb_share", "delete_smb_share", "update_nfs_export",
		"delete_nfs_export", "set_smb_share_acl", "set_path_acl",
	} {
		if !byName[n].Destructive {
			t.Errorf("%s can remove someone's access and must be marked destructive", n)
		}
	}
}

// An NFS export with no network restriction is reachable by anyone who can
// route to the box, which is the mistake this description exists to prevent.
func TestNFSDescriptionWarnsAboutOpenExports(t *testing.T) {
	for _, w := range ConfigWrites() {
		if w.Name != "create_nfs_export" {
			continue
		}
		if !strings.Contains(w.Description, "networks") {
			t.Error("the NFS description should tell the caller how to restrict access")
		}
		return
	}
	t.Fatal("create_nfs_export is missing")
}

// The ACL tool must say why recursion is unavailable, so a caller does not
// waste a turn discovering it.
func TestPathACLDescriptionExplainsTheLimit(t *testing.T) {
	for _, w := range ConfigWrites() {
		if w.Name != "set_path_acl" {
			continue
		}
		if !strings.Contains(w.Description, "recursive") {
			t.Error("set_path_acl should state that recursive application is refused")
		}
		return
	}
	t.Fatal("set_path_acl is missing")
}
