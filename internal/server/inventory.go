package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/cedricziel/truenas-mcp/internal/truenas"
)

// The inventory tool answers "what is on this box" in one call, shaped for a
// person looking at a rendered view rather than a model reasoning about one
// section. Each section is fetched independently and reported independently:
// an API key that may read pools but not apps still gets its pools, with the
// refusal named against the section it belongs to, instead of the whole
// answer failing on the first section the key cannot see.
//
// The read tools remain the way to look at any one section in depth. This
// tool projects every section down to the handful of fields a summary view
// shows, and bounds the one collection -- datasets -- that can run to
// thousands of entries.

// datasetLimit bounds the datasets section. Snapshots are excluded outright;
// this caps nested datasets, of which a busy box holds hundreds.
const datasetLimit = 200

// InventoryOutput is the inventory tool's result and what the inventory app
// renders. Field names are part of the contract with the embedded view.
type InventoryOutput struct {
	Host       InventoryHost        `json:"host" jsonschema:"the target's identity"`
	Pools      []InventoryPool      `json:"pools" jsonschema:"storage pools with capacity and health"`
	Datasets   []InventoryDataset   `json:"datasets" jsonschema:"datasets and zvols, bounded; see datasets_truncated"`
	Apps       []InventoryApp       `json:"apps" jsonschema:"installed applications and their state"`
	VMs        []InventoryVM        `json:"vms" jsonschema:"virtual machines and their state"`
	Containers []InventoryContainer `json:"containers" jsonschema:"system containers and their state"`
	Shares     []InventoryShare     `json:"shares" jsonschema:"SMB shares and NFS exports"`
	Alerts     []InventoryAlert     `json:"alerts" jsonschema:"current alerts"`

	DatasetsTruncated bool `json:"datasets_truncated,omitempty" jsonschema:"whether the datasets section was cut short"`

	// Errors names each section that could not be read and why. A section
	// listed here is absent from the result, not empty.
	Errors map[string]string `json:"errors,omitempty" jsonschema:"sections that could not be read, keyed by section name"`

	GeneratedAt string `json:"generated_at" jsonschema:"when this inventory was collected, RFC 3339"`
}

type InventoryHost struct {
	Hostname string `json:"hostname,omitempty"`
	Version  string `json:"version,omitempty"`
	Uptime   string `json:"uptime,omitempty"`
}

type InventoryPool struct {
	Name      string `json:"name"`
	Status    string `json:"status,omitempty"`
	Healthy   bool   `json:"healthy"`
	Size      int64  `json:"size,omitempty" jsonschema:"bytes"`
	Allocated int64  `json:"allocated,omitempty" jsonschema:"bytes"`
	Free      int64  `json:"free,omitempty" jsonschema:"bytes"`
}

type InventoryDataset struct {
	Name      string `json:"name"`
	Pool      string `json:"pool,omitempty"`
	Type      string `json:"type,omitempty"`
	Used      int64  `json:"used,omitempty" jsonschema:"bytes"`
	Available int64  `json:"available,omitempty" jsonschema:"bytes"`
}

type InventoryApp struct {
	Name                  string `json:"name"`
	State                 string `json:"state,omitempty"`
	Version               string `json:"version,omitempty"`
	UpgradeAvailable      bool   `json:"upgrade_available"`
	ImageUpdatesAvailable bool   `json:"image_updates_available"`
}

type InventoryVM struct {
	Name      string `json:"name"`
	State     string `json:"state,omitempty"`
	VCPUs     int64  `json:"vcpus,omitempty"`
	MemoryMiB int64  `json:"memory_mib,omitempty"`
}

type InventoryContainer struct {
	Name  string `json:"name"`
	State string `json:"state,omitempty"`
}

type InventoryShare struct {
	Kind    string `json:"kind" jsonschema:"smb or nfs"`
	Name    string `json:"name,omitempty"`
	Path    string `json:"path,omitempty"`
	Enabled bool   `json:"enabled"`
}

type InventoryAlert struct {
	Level   string `json:"level,omitempty"`
	Message string `json:"message,omitempty"`
	Since   string `json:"since,omitempty"`
}

// inventorySection is one independently fetched part of the inventory.
type inventorySection struct {
	name   string
	method string
	params []any
	apply  func(out *InventoryOutput, raw json.RawMessage) error
}

