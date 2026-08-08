## Context

See proposal.md — Why. The constraints that shape every decision below:

- **The middleware surface is far larger than the tool budget.** Curation is the design problem; transport is not.
- **The API moves on a six-month cadence.** Users will run 24.10 / 25.04 / 25.10 / 26.04 simultaneously. Any hand-transcribed copy of TrueNAS's schemas drifts.
- **The middleware is introspectable.** Method metadata and schemas are readable from the box itself, so transcribing them into tool definitions duplicates a source of truth.
- **Long-running work is job-shaped and event-driven.** Scrubs, replication, and app installs return a job id and stream progress over the same WebSocket. MCP tools are request/response.
- **The blast radius is asymmetric and extreme.** Most methods are harmless; a handful are unrecoverable.

## Goals / Non-Goals

**Goals:**

- Answer the common question in one round trip, with tool schemas that stay in the prompt cache.
- Keep the default surface small enough that tool selection stays accurate.
- Make the reachable surface an allowlist, so an unknown method added in a future TrueNAS release is not callable on day one.
- Keep consent decisions per-operation, so no safe operation shares a gate with a destructive one.
- Survive TrueNAS API version changes without a code change for the long tail.

**Non-Goals:**

- Exposing the full middleware surface. Explicitly rejected.
- Multi-server / fleet management. One target per process.
- A general-purpose TrueNAS SDK. This is an MCP server; the API client is a dependency, not a deliverable.
- Supporting the deprecated REST API, TrueNAS CORE, or SCALE releases before 25.04.

## Decisions

### D1: Asymmetric tool shape — dispatch for reads, flat for writes

Reads and writes have genuinely different shapes, so they get different patterns.

| | Reads | Writes |
|---|---|---|
| Number of operations | many | few that matter |
| Argument divergence | low (`id`, filters, options) | high (topology, ACLs, chart values) |
| Risk | none | per-operation, up to unrecoverable |
| Call frequency | high | low |
| Verdict | **dispatch compresses well** | **dispatch compresses badly and costs safety** |

Reads become ~6 concern-level tools with an `op` enum: `storage`, `sharing`, `apps`, `system`, `jobs`, `network`. Writes become individual flat tools.

*Alternative considered — one flat tool per operation everywhere.* Rejected: 30-40 similar-looking tools degrade selection accuracy, and most of them would be read variants that share nearly identical arguments.

*Alternative considered — dispatch everywhere, including writes.* Rejected on the annotation argument in D3.

*Note on token math:* dispatch does not meaningfully reduce token count — the `args` schema still has to describe everything the separate tools described. It reduces **tool-list slots**, which is the resource that actually degrades selection. This distinction should stay explicit so nobody later "optimizes" the wrong quantity.

### D2: Read tool arguments are a flat optional superset, not a discriminated union

Three options exist for typing `args` in a dispatch tool:

1. **Freeform object** — no validation, no model guidance, runtime-only errors. Strictly worse than separate tools.
2. **`oneOf` discriminated on `op`** — correct in principle; models handle conditional schemas measurably worse than flat ones, and client schema-validator support is uneven.
3. **Flat superset, most fields optional** — chosen.

Option 3's usual weakness is that a wide superset invites the model to pass irrelevant arguments. That weakness is small *specifically because this applies only to reads*, where operations share arguments (`id`, `name`, `filters`, `limit`). The superset stays narrow. Applying the same pattern to writes would make the superset wide and the weakness severe — which is D1 restated from the other direction.

The server validates argument relevance per `op` at dispatch time and returns a structured error naming the correct arguments, so a mistake costs one turn rather than producing a wrong result.

### D3: Consent is per-operation; annotations must never be bundled

MCP annotations (`readOnlyHint`, `destructiveHint`, `idempotentHint`) are per-tool, not per-operation. A `pools(op)` tool containing both `list` and `export` must be annotated destructive as a whole. The consequence is a failure chain, not just an inconvenience:

