## 1. Subscription support in the client

- [x] 1.1 Write failing tests in `internal/truenas/` against the existing fake target: opening a subscription delivers that source's events to its caller; two concurrent subscriptions do not cross; a message matching no subscription is discarded without affecting anyone.
- [x] 1.2 Add a subscription registry to `Client` and route server-initiated notifications to it, replacing the `continue` in `readLoop` that currently discards them. Keep discarding anything that matches no open subscription.
- [x] 1.3 Write failing tests for ending a subscription: reaching a bound unsubscribes; a cancelled context unsubscribes and returns without waiting; nothing is left registered afterward.
- [x] 1.4 Implement subscribe/unsubscribe, ensuring **every** exit path releases the registration. Comment why a leak here is worse than it looks — the connection is shared across the session's tools.
- [x] 1.5 Write failing tests for connection loss mid-subscription: the consumer is released with an error identifying the interruption, and the subscription is not resumed on reconnect. Check how `failPending` handles waiters today and follow that contract.
- [x] 1.6 Implement drop handling. Confirm the existing reconnect tests still pass **unchanged** — if any needs editing, stop and report rather than adjusting it.

## 2. Bounded collection

- [x] 2.1 Write failing tests for the collector: it stops at the requested count; at the byte bound; on a quiet period with no new event; on an overall deadline. Each outcome reports which bound applied.
- [x] 2.2 Implement the collector. It takes the count, the byte bound, the quiet period, and the deadline, and returns the entries plus which bound ended it. Keep it independent of both the log operation and the client so it can be tested directly.
- [x] 2.3 Write a failing test for a single entry larger than the whole byte bound, and make the result bounded rather than unbounded, saying content was withheld.

## 3. The logs operation

- [x] 3.1 Write failing tests for the op's shape: `apps` exposes `logs`; it requires an app name; `container` is optional; it maps to the event source rather than to a middleware method.
- [x] 3.2 Add the operation to the apps concern. An op backed by an event source has no `Method` in the sense the dispatcher assumes — decide how it declares itself and comment the choice. Do not build a general event-source declaration mechanism: one example is not enough to design one (see design.md, Non-Goals).
- [x] 3.3 Resolve an omitted `container` via the app's container list. Fail clearly when an app runs more than one and the caller named none, listing what they could have named.
- [x] 3.4 Correct the comment in `concerns.go` on the `containers` op that states TrueNAS exposes no method returning container log output. It is what stopped anyone looking for three releases — say what is actually true: no *method*, but an event source.
- [x] 3.5 Wire the op through the dispatch path, which currently assumes every op resolves to a method call.

## 4. End to end

- [x] 4.1 Add a server-level test driving `apps(op="logs", name=...)` against a fake target that emits events, asserting entries and timestamps reach the caller in order.
- [x] 4.2 Add a server-level test that a refusal from the event source — a stopped app — surfaces as the target's error rather than an empty result.
- [x] 4.3 Add a test proving no subscription outlives the call that opened it, on both the success and the error path. This is the leak the design calls the most dangerous failure.
- [x] 4.4 Confirm the read-only annotation test covers the new op, and that no existing test needed editing.

## 5. Ship

- [x] 5.1 Run `make format`, `make lint`, `go test ./...`, and `go vet -tags=integration ./internal/tools/`; all must pass.
- [x] 5.2 Update README.md if it enumerates the read operations.
- [x] 5.3 Verify against the live target if credentials are available; if not, say so plainly rather than implying it was exercised. The available target runs the pre-feature build (`ac7d2c6`), so this change could not be exercised there.
- [ ] 5.4 Sync the delta specs into `openspec/specs/` and archive the change.
- [ ] 5.5 Commit in reviewable pieces — client subscriptions, the collector, then the operation — and open a PR.
