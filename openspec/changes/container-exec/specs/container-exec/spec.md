## ADDED Requirements

### Requirement: Explicit execution opt-in
The server SHALL expose container_exec only when writes and TRUENAS_MCP_ENABLE_EXEC are enabled. The tool SHALL be destructive and non-idempotent, and SHALL require app, container and command. It SHALL never offer a host shell.

#### Scenario: Either gate disabled
- **WHEN** either execution or writes is disabled
- **THEN** container_exec is absent

### Requirement: Caller-scoped container transport
Execution SHALL use the caller's authenticated TrueNAS connection to obtain a short-lived internal token and connect to the same target's webshell. Both app and container SHALL be supplied, and TrueNAS SHALL validate membership and privileges. Tokens SHALL NOT appear in results or errors. Generic auth discovery SHALL remain blocked.

#### Scenario: Unauthorized caller
- **WHEN** TrueNAS rejects the token or container permissions
- **THEN** the tool fails without falling back to host execution or another credential

### Requirement: Bounded command results
The tool SHALL execute a shell command in an Apps container with /bin/sh, return combined terminal output and a completion exit code, and bound wall time (default 30, maximum 120 seconds) and retained output (default 16384, maximum 65536 bytes). Cancellation, timeout and output overflow SHALL close the shell connection. An incomplete result SHALL NOT claim successful completion or guaranteed process termination.

#### Scenario: Completed command
- **WHEN** the command finishes with an exit code
- **THEN** the result includes that code and combined output

#### Scenario: Deadline or output bound
- **WHEN** the time or output bound is reached
- **THEN** the connection closes and the result identifies incomplete execution

#### Scenario: Session isolation
- **WHEN** two callers execute commands
- **THEN** each uses only its own authenticated target client