```
prompts on the safe read → confirmation fatigue → user allowlists the tool
  → the destructive op now runs unprompted
```

Bundling a safe operation with a catastrophic one behind one gate trains the user to disarm the gate. Many clients — including Claude Code — allowlist at tool-name granularity, so this is not hypothetical.

This is the single strongest constraint in the design and it is what forces D1's asymmetry. Read dispatch tools are uniformly `readOnlyHint: true`, which is only truthful because no write operation is ever admitted into them.

### D4: Three-tier surface, read-only by default

```
TIER 1  concern-level read tools      always present, ~6 tools, readOnlyHint
TIER 2  describe_method/call_method   allowlist-gated long tail
TIER 3  explicit write tools          off unless enabled; per-op annotations
```

Approximately 8 tools visible by default, all read-only. The write tier is enabled by configuration. Independently, a denylist of unrecoverable methods (pool export/destroy, disk wipe/format) is **not unlockable by any configuration** — if a user needs those, the web UI is the correct place.

The API key's own privilege level remains the outer boundary. Server-side gating is defense in depth, not a substitute for a scoped key.

### D5: Discovery is allowlist-gated, and the allowlist bounds it absolutely

Progressive disclosure over the raw API inverts the safety posture from allowlist to denylist. An allowlist fails closed; a denylist maintained against an API that grows every release fails open. That is the wrong default for this domain.

So `describe_method` and `call_method` operate only over methods matching a curated allowlist of patterns. A method outside it is invisible to `describe_method` and rejected by `call_method`. Discovery buys version-resilience for the long tail *without* buying discovery's usual fail-open posture.

The allowlist is expressed as **patterns over namespaces** (e.g. `*.query`, `*.get_instance`, `smart.*`), not as an enumeration of method names. Patterns survive version changes; enumerations are the transcription problem all over again.

*Alternative considered — pure progressive disclosure as the whole surface (the `--help` model).* Rejected as the primary interface on latency grounds: it turns a one-round-trip question into four sequential inference turns, and the cost is paid every session because the model has no memory across sessions. Tool schemas are prompt-cached; discovery dialogue is not. Kept as the *tail* strategy, where the amortization argument does not apply because the calls are rare.

### D6: The curated tier is task-shaped; the discovery tier is namespace-shaped

The middleware's namespace hierarchy is an implementation artifact, not a user's mental model. Measured on the target: **74 top-level namespaces over 815 methods**, and what a person calls "my containers" is spread across `app`, `docker`, `container`, `lxc`, and `vm` as five separate namespaces. (Note that `virt.*`, which earlier drafts of this design referenced, does not exist on TrueNAS 26 — virtualization was reorganized into `vm` / `container` / `lxc`.)

Seventy-four namespaces is far past what a user should navigate, which settles the question: the curated tier cannot mirror the namespace tree.

The tension: a task-shaped taxonomy discloses better but must be hand-maintained, which is the maintenance burden discovery was meant to escape.

Resolution: spend judgment where it is cheap and high-leverage, and stay mechanical where it is expensive. Six hand-maintained task-shaped read tools is a small, stable surface. A hand-maintained taxonomy over the entire middleware is not. So the discovery tier is a thin, honest projection of the live namespace structure, and does not pretend to be organized.

### D7: `describe_method` summarizes rather than dumps

Returning raw JSON Schema is the lazy option and the worst one. Some create-methods carry very large schemas, and models fill large sparse schemas worse than small dense ones — so a faithful dump is both more expensive and less accurate than a summary.

`describe_method` returns the commonly-used fields, a worked example, and a count of omitted optional fields, with a `full` flag to escape to the complete schema. Which fields count as "commonly used" is derived from schema metadata (required-ness, defaults) rather than a hand-written list, so it does not drift.

Actual schema sizes must be measured against the live box before finalizing the summarization thresholds.