func inventorySections() []inventorySection {
	return []inventorySection{
		{name: "host", method: "system.info", apply: func(out *InventoryOutput, raw json.RawMessage) error {
			var info struct {
				Version  string `json:"version"`
				Hostname string `json:"hostname"`
				Uptime   string `json:"uptime"`
			}
			if err := json.Unmarshal(raw, &info); err != nil {
				return err
			}
			out.Host = InventoryHost{Hostname: info.Hostname, Version: info.Version, Uptime: info.Uptime}
			return nil
		}},
		{name: "pools", method: "pool.query", apply: func(out *InventoryOutput, raw json.RawMessage) error {
			var pools []struct {
				Name      string `json:"name"`
				Status    string `json:"status"`
				Healthy   bool   `json:"healthy"`
				Size      int64  `json:"size"`
				Allocated int64  `json:"allocated"`
				Free      int64  `json:"free"`
			}
			if err := json.Unmarshal(raw, &pools); err != nil {
				return err
			}
			out.Pools = []InventoryPool{}
			for _, p := range pools {
				out.Pools = append(out.Pools, InventoryPool(p))
			}
			return nil
		}},
		{
			name: "datasets", method: "pool.dataset.query",
			// Flat, with only the properties the summary shows: the default
			// dataset object carries every ZFS property and its children
			// nested inside it, which is the shape that made the read tool
			// project in the first place.
			params: []any{
				[]any{},
				map[string]any{"extra": map[string]any{"flat": true, "properties": []string{"used", "available"}}},
			},
			apply: func(out *InventoryOutput, raw json.RawMessage) error {
				var datasets []struct {
					Name      string      `json:"name"`
					Pool      string      `json:"pool"`
					Type      string      `json:"type"`
					Used      parsedValue `json:"used"`
					Available parsedValue `json:"available"`
				}
				if err := json.Unmarshal(raw, &datasets); err != nil {
					return err
				}
				sort.SliceStable(datasets, func(i, j int) bool { return datasets[i].Name < datasets[j].Name })
				if len(datasets) > datasetLimit {
					datasets = datasets[:datasetLimit]
					out.DatasetsTruncated = true
				}
				out.Datasets = []InventoryDataset{}
				for _, d := range datasets {
					out.Datasets = append(out.Datasets, InventoryDataset{
						Name: d.Name, Pool: d.Pool, Type: d.Type,
						Used: d.Used.Parsed, Available: d.Available.Parsed,
					})
				}
				return nil
			},
		},
		{name: "apps", method: "app.query", apply: func(out *InventoryOutput, raw json.RawMessage) error {
			var apps []struct {
				Name                  string `json:"name"`
				State                 string `json:"state"`
				Version               string `json:"version"`
				UpgradeAvailable      bool   `json:"upgrade_available"`
				ImageUpdatesAvailable bool   `json:"image_updates_available"`
			}
			if err := json.Unmarshal(raw, &apps); err != nil {
				return err
			}
			out.Apps = []InventoryApp{}
			for _, a := range apps {
				out.Apps = append(out.Apps, InventoryApp(a))
			}
			return nil
		}},
		{name: "vms", method: "vm.query", apply: func(out *InventoryOutput, raw json.RawMessage) error {
			var vms []struct {
				Name   string `json:"name"`
				VCPUs  int64  `json:"vcpus"`
				Cores  int64  `json:"cores"`
				Memory int64  `json:"memory"`
				Status struct {
					State string `json:"state"`
				} `json:"status"`
			}
			if err := json.Unmarshal(raw, &vms); err != nil {
				return err
			}
			out.VMs = []InventoryVM{}
			for _, v := range vms {
				vcpus := v.VCPUs
				if v.Cores > 0 {
					vcpus *= v.Cores
				}
				out.VMs = append(out.VMs, InventoryVM{Name: v.Name, State: v.Status.State, VCPUs: vcpus, MemoryMiB: v.Memory})
			}
			return nil
		}},
		{name: "containers", method: "container.query", apply: func(out *InventoryOutput, raw json.RawMessage) error {
			var containers []struct {
				Name   string          `json:"name"`
				Status json.RawMessage `json:"status"`
			}
			if err := json.Unmarshal(raw, &containers); err != nil {
				return err
			}
			out.Containers = []InventoryContainer{}
			for _, c := range containers {
				out.Containers = append(out.Containers, InventoryContainer{Name: c.Name, State: stateOf(c.Status)})
			}
			return nil
		}},
		{name: "smb", method: "sharing.smb.query", apply: func(out *InventoryOutput, raw json.RawMessage) error {
			var shares []struct {
				Name    string `json:"name"`
				Path    string `json:"path"`
				Enabled bool   `json:"enabled"`
			}
			if err := json.Unmarshal(raw, &shares); err != nil {
				return err
			}
			for _, s := range shares {
				out.Shares = append(out.Shares, InventoryShare{Kind: "smb", Name: s.Name, Path: s.Path, Enabled: s.Enabled})
			}
			return nil
		}},
		{name: "nfs", method: "sharing.nfs.query", apply: func(out *InventoryOutput, raw json.RawMessage) error {
			var exports []struct {
				Comment string `json:"comment"`
				Path    string `json:"path"`
				Enabled bool   `json:"enabled"`
			}
			if err := json.Unmarshal(raw, &exports); err != nil {
				return err
			}
			for _, e := range exports {
				out.Shares = append(out.Shares, InventoryShare{Kind: "nfs", Name: e.Comment, Path: e.Path, Enabled: e.Enabled})
			}
			return nil
		}},
		{name: "alerts", method: "alert.list", apply: func(out *InventoryOutput, raw json.RawMessage) error {
			var alerts []struct {
				Level     string `json:"level"`
				Formatted string `json:"formatted"`
				Datetime  struct {
					Date int64 `json:"$date"`
				} `json:"datetime"`
			}
			if err := json.Unmarshal(raw, &alerts); err != nil {
				return err
			}
			out.Alerts = []InventoryAlert{}
			for _, a := range alerts {
				since := ""
				if a.Datetime.Date > 0 {
					since = time.UnixMilli(a.Datetime.Date).UTC().Format(time.RFC3339)
				}
				out.Alerts = append(out.Alerts, InventoryAlert{Level: a.Level, Message: a.Formatted, Since: since})
			}
			return nil
		}},
	}
}

