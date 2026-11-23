# Proposal

## Why
MCP callers can inspect app containers but cannot execute maintenance or diagnostic commands inside them. Add explicitly opted-in, bounded execution using their own TrueNAS credentials.

## What Changes
- Add `container_exec` for Apps containers, requiring both write and exec configuration.
- Use TrueNAS webshell, with bounded combined terminal output and timeout; do not expose authentication tokens.
- Document permissions and command execution's broader authority over mounted data.

## Capabilities
### New Capabilities
- `container-exec`: bounded, per-caller execution inside an explicitly selected Apps container.
### Modified Capabilities
None.

## Impact
TrueNAS client, MCP registration, configuration, deployment template, tests and documentation. No host socket or SSH runtime dependency. System containers and host shells remain outside scope.