### D8: Jobs return immediately; polling is the baseline, subscription an enhancement

A tool that blocks for the duration of a scrub is unusable. Job-initiating operations return a job id and a resource URI immediately.

Status is available two ways:

- **Polling** via `jobs(op: "show")` — always works, is the baseline, and is the only path that can be relied on.
- **Subscription** — the server already holds a WebSocket subscribed to middleware job events, and maps them onto MCP `notifications/resources/updated`. This turns progress into a push rather than the model burning turns polling.

Subscription is built as an enhancement over a working poll path and never as the only path, because resource subscription is among the least-implemented parts of MCP across clients.

### D9: Resources are for user-referenced and server-pushed data, not for dodging the tool budget

The distinction is control locus, not cost: tools are model-controlled, resources are application/user-controlled. Resources pay off when a human points at them (attached directly, zero round trips, zero tool slots) and underperform when a model has to find them — in Claude Code, model-driven resource access routes through generic list/read tools, which reintroduces the round-trip tax.

So resources are used for:

- **Documentation** — the middleware filter DSL and ZFS property semantics, taught once as a resource instead of repeated in every tool description. Highest value-to-effort ratio in the whole design.
- **Addressable entities** — `truenas://pool/tank`, `truenas://app/<name>/logs`, `truenas://alerts`.
- **Job progress** — per D8.

Deliberately not used as a way to expose capability that should be a tool. Anything parameterized or computed is a tool; URIs address, they do not compute.

Overlap between a resource and a read tool covering the same data is intentional, not duplication — the control locus differs. Both project from one shared read layer.

### D10: Go, on the official MCP Go SDK, with our own middleware client

Go, for reasons that follow from the deployment model in D11a and D13: a single static binary produces a minimal container that runs unprivileged with no runtime dependencies, which is exactly what the container-deployment requirements ask for. Cross-compilation is free.

The argument that previously favoured Python — that TrueNAS ships an official Python client handling JSON-RPC, auth, and jobs — is weaker than it looked. The equivalent Go layer is small: the official `truenas/truenas-mcp` server implements its entire middleware client in about 620 lines over `gorilla/websocket`. That is a day or two of work, not a reason to choose a language.

**MCP layer: use the official Go SDK.** It is at v1.x with a compatibility guarantee, spec-complete, and provides the Streamable HTTP transport that D11a requires. Notably the official TrueNAS server hand-rolled its MCP protocol types instead; we do not, because transport and protocol conformance are exactly the work an SDK should absorb.

**Middleware client: our own, not a fork.** The official server is GPL-3.0, so vendoring its client package would make this project GPL-3.0 by derivation. At ~620 lines the cost of an independent implementation is low enough that it is not worth surrendering the licensing choice for. Its lifecycle differs anyway: theirs is built around a single process-wide API key, while D11b requires a connection per session under a caller-supplied credential.

Facts about middleware behavior — method names, job semantics, TLS and revocation rules — are freely usable regardless of license. Only the code is constrained.

*If GPL-3.0 is acceptable for this project*, forking their `truenas/` package instead is a legitimate shortcut that saves those two days. That is a licensing decision rather than a technical one, and it is the owner's to make.

*Alternative considered — Python on `truenas/api_client`.* Rejected once Go was chosen for deployment reasons; the official client's advantage does not outweigh shipping an interpreter and its dependencies in the container.

*Alternative considered — Rust.* Rejected: same hand-rolled-client cost as Go with no offsetting benefit, since this server's profile is dominated by network round trips and model inference.

### D15: The delivery pipeline is part of the first increment, not the last

The container image, its GitHub Actions build, and publication to GHCR are built early, against a server that does little more than start and report health. Deployment is then exercised continuously rather than discovered at the end.

