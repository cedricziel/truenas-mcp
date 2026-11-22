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

### Requirement: Notify subscribers when live resources change

When a middleware session provider is configured, the server SHALL support resource subscriptions in both read-only and write-enabled configurations. MCP 2026-07-28 clients SHALL use `subscriptions/listen`; older clients SHALL retain `resources/subscribe` and `resources/unsubscribe` compatibility. Updates SHALL contain the changed resource URI so the client can read its current state.

The supported URI and middleware event source mappings SHALL be `truenas://alerts` to `alert.list`, `truenas://pools` to `pool.query`, `truenas://apps` to `app.query`, and `truenas://job/{id}` to `core.get_jobs`. Job IDs SHALL be canonical positive decimal integers without a sign or leading zeroes. Static documentation, system health, and unknown URIs SHALL reject subscriptions. System health includes time-dependent values without a corresponding event source.

#### Scenario: Subscribe to live state

- **WHEN** a client subscribes to the pools resource and the target emits a `pool.query` event
- **THEN** the client receives `notifications/resources/updated` for `truenas://pools`
- **AND** a 2026-07-28 notification identifies the `subscriptions/listen` request in its subscription metadata

#### Scenario: Job events stay scoped to the subscribed job

- **WHEN** a client subscribes to `truenas://job/7` and the target emits an event for job 8
- **THEN** that event does not notify the client about job 7
- **AND** an event identifying job 7 does notify the client about job 7

#### Scenario: Caller credentials restrict subscriptions

- **WHEN** clients with different credentials subscribe to the same URI
- **THEN** each upstream subscription uses that caller's authenticated middleware connection
- **AND** events from one credential's connection do not notify sessions belonging to another credential
- **AND** target-side subscription permission failures are returned to the requesting client

#### Scenario: Subscriptions release upstream resources

- **WHEN** the last subscriber to an event source unsubscribes, cancels its listen request, or closes its MCP session
- **THEN** the upstream subscription is canceled
- **AND** multiple subscribers sharing the same authenticated connection and event source retain their subscription until the last subscriber leaves
- **AND** abandoned HTTP sessions expire after a bounded idle interval
- **AND** legacy HTTP clients SHOULD send periodic keepalive pings to retain subscriptions because an open standalone SSE stream does not prevent idle expiry
- **AND** an active modern listen POST prevents idle expiry until the listen request ends

#### Scenario: Upstream connection is interrupted

- **WHEN** the middleware event stream ends or its connection is interrupted
- **THEN** the server releases the stream and stops forwarding its events
- **AND** a modern listen request ends with an error describing the upstream failure
- **AND** an affected legacy MCP session closes so the client reconnects
- **AND** subscriptions do not replay events missed during the interruption
- **AND** clients must read current state and establish a new subscription after the middleware connection is restored

#### Scenario: Reject overlapping modern resource listeners

- **WHEN** a listen request repeats a URI or overlaps an active resource listener on the same MCP session
- **THEN** the server rejects the new request before changing existing subscriptions
- **AND** the original listener continues receiving notifications with its original subscription metadata
- **AND** direct legacy resource subscribe and unsubscribe requests on a modern session cannot replace or remove modern listeners
