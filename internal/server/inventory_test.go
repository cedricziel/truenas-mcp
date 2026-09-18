package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newInventoryTarget answers every inventory section with a small, realistic
// payload in the shape the middleware actually returns.
func newInventoryTarget(t *testing.T) *fakeTarget {
	t.Helper()
	f := newFakeTarget(t)
	f.respond("system.info", map[string]any{
		"version": "TrueNAS-26.04.0", "hostname": "hive", "uptime": "3 days, 2:10:05", "physical_cores": 8,
	})
	f.respond("pool.query", []map[string]any{
		{"name": "tank", "status": "ONLINE", "healthy": true, "size": 4000000000000, "allocated": 3600000000000, "free": 400000000000},
		{"name": "scratch", "status": "DEGRADED", "healthy": false, "size": 1000, "allocated": 100, "free": 900},
	})
	f.respond("pool.dataset.query", []map[string]any{
		{"name": "tank/media", "pool": "tank", "type": "FILESYSTEM", "used": map[string]any{"parsed": 1024, "rawvalue": "1K"}, "available": map[string]any{"parsed": 2048}},
		{"name": "tank", "pool": "tank", "type": "FILESYSTEM", "used": map[string]any{"parsed": 4096}, "available": map[string]any{"parsed": 2048}},
	})
	f.respond("app.query", []map[string]any{
		{"name": "paperless", "state": "RUNNING", "version": "2.1.0", "upgrade_available": true, "image_updates_available": false, "active_workloads": map[string]any{"big": "blob"}},
	})
	f.respond("vm.query", []map[string]any{
		{"name": "win11", "vcpus": 2, "cores": 2, "memory": 8192, "status": map[string]any{"state": "RUNNING", "pid": 123}},
	})
	f.respond("container.query", []map[string]any{
		{"name": "lxc-dev", "status": map[string]any{"state": "STOPPED"}},
		{"name": "lxc-old", "status": "RUNNING"},
	})
	f.respond("sharing.smb.query", []map[string]any{{"name": "media", "path": "/mnt/tank/media", "enabled": true}})
	f.respond("sharing.nfs.query", []map[string]any{{"comment": "backups", "path": "/mnt/tank/backups", "enabled": false}})
	f.respond("alert.list", []map[string]any{
		{"level": "WARNING", "formatted": "Pool scratch is DEGRADED", "datetime": map[string]any{"$date": 1757980800000}},
	})
	return f
}

func callInventory(t *testing.T, target *fakeTarget) map[string]any {
	t.Helper()
	client := discoveryClient(t, discoverySession(t, target), false)
	res, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: "inventory", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("call inventory: %v", err)
	}
	if res.IsError {
		t.Fatalf("inventory returned an error: %v", res.Content)
	}
	out, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("inventory returned no structured content: %#v", res)
	}
	return out
}

func items(t *testing.T, out map[string]any, key string) []map[string]any {
	t.Helper()
	list, ok := out[key].([]any)
	if !ok {
		t.Fatalf("%s = %#v, want a list", key, out[key])
	}
	var result []map[string]any
	for _, it := range list {
		result = append(result, it.(map[string]any))
	}
	return result
}

func TestInventoryAssemblesEverySection(t *testing.T) {
	out := callInventory(t, newInventoryTarget(t))

	if _, failed := out["errors"]; failed {
		t.Fatalf("no section should fail: %v", out["errors"])
	}
	host := out["host"].(map[string]any)
	if host["hostname"] != "hive" || host["version"] != "TrueNAS-26.04.0" {
		t.Errorf("host = %#v", host)
	}

	pools := items(t, out, "pools")
	if len(pools) != 2 || pools[0]["name"] != "tank" || pools[0]["allocated"] != float64(3600000000000) {
		t.Errorf("pools = %#v", pools)
	}
	if pools[1]["healthy"] != false || pools[1]["status"] != "DEGRADED" {
		t.Errorf("degraded pool not reported as such: %#v", pools[1])
	}

	// Datasets are sorted by name and reduced to byte counts.
	datasets := items(t, out, "datasets")
	if len(datasets) != 2 || datasets[0]["name"] != "tank" || datasets[1]["name"] != "tank/media" {
		t.Errorf("datasets = %#v", datasets)
	}
	if datasets[1]["used"] != float64(1024) || datasets[1]["available"] != float64(2048) {
		t.Errorf("dataset space not flattened to bytes: %#v", datasets[1])
	}
	if _, truncated := out["datasets_truncated"]; truncated {
		t.Error("two datasets is not a truncated list")
	}

	apps := items(t, out, "apps")
	if len(apps) != 1 || apps[0]["upgrade_available"] != true {
		t.Errorf("apps = %#v", apps)
	}
	if _, leaked := apps[0]["active_workloads"]; leaked {
		t.Error("the app payload was passed through instead of projected")
	}

	vms := items(t, out, "vms")
	if len(vms) != 1 || vms[0]["state"] != "RUNNING" || vms[0]["vcpus"] != float64(4) || vms[0]["memory_mib"] != float64(8192) {
		t.Errorf("vms = %#v", vms)
	}

	containers := items(t, out, "containers")
	if len(containers) != 2 || containers[0]["state"] != "STOPPED" || containers[1]["state"] != "RUNNING" {
		t.Errorf("container state should decode from both shapes: %#v", containers)
	}

	shares := items(t, out, "shares")
	if len(shares) != 2 {
		t.Fatalf("shares = %#v", shares)
	}
	if shares[0]["kind"] != "smb" || shares[0]["name"] != "media" || shares[1]["kind"] != "nfs" || shares[1]["enabled"] != false {
		t.Errorf("shares = %#v", shares)
	}

	alerts := items(t, out, "alerts")
	if len(alerts) != 1 || alerts[0]["level"] != "WARNING" || alerts[0]["since"] != "2025-09-16T00:00:00Z" {
		t.Errorf("alerts = %#v", alerts)
	}

	if _, err := time.Parse(time.RFC3339, out["generated_at"].(string)); err != nil {
		t.Errorf("generated_at is not RFC 3339: %v", out["generated_at"])
	}
}

