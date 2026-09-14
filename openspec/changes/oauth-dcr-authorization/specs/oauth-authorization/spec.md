## Purpose

Lets an MCP client that knows only the server's URL — such as Claude.ai's connector flow — discover, register itself, obtain a resource owner's consent, and receive a working OAuth access token, without any pre-shared client credentials or operator-side configuration per client.

## ADDED Requirements

### Requirement: OAuth is opt-in and off by default

The server SHALL serve no OAuth endpoint unless an issuer URL is configured. When unconfigured, the HTTP transport SHALL behave exactly as it does without this capability.

#### Scenario: Issuer not configured

- **WHEN** the server starts without an OAuth issuer URL configured
- **THEN** no discovery, registration, authorization, or token endpoint is served
- **AND** the existing raw-credential bearer path is unaffected

#### Scenario: Issuer configured

- **WHEN** the server starts with an OAuth issuer URL configured
- **THEN** discovery, registration, authorization, and token endpoints are served
- **AND** the existing raw-credential bearer path continues to work unchanged

### Requirement: Publish OAuth discovery metadata

The server SHALL publish Protected Resource Metadata and Authorization Server Metadata at their well-known paths, so a client can locate the authorization server and its endpoints without prior configuration.

#### Scenario: Client discovers the authorization server

- **WHEN** a client requests the protected resource metadata document
- **THEN** the response names this server's own OAuth issuer as an authorization server for the MCP resource

#### Scenario: Client discovers endpoint locations

- **WHEN** a client requests the authorization server metadata document
- **THEN** the response names the registration, authorization, and token endpoints
- **AND** it states that PKCE with S256 is required and that "none" is a supported token endpoint authentication method

### Requirement: Register clients dynamically

The server SHALL accept Dynamic Client Registration requests and issue a client identifier without requiring any pre-shared credential or operator action, so a client can onboard itself the first time it connects.

#### Scenario: New client registers

- **WHEN** a client submits a registration request naming its redirect URIs
- **THEN** the server issues a client identifier bound to exactly those redirect URIs
- **AND** no client secret is required to use it

#### Scenario: Registered client's redirect URI is later used

- **WHEN** an authorization request presents a client identifier and a redirect URI
- **THEN** the server accepts the request only if that redirect URI exactly matches one supplied at registration

#### Scenario: Registration request is malformed

- **WHEN** a registration request omits a redirect URI or supplies one that is not a valid absolute URI
- **THEN** the server refuses the registration and reports which field is invalid

#### Scenario: Registration request names a redirect URI with a disallowed scheme

- **WHEN** a registration request names a redirect URI that is not `https`, or is `http` on a non-loopback host
- **THEN** the server refuses the registration
- **AND** a client that runs its own local callback listener remains free to register an `http` redirect URI on a loopback address

### Requirement: Authorize a session by collecting the resource owner's TrueNAS credential

The authorization endpoint SHALL present the resource owner with a form to supply their own TrueNAS username and API key, and SHALL validate that credential against the target before issuing an authorization code, so a bad credential is caught before the client ever attempts to use it.

The consent form SHALL display the redirect URI the resulting code will be delivered to, so the resource owner has something to verify beyond the client's own self-chosen, unauthenticated name.

#### Scenario: Consent form displays the redirect destination

- **WHEN** the authorization endpoint renders the consent form
- **THEN** the redirect URI the request will deliver a code to is shown as visible page content, not only carried as a hidden form field

#### Scenario: Resource owner supplies a valid credential

- **WHEN** the resource owner submits a TrueNAS username and API key the target accepts
- **THEN** the server issues an authorization code bound to that credential and to the requesting client's redirect URI
- **AND** it redirects back to that redirect URI with the code

#### Scenario: Resource owner supplies an invalid credential

- **WHEN** the resource owner submits a TrueNAS username and API key the target rejects
- **THEN** the server does not issue a code
- **AND** the consent form reports the failure without asking the client to retry blindly

#### Scenario: Authorization request omits PKCE

- **WHEN** an authorization request does not include an S256 PKCE code challenge
- **THEN** the server refuses the request before presenting the consent form

#### Scenario: Authorization request names an unregistered redirect URI

- **WHEN** an authorization request's redirect URI does not exactly match one registered for that client
- **THEN** the server refuses the request and does not redirect to the supplied URI

### Requirement: Exchange an authorization code for tokens under PKCE

