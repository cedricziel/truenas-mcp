# Design

## Context
The server has per-session RPC clients but no webshell transport. TrueNAS 25.10's webshell command option selects an executable; newer middleware accepts a script. Both accept /bin/sh and subsequent terminal input. Upstream reference: middleware apps/webshell_app.py at TS-25.10.0 and master.

## Goals / Non-Goals
Provide bounded Apps-container execution. Do not provide interactive terminals, system containers, host execution, separate stderr, or guaranteed remote process cancellation.

## Decisions
Use a dedicated /websocket/shell connection with a short-lived auth.generate_token token obtained internally from the session client. Keep generic authentication discovery blocked. Select /bin/sh, then send a quoted child shell command with random completion framing. Split framing literals in transmitted input to avoid terminal echo being mistaken for completion. Reject shell-quoted commands over 3500 bytes before contacting middleware to stay below canonical terminal line limits. Treat early disconnect as incomplete, never exit zero. Retain bounded output and close transport on cancellation or overflow. This supports the old executable and new script interpretations without version guesses.

## Risks / Trade-offs
- PTY output merges streams and can translate line endings → document terminal output semantics.
- Disconnect does not guarantee descendants stop → explicitly report incomplete execution and avoid retry.
- Commands can modify mounted datasets and bypass method-level semantic restrictions → separate exec opt-in and destructive annotation; the middleware denylist still applies to RPC methods.
- Container must include /bin/sh → surface failure without fallback.

## Migration Plan
Default remains disabled. Deploy tested build to Hive, enable writes and execution, verify caller privileges and a harmless command. Roll back image and disable exec if verification fails.