// The dataset query must ask for the flat, property-limited shape: the
// default nests children and carries every ZFS property, which is the shape
// the read tools already had to project away.
func TestInventoryAsksForFlatDatasets(t *testing.T) {
	target := newInventoryTarget(t)
	callInventory(t, target)

	raw, ok := target.lastParams("pool.dataset.query")
	if !ok {
		t.Fatal("pool.dataset.query was never called")
	}
	var params []json.RawMessage
	if err := json.Unmarshal(raw, &params); err != nil || len(params) != 2 {
		t.Fatalf("params = %s", raw)
	}
	var options struct {
		Extra struct {
			Flat       bool     `json:"flat"`
			Properties []string `json:"properties"`
		} `json:"extra"`
	}
	if err := json.Unmarshal(params[1], &options); err != nil {
		t.Fatal(err)
	}
	if !options.Extra.Flat || len(options.Extra.Properties) == 0 {
		t.Errorf("dataset query options = %s, want flat with a property list", params[1])
	}
}

func TestInventoryBoundsDatasets(t *testing.T) {
	target := newInventoryTarget(t)
	var many []map[string]any
	for i := 0; i < datasetLimit+5; i++ {
		many = append(many, map[string]any{"name": fmt.Sprintf("tank/d%03d", i), "pool": "tank", "type": "FILESYSTEM"})
	}
	target.respond("pool.dataset.query", many)

	out := callInventory(t, target)
	if n := len(items(t, out, "datasets")); n != datasetLimit {
		t.Errorf("datasets = %d, want %d", n, datasetLimit)
	}
	if out["datasets_truncated"] != true {
		t.Error("a cut-short dataset list must say so")
	}
}

func itoa(i int) string {
	return strings.TrimSpace(strings.Repeat(" ", 0) + json.Number(fmtInt(i)).String())
}

func fmtInt(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

// A key that may read storage but not apps still gets its storage, with the
// refusal named against the section it belongs to.
func TestInventoryReportsRefusedSectionsWithoutFailing(t *testing.T) {
	target := newInventoryTarget(t)
	target.fail("app.query", -32001, "Not authorized")
	target.fail("sharing.nfs.query", -32001, "Not authorized")

	out := callInventory(t, target)

	errs, ok := out["errors"].(map[string]any)
	if !ok {
		t.Fatalf("errors = %#v", out["errors"])
	}
	if msg, _ := errs["apps"].(string); !strings.Contains(msg, "not permitted") {
		t.Errorf("apps error = %q, want a plain statement that the key lacks the privilege", msg)
	}
	// One protocol failing leaves the other's shares in place, and the
	// failure is reported under the key the view renders.
	if msg, _ := errs["shares"].(string); !strings.HasPrefix(msg, "nfs:") {
		t.Errorf("shares error = %q, want it attributed to nfs", msg)
	}
	if shares := items(t, out, "shares"); len(shares) != 1 || shares[0]["kind"] != "smb" {
		t.Errorf("smb shares should survive an nfs refusal: %#v", shares)
	}
	if _, present := out["apps"]; !present {
		t.Error("a failed section should still be present as an empty list so the view can render it")
	}
	if pools := items(t, out, "pools"); len(pools) != 2 {
		t.Errorf("pools should be unaffected by the app refusal: %#v", pools)
	}
}

func TestInventoryFailsOnlyWhenNothingCanBeRead(t *testing.T) {
	target := newFakeTarget(t)
	for _, s := range inventorySections() {
		target.fail(s.method, -32001, "Not authorized")
	}

	client := discoveryClient(t, discoverySession(t, target), false)
	res, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: "inventory", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("transport error: %v", err)
	}
	if !res.IsError {
		t.Fatal("an inventory with no readable section should be an error, not an empty success")
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "no inventory section could be read") {
		t.Errorf("error = %q", text)
	}
}

// A section whose payload has an unexpected shape is a decoding failure of
// that section, not of the call.
func TestInventoryReportsUndecodableSection(t *testing.T) {
	target := newInventoryTarget(t)
	target.respond("alert.list", "not a list")

	out := callInventory(t, target)
	errs, _ := out["errors"].(map[string]any)
	if msg, _ := errs["alerts"].(string); !strings.Contains(msg, "decoding alert.list") {
		t.Errorf("alerts error = %q", msg)
	}
}
