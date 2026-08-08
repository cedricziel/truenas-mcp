## Purpose

Manages the server's authenticated connection to a single TrueNAS SCALE instance over the versioned JSON-RPC 2.0 WebSocket API, including credential handling, API version pinning, reconnection, and connection-level error reporting.

## Requirements

### Requirement: Connect over the versioned JSON-RPC WebSocket API

The server SHALL connect to the target TrueNAS SCALE instance using the JSON-RPC 2.0 over WebSocket API. The server SHALL NOT use the deprecated REST API for any operation.

#### Scenario: Successful connection at startup

- **WHEN** the server starts with a valid host and API key
- **THEN** it opens a WebSocket connection to the target's JSON-RPC endpoint
- **AND** it reports the negotiated API version in its startup diagnostics

#### Scenario: Target is unreachable

- **WHEN** the target host does not accept a WebSocket connection
- **THEN** the server starts and remains available
- **AND** every tool call returns a structured error identifying the connection failure and the target host
- **AND** the error distinguishes an unreachable host from an authentication failure

### Requirement: Authenticate each connection with a user-linked API key

The server SHALL authenticate to the middleware with a user-linked API key. TrueNAS API keys are bound to a user account, so the key alone identifies the caller and no separate username is required or accepted. The credential SHALL be the one supplied by the MCP session on whose behalf the connection is opened, and SHALL NOT be accepted as a tool argument.

#### Scenario: Valid credential

- **WHEN** the server authenticates with a valid API key
- **THEN** authentication succeeds and the connection is established for that session
- **AND** the identity the target associates with the key becomes the session's identity

#### Scenario: Invalid or revoked API key

- **WHEN** the credential is rejected by the target
- **THEN** the server returns a structured error identifying authentication as the cause
- **AND** the error does not include the API key value

#### Scenario: Expired API key

- **WHEN** the target reports the credential as expired rather than invalid
- **THEN** the server distinguishes expiry from rejection in the error it returns

#### Scenario: A second authentication factor is required

- **WHEN** the target reports that a one-time password is required to complete login
- **THEN** the server reports that the credential cannot complete authentication unattended
- **AND** it does not leave the connection in a half-authenticated state

#### Scenario: Authentication rate limit reached

- **WHEN** the target rejects a login attempt for exceeding its authentication rate limit
- **THEN** the server distinguishes this from invalid credentials
- **AND** it does not immediately retry in a way that prolongs the limit

#### Scenario: Credentials are never accepted from the model

- **WHEN** any tool is invoked
- **THEN** no tool schema exposes a parameter accepting a host, API key, username, or password

### Requirement: Connect over the network rather than a local socket

The server SHALL reach the middleware over a network connection to the configured target address, including when it is running on the target itself. It SHALL NOT use a local middleware socket.

#### Scenario: Running on the target

- **WHEN** the server runs on the same machine as the target
- **THEN** it connects over the network to the configured target address using a supplied credential
- **AND** it does not attempt a local socket connection

#### Scenario: Identity is preserved on every connection

- **WHEN** any middleware connection is opened
- **THEN** it is authenticated with a specific user's credential
- **AND** no connection path exists that carries no user identity

### Requirement: Isolate connections between sessions

Each MCP session's middleware connection SHALL be isolated from every other session's. A connection SHALL NOT be shared across sessions or reused for a different credential.

#### Scenario: Concurrent sessions

- **WHEN** two sessions with different credentials are active
- **THEN** each has its own middleware connection authenticated with its own credential

#### Scenario: Connection is not reused across credentials

- **WHEN** a session ends and a new session begins with a different credential
- **THEN** the new session opens its own authenticated connection

### Requirement: Report the negotiated authentication mechanism

The API-key login mechanism differs by target release: releases before TrueNAS 26 transmit the raw key and rely entirely on TLS to protect it, while TrueNAS 26 and later use SCRAM-SHA-512, which does not transmit the key. The server SHALL record which mechanism was negotiated and SHALL surface it in startup diagnostics.

#### Scenario: Target negotiates a mechanism that transmits the key

- **WHEN** the target negotiates a mechanism that transmits the raw API key
- **THEN** the server records that the key's confidentiality depends on the transport
- **AND** startup diagnostics report the negotiated mechanism

#### Scenario: Target negotiates SCRAM

- **WHEN** the target negotiates SCRAM-SHA-512
- **THEN** startup diagnostics report that the key is not transmitted

### Requirement: Require TLS by default, with certificate verification configured separately

The server SHALL require a TLS transport by default. Certificate verification SHALL be a separate configuration setting from transport selection, so that a self-signed certificate can be accepted without falling back to plaintext.

#### Scenario: Plaintext transport without override

- **WHEN** the configured target uses a plaintext transport and no explicit plaintext override is set
- **THEN** the server refuses to connect
- **AND** it reports that TLS is required and names the override needed to proceed

#### Scenario: Self-signed certificate with verification relaxed

- **WHEN** the target presents a certificate that does not verify and certificate verification is explicitly disabled by configuration
- **THEN** the server connects over TLS
- **AND** it emits a startup warning that the certificate is unverified

#### Scenario: Self-signed certificate with verification left at its default

- **WHEN** the target presents a certificate that does not verify and verification has not been explicitly disabled
- **THEN** the server refuses to connect
- **AND** the error identifies certificate verification as the cause and names the setting that relaxes it, without suggesting plaintext as a remedy

#### Scenario: Explicit plaintext override on a trusted network

- **WHEN** the operator sets the explicit plaintext override
- **THEN** the server connects over the plaintext transport
- **AND** it emits a startup warning that the target may revoke the API key

### Requirement: Pin and report the API version

The server SHALL record the API version reported by the target and SHALL make it available for diagnostics and for version-conditional behavior.

#### Scenario: Target runs a supported release

- **WHEN** the target reports an API version from a supported release
- **THEN** the server proceeds and records the version

#### Scenario: Target runs an unsupported release

- **WHEN** the target reports an API version older than the minimum supported release
- **THEN** the server refuses to serve tool calls
- **AND** it reports the detected version and the minimum supported version

### Requirement: Recover from connection loss

The server SHALL detect a dropped connection and re-establish it with re-authentication, without requiring a server restart.

#### Scenario: Connection drops during idle

- **WHEN** the WebSocket connection closes while no request is in flight
- **THEN** the server re-establishes and re-authenticates the connection before serving the next tool call

#### Scenario: Connection drops during an in-flight request

- **WHEN** the connection closes while a request is awaiting a response
- **THEN** the pending call returns a structured error indicating the connection was interrupted
- **AND** the error states whether the operation may have been applied on the target

#### Scenario: Reconnection repeatedly fails

- **WHEN** reconnection attempts fail continuously
- **THEN** the server applies a backoff between attempts rather than reconnecting in a tight loop
- **AND** tool calls return a structured error naming the connection state

### Requirement: Serve exactly one target per process

The server SHALL be configured with a single target TrueNAS instance. Neither a tool nor an MCP session SHALL be able to select a different target.

#### Scenario: Multiple boxes

- **WHEN** an operator needs to reach more than one TrueNAS instance
- **THEN** they run one server instance per target
- **AND** no tool schema offers a host selection parameter

#### Scenario: Session cannot redirect the target

- **WHEN** a session supplies credentials
- **THEN** they are used against the configured target only
- **AND** no session-supplied value can change which host the server connects to
