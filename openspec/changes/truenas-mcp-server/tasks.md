## 1. Ground the design against the live box

Design decisions D5, D6, and D7 rest on assumptions taken from documentation and prior art. Verify them before fixing any tool surface.

- [x] 1.1 Record the target's API version and total method count; confirm whether curation scale matches the design's assumption
- [x] 1.2 Capture the live top-level namespace list; check it against the concern grouping in design D6
- [x] 1.3 Measure argument schema sizes for the largest create-methods; set the summarization thresholds in D7 from real numbers
- [x] 1.4 Confirm the shape of method introspection metadata — required-ness and defaults — that D7's derived field selection depends on
- [ ] 1.5 Confirm the middleware job event stream shape that job-tracking subscription depends on
- [x] 1.6 Evaluate the official `truenas/truenas-mcp` server against this project's requirements — run it, check whether its tool surface covers the needed workflows, and record what it does not do that justifies building separately
- [ ] 1.7 Review other prior art (`sonicaj/tn_mcp`) and record what to adopt or avoid
- [x] 1.8 Write up findings and note any design decision the measurements invalidate

## 2. Walking skeleton: project, pipeline, and published image

Per design D15, the delivery pipeline is exercised before any feature work. The server at the end of this group does nothing but start, validate configuration, and report health — but it does so from an image pulled off the registry and run on the target.

- [x] 2.1 Set up the Go module with `make lint` / `make format` / `make test` targets and the official MCP Go SDK as a dependency
- [x] 2.2 Choose and apply a project license, recording whether forking the GPL-3.0 official middleware client is acceptable per design D10
- [x] 2.3 Set up the test harness with unit tests and a separately-marked integration suite, keeping live-target credentials out of the repository
- [x] 2.4 Implement configuration loading from environment variables with fail-fast validation and the startup summary
- [x] 2.5 Implement the health signal
- [x] 2.6 Write the Dockerfile producing a minimal image running as a non-root user with no added capabilities
- [x] 2.7 Add the GitHub Actions workflow running build, test, and lint on every push
- [x] 2.8 Add image build and publication to GHCR, gated on tests and lint passing, with a moving tag on the default branch and an immutable tag on release tags
- [x] 2.9 Record the source commit in published image metadata and verify the image is pullable without registry credentials
- [x] 2.10 Pull the published image onto the target TrueNAS instance and verify it starts and reports health

## 3. Connection layer

- [x] 3.1 Write failing tests for connection configuration and credential loading, asserting no tool schema accepts credentials and that a key without its username is refused
- [x] 3.2 Implement configuration and username + API-key authentication against the JSON-RPC WebSocket API
- [x] 3.3 Write failing tests for TLS enforcement, certificate verification as a separate setting, and both override paths
- [x] 3.4 Implement TLS enforcement and the two independent overrides — plaintext transport and certificate verification
- [ ] 3.5 Implement negotiated-mechanism detection and startup reporting, covering both the key-transmitting mechanism and SCRAM
- [x] 3.6 Implement authentication rate-limit handling distinct from invalid credentials
- [ ] 3.7 Write failing tests for API version detection and refusal below the minimum supported release
- [ ] 3.8 Implement version detection, pinning, and unsupported-release refusal
- [ ] 3.9 Write failing tests for reconnection, backoff, and in-flight request interruption reporting
- [ ] 3.10 Implement reconnection with backoff and interrupted-request error reporting
- [x] 3.11 Implement structured connection errors distinguishing unreachable, unauthenticated, and unauthorized
- [ ] 3.12 Write failing tests for per-session connection isolation: no sharing across credentials, no unauthenticated connection path, no session-supplied target override
- [ ] 3.13 Implement per-session connection lifecycle with establish-once reuse and release on session close
- [x] 3.14 Verify the connection layer against the live box, including a forced disconnect and two concurrent differently-privileged sessions
- [ ] 3.15 Verify on the live box whether the target revokes API keys sent over a plaintext loopback connection, per design D11c

## 4. MCP transport and session authentication

- [ ] 4.1 Write failing tests asserting the server holds no TrueNAS credential and refuses sessions that supply none
- [x] 4.2 Implement the HTTP transport with a configurable bind address and port
- [ ] 4.3 Implement per-session credential extraction and middleware session establishment under the caller's identity
- [ ] 4.4 Write failing tests for TLS enforcement on the MCP boundary and the explicit plaintext override with its warning
- [ ] 4.5 Implement TLS enforcement on the MCP boundary
- [ ] 4.6 Write failing tests asserting a session cannot exceed its own credential's privileges and that no call is retried under another credential
- [ ] 4.7 Implement authorization failure reporting and mid-session revocation handling
- [ ] 4.8 Verify against the live box with two TrueNAS users of differing privilege that each session sees only what its user may see

## 5. Shared read layer

Both the read tools and the resource surface project from this. Build it once.

- [ ] 5.1 Write failing tests for result shaping and the truncation contract, including reported total counts
- [ ] 5.2 Implement the read layer with per-concern accessors, result shaping, and bounded results
- [ ] 5.3 Write failing tests for authorization failures surfacing distinctly from operation failures
- [ ] 5.4 Implement distinct authorization error reporting

## 6. Safety machinery

Build this before any tool exists, so no surface is ever added outside the gate.

- [ ] 6.1 Write failing tests asserting denylisted operations are unreachable under the broadest configuration
- [ ] 6.2 Implement the unrecoverable-operation denylist as a non-configurable constant
- [ ] 6.3 Write failing tests asserting configuration naming a denylisted operation is refused and reported at startup
- [ ] 6.4 Implement write-tier configuration gating with startup reporting of the exposed surface
- [ ] 6.5 Implement the CI assertion that every operation in a read tool's dispatch table is non-mutating, per design D3 and the read-tools annotation requirement
- [ ] 6.6 Verify the build fails when a mutating operation is added to a read tool