The reason is specific to this project rather than general good practice: the deployment target is an appliance whose custom-app flow gives poor error feedback, and D13 already commits to shipping a *verified* Compose definition rather than an illustrative one. A pipeline that first runs after the server is feature-complete would surface packaging, registry, permission, and Compose problems all at once, at the point where they are most expensive and least separable from application bugs.

So the first increment is a walking skeleton: a binary that starts, validates configuration, reports health, and is published as a tagged image that has been pulled and run on the target. Everything after that is filling it in.

### D11a: Streamable HTTP transport, because the server is deployed as a container on the target

The server ships as a container installed as a TrueNAS custom app. The client runs elsewhere — a laptop, another machine — and cannot spawn the server as a child process, so stdio is not available. The server listens over HTTP.

*Alternative considered — stdio over `docker exec` across SSH.* Rejected: it requires SSH access and Docker CLI privileges on every client machine, which is a larger grant than the API key it is trying to avoid, and it is fragile across client implementations.

This makes the client-to-server boundary real, which is what D11b answers.

### D11b: The MCP credential is a TrueNAS credential

Each client supplies its own TrueNAS username and API key on the MCP connection. The server opens the middleware session under that identity and stores no credential of its own.

The reasoning is that authentication without authorization is theatre here. If every session executes under one server-held API key, per-user MCP auth controls only who may connect, not what they may do — every authenticated user gets identical, root-equivalent reach. That directly contradicts D4, which treats the key's own privilege level as the outer bound on reachable capability.

Making the MCP credential a TrueNAS credential collapses the two concerns into one: TrueNAS's own privilege model does the enforcing, revocation is native to the TrueNAS UI, per-user privilege separation is real rather than nominal, and the container holds no secret worth stealing.

Consequences that must be designed for rather than discovered:

- The server holds **one middleware connection per authenticated session**, not one process-wide connection. Connection lifecycle becomes per-session.
- The target's authentication rate limit applies across sessions, so session establishment must not retry aggressively.
- The credential travels in a request header on the MCP boundary, so TLS on that boundary is mandatory rather than advisory (D11d).

*Alternative considered — a static shared bearer token.* Rejected as the primary mechanism for the reason above: it is a gate, not a privilege boundary, and it invites the false confidence that adding users is safe.

*Alternative considered — OAuth 2.1 per the MCP authorization spec.* Deferred, not rejected on principle. It becomes the right answer when an identity provider already exists and single sign-on is wanted. It does not solve the authorization problem on its own — an OAuth identity would still need mapping to a TrueNAS credential to mean anything, which is exactly what D11b does directly and without an authorization server to operate. Note also that TrueNAS itself offers no OIDC or OAuth; the middleware authenticates local users, API keys, AD, and LDAP, so OAuth is only ever a question about the client-to-server boundary.

### D11c: Connect to the middleware over the network, never the mounted UNIX socket

The official client can reach middleware over a local UNIX socket, and a container can be given that socket with a bind mount. It is tempting — no key, no TLS, no certificate handling.

Rejected, because a mounted middleware socket is root-equivalent access carrying no identity. It cannot express which user is asking, so it is incompatible with D11b and it erases D4's outer boundary entirely. Running on the same physical box does not change what the privilege model needs to be.

The server therefore connects over the network with a per-session credential exactly as it would from off-box, and the container is not granted the socket.

Whether the target applies its plaintext-key-revocation behavior to loopback connections must be verified on the live box before assuming a plaintext local hop is safe.

### D11d: Require TLS on the MCP boundary, terminated either here or in front

Because D11b puts a TrueNAS API key in a request header on every call, the client-to-server boundary carries credentials that TrueNAS itself revokes if it observes them in plaintext. Serving MCP over plaintext HTTP is therefore not offered as a convenience option, and the server refuses to start without TLS unless the operator sets an explicit override.

The requirement is about **the boundary the credential crosses**, not about which process performs the handshake. Two topologies satisfy it:

