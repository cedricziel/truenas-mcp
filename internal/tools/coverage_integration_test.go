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

// Reports how much of the target's API this server can actually reach. It is a
// coverage report rather than a pass/fail assertion -- the number is a design
// input, not a contract -- but it fails if reachability collapses, which would
// mean the roles metadata changed shape.
func TestDiscoveryCoverage(t *testing.T) {
	url, key := os.Getenv("TRUENAS_TEST_URL"), os.Getenv("TRUENAS_TEST_API_KEY")
	if url == "" || key == "" {
		t.Skip("needs a live target")
	}
	ctx := context.Background()
	c, err := truenas.Dial(ctx, truenas.Options{URL: url, InsecureSkipVerify: true, DialTimeout: 15 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if err := c.Login(ctx, key); err != nil {
		t.Fatal(err)
	}
	raw, err := c.Call(ctx, "core.get_methods")
	if err != nil {
		t.Fatal(err)
	}
	var methods map[string]struct {
		Roles []string `json:"roles"`
		Job   bool     `json:"job"`
	}
	if err := json.Unmarshal(raw, &methods); err != nil {
		t.Fatal(err)
	}

	var total, readable, writable, refused int
	for name, m := range methods {
		total++
		info := MethodInfo{Name: name, Roles: m.Roles, Job: m.Job}
		switch {
		case CheckDiscoverable(info, false) == nil:
			readable++
		case CheckDiscoverable(info, true) == nil:
			writable++
		default:
			refused++
		}
	}

	t.Logf("total=%d  readable=%d  writable=%d  refused=%d  reachable=%.0f%%",
		total, readable, writable, refused,
		float64(readable+writable)/float64(total)*100)

	if readable < 300 {
		t.Errorf("only %d readable methods; the roles metadata may have changed shape", readable)
	}
}