## 7. Read tools

- [ ] 7.1 Write failing tests for dispatch: strict `op` enum, unknown-operation errors listing valid operations
- [ ] 7.2 Implement the dispatch mechanism with the flat optional argument superset
- [ ] 7.3 Write failing tests for missing-argument and irrelevant-argument errors naming the correct arguments per operation
- [ ] 7.4 Implement per-operation argument relevance validation
- [ ] 7.5 Implement the `storage` tool (pools, datasets, snapshots, capacity)
- [ ] 7.6 Implement the `system` tool (health, alerts, version, updates)
- [ ] 7.7 Implement the `sharing` tool, SMB and NFS list/show only per the enum-width risk in design
- [ ] 7.8 Implement the `apps` tool (list, show, config, outdated images, upgrade summary, rollback versions, used ports)
- [ ] 7.9 Implement the `jobs` tool (list, show) — see task group 9
- [ ] 7.10 Apply `readOnlyHint` annotations to all read tools
- [ ] 7.11 Assert the default tool count stays within the ten-tool bound
- [ ] 7.12 Verify every read operation against the live box

## 8. Resources

- [ ] 8.1 Write failing tests for entity resource URIs, including the absent-entity error
- [ ] 8.2 Implement entity resources for pools, apps, app logs, and alerts, projecting from the shared read layer
- [ ] 8.3 Author the filter-syntax and ZFS-property documentation resources
- [ ] 8.4 Strip filter and property explanations from tool descriptions and point them at the documentation resources
- [ ] 8.5 Write and satisfy tests asserting resource and read-tool surfaces report consistent content
- [ ] 8.6 Assert no resource exposes data the read tier would refuse

## 9. Job tracking

- [ ] 9.1 Write failing tests asserting job-starting operations return without waiting for completion
- [ ] 9.2 Implement non-blocking job initiation returning job identity and resource URI
- [ ] 9.3 Write failing tests for job status polling across running, succeeded, failed, and unknown states
- [ ] 9.4 Implement job status polling and the recent-jobs listing
- [ ] 9.5 Implement job resource URIs reporting state consistent with polling
- [ ] 9.6 Verify a real long-running job on the live box is observable to completion by polling alone
- [ ] 9.7 Implement job event subscription mapped to resource update notifications, including the terminal-state notification
- [ ] 9.8 Verify polling remains sufficient with subscription unsupported by the client

## 10. Method discovery

- [ ] 10.1 Write failing tests asserting non-allowlisted methods are neither describable nor callable, and that description does not leak their arguments
- [ ] 10.2 Implement the namespace-pattern allowlist
- [ ] 10.3 Write failing tests asserting discovery honors write-tier gating and the denylist
- [ ] 10.4 Implement gating enforcement on the discovery path
- [ ] 10.5 Write failing tests for schema summarization: commonly-used fields, worked example, omitted-count, and the full-schema option
- [ ] 10.6 Implement `describe_method` with derived field selection per design D7
- [ ] 10.7 Implement `call_method`, distinguishing server refusals from target-reported failures and returning job identity for job-starting methods
- [ ] 10.8 Verify against the live box that a method added outside the allowlist is unreachable

## 11. Write tier

- [ ] 11.1 Write failing tests asserting no mutating tool is exposed by default and that no schema can enable the write tier
- [ ] 11.2 Write failing tests for target resolution: refusal on ambiguous or absent identifiers before any mutation is attempted
- [ ] 11.3 Implement target resolution and reporting of the resolved object
- [ ] 11.4 Implement the snapshot creation tool with accurate annotations
- [ ] 11.5 Implement the app pull-images tool with redeploy defaulting to enabled, returning a job identity
- [ ] 11.6 Implement the app redeploy, start, and stop tools
- [ ] 11.7 Implement the app upgrade tool
- [ ] 11.8 Implement the app rollback tool and assert it is present whenever any app mutation is
- [ ] 11.9 Assert app deletion is absent from v1 and that all v1 mutating tools disappear when the write tier is disabled
- [ ] 11.10 Verify the `app.pull_images` redeploy workflow end to end against the live box
- [ ] 11.11 Verify rollback recovers an app after an upgrade on the live box

## 12. TrueNAS app installation

The image and pipeline already exist from group 2. This group installs it as a TrueNAS app and verifies the end-to-end path.

- [x] 12.1 Extend the startup summary to report target address, transport and TLS state, and write-tier state
- [ ] 12.2 Extend the health signal to reflect target reachability
- [x] 12.3 Author the TrueNAS custom-app Compose definition with a top-level `services` key, mounting no host socket and requesting no privileged access
- [x] 12.4 Install the Compose definition on the live TrueNAS instance through Apps → Install via YAML and verify the server is reachable from another machine
- [ ] 12.5 Connect a real MCP client to the deployed container over TLS and verify an end-to-end tool call
- [ ] 12.6 Verify the app survives a restart and an image tag update

## 13. Ship

- [ ] 13.1 Run the full integration suite against the live box
- [ ] 13.2 Document configuration, per-user API key creation with least-privilege guidance, and the read-only default
- [ ] 13.3 Document the safety model: per-session credentials as the outer boundary, gating, denylist, and why consent is per-operation
- [ ] 13.4 Document the on-box availability trade-off and off-box deployment as the more robust option for troubleshooting
- [ ] 13.5 Verify the server end-to-end in a real MCP client, including tool selection accuracy on common questions
- [ ] 13.6 Run `make lint` and `make format`
- [ ] 13.7 Record which design open questions the implementation resolved
