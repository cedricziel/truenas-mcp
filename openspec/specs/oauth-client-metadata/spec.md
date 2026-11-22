## Purpose

Support public OAuth clients identified by Client ID Metadata Documents and identify the authorization server in authorization responses. This follows MCP revision 2026-07-28's client registration guidance and RFC 9207 while retaining Dynamic Client Registration compatibility.

CIMD is an IETF Internet-Draft, not an MCP extension. MCP 2026-07-28 references draft-00; this implementation also applies the URL validation, public-client authentication, redirect handling, and fetch protections described in draft-03. It does not implement confidential-client authentication or fetch referenced keys or logos. MCP deprecates DCR, but this server retains its registration endpoint and existing signed client IDs.

References:

- [MCP 2026-07-28 client registration](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization/client-registration)
- [OAuth Client ID Metadata Document draft-03](https://datatracker.ietf.org/doc/html/draft-ietf-oauth-client-id-metadata-document-03)
- [RFC 9207 authorization server issuer identification](https://www.rfc-editor.org/rfc/rfc9207.html)
- [RFC 8252 loopback redirect handling](https://www.rfc-editor.org/rfc/rfc8252.html#section-7.3)

## Requirements

### Requirement: Resolve public URL client identities

The authorization endpoint SHALL accept HTTPS URL client IDs through both GET and consent POST. A client ID URL SHALL have a non-root path and SHALL contain no userinfo, fragment, or dot path segments. The fetched document SHALL contain the exact original `client_id` string, a nonempty `client_name`, and registered `redirect_uris`. The server SHALL NOT normalize client IDs before comparison. Only public-client token endpoint authentication with `none` is supported. Shared secrets, private key material, and unsupported authentication, grant, or response types SHALL be rejected. Token endpoint authentication supplied through a valid `Authorization` scheme SHALL receive HTTP 401 `invalid_client` with a matching `WWW-Authenticate` challenge. Nonempty secret or assertion form parameters SHALL receive HTTP 400 `invalid_client`. Empty authentication form values SHALL be treated as omitted, and an empty first value SHALL NOT hide later nonempty values. Authentication refusal SHALL NOT consume an authorization code.

#### Scenario: Authorize a CIMD client

- **WHEN** a client presents a valid public HTTPS URL and matching metadata
- **THEN** the GET endpoint displays the application name, client URL, and redirect destination
- **AND** the POST endpoint validates the same identity and redirect before issuing a code
- **AND** the code binds the exact client ID, requested redirect, and S256 PKCE challenge

#### Scenario: Metadata identifies another URL

- **WHEN** the document's `client_id` differs from the requested URL, including equivalent URL spellings
- **THEN** authorization fails locally without redirecting to the client
- **AND** the invalid document is not cached

### Requirement: Match redirect destinations before issuing codes

HTTPS redirects and legacy DCR redirects SHALL match registered values exactly. A CIMD client using `http://127.0.0.1` or `http://[::1]` MAY change the callback port. This exception SHALL preserve the exact scheme, host, path, and query spelling. It SHALL NOT apply to `localhost`, other addresses, or HTTPS. Redirects SHALL have a host and SHALL contain no userinfo or fragment.

#### Scenario: Native client chooses an ephemeral callback port

- **WHEN** CIMD metadata registers `http://127.0.0.1/callback` and the client requests `http://127.0.0.1:49152/callback`
- **THEN** the authorization request is accepted
- **AND** the issued code binds the actual requested callback including port 49152
- **AND** token redemption requires that exact requested callback

#### Scenario: Consent POST changes the destination

- **WHEN** a consent POST changes the redirect host, path, or query to an unregistered value
- **THEN** authorization fails without issuing a code or redirecting

### Requirement: Bound and isolate client metadata fetching

The server SHALL fetch metadata using a dedicated HTTPS client with no proxy, cookies, or forwarded caller credentials. It SHALL validate all resolved addresses before dialing and connect directly to a validated IP while retaining normal TLS hostname verification. Private, loopback, link-local, multicast, and IANA special-use destinations SHALL be rejected, including mixed public and non-public DNS results. HTTP redirects SHALL NOT be followed. Fetching SHALL have a five-second total timeout, a 5 KiB body limit, and bounded response headers. Only HTTP 200 JSON documents that pass metadata validation SHALL be cached. The cache SHALL contain at most 128 entries for at most five minutes each and SHALL honor shorter cache lifetimes and no-store directives. Failed or malformed responses SHALL NOT be cached.

#### Scenario: Untrusted DNS attempts an internal request

- **WHEN** a client metadata hostname resolves to any non-public address
- **THEN** the server rejects it before opening a connection
- **AND** a public answer alongside a private answer does not bypass this check

#### Scenario: Upstream redirects or exceeds resource limits

- **WHEN** the metadata endpoint redirects, exceeds the body limit, times out, or returns an error
- **THEN** authorization fails locally
- **AND** the server does not forward the user's TrueNAS credential or follow the redirect

### Requirement: Identify the issuer in authorization responses

Authorization server metadata SHALL advertise `client_id_metadata_document_supported: true` and `authorization_response_iss_parameter_supported: true`. Every successful authorization redirect SHALL include `iss` equal to the advertised issuer. Any future redirected authorization error SHALL include the same issuer. Current authorization failures remain local responses and SHALL NOT redirect to unvalidated destinations.

#### Scenario: Existing DCR client authorizes

- **WHEN** a client registers through the existing DCR endpoint and completes authorization
- **THEN** its signed client ID remains accepted
- **AND** its authorization response preserves state and includes the advertised issuer
- **AND** token exchange retains exact client ID, redirect, PKCE, expiry, and replay checks
