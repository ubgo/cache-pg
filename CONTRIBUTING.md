# Contributing to ubgo/cache-pg

Thanks for helping improve the SQL (`database/sql`) adapter for `github.com/ubgo/cache`.

## Local gate (must be green before every commit / PR)

```sh
gofmt -w .
go build ./...
go test -race -count=1 ./...
golangci-lint run ./...
```

Or via the Taskfile: `task check`. CI runs the identical gate. Not done until **0 failures, 0 lint issues** (`golangci-lint` runs revive, staticcheck, govet, errcheck, gocritic, misspell, unconvert, ineffassign, unused — see `.golangci.yml`).

## Conformance contract

This adapter implements `cache.Cache` and **must keep passing** `github.com/ubgo/cache/cachetest`:

```go
func TestConformance(t *testing.T) { cachetest.Run(t, factory) }
```

`cachetest.Run` is the executable contract. Invariants it enforces:

- `Get` returns `(nil, cache.ErrNotFound)` on miss or expiry — never `(nil, nil)`. Every read query carries `expires_at IS NULL OR expires_at > now`.
- `ttl <= 0` means "no expiry" → `expires_at` stored as `NULL`.
- `SetNX` returns `(true, nil)` only when it created the row (delete-expired-then-insert-if-absent, in a transaction).
- `Incr`/`Decr` are atomic (transactional read-modify-write); a missing key is `0`.
- After `Close()`, every method returns `cache.ErrClosed`; `Close()` is idempotent and does not close the `*sql.DB`.

`pgcache_test.go` additionally pins `Migrate` idempotency and `Vacuum` removing expired rows. Keep all of it green.

## Docker-free tests (in-memory SQLite)

`pgcache_test.go` opens `file::memory:?cache=shared&_pragma=busy_timeout(5000)` with `SetMaxOpenConns(1)` (serialises writes, avoids in-memory lock churn) and runs the adapter with `WithDialect(pgcache.SQLite)`. SQL is authored once in Postgres `$N` syntax; `rebind` rewrites `$N → ?` for SQLite. So a single query string is exercised on both dialects — do not fork queries per dialect; extend `rebind` if a real syntax difference appears.

A real-Postgres smoke test belongs behind an optional `task smoke` (docker-compose); it is never part of the gate.

## Local dependency (`replace`)

`go.mod` carries `replace github.com/ubgo/cache => ../cache` because the contract is developed in a sibling repo and not yet tagged. **Do not edit `go.mod`, `go.sum`, `LICENSE`, `NOTICE`, or `.gitignore`** in a feature change. The `replace` is removed at release time.

```
ubgo/
  cache/      # contract + cachetest
  cache-pg/   # this module (replace -> ../cache)
```

## Doc-comment style

- Every exported symbol has a doc comment starting with its name (`revive`).
- Comments explain **why** / invariants / edge cases — e.g. why `expires_at` is unix-nanos, why `SetNX` deletes the expired row first, why `LIKE` uses `ESCAPE '\'`. Preserve these on refactor.
- `ctx` stays a named parameter even when unused (contract compliance); `.golangci.yml` already excludes that revive warning. Never rename it to `_`.
- Keep `doc.go` accurate, including the `database/sql`-vs-Ent rationale — that paragraph is load-bearing context.

## Pull requests

1. Keep the gate green.
2. Add/extend a test for any behaviour change (conformance first).
3. Update `README.md` / `CHANGELOG.md` on public behaviour changes.
4. One logical change per PR.
