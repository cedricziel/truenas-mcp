# Live-box grounding findings

Measured against the target on 2026-08-08. Task group 1.

## Target

```
TrueNAS-26.0.0-MASTER+20260614-020151
```

A **development build of TrueNAS 26**, not a 25.x release. Consequences:

- REST is fully removed, so the JSON-RPC-only decision is not merely forward-looking but required.
- SCRAM-SHA-512 is the default API-key mechanism, so the key is not transmitted and the plaintext-revocation concern is materially reduced on this target — though it still applies to any 25.x box the server might point at.
- A MASTER build is a moving target. Method inventory and schemas may shift between pulls without a release boundary.

## Scale (tasks 1.1, 1.2)

| Measure | Value |
|---|---|
| Total methods | **815** |
| Top-level namespaces | **74** |
| Job methods | **104** |
| Filterable methods | **75** |
| Methods granting `READONLY_ADMIN` | **414** |

Design assumed "roughly 1000-1500". Actual is 815 — same order of magnitude, so the curation thesis holds: 815 methods against ~8 default tools is roughly 100:1.

Largest namespaces: `pool` 81, `vm` 50, `system` 49, `app` 41, `iscsi` 39, `nvmet` 37, `interface` 24, `cloudsync` 21, `zfs` 21, `sharing` 19.

## Introspection metadata (task 1.4)

Per-method keys available from `core.get_methods`:

```
accepts  returns  roles  job  filterable  filterable_schema
description  cli_description  examples  no_auth_required
no_authz_required  downloadable  uploadable  check_pipes
cli_private  pass_application
```

Richer than assumed. Three fields matter more than expected:

- **`roles`** — the middleware's own RBAC requirement per method. This is an authoritative, version-tracking read/write signal.
- **`job`** — boolean, so job methods are identifiable without heuristics.
- **`filterable`** — identifies which methods take the query filter DSL (75 of them).

### `roles` gives a clean read/write split

```
pool.query           ['POOL_READ','POOL_WRITE','READONLY_ADMIN','SHARING_ADMIN']
app.query            ['APPS_READ','APPS_WRITE','READONLY_ADMIN','SHARING_ADMIN']
app.pull_images      ['APPS_WRITE']                      job=True
app.delete           ['APPS_WRITE']                      job=True
app.start            ['APPS_WRITE']                      job=True
pool.dataset.delete  ['DATASET_DELETE']
```

A method is readable exactly when `READONLY_ADMIN` appears in its roles — 414 of 815.

## Argument schema sizes (task 1.3)

Serialized `accepts` length in characters:

| Method | Chars | ≈ tokens |
|---|---|---|
| `directoryservices.update` | 53,138 | ~13,000 |
| `sharing.smb.create` | 31,172 | ~8,000 |
| `sharing.smb.update` | 31,125 | ~8,000 |
| `pool.dataset.create` | 22,312 | ~5,600 |
| `cloudsync.credentials.create` | 23,754 | ~6,000 |
| `pool.query` / `app.query` | 3,716 | ~900 |
| `app.create` | 2,405 | ~600 |
| `app.rollback` | 791 | ~200 |
| `app.pull_images` | 622 | ~150 |
| `app.redeploy` | 194 | ~50 |

Design D7's premise is confirmed and then some: a raw schema dump for `sharing.smb.create` alone would cost roughly 8,000 tokens, and `directoryservices.update` about 13,000. Summarization is mandatory, not an optimization.

**However:** only **34 of 815** methods have a populated `examples` field. D7 assumed `describe_method` could return a worked example derived from introspection. For 96% of methods there is nothing to derive, so examples must be synthesized or authored.

## The app surface (task 1.6 follow-up)

Far richer than scoped, and directly relevant to the driving use case:

```
app.pull_images   [JOB]   app_name + {redeploy: bool = true}
app.redeploy      [JOB]
app.rollback      [JOB]   + app.rollback_versions
app.upgrade       [JOB]   + app.upgrade_bulk, app.upgrade_summary
app.start/stop/update  [JOB]
app.outdated_docker_images     ← answers "what needs updating?"
app.latest, app.available, app.config, app.query
app.image.pull [JOB], app.image.query, app.image.delete
```

`app.pull_images` signature confirmed:

- `app_name` — string, required
- `options.redeploy` — boolean, **default `true`**