1. **The server terminates TLS**, configured with a certificate and key.
2. **A reverse proxy terminates TLS** and forwards to the server over a trusted local network. The plaintext override is then correct rather than a compromise, because the credential never crosses an untrusted network in the clear.

The server cannot distinguish these from the inside — a plaintext listener looks the same whether a proxy fronts it or not — so the override remains explicit and still logs a warning. The warning is accurate: it says the credential is transmitted in the clear *on that connection*, which is true and is the operator's to judge.

What this rules out is the case the warning exists for: a plaintext listener reachable directly from an untrusted network, where the override is being used to skip certificate work rather than because something else is doing it.

### D11: One target server per process

Multi-server support would add a `host` argument to every tool — schema bloat on the hot path, paid by every user — plus connection-pool and credential-set complexity, to serve a minority case. Users with several boxes run several server instances, which is well-supported by MCP clients and costs nothing at the protocol level.

Revisitable, but not on the v1 critical path.

### D12: v1 ships read-only plus full app lifecycle and snapshot creation

App management is the driving use case, and measurement against the target showed the middleware surface supports it far better than earlier drafts assumed. The `app` namespace carries 41 methods, and crucially **`app.rollback` exists alongside `app.rollback_versions`**. An earlier version of this decision claimed a redeploy was "not cleanly reversible"; that is wrong on this target, and it changes the risk calculus enough to widen the scope rather than narrow it.

The v1 write tier is therefore the full app lifecycle plus snapshot creation:

| Operation | Method | Notes |
|---|---|---|
| Pull images and redeploy | `app.pull_images` | the driving workflow; `redeploy` defaults to `true` |
| Redeploy | `app.redeploy` | |
| Start / Stop | `app.start` / `app.stop` | trivially reversible |
| Upgrade | `app.upgrade` | version change, reversible via rollback |
| Roll back | `app.rollback` | **the safety net that justifies the rest** |
| Create snapshot | snapshot creation | additive companion to any risky operation |

Rollback is what makes this set defensible. Every operation above either reverses itself or is reversed by `app.rollback`, so the worst realistic outcome is a service interrupted for as long as it takes to notice and roll back. That is a categorically different risk from removing a dataset, and it is why blast radius rather than mutation-count is the ordering principle.

The corresponding read operations matter as much as the writes, because they are what let a caller decide whether to act at all: `app.outdated_docker_images` answers "what needs updating", `app.upgrade_summary` answers "what would this upgrade do", and `app.rollback_versions` answers "what can I fall back to". A write tier without these forces the model to act blind.

Still deferred: everything whose blast radius is data rather than a service — dataset deletion, share reconfiguration, pool operations, and **app deletion**, which per D14 must never be reachable with volume removal enabled.

All app lifecycle methods are job-typed, so they exercise the job-tracking machinery in D8 rather than bypassing it — a redeploy returns a job identity and is observed to completion, never blocked on.

*Alternative considered — snapshot creation only, as originally scoped.* Rejected once a concrete workflow was known: it would have shipped a write tier that exercised the machinery but served nobody's actual use case.

*Alternative considered — the four-operation lifecycle subset of the previous draft.* Rejected once `app.rollback` was found: excluding rollback would have left the risky operations in scope while omitting the one that makes them recoverable.

### D14: The denylist gates argument values, not only method names

Some middleware methods are safe or unsafe depending on their arguments. Deleting an app is recoverable by reinstalling it; deleting an app while removing its volumes destroys data. The method is identical in both cases.

A method-name denylist forces a false choice between banning a useful operation outright and permitting data destruction inside it. So denylist entries SHALL be able to constrain argument values, and an operation whose arguments match a denied combination is refused with the same finality as a denied method.

This applies uniformly across the write tier and the discovery path, since both can reach methods with arguments.

Consequence for deferred work: when app deletion is eventually added, the volume-removal argument is permanently denied rather than gated behind configuration. The same reasoning applies to recursive dataset deletion.

