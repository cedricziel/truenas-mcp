## Why

A caller can see every app installed on the box but cannot see what is available to install. The catalog is reachable only through the raw `call_method` escape hatch, which means an agent has to first guess that `app.available` exists and then hand-build middleware filter and `select` clauses to get anything usable out of it.

Reaching it that way is also dangerous rather than merely inconvenient. On a live target `app.available` returns 403 entries, each carrying its full HTML readme, schema, and version history. An agent that discovers the method and calls it plainly loses its context window — the same failure mode already fixed for `app.query`, except roughly an order of magnitude larger, and with no projection guarding it because it is not an operation.

## What Changes

- New `catalog` concern, exposed as a read-only dispatch tool covering apps available to install. It is separate from `apps`, which continues to cover only what is already installed — "what can I install" and "what is installed" are different questions, and a model choosing between two well-named tools does better than one choosing between operations on an overloaded one.
- Operations: `list` (browse, projected, filterable by category), `categories` (the category vocabulary, so a caller can filter without guessing), and `show` (one app in full, by name).
- `list` projects each entry down to identity and version fields by default, with `full=true` as the existing escape hatch. Without this the operation would be unusable for the reason described above.
- Operations gain the ability to **declare** which of their arguments become middleware query filters and under which comparison operator, replacing the hardcoded mapping in `middlewareParams`. This is the enabling change, not a cleanup: `show` needs `name` as a query filter rather than a positional parameter, and `category` needs the `rin` operator rather than `=` because the middleware field is a list. Neither is expressible today.
- The existing hardcoded `pool` and `dataset` filters move onto the same declaration, so there is one mechanism rather than a declared one beside a hardcoded one.

Not included, deliberately:

- Installing from the catalog. That is `app.create`, a write with a far larger blast radius and its own design questions (config schema, storage, ports); it belongs in the write tier and in its own change.
- `app.latest` and `app.similar`. Both are curated slices of the same data and can wait until someone wants them; they remain reachable through discovery.
- `catalog.get_app_details`. It requires a second `app_version_details` object argument that the flat dispatch shape cannot express, and `app.available` filtered by name answers the same question through the mechanism that already exists.

## Capabilities

### New Capabilities

<!-- None. Browsing the catalog is part of the existing read surface, not a new
     capability: it is another concern-level dispatch tool with the same
     annotations, shaping, and gating as the seven that exist. Introducing a
     capability per concern would fragment read-tools into a spec per tool. -->

### Modified Capabilities

- `read-tools`: Adds a requirement that an operation declares its own middleware filter mappings — which argument maps to which field under which comparison operator — rather than the dispatcher hardcoding the set of filterable arguments. Extends the concern list to include the catalog, and requires that a browse operation over a large upstream collection project by default.

## Impact

- `internal/tools/concerns.go` — new `Catalog()` concern; `ReadConcerns()` grows to eight; `pool`/`dataset` filter declarations move onto the new mechanism.
- `internal/tools/dispatch.go` — `Op` gains a filter declaration; `Resolve` must accept a declared filter argument as valid input for that operation.
- `internal/server/concern_tool.go` — `middlewareParams` builds filters from the op's declaration instead of a hardcoded chain; `DispatchInput` gains a `category` argument.
- No change to the write tier, the denylist, or discovery gating. Every operation added here is read-only and carries the existing `readOnlyHint` annotation.

Observed while scoping, pre-existing and not addressed here: the `read-tools` scenario "Default tool surface stays small" asserts the read tier exposes no more than ten tools. It already exposes thirteen with the write tier disabled (seven concerns, `jobs`, `server_info`, `system_info`, and the three discovery tools). This change makes it fourteen. The bound is already stale and the code carries a comment explaining why the original budget was abandoned; correcting the scenario is left to a separate change rather than quietly rewritten to match whatever the count happens to be.
