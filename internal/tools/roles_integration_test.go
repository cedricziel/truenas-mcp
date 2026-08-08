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
func liveMethods(t *testing.T) map[string]methodMeta {
	t.Helper()

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
	var methods map[string]methodMeta
	if err := json.Unmarshal(raw, &methods); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return methods
}

func TestReadConcernsOnlyUseReadOnlyMethods(t *testing.T) {
	methods := liveMethods(t)

	for _, concern := range ReadConcerns() {
		for _, op := range concern.Ops {
			meta, ok := methods[op.Method]
			if !ok {
				t.Errorf("%s.%s: method %q does not exist on the target",
					concern.Name, op.Name, op.Method)
				continue
			}

			readable := IsSafeUnroledRead(op.Method)
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

// methodMeta is the slice of core.get_methods this project relies on.
type methodMeta struct {
	Roles   []string `json:"roles"`
	Job     bool     `json:"job"`
	Accepts []struct {
		Name     string `json:"_name_"`
		Required bool   `json:"_required_"`
	} `json:"accepts"`
}

// An operation whose middleware method takes a required parameter must declare
// a required argument of its own. Without one the tool accepts a call it cannot
// satisfy and the target answers null -- a silent wrong answer rather than an
// error, which is the worst failure mode available.
func TestOpsDeclareRequiredArgsTheMethodDemands(t *testing.T) {
	methods := liveMethods(t)

	for _, concern := range ReadConcerns() {
		for _, op := range concern.Ops {
			meta, ok := methods[op.Method]
			if !ok {
				continue // covered by the roles test
			}

			needed := 0
			for _, param := range meta.Accepts {
				if param.Required {
					needed++
				}
			}

			if needed > 0 && len(op.Required) == 0 {
				t.Errorf("%s.%s: method %q takes %d required parameter(s) but the "+
					"operation declares none; a call without them returns null",
					concern.Name, op.Name, op.Method, needed)
			}
			if needed == 0 && len(op.Required) > 0 {
				t.Errorf("%s.%s: method %q takes no required parameters but the "+
					"operation demands %v", concern.Name, op.Name, op.Method, op.Required)
			}
		}
	}
}
