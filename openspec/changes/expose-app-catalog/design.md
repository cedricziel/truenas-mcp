## Context

See proposal.md — Why.

The constraint that shapes everything here is in `middlewareParams` (`internal/server/concern_tool.go`). It turns dispatch arguments into middleware parameters through a hardcoded chain: `id`, `name`, and `path` become the single positional parameter; `pool` and `dataset` become equality filters; everything else is dropped. Which arguments an operation *accepts* is declared per-op (`Op.Args`), but what the dispatcher then *does* with them is not — that lives in the dispatcher, keyed by argument name across all concerns.

Two properties of the catalog collide with that. `show` must filter `app.available` by name, because the method is query-shaped and takes a filter list rather than a name positionally — but `name` is unconditionally positional in the chain. And `category` filters `categories`, a list-valued field, which needs a membership operator; the chain only ever emits `=`.

Both were verified against a live TrueNAS 26 target: `app.available` with `[["categories", "rin", "media"]]` returns the media apps, and with `[["name", "=", "plex"]]` returns the single entry. `catalog.get_app_details` was rejected as the detail source because it requires a second `app_version_details` object argument that the flat dispatch shape cannot carry.

## Goals / Non-Goals

**Goals:**

- One declaration site for how an operation's arguments reach the target, so adding a filter is a data change in the concern table rather than an edit to shared dispatch logic.
- The catalog usable in one call without the caller knowing middleware filter syntax.
- A browse that cannot exhaust a context window by default.

**Non-Goals:**

- Reworking how positional arguments are chosen. `id`/`name`/`path` stay as they are except where an operation declares otherwise; converting every positional to a declaration is a larger change with no caller-visible payoff here.
- A general filter-expression language. Operations declare fixed argument→field→operator triples; callers do not compose predicates. `call_method` remains the escape hatch for anything richer.
- Substring or fuzzy search over the catalog. Category plus exact name covers the cases that motivated this; a `query` argument can be added later against the same mechanism.

## Decisions

**Filters are declared as a triple, not a field map.** `Op` gains a slice of `{Arg, Field, Operator}` rather than `map[string]string`. A map cannot express the operator, and the operator is not derivable from the field name — `categories` needs membership while `pool` needs equality, and only the operation author knows which. Alternative considered: infer the operator from the value's type at runtime (list-valued fields get membership). Rejected as guessing: the field's shape lives on the target, not in the value the caller sent, so the inference would be right by luck.

**Declared filters widen what `Resolve` accepts.** An operation's allowed argument set becomes its positional `Args` plus the `Arg` of each declared filter, so a filter argument is neither refused nor silently dropped. This keeps the existing refuse-rather-than-ignore contract intact: an argument is valid exactly where some declaration mentions it.

**`pool` and `dataset` migrate to the same declaration.** Leaving them hardcoded would mean two mechanisms doing the same job, and the next filter would land in whichever one its author noticed first. This is the failure that produced the `limit` bug — per-case data maintained in one place while the behavior lived in another — and it is cheap to avoid now and expensive to unpick later.

**Positional wins over filter when an operation declares both.** An operation that takes an identifier positionally is fetching one record; a filter alongside it would be meaningless. Rather than defining a merge, the dispatcher resolves positional first and returns, which is what it already does.

**`show` is `app.available` filtered by name, not `catalog.get_app_details`.** The latter answers a slightly richer question but needs a second object argument, which would mean either a freeform object parameter — forbidden by the read-tools spec's "Arguments are not freeform" scenario — or a special case in the dispatcher for one method. Filtering `app.available` reuses the mechanism this change is already adding.

**`categories` is its own operation rather than documentation.** The category vocabulary is target-specific and changes as trains are added. Twenty short strings is a cheap call, and the alternative is a caller guessing a category name and reading an empty result as "nothing installed in that category" rather than "wrong word".

**The projection list mirrors `apps.list`'s reasoning.** Name, title, categories, train, both version fields, and installed state are what answer "what could I install and is it already here". Everything else — readme, schema, maintainers, icon, version history — is the bulk, and `full=true` returns it. The `installed` field earns its place specifically because without it a caller must cross-reference against `apps(op="list")` to avoid proposing something already running.

## Risks / Trade-offs

- **The catalog concern makes fourteen tools in the read tier** → Accepted, and called out in the proposal rather than absorbed silently. The tool-count bound in the read-tools spec is already stale at thirteen; a distinct, well-named tool is what protects selection accuracy, not the count. Correcting the bound is left to its own change so this one does not quietly rewrite a requirement it happens to breach.
- **Migrating `pool`/`dataset` touches working behavior for no user-visible gain** → Covered by keeping the existing storage tests unchanged: if `storage(op="list_datasets", pool=...)` still narrows at the target and still reports the filtered total, the migration is transparent. Any change to those tests would signal the refactor altered behavior, which it must not.
- **A declared filter whose field does not exist upstream fails at the target, not at the server** → Accepted. The server cannot know a middleware field's name is valid without introspecting every method's return shape, which it does not do for any other operation either. The failure surfaces as a target error, which the read-tools spec already requires be distinguishable from a server refusal.
- **`app.available` is served from the target's synced catalog, so it can be stale** → Out of scope to fix, but worth knowing: `catalog.sync` refreshes it and is a write. A caller reading a version here is reading what the box last synced, not what upstream publishes now. This is the same class of staleness as `update_available` on installed apps.
