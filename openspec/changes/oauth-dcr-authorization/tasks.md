## 1. Configuration

- [x] 1.1 Add `TRUENAS_MCP_OAUTH_ISSUER`, `TRUENAS_MCP_OAUTH_ENCRYPTION_KEY`, `TRUENAS_MCP_OAUTH_ACCESS_TOKEN_TTL`, and `TRUENAS_MCP_OAUTH_REFRESH_TOKEN_TTL` to `internal/config/config.go`, with `TRUENAS_MCP_OAUTH_ISSUER` presence deciding whether OAuth is enabled; verify with a table-driven test in `internal/config/config_test.go` covering each var's default and parse errors.
- [x] 1.2 Generate an ephemeral encryption key and emit the restart-invalidation warning when `TRUENAS_MCP_OAUTH_ENCRYPTION_KEY` is unset but OAuth is enabled; verify via a config test asserting the warning text appears only in that case.
- [x] 1.3 Reject `TRUENAS_MCP_ALLOW_PLAINTEXT` when an OAuth issuer is configured, per the "Refuse plaintext once OAuth is enabled" requirement; verify with a config validation test.
- [x] 1.4 Extend `Config.Summary()` to report OAuth enabled/disabled (never the key or issuer secrets) and confirm no test or log path can print the encryption key.

## 2. Stateless token/client encoding

- [x] 2.1 Add an `internal/oauth` package with an AEAD seal/open pair (e.g. XChaCha20-Poly1305 or AES-GCM) keyed by the configured/generated encryption key; verify with unit tests round-tripping a payload and rejecting a tampered ciphertext.
- [x] 2.2 Implement signed, self-contained `client_id` encoding/decoding carrying `redirect_uris`, `client_name`, and `created_at`; verify with unit tests covering valid decode, tamper detection, and redirect URI extraction.
- [x] 2.3 Implement sealed authorization-code encoding carrying the TrueNAS username+API key, `client_id`, `redirect_uri`, PKCE `code_challenge`, and a short expiry; verify with unit tests covering round-trip, expiry rejection, and payload confidentiality (ciphertext does not contain the plaintext key).
- [x] 2.4 Implement sealed access-token and refresh-token encoding carrying the TrueNAS username+API key and an expiry, using the configured TTLs; verify with unit tests covering round-trip and expiry rejection for both token kinds.
- [x] 2.5 Implement an in-process, TTL-swept set of consumed authorization-code IDs for single-use enforcement; verify with a unit test that a second redemption of the same code ID is rejected while a fresh code is accepted.

## 3. Discovery metadata

- [x] 3.1 Serve RFC 9728 Protected Resource Metadata at `/.well-known/oauth-protected-resource`, naming the configured issuer; verify with an HTTP test asserting the JSON shape and that the route 404s when OAuth is disabled.
- [x] 3.2 Serve RFC 8414 Authorization Server Metadata at `/.well-known/oauth-authorization-server`, naming the registration/authorization/token endpoints, `code_challenge_methods_supported: ["S256"]`, and `token_endpoint_auth_methods_supported: ["none"]`; verify with an HTTP test asserting the JSON shape.

## 4. Dynamic Client Registration

- [x] 4.1 Implement the RFC 7591 registration endpoint, validating that each submitted redirect URI is an absolute URI and issuing a signed `client_id` from task 2.2; verify with HTTP tests for a valid registration, a missing redirect URI, and a malformed redirect URI.

## 5. Authorization endpoint

- [x] 5.1 Implement the `GET /authorize` handler: validate `client_id`, exact-match the `redirect_uri` against the registered set, require `code_challenge_method=S256`, and render a dependency-free HTML consent form carrying the request through hidden fields; verify with HTTP tests for a valid request, a redirect URI mismatch, and a missing PKCE challenge.
- [x] 5.2 Implement the consent form's `POST /authorize` handler: validate the submitted TrueNAS username/API key via `SessionManager.Open` (closing the probe connection immediately after), issue a sealed authorization code from task 2.3 on success, and redirect to `redirect_uri` with `code` and `state`; verify with HTTP tests for accepted and rejected credentials, asserting the rejected case never redirects and never echoes the submitted key.

## 6. Token endpoint

- [x] 6.1 Implement the `authorization_code` grant: verify the PKCE code verifier against the code's recorded challenge, enforce single-use via task 2.5, enforce expiry, and issue an access token (and a refresh token unless disabled); verify with HTTP tests for a valid exchange, a mismatched verifier, a replayed code, and an expired code.
- [x] 6.2 Implement the `refresh_token` grant, issuing a new access token bound to the same TrueNAS credential; verify with HTTP tests for a valid refresh, an expired refresh token, and the grant being absent/erroring when refresh tokens are disabled.

## 7. Resource server integration

- [x] 7.1 Extend `CredentialFromRequest` in `internal/server/session.go` to recognize the OAuth access token format (distinct prefix), unseal it via task 2.4, and return the underlying TrueNAS API key so the existing `SessionManager`/`NewSessionProvider` path is unchanged downstream; verify with unit tests covering a valid access token, an expired one, and a tampered one, alongside the existing raw-key tests.
- [x] 7.2 Update the `WWW-Authenticate` challenge in `mcpHandler.ServeHTTP` and `RequireCredential` to point at the protected resource metadata URL when OAuth is enabled; verify with an HTTP test asserting the header's `resource_metadata` parameter.

## 8. Wiring

- [x] 8.1 Mount the discovery, registration, authorization, and token routes alongside the existing MCP handler in `internal/server/mcp.go`, gated on OAuth being enabled; verify with an HTTP test that the routes are absent when `TRUENAS_MCP_OAUTH_ISSUER` is unset.
- [x] 8.2 Wire the new config fields through `cmd/truenas-mcp/main.go` into the OAuth-enabled server construction; verify by running the server with OAuth env vars set and confirming (via a smoke test or manual `curl`) that discovery metadata is served.

## 9. Documentation

- [x] 9.1 Add a README section documenting how to connect an OAuth-capable MCP client (e.g. Claude.ai) — the env vars to set, and that the raw-API-key path still works for clients that don't need OAuth.

## 10. End-to-end verification

- [x] 10.1 Write an integration test that drives the full flow — register a client, complete authorization with a valid TrueNAS credential, exchange the code with PKCE, call an MCP tool with the resulting access token, refresh the token, and confirm the refreshed token still works — verifying it against the fake TrueNAS target already used by `internal/truenas/fake_test.go`. (Implemented in `internal/server/oauth_wiring_test.go` against `session_provider_test.go`'s `fakeTarget` instead: that file's own comment explains why the `internal/truenas/fake_test.go` fake can't be imported across packages, and `internal/server` is the package that actually wires the MCP handler and OAuth handler together, so the true end-to-end path is exercised there.)
- [x] 10.2 Run `go test ./...` and confirm it passes, then run `openspec validate --strict` against this change and confirm it passes.
