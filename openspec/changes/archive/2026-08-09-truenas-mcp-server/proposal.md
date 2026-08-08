## Why

TrueNAS SCALE exposes a large introspectable middleware API (JSON-RPC 2.0 over WebSocket), but no good path for an LLM client to reach it. The naive approach — one MCP tool per middleware method — is unworkable: the method surface is roughly two orders of magnitude larger than the tool budget a model can select from reliably. The interesting problem is not transport, it is curation and blast radius.

Blast radius matters more here than in a typical API wrapper. A TrueNAS box holds the user's only copy of things. A design that bundles `list pools` and `export pool` behind a single consent gate trains the user to disarm that gate.

## What Changes

- New MCP server exposing TrueNAS SCALE over the versioned JSON-RPC 2.0 WebSocket API (`/api/current`), authenticated with a user-linked API key via `auth.login_with_api_key`. The deprecated REST API is not supported at all — it is removed in TrueNAS 26.
- **Asymmetric tool surface.** Reads use action-dispatch tools (`storage`, `sharing`, `apps`, `system`, `jobs`, `network`) — one tool per concern with an `op` enum. Writes use explicit flat tools, one per operation, so each carries its own MCP annotation and its own consent decision.
- **Read-only by default.** The write tier is off unless explicitly enabled by configuration. A hard denylist of catastrophic methods (pool export/wipe, disk format) is not unlockable by configuration at all.
- **Allowlist-gated discovery tail.** `describe_method` / `call_method` cover the long tail of middleware namespaces without hand-written tools, but only for methods matching a curated allowlist. Discovery never widens the reachable surface beyond that allowlist.
- **Resources for addressable entities and documentation**, not as a tool-budget escape hatch. Includes docs resources that teach the middleware filter DSL and ZFS property semantics once, instead of repeating them in every tool description.
- **Jobs return immediately** with a job id and a resource URI rather than blocking. Polling via `jobs(op: "show")` is the baseline; resource subscription driven by middleware events is an enhancement layered on top.
- **Deployed as a container**, installable as a TrueNAS custom app through the Docker Compose YAML flow, with a tested Compose definition shipped as an artifact rather than described in prose.
- **Served over HTTP, not stdio**, because a container on the appliance cannot be spawned by the client as a child process.
- **Each client authenticates with its own TrueNAS credential.** The server holds no TrueNAS credential of its own. Authentication and authorization become the same thing: a session can reach exactly what its user's own API key permits, and revocation happens in the TrueNAS UI. A shared server-held key would make per-user access control nominal rather than real.
- **No host socket access.** The container reaches the middleware over the network even when running on the target, so that every connection carries a user identity.
- Single target server per process. Multi-server support is explicitly out of scope.

## Capabilities

### New Capabilities

- `truenas-connection`: Connection lifecycle against the JSON-RPC WebSocket API — API-key authentication, TLS and certificate requirements, API version negotiation and pinning, reconnection, per-session connection isolation, and connection-level error reporting.
- `mcp-transport`: The client-facing boundary — the HTTP transport, per-session TrueNAS credential authentication, privilege bounding, TLS requirements, and middleware session reuse.
- `container-deployment`: Packaging and installation — the published image, the TrueNAS custom-app Compose definition, environment-only configuration, startup validation, health reporting, and unprivileged execution.
- `read-tools`: The action-dispatch read surface — the concern-level tools, their `op` enums, shared argument shapes, and result shaping. All read-only.
- `write-tools`: The opt-in mutation surface — enablement gating, per-operation tools with correct MCP annotations, the non-unlockable denylist, and confirmation semantics.
- `method-discovery`: The long-tail escape hatch — `describe_method` and `call_method`, the allowlist model that bounds them, and schema summarization behavior.
- `job-tracking`: Asynchronous middleware jobs — non-blocking initiation, job identity and addressing, status polling, and event-driven update notifications.
- `mcp-resources`: The resource surface — addressable entity URIs, documentation resources, and subscription behavior.

### Modified Capabilities

<!-- None. This is a greenfield project with no existing specs. -->

## Impact

- **New codebase.** Go, on the official MCP Go SDK, with our own JSON-RPC/WebSocket middleware client. Go produces a single static binary and therefore a minimal unprivileged container, which is what the deployment model asks for. The middleware client is written rather than forked: the official `truenas/truenas-mcp` server is GPL-3.0, and at roughly 620 lines the equivalent layer is cheaper to write than the licensing constraint is to accept.
- **Delivery pipeline is first-increment work**, not a final step: GitHub Actions builds and publishes to GHCR from the outset, and the first published image is pulled and run on the target before feature work begins.
- **Relationship to the official server.** `truenas/truenas-mcp` exists and covers adjacent ground with 52 flat tools, but it is stdio-only — so it cannot be deployed as a container — it holds a single server-wide API key, and its app coverage is catalog-oriented, with no support for pulling images and redeploying an existing app. Those gaps, not tool-shape preference, are what justify a separate implementation.
- **External dependency on TrueNAS SCALE 25.04 or later**, where the versioned JSON-RPC API is the supported path. Earlier releases are not targeted.
- **Requires each user to hold their own TrueNAS API key.** That key's privilege level is the outer boundary on what their session can reach; server-side gating is defense in depth layered inside it. Onboarding a user means creating a key in TrueNAS, not configuring the server.
- **Deployment artifacts:** a published container image and a TrueNAS custom-app Compose definition, both verified by installing them on a real instance. TrueNAS 25.10 and later require a top-level `services` key in custom app YAML.
- **Operational trade-off:** installed as a TrueNAS app, the server is unavailable precisely when the box is unhealthy. The design does not prevent running it elsewhere, and documentation must say so rather than presenting on-box installation as the only path.
- **Development depends on a live TrueNAS SCALE instance** for integration testing. Method inventory, schema sizes, and namespace naming are verified against a real box rather than from documentation.
- No impact on existing systems; nothing exists yet.