The token endpoint SHALL exchange a valid, unexpired, previously-unused authorization code for an access token, verifying the PKCE code verifier against the code challenge recorded at authorization time. The token endpoint SHALL also require a `redirect_uri` on this grant and SHALL verify it matches the one recorded when the code was issued, since a redirect URI is always present at that point.

#### Scenario: Valid code exchange

- **WHEN** a client exchanges a valid authorization code together with the matching PKCE code verifier and the same redirect URI used at authorization
- **THEN** the server issues an access token that, when presented to the MCP transport, authenticates as the TrueNAS credential collected at authorization time

#### Scenario: Redirect URI is missing or does not match

- **WHEN** a client exchanges an authorization code without a redirect URI, or with one that does not match the redirect URI recorded when the code was issued
- **THEN** the server refuses the exchange and issues no token

#### Scenario: Code verifier does not match

- **WHEN** a client exchanges an authorization code with a PKCE code verifier that does not match the recorded code challenge
- **THEN** the server refuses the exchange and issues no token

#### Scenario: Code already used

- **WHEN** a client attempts to exchange an authorization code a second time
- **THEN** the server refuses the exchange

#### Scenario: Code expired

- **WHEN** a client exchanges an authorization code after its short validity window has passed
- **THEN** the server refuses the exchange

### Requirement: Refresh an access token without repeating consent

The token endpoint SHALL support exchanging a refresh token for a new access token, so a long-lived client is not forced to repeat the consent screen every time an access token expires, unless the operator has disabled refresh token issuance.

#### Scenario: Valid refresh

- **WHEN** a client exchanges a valid, unexpired refresh token
- **THEN** the server issues a new access token authenticating as the same TrueNAS credential

#### Scenario: Refresh tokens disabled

- **WHEN** the operator has disabled refresh token issuance
- **THEN** the token endpoint issues no refresh token on any grant
- **AND** a client whose access token has expired must repeat the authorization request

#### Scenario: Refresh token expired

- **WHEN** a client exchanges a refresh token after its validity window has passed
- **THEN** the server refuses the exchange
- **AND** the client must repeat the authorization request

### Requirement: An OAuth access token authenticates exactly like the underlying TrueNAS credential

A request bearing a valid OAuth access token SHALL be authenticated as the TrueNAS credential that was collected when that token's authorization was granted, and SHALL be bound by that credential's privileges exactly as the existing raw-API-key bearer path is.

#### Scenario: Access token used on the MCP transport

- **WHEN** a client presents a valid OAuth access token to the MCP transport
- **THEN** the session runs under the TrueNAS credential collected at authorization
- **AND** it is bounded by that credential's privileges the same way a raw API key would be

#### Scenario: Access token expired

- **WHEN** a client presents an OAuth access token past its expiry
- **THEN** the server refuses the request as unauthenticated

#### Scenario: Underlying TrueNAS credential revoked

- **WHEN** the TrueNAS API key underlying a still-unexpired OAuth access token is revoked on the target
- **THEN** subsequent requests bearing that access token fail with an authentication error, exactly as a revoked raw API key would

### Requirement: OAuth state is never persisted to disk

Registered clients, authorization codes, access tokens, and refresh tokens SHALL be representable entirely as values the server can verify using an in-memory key, without writing any of them to a mounted file, dataset, or external store.

Whether a restart invalidates them depends entirely on whether that key is stable across the restart: a configured key that does not change preserves them, and their absence or change does not, by design -- the server has nothing to compare an incoming key against except itself, so it cannot detect or warn about the "operator configured a *different* key" case the way it can the "no key was configured at all" case below.

#### Scenario: Server restarts with a stable configured key

- **WHEN** the server process restarts with the same operator-configured encryption key it had before
- **THEN** no file or dataset written by the previous process is required for the server to start
- **AND** previously issued client registrations, authorization codes, and unexpired tokens remain valid

#### Scenario: Server restarts without a configured key

- **WHEN** the server process restarts without an encryption key configured
- **THEN** a fresh key is generated for the new process
- **AND** every previously issued client registration, authorization code, and token becomes unverifiable
- **AND** the operator is warned at startup that this happened

### Requirement: Refuse plaintext once OAuth is enabled

The server SHALL refuse to start with the plaintext override set while an OAuth issuer is configured, because the authorization endpoint collects a TrueNAS credential through a browser form on this same boundary.

#### Scenario: Plaintext override set with OAuth enabled

- **WHEN** the operator sets the plaintext override while an OAuth issuer URL is also configured
- **THEN** the server refuses to start
- **AND** it reports that TLS is required whenever OAuth is enabled
