## Context

See proposal.md - Why/What Changes for motivation. The constraints that shape this design:

- The server holds no TrueNAS credential of its own today (`internal/config/config.go`, `openspec/specs/mcp-transport/spec.md`). Every HTTP session is bounded by a credential its caller supplied. OAuth must be a new *transport* for that same caller-supplied credential, not a new trust root that could serve a session independent of it.
- TrueNAS itself is not an OAuth/OIDC provider, so there is no upstream authorization server to redirect to. truenas-mcp has to play the authorization server role itself.
- The server's whole configuration story is environment variables only — "no mounted file and no persistent dataset" (`internal/config/config.go` package doc). It is deployed as a container on the TrueNAS appliance it manages, where attaching a database or a persistent volume for OAuth state is disproportionate to what OAuth actually needs to remember.
- Existing credential extraction (`CredentialFromRequest` in `internal/server/session.go`) and session establishment (`SessionManager` in the same file) already do exactly what an OAuth-authenticated request still needs at the end of the day: turn a bearer value into a live, per-caller middleware connection. OAuth should feed that same pipe rather than replace it.

## Goals / Non-Goals

**Goals:**
- Let an OAuth 2.1 client that only knows the server's URL (Claude.ai's connector flow) discover, register, obtain user consent, and receive a working access token, with zero pre-shared configuration.
- Preserve the existing invariant: every session still runs under a TrueNAS credential the resource owner supplied, at their own privilege level; the server still holds no credential that could serve a session on its own.
- Keep the server appliance-friendly: no database, no persistent volume, survivable container restarts for the common case (operator sets one more env var).

**Non-Goals:**
- No OpenID Connect identity layer (no ID tokens, no `userinfo` endpoint, no notion of "who is this human" beyond the TrueNAS credential they typed in). MCP authorization only needs an access token that grants API access, not identity.
- No confidential-client support (`client_secret`, `client_secret`-authenticated token requests). MCP clients are treated as public clients and PKCE is mandatory instead, matching how Claude.ai and the reference MCP SDKs register today.
- No scopes narrower than "whatever this TrueNAS credential can do." The server already delegates all privilege bounding to TrueNAS; inventing MCP-side scopes on top would be a second, redundant authorization model.
- No administrative UI for listing or revoking issued tokens/registered clients. Because nothing is stored server-side (see Decisions), there is nothing to list; revocation is documented as "revoke the TrueNAS API key," exactly as it is today for the raw-bearer path.
- No shared/multi-replica-safe state. This design assumes a single running instance, matching how the server is deployed today (one container per appliance).

## Decisions

### Stateless, encrypted self-contained tokens instead of a client/token store

**Decision:** Registered clients, authorization codes, access tokens, and refresh tokens are all *self-contained*: each is a value the server can verify and decrypt using one symmetric key it holds, rather than a row looked up in a database. The server's only new piece of long-lived state is that one key.

- `client_id` returned from Dynamic Client Registration encodes the registered `redirect_uris`, `client_name`, and `created_at`, HMAC-signed so a client can't forge or widen its own redirect URIs later. This does not need to be *confidential* — only tamper-evident — since client metadata isn't a secret.
- Authorization codes, access tokens, and refresh tokens encode the resource owner's TrueNAS username + API key, the associated `client_id`/`redirect_uri`, and an expiry, sealed with an AEAD cipher (the payload is a secret, so it must be both tamper-evident and confidential).

