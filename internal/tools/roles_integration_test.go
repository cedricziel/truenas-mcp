//go:build integration

package tools

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/cedricziel/truenas-mcp/internal/truenas"
)

// The middleware declares an RBAC role requirement per method, and a method is
// non-mutating exactly when it grants READONLY_ADMIN. Deriving the read/write
// split from that is better than maintaining a list here: it is the target's
// own answer, and it tracks API versions without a code change.
func TestReadConcernsOnlyUseReadOnlyMethods(t *testing.T) {
	url := os.Getenv("TRUENAS_TEST_URL")
	key := os.Getenv("TRUENAS_TEST_API_KEY")
	if url == "" || key == "" {
		t.Skip("set TRUENAS_TEST_URL and TRUENAS_TEST_API_KEY to run integration tests")
	}

	ctx := context.Background()
	c, err := truenas.Dial(ctx, truenas.Options{
		URL:                url,
		InsecureSkipVerify: os.Getenv("TRUENAS_TEST_INSECURE") == "true",
		DialTimeout:        15 * time.Second,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()

	if err := c.Login(ctx, key); err != nil {
		t.Fatalf("login: %v", err)
	}

	raw, err := c.Call(ctx, "core.get_methods")
	if err != nil {
		t.Fatalf("core.get_methods: %v", err)
	}
	var methods map[string]struct {
		Roles []string `json:"roles"`
		Job   bool     `json:"job"`
	}
	if err := json.Unmarshal(raw, &methods); err != nil {
		t.Fatalf("decode: %v", err)
	}

	for _, concern := range ReadConcerns() {
		for _, op := range concern.Ops {
			meta, ok := methods[op.Method]
			if !ok {
				t.Errorf("%s.%s: method %q does not exist on the target",
					concern.Name, op.Name, op.Method)
				continue
			}

			readable := false
			for _, r := range meta.Roles {
				if r == "READONLY_ADMIN" {
					readable = true
					break
				}
			}
			if !readable {
				t.Errorf("%s.%s: method %q does not grant READONLY_ADMIN (roles=%v); "+
					"it must not be reachable from a read tool",
					concern.Name, op.Name, op.Method, meta.Roles)
			}
		}
	}
}
