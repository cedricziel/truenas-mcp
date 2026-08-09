## Context

See proposal.md — Why.

Everything in this server is request/response: `Client.Call` writes a JSON-RPC request with an id, parks a waiter under that id, and `readLoop` hands the reply back when it arrives. Server-initiated messages carry no id and no waiter, and `readLoop` steps past them:

    // Server-initiated notifications carry no ID and no waiter.
    if resp.ID == 0 && resp.Method != "" {
        continue
    }

That `continue` is the whole reason event sources are unreachable. Nothing else about the design forbids them.

Confirmed live against TrueNAS 26 during scoping:

- The subscription name carries its arguments as a JSON suffix: `app.container_log_follow:{"app_name":"…","container_id":"…","tail_lines":5}`.
- Arguments are `app_name`, `container_id`, and `tail_lines` (default 500; `null` means the entire log).
- Each event carries `{data, timestamp}` — one log line, and its time converted to the target's configured zone.
- The source replays `tail_lines` of backlog immediately on subscribe, then follows indefinitely.
- It declares `APPS_READ`.
- It refuses an app that is not RUNNING, CRASHED, or DEPLOYING, and refuses a `container_id` that does not belong to the named app.

## Goals / Non-Goals

**Goals:**

- Make event sources reachable at all, in a way the next one to be exposed can reuse without redesign.
- Answer "why is this app broken" in one call, without the caller knowing what an event source is.
- Bound the result on the axis text actually grows along.

**Non-Goals:**

- A live follow. See proposal.md for why the tail answers the question and the follow costs several times more.
- A general event-source dispatcher declared in the concern table. One event source is not enough evidence to know what that abstraction should look like; a second one will teach more than guessing now.
- Resuming a subscription across a reconnect. A consumer cannot tell how much it missed, so a resumed stream would misrepresent itself as continuous.

## Decisions

**The tail is the backlog, and the backlog is a side effect of subscribing.** The source replays `tail_lines` before it starts following, so subscribing with `tail_lines: N`, taking what arrives, and unsubscribing yields exactly `tail -n N`. No filtering or bookkeeping is required. Alternative considered: subscribe with `tail_lines: null` and take the first N. Rejected — that asks the target to replay the entire log to deliver twenty lines.

**Stopping needs a quiet period, not just a count.** A container with fewer lines than requested never delivers N events, and one that is actively logging never stops delivering them. So collection ends at whichever comes first: the requested count, the byte bound, a short quiet period with no new event, or an overall deadline. The quiet period is what separates "the backlog is drained" from "the container is idle", and both mean the same thing to a caller asking for a tail. Alternative considered: rely on the count alone. Rejected — an app that logged ten lines at startup and nothing since is the exact case someone debugging looks at.

**The byte bound is a first-class result, not a safety net.** `tail_lines` bounds count; nothing bounds width. A container emitting a base64 payload or a stack trace per line produces a response orders of magnitude past what the line count suggests. The response says which bound applied, because "200 lines" and "as much as fitted" are different answers and a caller acting on the second needs to know it is looking at a fragment. This mirrors the existing `Truncated` contract rather than inventing one.

**Subscription plumbing lives in the client, not the tool.** `readLoop` already owns the connection and already sees these messages; routing them is a few lines there and a registry keyed by subscription. Putting it in the tool would mean a second reader on the same socket. The registry is deliberately not exposed through the discovery tier — `core.subscribe` stays excluded there, since a caller driving subscriptions directly could open unbounded streams on a shared connection.

**The operation is `apps(op="logs")`, not a new concern.** Logs are an attribute of an installed app; `apps` already covers containers, images, and state. The `container` argument is optional — most apps run one container, and `app.container_ids` resolves it. Requiring the caller to pass a 64-character container id for the common case would make the operation unpleasant enough to avoid.

**A refusal from the source is returned as the target's.** A stopped app produces a refusal, and it is a correct and informative one. Translating it into an empty result would tell the caller the app has no logs, which is a different and wrong claim.

## Risks / Trade-offs

- **An abandoned subscription streams forever on a shared connection** → The single most dangerous failure here. Every exit path — bound reached, deadline, quiet period, caller cancellation, error — must unsubscribe, and the tests must cover the paths that are not the happy one. The connection is shared across a session's tools, so a leak degrades everything, not just logs.
- **The quiet period is a heuristic and can truncate a slow producer** → Accepted, and reported: if collection ends on the quiet period rather than the count, the response says the tail is what was available, not that the log is that short. A caller wanting more calls again.
- **Timestamps come from the target's configured timezone, not UTC** → The source converts using the system's timezone setting, so entries are returned as sent rather than normalized. Normalizing would mean reparsing a string the target already parsed, and getting it wrong in a way that is hard to notice. The values are passed through as received.
- **Reconnect during a tail returns an error where a retry might have succeeded** → Accepted per the spec: silently re-subscribing would deliver a stream with an unknown gap in it, presented as continuous. An error the caller can retry is the honest failure.
- **Log content is returned verbatim and may contain secrets** → Not mitigated, and not mitigable here: this server cannot know which substrings of an arbitrary application's output are sensitive, and redaction heuristics would corrupt legitimate content while missing real secrets. Worth noting because the operation is read-only and therefore reachable with the write tier disabled — the bound on it is the caller's own API key carrying `APPS_READ`, which is the same bound the web UI applies.