// parsedValue is the middleware's {parsed, rawvalue} property object; only
// the byte count matters here.
type parsedValue struct {
	Parsed int64 `json:"parsed"`
}

// stateOf reads a container's status, which the middleware reports either as
// a bare string or as an object with a state field depending on release.
func stateOf(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var obj struct {
		State string `json:"state"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return obj.State
	}
	return ""
}

// collectInventory runs every section concurrently on the session's one
// connection and assembles what came back. It fails as a whole only when
// nothing at all could be read: that is a dead session, not a narrow key.
func collectInventory(ctx context.Context, client *truenas.Client, now time.Time) (InventoryOutput, error) {
	sections := inventorySections()

	type outcome struct {
		raw json.RawMessage
		err error
	}
	results := make([]outcome, len(sections))

	var wg sync.WaitGroup
	for i, s := range sections {
		wg.Add(1)
		go func(i int, s inventorySection) {
			defer wg.Done()
			raw, err := client.Call(ctx, s.method, s.params...)
			results[i] = outcome{raw: raw, err: err}
		}(i, s)
	}
	wg.Wait()

	out := InventoryOutput{
		Pools: []InventoryPool{}, Datasets: []InventoryDataset{}, Apps: []InventoryApp{},
		VMs: []InventoryVM{}, Containers: []InventoryContainer{}, Shares: []InventoryShare{},
		Alerts: []InventoryAlert{}, GeneratedAt: now.UTC().Format(time.RFC3339),
	}
	// Shares merge two sections; a failure in either is reported under the
	// key the view knows, naming which protocol was refused.
	failed := 0
	fail := func(name, msg string) {
		if out.Errors == nil {
			out.Errors = map[string]string{}
		}
		key := name
		if name == "smb" || name == "nfs" {
			key = "shares"
			msg = name + ": " + msg
		}
		if prev, ok := out.Errors[key]; ok {
			msg = prev + "; " + msg
		}
		out.Errors[key] = msg
	}

	for i, s := range sections {
		r := results[i]
		if r.err != nil {
			failed++
			fail(s.name, describeSectionError(r.err))
			continue
		}
		if err := s.apply(&out, r.raw); err != nil {
			failed++
			fail(s.name, fmt.Sprintf("decoding %s: %v", s.method, err))
		}
	}

	if failed == len(sections) {
		return out, fmt.Errorf("no inventory section could be read; first failure: %s", firstError(out.Errors))
	}
	return out, nil
}

func describeSectionError(err error) string {
	switch {
	case errors.Is(err, truenas.ErrUnauthorized):
		return "this API key is not permitted to read it"
	case errors.Is(err, truenas.ErrUnauthenticated):
		return "the session is no longer authenticated"
	}
	return err.Error()
}

func firstError(errs map[string]string) string {
	keys := make([]string, 0, len(errs))
	for k := range errs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return "unknown"
	}
	return keys[0] + ": " + errs[keys[0]]
}
