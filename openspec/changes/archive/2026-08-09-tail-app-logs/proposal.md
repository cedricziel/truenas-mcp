## Why

A caller can see that an app is running, which containers it runs, and which images those containers execute — and then cannot see why any of it is failing. Logs are the first thing a person reaches for when an app misbehaves, and this server offers no path to them at all.

The code says that is a middleware limitation. `concerns.go` records that "TrueNAS 26 exposes no JSON-RPC method returning container log output", and `apps(op="containers")` exists as the nearest available substitute. That comment is literally true and thoroughly misleading. Container logs are exposed — as an **event source** rather than a method, and event sources do not appear in `core.get_methods`, so neither `search_methods` nor any amount of method introspection can find one. The capability was there the whole time, one namespace over from where anyone looked.

`app.container_log_follow` was confirmed working against a live TrueNAS 26 target during scoping, streaming this server's own container output. It declares `APPS_READ`, so it is a read by the target's own classification.

## What Changes

- New read operation returning the tail of one container's log output.
- **Middleware event subscriptions become possible at all.** The client currently discards every server-initiated notification — `readLoop` sees messages with no id and no waiter and continues past them. Nothing in this server has ever consumed one. This is the enabling change and the bulk of the work.
- The operation is a bounded tail, not a follow. The event source replays a requested number of backlog lines on subscribe and then streams indefinitely; this takes the backlog and unsubscribes. That is `tail -n`, which is the question an agent actually asks, and it fits the request/response shape of a tool. A live follow does not.
- Results are bounded by lines **and** by bytes, and say which bound was hit. A line-count cap alone does not bound a response: a single log line can be arbitrarily long.
- Log content is returned with its timestamps, so a caller that calls again can recognize what it has already seen without this server holding per-caller cursor state.

Not included, deliberately:

- **Follow / streaming.** Delivering a live stream through MCP means either resource subscriptions, where the client is told "something changed" and must re-read, or polling against a cursor the event source does not offer. Both are substantially more work than the tail, and the tail answers the common question. Revisit when something concrete needs it.
- **`app.stats`**, the other event source found alongside this one. It becomes reachable once subscriptions exist, but a metrics stream is its own design question.
- **Log search or filtering.** The event source offers no predicate; filtering here would mean fetching everything and discarding most of it, which is what the byte bound exists to prevent.
- **Logs for stopped apps.** The event source refuses an app that is not RUNNING, CRASHED, or DEPLOYING. The refusal is the target's and is passed through rather than worked around.

## Capabilities

### New Capabilities

- `event-subscriptions`: Subscribing to middleware event sources and consuming what they emit — establishing a subscription, routing events to the caller that asked for them, ending a subscription, and behaving correctly when the connection drops mid-stream. This is genuinely new: it is the first thing in this server driven by the target rather than by a caller's request, and it is not describable as a variation on the request/response tools that exist.

### Modified Capabilities

- `read-tools`: Adds a requirement that a read operation may be served by an event source rather than a method, and that such an operation bounds its result by size as well as by count and reports which bound applied.

## Impact

- `internal/truenas/` — the client gains subscription support: correlate a subscription, route its events, unsubscribe, and fail cleanly on reconnect. `readLoop`'s discard of server-initiated notifications is where this lands.
- `internal/tools/concerns.go` — a logs operation on the apps concern, and the correction of the comment that says this is impossible.
- `internal/server/` — the dispatch path currently assumes every operation resolves to a middleware method call; an event-source-backed operation does not.
- No change to the write tier or the denylist. `app.container_log_follow` declares `APPS_READ` and belongs in the read surface.

Worth stating plainly, since it is the most reusable thing here: the reason this capability was invisible is that `search_methods` can only see methods. Any future agent will hit the same wall for any other event source. Whether discovery should surface event sources too is a real question this change does not answer.
