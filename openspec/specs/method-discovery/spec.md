## Purpose

Provides a bounded escape hatch to the long tail of TrueNAS middleware methods that do not justify hand-written tools, so that peripheral capability stays reachable across TrueNAS API versions without widening the server's reachable surface.

## Requirements

### Requirement: Bound discovery by a curated allowlist

The server SHALL restrict discovery to methods matching a curated allowlist. A method outside the allowlist SHALL be invisible to description and rejected on invocation.

#### Scenario: Allowlisted method

- **WHEN** a method matching the allowlist is described or invoked
- **THEN** the server serves the request

#### Scenario: Method outside the allowlist

- **WHEN** a method not matching the allowlist is invoked
- **THEN** the server refuses the call
- **AND** the error states the method is not exposed by this server

#### Scenario: Method outside the allowlist is not discoverable

- **WHEN** a method not matching the allowlist is requested for description
- **THEN** the server does not return its schema
- **AND** the response does not reveal the method's arguments

#### Scenario: Unknown method added by a future TrueNAS release

- **WHEN** the target exposes a method that did not exist when the allowlist was written and that does not match any allowlist pattern
- **THEN** that method is not reachable through discovery

### Requirement: Express the allowlist as namespace patterns

The allowlist SHALL be expressed as patterns over method namespaces rather than as an enumeration of individual method names, so it remains valid as the target's API version changes.

#### Scenario: Target upgraded to a newer API version

- **WHEN** the target is upgraded and existing methods are renamed or added within an allowlisted namespace pattern
- **THEN** discovery continues to serve them without a change to the server

### Requirement: Exclude mutating methods from discovery unless the write tier is enabled

Discovery SHALL be subject to the same gating as the write tier. Mutating methods SHALL NOT be reachable through discovery while the write tier is disabled, and denylisted methods SHALL NOT be reachable through discovery under any configuration.

#### Scenario: Mutating method with write tier disabled

- **WHEN** a mutating method is invoked through discovery while the write tier is disabled
- **THEN** the server refuses the call
- **AND** the error identifies the write tier as disabled

#### Scenario: Denylisted method through discovery

- **WHEN** a denylisted method is invoked through discovery with the write tier enabled
- **THEN** the server refuses the call

### Requirement: Summarize schemas rather than returning them raw

Method description SHALL return the commonly-used arguments, a worked example, and a count of omitted optional arguments, with an explicit option to retrieve the complete schema.

#### Scenario: Method with a large argument schema

- **WHEN** a method with many optional arguments is described without requesting the full schema
- **THEN** the response contains the required and commonly-used arguments and a worked example
- **AND** the response states how many optional arguments were omitted and how to retrieve them

#### Scenario: Full schema requested

- **WHEN** description is requested with the full-schema option
- **THEN** the complete argument schema is returned

#### Scenario: Field selection is derived, not hand-maintained

- **WHEN** the target's schema for a method changes between API versions
- **THEN** the summarized field selection reflects the new schema without a change to the server

### Requirement: Report invocation failures distinctly from refusals

Discovery invocation SHALL distinguish a server-side refusal from a failure reported by the target.

#### Scenario: Target rejects the arguments

- **WHEN** an allowlisted method is invoked with arguments the target rejects
- **THEN** the server returns the target's validation failure
- **AND** the error is identifiable as originating from the target rather than from server gating

#### Scenario: Invocation returns a job

- **WHEN** an invoked method starts a long-running job on the target
- **THEN** the server returns immediately with the job's identity rather than blocking
