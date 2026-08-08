## Purpose

Serves MCP over an HTTP transport reachable from other machines, and authenticates each client session with its own TrueNAS credential so that what a session may do is bounded by that user's own privileges on the target rather than by a shared server-held key.

## ADDED Requirements

### Requirement: Serve MCP over an HTTP transport

The server SHALL serve MCP over an HTTP transport on a configurable bind address and port, so that clients on other machines can connect to it.

#### Scenario: Client connects from another machine

- **WHEN** a client on another machine connects to the configured address and port with valid credentials
- **THEN** the MCP session is established and tools are available

#### Scenario: Bind address is configurable

- **WHEN** the operator configures a bind address and port
- **THEN** the server listens on exactly that address and port

### Requirement: Authenticate each session with a caller-supplied TrueNAS credential

Each client SHALL supply a TrueNAS username and API key when establishing a session. The server SHALL NOT hold a TrueNAS credential of its own and SHALL NOT fall back to one.

#### Scenario: Valid caller credential

- **WHEN** a client establishes a session with a username and API key the target accepts
- **THEN** the session is established
- **AND** the server opens a middleware session under that identity

#### Scenario: Missing credential

- **WHEN** a client attempts to establish a session without supplying a credential
- **THEN** the server refuses the session
- **AND** no tool is served

#### Scenario: Credential rejected by the target

- **WHEN** the supplied credential is rejected by the target
- **THEN** the server refuses the session
- **AND** the error identifies authentication as the cause without disclosing the key

#### Scenario: No server-held fallback credential

- **WHEN** the server is inspected in any configuration
- **THEN** it holds no TrueNAS credential that could serve a session whose caller did not supply one

### Requirement: Bound each session by its own caller's privileges

Every middleware call SHALL execute under the credential of the session that requested it. A session SHALL NOT be able to reach capability its own credential does not permit.

#### Scenario: Caller lacks privilege for an operation

- **WHEN** a session invokes an operation its credential is not privileged for
- **THEN** the target refuses it and the server reports an authorization failure
- **AND** the server does not retry the call under any other credential

#### Scenario: Two sessions with different privileges

- **WHEN** two sessions with differently-privileged credentials invoke the same operation
- **THEN** each result reflects that session's own privileges

#### Scenario: Credential revoked mid-session

- **WHEN** a session's API key is revoked on the target while the session is open
- **THEN** subsequent calls fail with an authentication error
- **AND** the session does not continue to operate on a previously established connection

### Requirement: Require TLS on the MCP transport

The server SHALL require TLS on its HTTP transport by default, because caller credentials are transmitted on it. Plaintext SHALL require an explicit operator override.

#### Scenario: No TLS configured and no override

- **WHEN** the server starts without TLS configured and without the plaintext override
- **THEN** it refuses to start
- **AND** it reports that TLS is required and names the override

#### Scenario: Explicit plaintext override

- **WHEN** the operator sets the plaintext override
- **THEN** the server serves over plaintext
- **AND** it emits a startup warning that caller API keys will be transmitted in the clear and may be revoked by the target

### Requirement: Establish middleware sessions once and reuse them

The server SHALL establish a middleware session per authenticated MCP session and reuse it for that session's lifetime, rather than authenticating per tool call.

#### Scenario: Repeated tool calls in one session

- **WHEN** a session invokes several tools in succession
- **THEN** the server reuses the established middleware session
- **AND** it does not re-authenticate for each call

#### Scenario: Target authentication rate limit reached

- **WHEN** session establishment is refused for exceeding the target's authentication rate limit
- **THEN** the server reports that cause distinctly from invalid credentials
- **AND** it does not automatically retry in a way that prolongs the limit

#### Scenario: Session ends

- **WHEN** an MCP session closes
- **THEN** its middleware session is released
