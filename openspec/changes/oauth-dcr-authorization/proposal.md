## Why

Every HTTP caller today authenticates by presenting a raw TrueNAS API key as a bearer token. That works for an operator wiring up their own client, but it is incompatible with hosted MCP clients such as Claude.ai, which connect to remote MCP servers only through OAuth 2.1: they discover an authorization server via metadata, register themselves as a client dynamically (RFC 7591), and only ever hold an OAuth access token — never a credential typed in by the person configuring the client. Without an OAuth front end, this server cannot be added to Claude.ai or any other platform that follows the MCP Authorization spec.

## What Changes

- Add an OAuth 2.1 authorization server role to the HTTP transport, played by truenas-mcp itself, since TrueNAS has no OAuth/OIDC identity provider of its own to defer to.
- Serve RFC 9728 Protected Resource Metadata and RFC 8414 Authorization Server Metadata at their well-known paths.
- Add a Dynamic Client Registration endpoint (RFC 7591) so clients like Claude.ai can register without a pre-shared `client_id`.
- Add an authorization endpoint that collects the resource owner's TrueNAS username and API key through a consent screen, validates the credential against the target before issuing a code, and requires PKCE (S256) on every flow.
- Add a token endpoint issuing OAuth access tokens (and refresh tokens) that are self-contained (encrypted, not server-stored), each bound to the TrueNAS credential the resource owner supplied during authorization.
- Extend the existing bearer-credential extraction (`CredentialFromRequest`) to recognize an OAuth access token, decrypt it, and drive the existing per-caller `SessionManager` exactly as it does today for a raw API key — the underlying "no server-held credential, session bounded by the caller's own privileges" invariant does not change.
- Keep the existing raw-API-key bearer/`X-TrueNAS-API-Key` path working unchanged, as a non-OAuth escape hatch for operators and scripts that do not need the OAuth dance.
- Add configuration for enabling OAuth, the issuer URL, and token/client TTLs; extend startup validation and warnings accordingly.
- **BREAKING**: none. OAuth is additive; existing raw-key callers are unaffected.

## Capabilities

### New Capabilities
- `oauth-authorization`: discovery metadata (protected resource + authorization server), dynamic client registration, the authorization/consent endpoint that exchanges a TrueNAS credential for a code, the token endpoint (PKCE-required authorization_code and refresh_token grants), and the stateless token/registration format that keeps the credential mapping out of server-side storage.

### Modified Capabilities
- `mcp-transport`: "Authenticate each session with a caller-supplied TrueNAS credential" is extended so an OAuth access token issued by the `oauth-authorization` capability is one of the accepted forms of that caller-supplied credential, alongside the existing raw API key. The 401 response's `WWW-Authenticate` challenge points clients at the protected resource metadata so OAuth-capable clients can discover the flow instead of guessing a header name.

## Impact

- `internal/server/session.go`: `CredentialFromRequest` gains a branch to recognize and unwrap an OAuth access token into the underlying TrueNAS API key; `ErrNoCredential` and the `WWW-Authenticate` challenge text change to mention discovery.
- `internal/server/mcp.go`: new routes for discovery metadata, DCR, authorize, and token, mounted alongside the existing MCP handler; `RequireCredential`'s challenge header updated.
- `internal/config/config.go`: new environment variables to enable OAuth, set the issuer URL, and configure token/client TTLs and the signing/encryption key; new validation and startup warnings.
- New package (e.g. `internal/oauth`) implementing metadata documents, DCR, the consent UI, PKCE-verified code exchange, and stateless token encoding/decoding.
- `openspec/specs/mcp-transport/spec.md`: delta for the modified requirement above.
- `README.md`: new section documenting how to connect Claude.ai (or another MCP platform) via OAuth, versus the existing raw-API-key instructions.
- No changes to the TrueNAS-facing client (`internal/truenas`) or to stdio mode, which is unaffected by this change.
