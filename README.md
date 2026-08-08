# truenas-mcp

An MCP server for TrueNAS SCALE. Read-first, deployable as a TrueNAS app, and
authenticated with each user's own API key.

> **Status: working, young.** Every capability in the design is implemented and
> verified against a live TrueNAS 26 box. Expect rough edges rather than gaps.

## Why this exists

iX ship an official [`truenas/truenas-mcp`](https://github.com/truenas/truenas-mcp),
and if it fits your needs you should use it. This one exists for three things it
does not do:

- **It only speaks stdio**, so it cannot be deployed as a container on the NAS
  and reached from elsewhere.
- **It holds a single server-wide API key**, so every caller gets identical
  reach no matter how they authenticate.
- **Its app coverage is catalog-shaped** — install, uninstall, browse — with no
  way to pull new images and redeploy an app you already run.

## Design

Three ideas do most of the work.

**The credential is the authorization.** Callers supply their own TrueNAS API
key; this server stores none. A session reaches exactly what that user's key
permits, revocation happens in the TrueNAS UI, and there is no shared secret to
leak. Authentication and authorization stop being two systems that can disagree.

**Reads and writes get different tool shapes.** Reads are grouped into
concern-level tools with an `op` enum, because they share most of their
arguments and 815 middleware methods cannot each become a tool. Writes are
individual tools — MCP annotations are per-tool, so bundling a safe operation
with a destructive one behind one `op` parameter would put both behind a single
consent gate, and a user who tires of confirming `list_pools` will allowlist the
tool that can also export a pool.

**Read-only by default.** Mutating tools appear only when explicitly enabled.
Separately, a denylist of unrecoverable operations is not reachable under any
configuration, and it constrains argument values rather than just method names —
deleting an app is recoverable, deleting it along with its volumes is not, and
those are the same method.

The full reasoning, including what was measured against a live box and which
decisions that overturned, is in [`openspec/changes/truenas-mcp-server/`](openspec/changes/truenas-mcp-server/).

## Requirements

- TrueNAS SCALE **25.04 or later**. The REST API is removed in TrueNAS 26; this
  server speaks only the versioned JSON-RPC 2.0 WebSocket API.
- A TrueNAS API key per user. Create them under **Credentials → API Keys**.

## Deploying as a TrueNAS app

Copy [`deploy/truenas-custom-app.yaml`](deploy/truenas-custom-app.yaml), adjust
`TRUENAS_MCP_TARGET`, and paste it into **Apps → Discover → Install via YAML**.

It mounts no host socket and requests no privileged access. The server reaches
the middleware over the network even when running on the same box, so that every
connection carries a user identity rather than root-equivalent socket access.

### Running it elsewhere

Nothing requires the server to run on the machine it manages, and there is a
good reason not to: installed as a TrueNAS app, it is unavailable exactly when
the box is unhealthy — which is when you most want to ask it what is wrong.

```
docker run -p 8080:8080 \
  -e TRUENAS_MCP_TARGET=nas.local \
  -e TRUENAS_MCP_TLS_CERT=/tls/cert.pem \
  -e TRUENAS_MCP_TLS_KEY=/tls/key.pem \
  ghcr.io/cedricziel/truenas-mcp:main
```

## Configuration

All configuration is environment variables; no config file or persistent volume
is needed. Invalid configuration refuses to start rather than running degraded.

| Variable | Default | Meaning |
|---|---|---|
| `TRUENAS_MCP_TARGET` | *required* | TrueNAS host, optionally `host:port` |
| `TRUENAS_MCP_LISTEN` | `:8080` | Bind address |
| `TRUENAS_MCP_TLS_CERT` / `TRUENAS_MCP_TLS_KEY` | — | Serve MCP over TLS |
| `TRUENAS_MCP_ALLOW_PLAINTEXT` | `false` | Serve without TLS (see below) |
| `TRUENAS_MCP_TARGET_INSECURE` | `false` | Accept the target's certificate unverified |
| `TRUENAS_MCP_TARGET_ALLOW_PLAINTEXT` | `false` | Connect to the target without TLS |
| `TRUENAS_MCP_ENABLE_WRITES` | `false` | Expose mutating tools |

**No credential is configurable here.** Callers supply their own.

### On the two TLS settings

Transport scheme and certificate verification are deliberately separate.

TrueNAS ships a self-signed certificate issued for `CN=localhost` with only
`DNS:localhost` as a SAN, so no address you can reach it by will validate. The
fix is `TRUENAS_MCP_TARGET_INSECURE=true`, which keeps the connection encrypted
and merely unauthenticated. If certificate problems forced you onto plaintext
instead, TrueNAS would see your API key in the clear — and revoke it.

`TRUENAS_MCP_ALLOW_PLAINTEXT` is about the boundary callers cross, which carries
their API keys. It is correct when a reverse proxy terminates TLS in front of
the server, and wrong when the plaintext listener is reachable directly.

## Connecting a client

```bash
claude mcp add --scope user --transport http truenas \
  https://your-host/mcp \
  --header "Authorization: Bearer $YOUR_TRUENAS_API_KEY"
```

The key may also be sent as `X-TrueNAS-API-Key`, for clients that cannot set an
`Authorization` header. Requests without either are refused with `401`.

## Current state

Working:

- Streamable HTTP transport, per-session credentials, `401` without one
- JSON-RPC middleware client: concurrent calls on one connection, structured
  errors distinguishing unreachable / unauthenticated / unauthorized / rate
  limited, and interrupted requests reported as *may have been applied*
- Session reconnection when a connection dies, and refusal to run against a
  release older than 25.04
- Container image, CI, GHCR publication, TrueNAS app deployment

**Not implemented:** job progress via resource *subscription*. Polling covers
the same ground and is the path the design treats as reliable — subscription
was always an enhancement over it, and MCP client support for it is thin.

### Tools

| Tool | Operations |
|---|---|
| `storage` | `list_pools`, `show_pool`, `list_datasets`, `show_dataset`, `list_snapshots` |
| `system` | `info`, `alerts`, `list_services`, `update_status`, `version`, `audit_log` |
| `sharing` | `list_smb`, `show_smb`, `smb_acl`, `list_nfs`, `show_nfs`, `list_web` |
| `virtualization` | `list_vms`, `show_vm`, `vm_devices`, `list_containers`, `show_container`, `container_devices` |
| `backup` | `list_cloud_syncs`, `show_cloud_sync`, `cloud_credentials`, `list_replications`, `show_replication`, `list_rsync_tasks`, `list_snapshot_tasks` |
| `filesystem` | `list_directory`, `stat`, `space`, `acl` |
| `apps` | `list`, `show`, `config`, `containers`, `outdated_images`, `upgrade_summary`, `rollback_versions`, `used_ports` |
| `jobs` | `list`, `show` |
| `search_methods` | find middleware methods by name |
| `describe_method` | a method's arguments, summarised |
| `call_method` | invoke a method directly |
| `server_info` | — |
| `system_info` | — |

Every tool declares a complete MCP annotation set — `title`, `readOnlyHint`,
`destructiveHint`, `idempotentHint`, `openWorldHint`. The spec defaults for
`destructiveHint` and `openWorldHint` are *true*, so an unset field does not
mean "unknown", it means "assume the worst" — and a read tool treated as
destructive produces prompts on safe operations, which is what teaches people
to click through the prompts that matter.

Every method behind the read tools is
verified against the target's own RBAC metadata to grant `READONLY_ADMIN`,
so "this tool cannot mutate" is checked rather than asserted.

The `apps` operations `outdated_images`, `upgrade_summary`, and
`rollback_versions` exist so a caller can decide *whether* to act before the
write tier can act — a mutation surface without them forces the model to
guess. All three take an app `name`; the middleware has no fleet-wide
equivalent.

### Write tools

Off by default. Set `TRUENAS_MCP_ENABLE_WRITES=true` to expose them.

| Tool | Effect | Annotated |
|---|---|---|
| `app_pull_images` | pull latest images and redeploy | destructive |
| `app_redeploy` | redeploy without pulling | destructive |
| `app_stop` | stop a running app | destructive, idempotent |
| `app_upgrade` | upgrade to a newer version | destructive |
| `app_rollback` | roll back a bad upgrade or pull | destructive |
| `app_start` | start a stopped app | idempotent |
| `create_snapshot` | snapshot a dataset | additive |
| `create_smb_share` | share a path over SMB | additive |
| `update_smb_share` | change an SMB share | destructive |
| `delete_smb_share` | stop sharing over SMB | destructive |
| `create_nfs_export` | export a path over NFS | additive |
| `update_nfs_export` | change an NFS export | destructive |
| `delete_nfs_export` | stop exporting over NFS | destructive |
| `set_smb_share_acl` | who may connect to a share | destructive |
| `set_path_acl` | filesystem permissions on a path | destructive |

**Share and permission configuration is the point.** It is the hardest part of
running TrueNAS and the least destructive: a misconfigured share is a support
thread, not data loss. Handing that to an assistant is squarely what this
server is for.

The one genuine hazard lives in an *argument*, not a method. `filesystem.setacl`
accepts `recursive`, `traverse`, and `stripacl` — recursive plus stripacl walks
a whole dataset discarding every ACL, which locks people out of terabytes and
cannot be undone without knowing what the previous permissions were. All three
are refused permanently, so setting one path's ACL stays available while the
unbounded form does not. That distinction is the entire reason the denylist
gates argument values rather than method names.

Each is a separate tool, so each is a separate consent decision — bundling
them behind one `op` would put `app_stop` behind the same gate as `app_start`.
`app_rollback` ships whenever the others do; it is the recovery path that makes
exposing them defensible.

Mutations never block. They return a `job_id` immediately; follow it with
`jobs(op="show", job_id=…)`.

**Denied under every configuration:** pool export, dataset deletion, disk wipe,
boot detach, snapshot destruction — and `app.delete` with `remove_ixvolumes`,
because the danger there is in the argument, not the method. None of this is
switchable; use the web interface.

**On app logs:** TrueNAS 26 exposes no JSON-RPC method that returns container
log output — the web UI streams it over a separate channel. `apps containers`
returns the container identities such a transport would need, and is useful on
its own. Log streaming is tracked as future work.

### The discovery escape hatch

The middleware has 815 methods across 74 namespaces. Most will never justify a
dedicated tool, so `search_methods` / `describe_method` / `call_method` cover
the tail without a code change per release.

Reachability is decided by the target's **own RBAC metadata**, not by guessing
from method names: a method is readable exactly when it grants
`READONLY_ADMIN`, and mutating methods need the write tier. That is the
middleware's own answer, so it is exact and tracks API versions without a
change here. It reaches **94% of the API** — 411 readable, 359 mutating.

The 6% withheld is deliberate:

- **`core.bulk`** invokes arbitrary methods; reachable, it would bypass the
  denylist, the write tier, and every other gate here.
- **`auth.*`** is the server's to manage. A caller driving it could mint a
  token that outlives the session and never appears in the API keys UI — a
  credential the operator never issued.
- **Methods declaring no roles at all.** On this target those are session and
  protocol plumbing, not harmless reads, so "no privilege check" is treated as
  unknown risk rather than no risk.

`describe_method` summarises rather than dumps. Measured on a live target,
`sharing.smb.create`'s schema is ~31,000 characters and
`directoryservices.update`'s ~53,000; models also fill large sparse schemas
less accurately than small dense ones, so a faithful dump costs more and works
worse. Pass `full=true` when you really want it.

### Resources

| URI | Content |
|---|---|
| `truenas://alerts` | current alerts |
| `truenas://system/health` | version, hostname, uptime, hardware |
| `truenas://pools` | pools with capacity and health |
| `truenas://apps` | installed apps and their state |
| `truenas://job/{id}` | a long-running operation's progress |
| `truenas://docs/query-filters` | filter syntax for `call_method` |
| `truenas://docs/dataset-properties` | ZFS field meanings and inheritance |

Resources differ from tools by *control locus*, not cost: tools are
model-controlled, resources are what a person attaches. They pay off when a
human points at one — no round trip, no tool budget — and underperform when a
model has to go find them, since model-driven resource access routes through
generic list/read tools and reintroduces the round trips it was meant to avoid.

So: addressable entities and reference material here, anything computed or
parameterised stays a tool. The documentation resources are the best value in
the design — they teach the filter syntax and ZFS semantics once instead of
repeating them in every tool description, where the tokens would be paid on
every request. A test asserts tool descriptions do not restate them.

## Releases

Releasing runs through [release-please](https://github.com/googleapis/release-please)
and nowhere else. It reads the conventional-commit history on `main`, keeps a
release PR open with the next version and changelog, and cutting a release is
merging that PR.

```
push to main ──▶ ci.yml        test, lint, publish :main and :sha-<commit>
             └─▶ release.yml   maintain the release PR
                                  │
                merge PR ─────────┴─▶ tag vX.Y.Z, GitHub release,
                                      publish :X.Y.Z :X.Y :latest
```

`ci.yml` deliberately does not react to tags, so a version tag cannot appear
without a release. The release job re-runs the tests against the tagged commit
before publishing — the tag is a different commit from the one CI last checked,
and a release is only as trustworthy as the tests that gated it.

**Token.** Set a `RELEASE_PLEASE_TOKEN` repository secret to a PAT with
`contents: write` and `pull-requests: write`. Without it the workflow falls back
to `GITHUB_TOKEN`, which works but cannot trigger downstream workflows — so the
release tag would not start the publish job.

## Development

```bash
make test     # unit tests
make lint     # go vet + golangci-lint
make build
make image
```

Integration tests need a live TrueNAS and are excluded from `make test`:

```bash
TRUENAS_TEST_URL=wss://nas.local/api/current \
TRUENAS_TEST_API_KEY=... \
TRUENAS_TEST_INSECURE=true \
go test -tags=integration ./...
```

This project is specified with [OpenSpec](https://github.com/Fission-AI/OpenSpec);
`openspec/changes/truenas-mcp-server/` holds the proposal, design decisions,
capability specs, and task breakdown.

## License

MIT. See [LICENSE](LICENSE).

The middleware client here is written rather than adapted from
`truenas/truenas-mcp`, which is GPL-3.0 — that is what keeps this project's
licensing choice open.
