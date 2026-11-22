## Purpose

Provide user-selected diagnostic prompts and live argument completion through MCP. Prompts return read-only evidence plans. Completion reads names and recent job IDs under the caller's own credential.

## Requirements

### Requirement: Publish diagnostic prompts

The server SHALL publish `diagnose_pool`, `investigate_app`, and `review_capacity` when a middleware session provider is available. `diagnose_pool` SHALL require a `pool` argument. `investigate_app` SHALL require an `app` argument. `review_capacity` SHALL accept an optional `pool` argument.

#### Scenario: Select a workflow

- **WHEN** a client requests one of these prompts with valid arguments
- **THEN** the server returns a user-role message with a read-only evidence plan
- **AND** obtaining the prompt does not open a middleware connection or execute tools

#### Scenario: No middleware provider

- **WHEN** the server has no middleware session provider
- **THEN** it publishes no diagnostic prompts or completion capability

### Requirement: Ground plans in available read operations

Pool diagnosis SHALL use pool details, datasets, alerts, and recent jobs. App investigation SHALL use app details, returned container IDs, bounded log tails, alerts, and recent jobs. Capacity review SHALL compare pool and dataset usage, quotas, reservations, and snapshot evidence without double-counting shared space.

Every plan SHALL require evidence-backed conclusions, disclosure of incomplete results, and separation of proposed repairs from the diagnostic workflow. Plans SHALL NOT execute mutations. Plans SHALL report permission failures rather than bypass the caller's privileges.

#### Scenario: App logs need a container

- **WHEN** an app investigation reaches log collection
- **THEN** the plan selects container IDs from `apps(op="containers")` before using `apps(op="logs")`
- **AND** it records unavailable logs as missing evidence

#### Scenario: Capacity data has no history

- **WHEN** capacity review has only current usage
- **THEN** the plan does not infer a growth rate

### Requirement: Validate prompt arguments

The server SHALL reject unknown arguments, missing required arguments, empty supplied names, names with surrounding whitespace or control characters, invalid UTF-8, and names exceeding 256 bytes with JSON-RPC error `-32602`. Supplied names SHALL be quoted as literal identifiers in generated plans.

#### Scenario: Unknown prompt

- **WHEN** a client requests an unknown prompt name
- **THEN** the server returns JSON-RPC error `-32602`

### Requirement: Complete pool and app names

The server SHALL complete `pool` for `diagnose_pool` and `review_capacity`, and `app` for `investigate_app`. Completion SHALL use `pool.query` or `app.query` under the calling user's middleware session. The query SHALL select only names, sort by name, escape the supplied prefix in its filter, and request at most 51 records.

#### Scenario: Prefix contains a regex metacharacter

- **WHEN** a client completes the prefix `ta.`
- **THEN** the query matches names beginning with the literal prefix `ta.`
- **AND** the response excludes names beginning with `taz`

### Requirement: Complete recent job IDs

The server SHALL complete the `id` argument for the resource template `truenas://job/{id}`. It SHALL inspect at most the 101 most recent jobs, select only their IDs, and match the supplied decimal prefix locally. Job completion SHALL be a bounded view of recent jobs rather than a complete job inventory.

#### Scenario: Complete a numeric prefix

- **WHEN** the recent job window contains IDs `123`, `120`, and `42` and the prefix is `12`
- **THEN** completion returns `120` and `123`

#### Scenario: Recent job window fills

- **WHEN** the target returns the full 101-record window
- **THEN** completion sets `hasMore` to true and omits an exact total
- **AND** the response does not claim that older matching IDs are absent

### Requirement: Bound completion responses and preserve errors

Completion SHALL return at most 50 unique, sorted, prefix-matching strings. It SHALL set `hasMore` when matches exceed that limit or the upstream query reaches its scan bound. It SHALL omit an exact total when the scan bound is reached. It SHALL return an empty list when no candidates match within the available window.

Unknown references and unsupported argument names SHALL return JSON-RPC error `-32602`. Invalid prefixes SHALL return the same error before opening a middleware session. Prefixes SHALL contain at most 256 bytes, valid UTF-8, no control characters, and no surrounding whitespace. Job prefixes SHALL be empty or decimal digits within the signed 64-bit range.

#### Scenario: Caller lacks permission

- **WHEN** the target refuses the completion query under the caller's credential
- **THEN** completion returns the target failure
- **AND** it does not return fabricated candidates or use another credential

#### Scenario: Unknown template

- **WHEN** a client requests completion for an unregistered resource template
- **THEN** the server returns JSON-RPC error `-32602`