So the driving command's explicit `{"redeploy": true}` restates the default.

Critically, **`app.rollback` exists**. Design D12 asserted that a redeploy is "not cleanly reversible"; that is wrong on this target. A bad image pull is recoverable via rollback, and `app.rollback_versions` enumerates the options.

## Not yet done

- **1.5** — the job *event stream* shape is unverified. `core.get_jobs` exists and requires no specific role, and 104 methods are job-typed, but confirming the push-notification payload needs a live WebSocket subscription rather than `midclt`.
- **1.7** — other prior art (`sonicaj/tn_mcp`) not reviewed.

## Design impact

| Decision | Impact |
|---|---|
| **D5** allowlist | **Improve.** Role-based gating beats namespace patterns — authoritative, tracks versions automatically, semantically exact. |
| **D3** read-tool CI assertion | **Improve.** Derive "is this op mutating" from `roles` rather than hand-maintaining a denylist. |
| **D7** schema summarization | **Confirmed** on size; **partially invalidated** on examples — 96% of methods have none to derive. |
| **D6** concern grouping | **Correct.** No `virt` namespace on 26; virtualization is `vm` / `container` / `lxc` / `docker`. |
| **D12** write scope | **Revise.** App surface is richer than scoped, and `app.rollback` makes redeploy reversible. |
| Minimum version | Target is 26; the 25.04 floor still stands but is untested here. |

---

# Implementation outcomes

Recorded at the end of the build (task 13.7).

## Design open questions, resolved

- **Does `network` earn a place in the default read tier?** No. It was dropped;
  the surface settled on `storage` / `system` / `apps` / `jobs`. Nothing has
  wanted it.
- **Should job resources expose intermediate percentages or only state
  transitions?** Percentages. The live target reports smooth progress
  (`RUNNING 20%` → `SUCCESS 100%`) at a volume that is not chatty.

## Decisions the implementation overturned

| Decision | What happened |
|---|---|
| **D5** allowlist as namespace patterns | Became patterns over *method shapes* (`.query`, `.create`). Write shapes must be matched **before** read shapes: `app.pull_images` ends in `_images`, which also reads as a read suffix, and matching reads first let it through the gate. |
| **D7** summarise the argument schema | Correct in principle, wrong in level. Middleware methods take one object parameter wrapping the real arguments, so top-level summarising reduced nothing. Flattening into the object gives ~3.8× on `sharing.smb.create`. |
| **D7** derive worked examples from `examples` | Only 34 of 815 methods populate it. Examples are constructed from required parameters instead. |
| **D3/D5** hand-maintained read/write split | Replaced by the target's own `roles` metadata: a method is readable exactly when it grants `READONLY_ADMIN`. Asserted live for every read operation. |
| **Connection spec** username + API key | Wrong. `auth.login_with_api_key` takes only the key; TrueNAS keys are user-linked. The requirement came from the Python client's README, not the middleware. |
| **D6** `virt.*` namespace | Does not exist on TrueNAS 26; virtualization is `vm` / `container` / `lxc`. |
| **D12** redeploy is "not cleanly reversible" | Wrong: `app.rollback` exists, which widened the v1 write scope rather than narrowing it. |

## Bugs the tests caught

- `outdated_images` declared a required parameter as optional, so calls without
  it returned `null` — a silent wrong answer. Now derived from the target's
  `_required_` flags in both directions.
- Three `any`-typed output fields generated the bare `true` JSON Schema, which
  Claude Code rejects during tool-list validation. Because a client that cannot
  parse the list drops the whole server, three fields disabled all sixteen tools.
- A recursive `SchemaParam` could not be expressed as an output schema at all.

## Not implemented

- **Job event subscription** (9.7, 9.8) and the event stream shape it depends on
  (1.5). Polling is specified as the reliable path and is verified working; the
  subscription was always designed as an enhancement over it.
- **Negotiated auth mechanism reporting** (3.5). The target runs TrueNAS 26,
  where SCRAM is the default and the key is not transmitted, so the reporting
  this would add has no decision hanging on it today.
- **Plaintext loopback revocation check** (3.15). Untested; the deployment does
  not use a plaintext loopback hop.
- **Two-user privilege verification** (4.8). The per-session credential path is
  verified, but not with two TrueNAS users of differing privilege.
