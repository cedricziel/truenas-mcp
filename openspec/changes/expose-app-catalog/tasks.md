## 1. Declare filters on operations

- [x] 1.1 Write failing tests in `internal/tools/dispatch_test.go`: an op declaring a filter accepts that argument through `Resolve`; an op not declaring it still refuses it naming the valid arguments; declaring the same argument name positionally on one op and as a filter on another leaves each op's own validation unaffected.
- [x] 1.2 Add the filter declaration to `Op` in `internal/tools/dispatch.go` — argument name, middleware field, comparison operator — and widen `Resolve`'s allowed set to include declared filter arguments alongside `Args`. Comment why the operator is declared rather than inferred (see design.md).
- [x] 1.3 Write failing tests in `internal/server/` covering `middlewareParams`: a declared filter becomes a middleware filter triple using the declared operator; a declared membership operator is emitted as such rather than `=`; an op with both a positional identifier and a declared filter resolves positionally.
- [x] 1.4 Rewrite `middlewareParams` in `internal/server/concern_tool.go` to build filters from the op's declaration instead of the hardcoded `pool`/`dataset` chain. Keep positional resolution first and unchanged.

## 2. Migrate the existing hardcoded filters

- [x] 2.1 Move `storage/list_datasets`'s `pool` and `storage/list_snapshots`'s `dataset` onto the new declaration, with the equality operator.
- [x] 2.2 Confirm the existing storage tests still pass **unchanged** — narrowing still happens at the target and the reported total is still the filtered count. Any edit needed to those tests means the migration altered behavior; stop and investigate rather than adjusting the test.
- [x] 2.3 Delete the now-dead hardcoded filter branch and confirm no other code keys off `Op.Args` for `pool`/`dataset`.

## 3. Add the catalog concern

- [ ] 3.1 Add a `category` argument to `DispatchInput` in `internal/server/concern_tool.go`, carried into `args()` like the other narrowing arguments.
- [ ] 3.2 Write failing tests for the `Catalog()` concern's shape: it exposes `list`, `categories`, and `show`; `show` requires `name`; `list` declares a projection; no operation maps to a mutating middleware method.
- [ ] 3.3 Add `Catalog()` to `internal/tools/concerns.go` — `list` → `app.available` (filter `category` → field `categories`, membership operator; projected to name, title, categories, train, latest_version, latest_app_version, installed), `categories` → `app.categories` (no arguments), `show` → `app.available` (filter `name` → field `name`, equality; no projection). Write the concern's doc comment in the house style: why it is separate from `apps`, and why `list` must project.
- [ ] 3.4 Register the concern in `ReadConcerns()` and update the count in the comment above it.

## 4. Verify end to end

- [ ] 4.1 Add a server-level test proving a catalog browse over a fake multi-entry response is bounded and projected, and reports total, truncated, and projected.
- [ ] 4.2 Add a server-level test proving `show` returns the complete record for one named entry rather than a projected one.
- [ ] 4.3 Add a server-level test proving a category-narrowed browse sends the membership filter to the target and reports the filtered total, not the unfiltered one.
- [ ] 4.4 Confirm the existing read-only annotation test covers the new tool, extending its concern list if it enumerates them by name.

## 5. Ship

- [ ] 5.1 Run `make format`, `make lint`, and `go test ./...`; all must pass.
- [ ] 5.2 Check whether README.md enumerates the read tools; if it does, add the catalog concern in the same increment.
- [ ] 5.3 Sync the delta spec into `openspec/specs/read-tools/spec.md` and archive the change.
- [ ] 5.4 Commit in reviewable pieces — the filter mechanism plus its migration, then the catalog concern, then the spec sync — and open a PR.
