## Purpose

Exposes middleware jobs through the MCP Tasks extension, so a client that speaks it follows a mutation with the protocol's own task lifecycle instead of a server-specific polling tool, while a client that does not is unaffected.

## Requirements

### Requirement: Advertise the extension where jobs can start

The server SHALL declare `io.modelcontextprotocol/tasks` in its capabilities exactly when the write tier is enabled and a middleware session is available.

#### Scenario: Read-only server

- **WHEN** the server is built without the write tier
- **THEN** `server/discover` does not list the tasks extension

### Requirement: Return a task only to a client that declared the extension

When a write tool starts a job and the calling client declared the extension in its request capabilities, the server SHALL answer with a `CreateTaskResult` (`resultType: "task"`) carrying `taskId`, `status: "working"`, `createdAt`, `lastUpdatedAt`, `ttlMs`, and `pollIntervalMs`. A client that did not declare it SHALL receive the ordinary job-started result. A tool that starts no job SHALL never return a task.

#### Scenario: Undeclared client

- **WHEN** a client calls a write tool without declaring the extension
- **THEN** the result is the job-started result with `resultType: "complete"` and no task fields

#### Scenario: Declared client

- **WHEN** a client declaring the extension calls a write tool
- **THEN** the result is a `CreateTaskResult` and the job's own result is not included

### Requirement: Map the job lifecycle onto the task lifecycle

`tasks/get` SHALL report a running job as `working` with progress in `statusMessage`, a succeeded job as `completed` with the job's outcome in `result`, a failed job as `failed` with a JSON-RPC error in `error`, and an aborted job as `cancelled`. Terminal tasks SHALL suggest no poll interval.

#### Scenario: Job succeeds

- **WHEN** the job behind a task reaches SUCCESS
- **THEN** `tasks/get` returns status `completed` and a `result` whose structured content names the job, method, target, state, and the job's return value

#### Scenario: Job fails

- **WHEN** the job reaches FAILED
- **THEN** `tasks/get` returns status `failed` and an `error` naming the method and the target's message

### Requirement: Tasks are bound to the server that minted them

Task ids SHALL be random, minted per server, retained for a bounded TTL, and unknown to any other server. Unknown, expired, and pruned tasks SHALL be reported with JSON-RPC error `-32602`.

#### Scenario: Another credential

- **WHEN** a task id minted under one credential is polled under another
- **THEN** the server answers `-32602`

#### Scenario: Target pruned the job

- **WHEN** the target no longer has the job behind a task
- **THEN** `tasks/get` answers `-32602` and says the task expired

### Requirement: Cancellation is cooperative

`tasks/cancel` SHALL ask the target to abort the job and acknowledge the request whether or not the job could still stop. `tasks/update` SHALL acknowledge a known task and apply nothing, since no task here requests input.

#### Scenario: Cancel a finished job

- **WHEN** `tasks/cancel` names a task whose job already finished
- **THEN** the target's refusal to abort is not surfaced and the request is acknowledged
