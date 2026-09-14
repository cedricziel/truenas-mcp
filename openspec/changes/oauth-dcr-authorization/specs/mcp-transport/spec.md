## MODIFIED Requirements

### Requirement: Authenticate each session with a caller-supplied TrueNAS credential

Each client SHALL supply the TrueNAS API key it will run under when establishing a session, either directly as a raw API key or indirectly as an OAuth access token issued by the `oauth-authorization` capability and bound to such a key. (That capability's own authorization step is what collects a TrueNAS username and API key from the resource owner -- the username only for display back to them, never required by the target itself.) The server SHALL NOT hold a TrueNAS credential of its own and SHALL NOT fall back to one.

#### Scenario: Valid caller credential

- **WHEN** a client establishes a session with an API key the target accepts
- **THEN** the session is established
- **AND** the server opens a middleware session under that identity

#### Scenario: Valid OAuth access token

- **WHEN** a client establishes a session presenting a valid, unexpired OAuth access token instead of a raw API key
- **THEN** the server resolves it to the TrueNAS credential it was issued for
- **AND** the session is established under that identity exactly as it would be for the equivalent raw API key

#### Scenario: Missing credential

- **WHEN** a client attempts to establish a session without supplying a credential
- **THEN** the server refuses the session
- **AND** no tool is served
- **AND** if OAuth is enabled, the refusal points the client at the protected resource metadata so it can discover how to obtain one

#### Scenario: Credential rejected by the target

- **WHEN** the supplied credential is rejected by the target
- **THEN** the server refuses the session
- **AND** the error identifies authentication as the cause without disclosing the key

#### Scenario: No server-held fallback credential

- **WHEN** the server is inspected in any configuration
- **THEN** it holds no TrueNAS credential that could serve a session whose caller did not supply one
