## Purpose

Reconciles TrueNAS's asynchronous, event-driven job model with MCP's request/response tool model, so that long-running operations never block a tool call and their progress remains observable.

## Requirements

### Requirement: Never block a tool call on job completion

An operation that starts a long-running job on the target SHALL return as soon as the job is accepted. The server SHALL NOT wait for the job to finish before responding.

#### Scenario: Long-running operation started

- **WHEN** an operation that starts a job is invoked
- **THEN** the tool returns once the target accepts the job
- **AND** the response contains the job's identity and its addressable URI

#### Scenario: Target refuses to start the job

- **WHEN** the target rejects the request to start a job
- **THEN** the tool returns a structured error
- **AND** no job identity is returned

### Requirement: Make job status available by polling

The server SHALL expose job status through a read operation that reports state, progress, and outcome. Polling SHALL be sufficient to observe a job to completion without any notification support in the client.

#### Scenario: Job in progress

- **WHEN** job status is requested for a running job
- **THEN** the response reports the running state and the progress reported by the target

#### Scenario: Job completed successfully

- **WHEN** job status is requested for a job that finished successfully
- **THEN** the response reports success and the job's result

#### Scenario: Job failed

- **WHEN** job status is requested for a job that failed
- **THEN** the response reports failure and the error reported by the target

#### Scenario: Unknown job identity

- **WHEN** job status is requested for an identity the target does not recognize
- **THEN** the server returns a structured error distinguishing an unknown job from a failed one

### Requirement: List recent jobs

The server SHALL expose a read operation listing recent jobs with their state, so that a user can ask what the system has been doing without knowing a job identity.

#### Scenario: Listing recent activity

- **WHEN** the recent jobs list is requested
- **THEN** the response contains recent jobs with their identity, description, state, and completion time where applicable

### Requirement: Address jobs as resources

Each job SHALL have a stable resource URI whose content reflects the job's current state.

#### Scenario: Reading a job resource

- **WHEN** a job's resource URI is read
- **THEN** the content reports the same state, progress, and outcome available through polling

### Requirement: Push job updates where the client supports subscription

The server SHALL map the target's job events onto MCP resource update notifications for subscribed job resources. This SHALL be additive to polling and SHALL NOT be required for job observation.

#### Scenario: Client subscribes to a job resource

- **WHEN** a client subscribes to a running job's resource and the job's state changes on the target
- **THEN** the server emits a resource update notification for that URI

#### Scenario: Client does not support subscription

- **WHEN** a client that does not subscribe observes a job
- **THEN** polling reports the job's full lifecycle through to completion

#### Scenario: Job reaches a terminal state

- **WHEN** a subscribed job completes or fails
- **THEN** the server emits a final update notification reflecting the terminal state
