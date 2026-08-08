package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Resources are for data a person points at, and for reference material.
//
// The distinction from tools is control locus, not cost: tools are
// model-controlled, resources are user- and application-controlled. A resource
// pays off when a human attaches it — no round trip, no tool budget — and
// underperforms when a model has to go find it, since model-driven resource
// access routes through generic list/read tools and reintroduces the round
// trips it was meant to avoid.
//
// So: addressable entities and documentation here, anything computed or
// parameterised stays a tool. URIs address; they do not compute.

const (
	uriAlerts    = "truenas://alerts"
	uriHealth    = "truenas://system/health"
	uriPools     = "truenas://pools"
	uriApps      = "truenas://apps"
	uriDocFilter = "truenas://docs/query-filters"
	uriDocZFS    = "truenas://docs/dataset-properties"
)

func registerResources(srv *mcp.Server, session sessionFor) {
	// Documentation resources are the highest value-to-effort item in the
	// design: they teach the filter syntax and ZFS property semantics once,
	// instead of repeating them in every tool description where they would be
	// paid for on every request.
	srv.AddResource(&mcp.Resource{
		URI:         uriDocFilter,
		Name:        "TrueNAS query filter syntax",
		Description: "How to write filters for query-shaped middleware methods.",
		MIMEType:    "text/markdown",
	}, staticResource(uriDocFilter, docQueryFilters))

	srv.AddResource(&mcp.Resource{
		URI:         uriDocZFS,
		Name:        "ZFS dataset properties",
		Description: "What the dataset fields mean and which are inherited.",
		MIMEType:    "text/markdown",
	}, staticResource(uriDocZFS, docDatasetProperties))

	if session == nil {
		return
	}

	srv.AddResource(&mcp.Resource{
		URI:         uriAlerts,
		Name:        "Current alerts",
		Description: "Everything the target is currently complaining about.",
		MIMEType:    "application/json",
	}, liveResource(uriAlerts, session, "alert.list"))

	srv.AddResource(&mcp.Resource{
		URI:         uriHealth,
		Name:        "System health",
		Description: "Version, hostname, uptime, and hardware summary.",
		MIMEType:    "application/json",
	}, liveResource(uriHealth, session, "system.info"))

	srv.AddResource(&mcp.Resource{
		URI:         uriPools,
		Name:        "Pools",
		Description: "Every pool with its capacity and health.",
		MIMEType:    "application/json",
	}, liveResource(uriPools, session, "pool.query"))

	srv.AddResource(&mcp.Resource{
		URI:         uriApps,
		Name:        "Installed apps",
		Description: "Installed applications and their state.",
		MIMEType:    "application/json",
	}, liveResource(uriApps, session, "app.query"))

	// Job progress. Templated because a job id is not known in advance; this
	// is the one place a URI stands in for something dynamic, and it is
	// justified because a job genuinely is an addressable object.
	srv.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "truenas://job/{id}",
		Name:        "Job",
		Description: "State and progress of a long-running operation.",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		id := strings.TrimPrefix(req.Params.URI, "truenas://job/")
		var jobID int64
		if _, err := fmt.Sscanf(id, "%d", &jobID); err != nil {
			return nil, fmt.Errorf("%q is not a job URI", req.Params.URI)
		}

		s, err := session(ctx)
		if err != nil {
			return nil, err
		}
		job, err := s.Client().Job(ctx, jobID)
		if err != nil {
			return nil, err
		}
		return jsonResource(req.Params.URI, job)
	})
}

// liveResource reads current state from the target under the caller's own
// credential. Reading never changes anything on the target.
func liveResource(uri string, session sessionFor, method string) mcp.ResourceHandler {
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		s, err := session(ctx)
		if err != nil {
			return nil, err
		}
		raw, err := s.Client().Call(ctx, method)
		if err != nil {
			return nil, err
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      req.Params.URI,
				MIMEType: "application/json",
				Text:     string(raw),
			}},
		}, nil
	}
}

func staticResource(uri, body string) mcp.ResourceHandler {
	return func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      req.Params.URI,
				MIMEType: "text/markdown",
				Text:     body,
			}},
		}, nil
	}
}

func jsonResource(uri string, v any) (*mcp.ReadResourceResult, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{
			URI:      uri,
			MIMEType: "application/json",
			Text:     string(b),
		}},
	}, nil
}

const docQueryFilters = `# TrueNAS query filters

Query-shaped middleware methods (` + "`pool.query`, `app.query`, `core.get_jobs`" + `)
take a list of filters and an options object.

A filter is a three-element list: ` + "`[field, operator, value]`" + `.

    [["name", "=", "tank"]]
    [["state", "!=", "RUNNING"]]
    [["used.parsed", ">", 1000000000]]

Operators: ` + "`=`, `!=`, `>`, `>=`, `<`, `<=`, `~` (regex), `in`, `nin`" + `.

Combine with ` + "`AND`" + ` by listing several filters; use ` + "`OR`" + ` explicitly:

    [["OR", [["state", "=", "RUNNING"], ["state", "=", "DEPLOYING"]]]]

Nested fields use dots: ` + "`used.parsed`, `progress.percent`" + `.

Options control the shape of the result:

    {"limit": 10, "offset": 0, "order_by": ["name"], "select": ["name", "state"]}

This server's read tools build filters for you; you only need this when
calling a method directly through ` + "`call_method`" + `.
`

const docDatasetProperties = `# ZFS dataset properties

Fields returned by ` + "`pool.dataset.query`" + ` that are worth knowing.

## Space

- ` + "`used`" + ` — space consumed by this dataset and its descendants
- ` + "`available`" + ` — space still writable, bounded by quota or pool free space
- ` + "`referenced`" + ` — space referenced by this dataset alone
- ` + "`quota`" + ` / ` + "`refquota`" + ` — hard limits; ` + "`refquota`" + ` excludes descendants
- ` + "`reservation`" + ` / ` + "`refreservation`" + ` — space guaranteed to this dataset

Each appears as an object with ` + "`parsed`" + ` (bytes, for arithmetic) and
` + "`rawvalue`" + ` (the string ZFS reports). Compare on ` + "`parsed`" + `.

## Inheritance

Most properties carry a ` + "`source`" + ` field:

- ` + "`INHERITED`" + ` — taken from the parent; changing the parent changes this
- ` + "`LOCAL`" + ` — set on this dataset explicitly
- ` + "`DEFAULT`" + ` — never set anywhere

A property showing ` + "`INHERITED`" + ` is not independently configured, which
matters before changing anything: the change belongs on the parent.

## Common properties

- ` + "`compression`" + ` — ` + "`LZ4`" + ` is the usual default and rarely worth changing
- ` + "`recordsize`" + ` — tune only for known workloads; the default suits most
- ` + "`atime`" + ` — ` + "`OFF`" + ` reduces writes; safe unless something depends on it
- ` + "`deduplication`" + ` — costs large amounts of RAM; almost never worth it
- ` + "`encryption`" + ` / ` + "`keyformat`" + ` — set at creation, not changeable later
- ` + "`readonly`" + ` — commonly ` + "`ON`" + ` for replication targets
`
