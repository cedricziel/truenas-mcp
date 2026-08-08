## Purpose

Provides the default read surface as a small set of concern-level action-dispatch tools, so that common questions about storage, sharing, apps, jobs, and system health are answerable in a single round trip without exposing any mutating operation.

## Requirements

### Requirement: Expose reads as concern-level dispatch tools

The server SHALL expose read operations through tools scoped to a user-facing concern rather than one tool per middleware method. Each tool SHALL take an `op` parameter selecting the operation.

The v1 concerns are `storage`, `sharing`, `apps`, `system`, and `jobs`.

#### Scenario: Default tool surface stays small

- **WHEN** a client lists the server's tools with the write tier disabled
- **THEN** the number of exposed tools does not exceed ten

#### Scenario: Reading pool capacity

- **WHEN** the model calls `storage` with the operation that lists pools
- **THEN** the server returns pool identity and capacity in a single tool call
- **AND** no additional tool call is required to answer how full a pool is

### Requirement: Constrain operations with a strict enum

Each dispatch tool's `op` parameter SHALL be a strict enumeration of that tool's supported operations.

#### Scenario: Unrecognized operation

- **WHEN** a tool is called with an `op` value outside its enumeration
- **THEN** the server returns a structured error
- **AND** the error lists the valid operations for that tool

### Requirement: Type arguments as a flat optional superset

Dispatch tool arguments SHALL be a flat set of optional parameters shared across that tool's operations, rather than a freeform object or a schema discriminated on `op`.

#### Scenario: Required argument omitted

- **WHEN** an operation that requires an identifier is called without one
- **THEN** the server returns a structured error naming the missing argument
- **AND** the error names the operation it applies to

#### Scenario: Irrelevant argument supplied

- **WHEN** an argument that does not apply to the selected operation is supplied
- **THEN** the server returns a structured error naming the arguments valid for that operation
- **AND** the server does not silently ignore the argument

#### Scenario: Arguments are not freeform

- **WHEN** a client inspects a dispatch tool's schema
- **THEN** every accepted argument is individually declared and typed
- **AND** no argument accepts an unconstrained object

### Requirement: Read tools contain no mutating operation

Every operation reachable through a read tool SHALL be free of side effects on the target. Read tools SHALL be annotated `readOnlyHint: true`.

#### Scenario: Annotation matches behavior

- **WHEN** the tool list is inspected
- **THEN** every concern-level dispatch tool is annotated read-only

#### Scenario: A mutating operation is introduced by mistake

- **WHEN** an operation whose underlying method mutates target state is added to a read tool's dispatch table
- **THEN** the build fails
- **AND** the failure names the offending operation and tool

### Requirement: Report per-operation authorization failures distinctly

When the configured API key lacks privilege for a requested operation, the server SHALL distinguish that from a failure of the operation itself.

#### Scenario: API key lacks privilege

- **WHEN** an operation is rejected by the target for insufficient privilege
- **THEN** the server returns a structured error identifying it as an authorization failure
- **AND** the error names the operation that was refused

### Requirement: Shape results for consumption rather than passing through raw payloads

Read operations SHALL return results shaped for the question being asked rather than the target's unmodified response, and SHALL bound result size.

#### Scenario: Large collection returned

- **WHEN** an operation would return more items than the configured result limit
- **THEN** the server returns the bounded set
- **AND** the response states the total count and that the result was truncated

#### Scenario: Verbose upstream payload

- **WHEN** the target returns a payload containing fields irrelevant to the operation
- **THEN** the server returns the fields relevant to the operation
- **AND** the response indicates how to retrieve the complete record
