//go:build integration

// These tests run against a real TrueNAS instance and are excluded from the
// default suite. Run them with:
//
//	TRUENAS_TEST_URL=wss://host/api/current \
//	TRUENAS_TEST_API_KEY=$(op read "op://Private/truenas-mcp integration key (hive)/password") \
//	go test -tags=integration ./internal/truenas/
//
// They exist because the fake middleware encodes this project's belief about
// how the API behaves; only a live box can falsify that belief.
package truenas

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func liveOptions(t *testing.T) (Options, string) {
	t.Helper()

	url := os.Getenv("TRUENAS_TEST_URL")
	key := os.Getenv("TRUENAS_TEST_API_KEY")
	if url == "" || key == "" {
		t.Skip("set TRUENAS_TEST_URL and TRUENAS_TEST_API_KEY to run integration tests")
	}
	insecure := os.Getenv("TRUENAS_TEST_INSECURE") == "true"

	return Options{URL: url, InsecureSkipVerify: insecure, DialTimeout: 15 * time.Second}, key
}

func liveClient(t *testing.T) *Client {
	t.Helper()

	opts, key := liveOptions(t)
	c, err := Dial(context.Background(), opts)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	if err := c.Login(context.Background(), key); err != nil {
		t.Fatalf("login: %v", err)
	}
	return c
}

func TestLiveLoginAndVersion(t *testing.T) {
	c := liveClient(t)

	raw, err := c.Call(context.Background(), "system.version")
	if err != nil {
		t.Fatalf("system.version: %v", err)
	}
	var version string
	if err := json.Unmarshal(raw, &version); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(version, "TrueNAS-") {
		t.Fatalf("unexpected version string %q", version)
	}
	t.Logf("target version: %s", version)
}

// The key alone must be sufficient. If the middleware ever required a
// username this would fail, and the connection spec would be wrong.
func TestLiveLoginTakesOnlyTheKey(t *testing.T) {
	opts, key := liveOptions(t)

	c, err := Dial(context.Background(), opts)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()

	if err := c.Login(context.Background(), key); err != nil {
		t.Fatalf("login with the key alone should succeed: %v", err)
	}
}

func TestLiveRejectsBadKey(t *testing.T) {
	opts, _ := liveOptions(t)

	c, err := Dial(context.Background(), opts)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()

	err = c.Login(context.Background(), "1-thiskeyisnotvalid")
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("want ErrUnauthenticated, got %v", err)
	}
}

// Concurrency on one connection is the property most likely to break against
// a real server, since the fake replies in lockstep.
func TestLiveConcurrentCalls(t *testing.T) {
	c := liveClient(t)

	const n = 8
	errs := make(chan error, n)
	for range n {
		go func() {
			_, err := c.Call(context.Background(), "system.version")
			errs <- err
		}()
	}
	for range n {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent call: %v", err)
		}
	}
}

// The driving workflow: identify the app, then confirm the method that pulls
// its images is reachable. Nothing here mutates the target.
func TestLiveAppQuery(t *testing.T) {
	c := liveClient(t)

	raw, err := c.Call(context.Background(), "app.query")
	if err != nil {
		t.Fatalf("app.query: %v", err)
	}
	var apps []struct {
		Name  string `json:"name"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(raw, &apps); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(apps) == 0 {
		t.Skip("no apps installed on the target")
	}
	t.Logf("%d apps installed; first: %s (%s)", len(apps), apps[0].Name, apps[0].State)
}

// Introspection is what the discovery tier and the roles-based read/write
// split both depend on, so its shape is asserted rather than assumed.
func TestLiveMethodIntrospection(t *testing.T) {
	c := liveClient(t)

	raw, err := c.Call(context.Background(), "core.get_methods")
	if err != nil {
		t.Fatalf("core.get_methods: %v", err)
	}
	var methods map[string]struct {
		Job        bool     `json:"job"`
		Roles      []string `json:"roles"`
		Filterable bool     `json:"filterable"`
	}
	if err := json.Unmarshal(raw, &methods); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(methods) < 100 {
		t.Fatalf("expected a large method surface, got %d", len(methods))
	}

	pullImages, ok := methods["app.pull_images"]
	if !ok {
		t.Fatal("app.pull_images must exist: it is the driving workflow")
	}
	if !pullImages.Job {
		t.Error("app.pull_images must be job-typed")
	}

	poolQuery, ok := methods["pool.query"]
	if !ok {
		t.Fatal("pool.query must exist")
	}
	readable := false
	for _, r := range poolQuery.Roles {
		if r == "READONLY_ADMIN" {
			readable = true
		}
	}
	if !readable {
		t.Error("pool.query must grant READONLY_ADMIN: the read/write split depends on it")
	}

	t.Logf("%d methods; app.pull_images job=%v; pool.query roles=%v",
		len(methods), pullImages.Job, poolQuery.Roles)
}