### D13: Ship a container image plus a TrueNAS custom-app Compose example

TrueNAS moved apps from Kubernetes to Docker in 24.10, and custom apps are installed by pasting Docker Compose YAML into *Apps → Discover → Install via YAML*. From 25.10 that YAML must carry a top-level `services` key.

So the deliverable is a container image plus a known-good Compose file that a user pastes in — not a document describing how they might write one. The Compose example is a tested artifact, because the failure mode of getting it wrong is a user debugging YAML against an appliance UI with poor error reporting.

The container is stateless: all configuration arrives as environment variables or per-session credentials, and nothing needs a persistent dataset. That keeps the app definition small and makes upgrades a tag change.

## Risks / Trade-offs

- **A dispatch tool with a wide `op` enum stops paying.** Past roughly ten operations with divergent arguments, the flat superset widens and D2's weakness reasserts itself. → `sharing` is the tool at risk, since SMB/NFS/iSCSI share a user concern but few arguments. Mitigated for v1 by keeping only SMB and NFS list/show operations in it and pushing iSCSI's target/extent/initiator model to the discovery tier. Watch the enum width as a design smell.

- **The `op` enum makes typos a runtime rather than a selection-time error.** With separate tools the client cannot call a nonexistent tool at all. → Strict enum on `op`, which models respect well, plus structured errors that name valid operations. Largely but not entirely mitigated.

- **The allowlist could become a maintenance burden if written as method names.** → Enforced as namespace patterns per D5. If pattern maintenance starts looking like enumeration maintenance, that is a signal the tier boundary is wrong.

- **Resource subscription support is thin and uneven across clients.** → Poll path is the baseline and is always sufficient; subscription is strictly additive.

- **`readOnlyHint: true` on the read tools is a promise that is easy to break later.** A future contributor adding a convenient write operation to `storage(op)` silently invalidates the annotation and the entire consent model. → Enforced by test, not by convention: the read tools' dispatch table is asserted against the write-method denylist in CI.

- **Documentation-derived assumptions may not match the live box.** Method inventory, namespace naming, and schema sizes are taken from docs and prior art. → Verified against the live TrueNAS SCALE instance as the first task, before any tool surface is fixed.

- **The diagnostic tool lives on the machine being diagnosed.** Running as a TrueNAS app means the server is unavailable in exactly the situations where troubleshooting matters most — the box is down, the pool is offline, the app service failed to start. This is a genuine weakness of the deployment model, not a detail. → Nothing in the design prevents running the container elsewhere, since D11c already requires a network connection to the middleware rather than a local socket. Documentation must state the trade-off plainly and note that off-box deployment is the more robust choice for the troubleshooting use case, rather than presenting on-box installation as the only path.

- **Per-session middleware connections multiply connection cost and interact with the target's auth rate limit.** D11b means N clients produce N middleware sessions, and the target permits a bounded number of authentication attempts per minute. → Sessions are established once and reused for their lifetime rather than per tool call; failed authentication is not retried automatically. Observed limits are verified against the live box.

- **Credentials in headers make TLS misconfiguration a credential-disclosure bug, not a warning.** → D11d makes TLS the default with an explicit override, and the same startup warning as the middleware hop. Documentation must not offer plaintext as a convenience for LAN use.

- **Prompt-cache benefits assume a stable tool list.** Dynamic tool registration (materializing tools on demand via `notifications/tools/list_changed`) was considered and rejected for v1 — it invalidates the cache, and client support is uneven. Revisit only if the default surface grows past what selection accuracy tolerates.

## Open Questions

- Whether `network` earns a place in the default read tier or belongs in the discovery tail. It is the least-requested concern of the six and the easiest to cut if measurement shows the tool list is too wide. Deferrable — it changes one tool's presence, not the architecture.
- Whether job progress resources should expose intermediate percentages or only state transitions. Depends on observed event volume from the live box; affects notification chattiness, not the surface.
