---
name: verify
description: Verify truenas-mcp Go changes using isolated fake TrueNAS services, static checks, and OpenSpec validation. Use before a PR or after changing tool behavior.
---

# Verify truenas-mcp

Run from the repository root. Use `GOCACHE` and `GOMODCACHE` in writable
scratch directories if default caches are unavailable.

1. Run `gofmt -l` on changed Go files and `git diff --check`.
2. Run `go vet ./...` and `go test ./...`.
3. Build in a unique scratch directory with cleanup:

   ```sh
   verify_dir=$(mktemp -d /tmp/truenas-mcp-verify.XXXXXX)
   trap 'rm -rf "$verify_dir"' EXIT
   go build -o "$verify_dir/truenas-mcp" ./cmd/truenas-mcp
   ```

4. When specs change, run `openspec validate --specs --strict --no-interactive`.

The default Go tests start fake HTTP and WebSocket targets on ephemeral loopback
ports with test credentials and cleanup hooks. They need local listener access.
They cover file continuation beyond 64 KiB, UTF-8 boundaries, config projection,
full-config revisions, partial updates preserving hidden fields and large integers,
and write gating.

Do not use `test-integration` or build tag `integration` for this workflow:
those tests contact a live TrueNAS target. Do not launch a service against user
state. For a separate smoke run, use a unique directory outside the checkout,
ephemeral ports, test credentials, and process cleanup on every exit path.
