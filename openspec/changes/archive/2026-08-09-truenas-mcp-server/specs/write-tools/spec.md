## Purpose

Provides the opt-in mutation surface, structured so that every mutating operation carries its own consent decision and its own accurate MCP annotation, and so that unrecoverable operations remain unreachable regardless of configuration.

## ADDED Requirements

### Requirement: Disable the write tier by default

The server SHALL expose no mutating tool unless the write tier is explicitly enabled by configuration. Enablement SHALL NOT be possible through a tool call.

#### Scenario: Default configuration

- **WHEN** the server starts with no write-tier configuration
- **THEN** the tool list contains no mutating tool
- **AND** the server reports at startup that it is running read-only

#### Scenario: Write tier enabled

- **WHEN** the operator enables the write tier by configuration
- **THEN** the permitted mutating tools appear in the tool list
- **AND** the server reports at startup which mutating tools are exposed

#### Scenario: Model attempts to widen its own permissions

- **WHEN** any tool is invoked
- **THEN** no tool schema offers a parameter that enables the write tier or alters gating

### Requirement: Expose each mutating operation as its own tool

Mutating operations SHALL be exposed as individual tools, one per operation. Mutating operations SHALL NOT be reachable through an `op` parameter on a shared tool.

#### Scenario: Mutations are individually addressable

- **WHEN** the write tier is enabled and the tool list is inspected
- **THEN** each mutating operation appears as a separately named tool

#### Scenario: Safe and destructive operations are not bundled

- **WHEN** the tool list is inspected
- **THEN** no tool exposes both a non-mutating and a destructive operation
- **AND** consenting to one mutating tool does not grant any other mutating operation

### Requirement: Annotate each mutating tool according to its actual effect

Each mutating tool SHALL declare MCP annotations that accurately describe that specific operation, including `destructiveHint` and `idempotentHint`.

#### Scenario: Destructive operation

- **WHEN** a tool's operation removes or overwrites data or access
- **THEN** the tool is annotated destructive

#### Scenario: Additive operation

- **WHEN** a tool's operation only creates new state and does not remove or overwrite existing state
- **THEN** the tool is not annotated destructive
- **AND** its annotations reflect whether repeating it is safe

### Requirement: Refuse denied argument combinations as finally as denied methods

The denylist SHALL be able to constrain argument values, not only method names, so that an operation which is safe or unsafe depending on its arguments can be permitted in its safe form and refused in its unsafe one.

#### Scenario: Method permitted, argument combination denied

- **WHEN** an operation is invoked whose method is permitted but whose arguments match a denied combination
- **THEN** the server refuses the call
- **AND** the error names the argument that caused the refusal
- **AND** the refusal is not overridable by configuration

#### Scenario: Same method in its safe form

- **WHEN** the same method is invoked without the denied argument combination
- **THEN** the operation proceeds

#### Scenario: Denied argument combination through discovery

- **WHEN** a denied argument combination is invoked through the discovery path rather than a tool
- **THEN** the server refuses the call

#### Scenario: Denied arguments are not exposed as tool parameters

- **WHEN** a mutating tool's schema is inspected
- **THEN** no parameter offers a value that the denylist would refuse

### Requirement: Refuse unrecoverable operations unconditionally

The server SHALL maintain a denylist of unrecoverable operations — including pool export and destruction, and disk wipe and format — which SHALL NOT be exposed as tools and SHALL NOT be reachable by any other means. No configuration SHALL make them reachable.

#### Scenario: Denylisted operation is not exposed

- **WHEN** the write tier is enabled with the broadest permitted configuration
- **THEN** no tool exposes a denylisted operation

#### Scenario: Denylisted operation reached indirectly

- **WHEN** a denylisted method is requested through any other server capability
- **THEN** the server refuses the call
- **AND** the error states the operation is permanently unavailable and directs the user to the TrueNAS web interface

#### Scenario: Configuration attempts to unlock a denylisted operation

- **WHEN** configuration names a denylisted operation for exposure
- **THEN** the server refuses to expose it
- **AND** the server reports the refused entry at startup rather than failing silently

### Requirement: Identify the target of a mutation before applying it

A mutating tool SHALL resolve and report the concrete target it will act upon, so that consent is given against a specific object rather than a name that may be ambiguous.

#### Scenario: Target resolves to exactly one object

- **WHEN** a mutating tool is invoked with an identifier resolving to one object
- **THEN** the operation proceeds
- **AND** the result names the object that was acted upon

#### Scenario: Target is ambiguous or absent

- **WHEN** the supplied identifier matches no object or more than one
- **THEN** the server refuses the operation without contacting the target for a mutation
- **AND** the error reports what the identifier matched

### Requirement: Expose the app lifecycle as the v1 write surface

The v1 write tier SHALL expose app image pull with redeploy, redeploy, start, stop, upgrade, rollback, and snapshot creation, and no other mutating operation.

#### Scenario: v1 write tier enabled

- **WHEN** the write tier is enabled on a v1 server
- **THEN** exactly the v1 operations are exposed as mutating tools
- **AND** no operation whose blast radius is data rather than a service is exposed

#### Scenario: Gating machinery is exercised

- **WHEN** the v1 write tier is disabled
- **THEN** no app lifecycle tool and no snapshot creation tool is present

#### Scenario: App deletion is not in v1

- **WHEN** the v1 write tier is enabled
- **THEN** no tool deletes an app

### Requirement: Pull images and redeploy an existing app

The server SHALL expose pulling an app's images with an option to redeploy it afterwards.

#### Scenario: Pulling images with redeploy

- **WHEN** the tool is invoked for an installed app with redeploy requested
- **THEN** the server starts the operation and returns its job identity without waiting for completion

#### Scenario: Redeploy option defaults to enabled

- **WHEN** the tool is invoked without specifying whether to redeploy
- **THEN** the app is redeployed after images are pulled

#### Scenario: App is not installed

- **WHEN** the tool is invoked for an app that is not installed
- **THEN** the server refuses the operation without starting a job
- **AND** the error names the app that was not found

### Requirement: Provide the reads that inform a lifecycle decision

The server SHALL expose, as read operations, which apps have outdated images, what an upgrade would change, and which versions an app can be rolled back to.

#### Scenario: Deciding what needs updating

- **WHEN** the outdated-images read operation is invoked
- **THEN** the response identifies apps whose images are out of date

#### Scenario: Deciding whether to upgrade

- **WHEN** the upgrade-summary read operation is invoked for an installed app
- **THEN** the response describes what the upgrade would change

#### Scenario: Deciding whether a rollback is available

- **WHEN** the rollback-versions read operation is invoked for an installed app
- **THEN** the response lists the versions it can be rolled back to

#### Scenario: Reads remain available with the write tier disabled

- **WHEN** the write tier is disabled
- **THEN** these read operations remain available

### Requirement: Expose rollback whenever any app mutation is exposed

Rollback SHALL be exposed whenever the write tier is enabled, so that no exposed app mutation lacks its recovery path.

#### Scenario: Write tier enabled

- **WHEN** the write tier is enabled
- **THEN** a rollback tool is present alongside the mutating app tools

#### Scenario: Rolling back after a bad upgrade

- **WHEN** rollback is invoked for an app with an available prior version
- **THEN** the server starts the rollback and returns its job identity

#### Scenario: No prior version available

- **WHEN** rollback is invoked for an app with no version to roll back to
- **THEN** the server refuses the operation without starting a job
