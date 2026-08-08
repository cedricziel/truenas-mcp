## Purpose

Exposes addressable TrueNAS entities and reference documentation as MCP resources, so users can attach specific system state directly into a conversation and so shared query semantics are taught once rather than repeated in every tool description.

## Requirements

### Requirement: Expose addressable entities as resources

The server SHALL expose stable resource URIs for entities a user would refer to by name, including pools, applications, application logs, and the system alert list.

#### Scenario: Attaching a pool

- **WHEN** a user attaches a pool's resource URI to a conversation
- **THEN** the pool's current state is available in context without any tool call

#### Scenario: Attaching alerts

- **WHEN** the alert list resource is read
- **THEN** the content contains the target's current alerts

#### Scenario: Entity no longer exists

- **WHEN** a resource URI is read for an entity that no longer exists on the target
- **THEN** the server returns an error identifying the entity as absent rather than returning empty content

### Requirement: Publish reference documentation as resources

The server SHALL publish documentation resources covering the middleware query filter syntax and ZFS dataset property semantics. Tool descriptions SHALL reference these resources rather than restating their content.

#### Scenario: Filter syntax documentation

- **WHEN** the filter syntax documentation resource is read
- **THEN** the content explains the query filter form with worked examples

#### Scenario: Tool descriptions stay lean

- **WHEN** tool descriptions are inspected
- **THEN** no tool description restates the full filter syntax or the ZFS property reference
- **AND** descriptions needing that context point to the documentation resource

### Requirement: Restrict resources to read-only content

Reading any resource SHALL have no effect on the target's state.

#### Scenario: Resource read

- **WHEN** any resource URI is read
- **THEN** the target's configuration and data are unchanged

#### Scenario: Resources do not bypass gating

- **WHEN** the resource list is inspected with the write tier disabled
- **THEN** no resource exposes data the read tier would refuse
- **AND** no resource offers a mutating action

### Requirement: Keep parameterized queries in tools, not resources

Capability requiring computation, filtering, sorting, or arguments SHALL be exposed as a tool. Resources SHALL address existing entities and static documentation only.

#### Scenario: Computed question

- **WHEN** a user asks a question requiring filtering or aggregation across entities
- **THEN** it is served by a tool rather than a resource

### Requirement: Permit overlap between resources and read tools

The same underlying data MAY be reachable both as a resource and through a read tool. Both surfaces SHALL derive from a single read implementation and SHALL report consistent content.

#### Scenario: Same data through both surfaces

- **WHEN** the alert list is obtained through both its resource URI and the corresponding read operation
- **THEN** both report the same alerts