**Alternative considered:** a persistent store (embedded KV file, SQLite, external database) for clients and tokens, enabling real server-side revocation and a normal expiry sweep. Rejected as disproportionate: it would be the first piece of durable state this server has ever needed, would need a mounted volume or dataset (which the project has deliberately avoided since the original design, per `config.go`'s package doc), and gains only two things this design gets a weaker but adequate version of anyway — see the revocation trade-off below.

**Consequence:** rotating or losing the encryption key invalidates every outstanding registration, code, and token at once. This is treated as acceptable and is covered under Risks / Trade-offs, not as a defect to design around.

### The encryption key is an env var, generated ephemerally if absent

**Decision:** `TRUENAS_MCP_OAUTH_ENCRYPTION_KEY` holds a 32-byte key (base64 or hex). If unset while OAuth is enabled, the server generates a random key at startup and emits a warning that restarting the process invalidates every outstanding OAuth registration/code/token (the underlying TrueNAS credentials on the target are never affected — only the wrapper around them). This mirrors the existing `TRUENAS_MCP_API_KEY`-in-stdio-mode pattern: a secret an operator can supply through the environment, with the server refusing to silently make one up a *credential* but permitting an ephemeral *wrapper key* so OAuth works out of the box for a quick trial.

### OAuth is enabled by the presence of an issuer URL, not a separate boolean

**Decision:** `TRUENAS_MCP_OAUTH_ISSUER` (the externally-reachable `https://` base URL clients will see as `issuer` in discovery metadata) is both the switch and the configuration: setting it turns OAuth on, leaving it unset leaves the server exactly as it behaves today. This follows the existing pattern where `TLSCertFile`/`TLSKeyFile` presence (not a `TRUENAS_MCP_TLS_ENABLED` flag) decides whether TLS is on.

### Authorization endpoint re-uses `SessionManager.Open` to validate the credential before issuing a code

**Decision:** the consent screen's POST handler calls the same `SessionManager.Open` used for every other credential check, then immediately closes that probe connection. A bad TrueNAS username/API key fails at consent time with a message the resource owner can act on, instead of surfacing later as an opaque 401 on the first MCP tool call. This adds no new TrueNAS-facing code path — it is the existing one, called one step earlier.

### Replay protection for authorization codes is an in-process cache, not persistence

**Decision:** codes are single-use, enforced by a short (60s) expiry embedded in the code plus an in-memory set of already-consumed code IDs, swept on expiry. This is process-local state (not a mounted file or external store) and is acceptable to lose on restart — a code that no longer exists because the process restarted simply fails to redeem, which is the same failure mode as an expired code. Its one real limitation — it does not protect against replay across multiple replicas of the server — is called out under Risks / Trade-offs rather than solved here, since the server is not deployed that way today.

### Refresh tokens are supported, with a moderate default access-token TTL

**Decision:** the token endpoint supports both `authorization_code` and `refresh_token` grants. Access tokens default to a 1-hour TTL (`TRUENAS_MCP_OAUTH_ACCESS_TOKEN_TTL`); refresh tokens default to 30 days (`TRUENAS_MCP_OAUTH_REFRESH_TOKEN_TTL`, `0` disables refresh-token issuance entirely). Without refresh tokens, a long-lived hosted client like Claude.ai would force the resource owner back through the browser consent screen every hour, which is a worse experience than the one-time raw-API-key setup this change is meant to improve on.

### The legacy raw-API-key bearer path is unchanged and stays enabled

**Decision:** `CredentialFromRequest` gains a branch that recognizes the OAuth access token's format (a distinct, versioned prefix) and decrypts it; anything else is still treated as a raw TrueNAS API key exactly as today. There is no migration step and no deprecation — operators who already script against the raw header keep working unmodified, and the two forms can be used side-by-side by different callers.

### Plaintext is refused outright once OAuth is enabled, with no override

**Decision:** `AllowPlaintext` (`TRUENAS_MCP_ALLOW_PLAINTEXT`) is rejected at config validation time when `TRUENAS_MCP_OAUTH_ISSUER` is set. The existing plaintext override exists for a caller who already holds a secret and chooses to transmit it insecurely; the OAuth consent screen *collects* that secret via an HTML form in a browser, which is a meaningfully different — and less recoverable — exposure to ask an operator to opt out of.

## Risks / Trade-offs

- **[Risk]** Losing or rotating `TRUENAS_MCP_OAUTH_ENCRYPTION_KEY` invalidates every outstanding client registration, authorization code, and token at once, breaking every connected OAuth client simultaneously. → **Mitigation:** the key is a plain env var operators can set once and keep stable across redeploys, exactly like any other config value; the startup warning makes the ephemeral-key case explicit so an operator doing more than a quick trial sets one deliberately.
- **[Risk]** The in-process authorization-code replay cache does not protect against replay if the server is ever scaled to multiple replicas. → **Mitigation:** documented as a known limitation; single-instance deployment (today's actual deployment shape) is unaffected. Revisit with a shared cache (e.g. the target itself, or a small external store) only if multi-replica deployment becomes a real requirement.
- **[Risk]** No way to revoke one specific issued access/refresh token before its expiry, since nothing is stored server-side to delete. → **Mitigation:** short default access-token TTL bounds exposure; the real kill switch — revoking the TrueNAS API key on the target — already immediately invalidates every credential form built on top of it, OAuth included, matching the existing "Credential revoked mid-session" behavior in `mcp-transport`'s spec.
- **[Risk]** Dynamic Client Registration is intentionally unauthenticated (any caller can self-register a client, per RFC 7591's open model). → **Mitigation:** registration alone grants no access to any TrueNAS data — the authorization step still requires a human to submit a valid TrueNAS username and API key through the consent screen, so a self-registered client is worthless without that.
- **[Risk]** The consent screen is a new place a TrueNAS API key is typed into a browser, and thus a new phishing/XSS surface. → **Mitigation:** the page ships with no third-party script or style dependencies, is served only over TLS (see the plaintext decision above), and the submitted value is never echoed back or logged, matching the existing discipline in `CredentialFromRequest`.

## Migration Plan

Purely additive and off by default: a deployment that does not set `TRUENAS_MCP_OAUTH_ISSUER` behaves exactly as it does today, byte-for-byte. Enabling it is a config-only change (set the issuer URL, optionally set the encryption key for restart-stability); no data migration is needed because nothing persists. Rollback is unsetting `TRUENAS_MCP_OAUTH_ISSUER` (or redeploying the prior image), which simply stops serving the new endpoints — existing raw-API-key callers are never affected either way.
