# cache-pg — feature cookbook

Exhaustive, example-driven reference for every exported identifier in
`github.com/ubgo/cache-pg` (package `pgcache`).

Import path:

```go
import pgcache "github.com/ubgo/cache-pg"
```

`pgcache.Cache` implements [`cache.Cache`](https://github.com/ubgo/cache) over
the standard library `database/sql`, targeting Postgres 14+ in production and
SQLite for in-process tests. It passes the shared `cachetest.Run` suite.

## Pages

- [Construction & options](construction.md) — `New`, `WithDialect`, `WithTable`, `WithClock`, the `Cache`/`Option`/`Dialect` types and the `Postgres`/`SQLite` constants.
- [Schema operations](schema.md) — `Migrate`, `Vacuum`.
- [Cache methods](cache-methods.md) — every `cache.Cache` method and its exact SQL.

## Capability matrix

| Exported symbol | Kind | SQL behavior | Page |
|---|---|---|---|
| `New` | constructor | wraps a `*sql.DB` | [Construction](construction.md#new) |
| `Cache` | type | the adapter | [Construction](construction.md#cache) |
| `Option` | type | functional option | [Construction](construction.md#option) |
| `Dialect` | type | placeholder/DDL selector | [Construction](construction.md#dialect) |
| `Postgres` | const | `$1` placeholders (default) | [Construction](construction.md#postgres) |
| `SQLite` | const | `?` placeholders | [Construction](construction.md#sqlite) |
| `WithDialect` | option | select dialect | [Construction](construction.md#withdialect) |
| `WithTable` | option | table name (default `cache_entries`) | [Construction](construction.md#withtable) |
| `WithClock` | option | time source (tests) | [Construction](construction.md#withclock) |
| `Migrate` | method | `CREATE TABLE/INDEX IF NOT EXISTS` | [Schema](schema.md#migrate) |
| `Vacuum` | method | `DELETE` expired rows | [Schema](schema.md#vacuum) |
| `Get` | method | `SELECT … WHERE key=$1 AND not-expired` | [Cache methods](cache-methods.md#get) |
| `GetMulti` | method | per-key `Get` loop | [Cache methods](cache-methods.md#getmulti) |
| `Has` | method | `Get` + not-found check | [Cache methods](cache-methods.md#has) |
| `TTL` | method | `SELECT expires_at` | [Cache methods](cache-methods.md#ttl) |
| `Set` | method | `INSERT … ON CONFLICT DO UPDATE` | [Cache methods](cache-methods.md#set) |
| `SetMulti` | method | upserts in one tx | [Cache methods](cache-methods.md#setmulti) |
| `SetNX` | method | tx: delete-expired then `INSERT … DO NOTHING` | [Cache methods](cache-methods.md#setnx) |
| `Expire` | method | `UPDATE expires_at` | [Cache methods](cache-methods.md#expire) |
| `Touch` | method | `Expire` 1h | [Cache methods](cache-methods.md#touch) |
| `Incr` / `Decr` | method | tx select+upsert, 8-byte int64 | [Cache methods](cache-methods.md#counters) |
| `Del` | method | per-key `DELETE` | [Cache methods](cache-methods.md#del) |
| `DeleteByPrefix` | method | `DELETE … LIKE $1 ESCAPE '\'` | [Cache methods](cache-methods.md#deletebyprefix) |
| `Flush` | method | `DELETE FROM table` | [Cache methods](cache-methods.md#flush) |
| `Iterate` | method | `SELECT … ORDER BY key` | [Cache methods](cache-methods.md#iterate) |
| `Ping` | method | `db.PingContext` | [Cache methods](cache-methods.md#ping) |
| `Close` | method | marks closed only | [Cache methods](cache-methods.md#close) |
| `Stats` | method | live `COUNT(*)` → `Entries` | [Cache methods](cache-methods.md#stats) |

There are **no `ErrUnsupported` cases** — the SQL adapter serves the full
contract. Expired rows are filtered on every read, so `Vacuum` is purely space
reclamation, never a correctness concern.
