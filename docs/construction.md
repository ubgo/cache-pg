# Construction & options

### New

`func New(db *sql.DB, opts ...Option) *Cache`

What it is: wraps an already-open `*sql.DB`. Defaults: Postgres dialect, table
`cache_entries`, `time.Now` clock. Call `Migrate` once before use. `Close`
marks the adapter closed only — it never closes the `*sql.DB` you own.

Use cases:

- Reuse your app's existing Postgres pool as a durable cache.
- In-process tests / embedded use on SQLite (no Docker).

```go
package main

import (
	"context"
	"database/sql"

	_ "github.com/jackc/pgx/v5/stdlib"
	pgcache "github.com/ubgo/cache-pg"
)

func main() {
	db, _ := sql.Open("pgx", "postgres://localhost/app")
	c := pgcache.New(db) // Postgres dialect by default
	defer c.Close()

	ctx := context.Background()
	_ = c.Migrate(ctx)
	_ = c.Set(ctx, "k", []byte("v"), 0)
}
```

### Cache

`type Cache struct { ... }`

What it is: the `database/sql` adapter. Concurrency-safe (delegates to the
`*sql.DB` pool); transactions guard `SetNX`, `Incr`, `Decr`, `SetMulti`.

```go
var generic cache.Cache = pgcache.New(db)
```

### Option

`type Option func(*Cache)` — the functional-option type used by `New`.

```go
c := pgcache.New(db, pgcache.WithTable("kv"), pgcache.WithDialect(pgcache.Postgres))
```

### Dialect

`type Dialect int`

What it is: selects SQL placeholder + DDL syntax. Queries are authored once in
Postgres `$1,$2` form; for SQLite a single `rebind` rewrites them to `?` — there
is deliberately no second query set to keep in sync.

```go
d := pgcache.SQLite
c := pgcache.New(db, pgcache.WithDialect(d))
```

### Postgres

`const Postgres Dialect = iota` — `$1` placeholders. The **default**; use for
production Postgres.

```go
c := pgcache.New(db) // equivalent to WithDialect(pgcache.Postgres)
```

### SQLite

`const SQLite Dialect` — `?` placeholders. Use for the in-process conformance
suite, embedded apps, or tests with no Docker.

```go
import _ "modernc.org/sqlite"

db, _ := sql.Open("sqlite", "file:cache.db")
c := pgcache.New(db, pgcache.WithDialect(pgcache.SQLite))
_ = c.Migrate(ctx)
```

### WithDialect

`func WithDialect(d Dialect) Option`

What it is: selects the SQL dialect (default `Postgres`).

Use cases:

- Run the same caching code on Postgres in prod and SQLite in tests.

```go
c := pgcache.New(db, pgcache.WithDialect(pgcache.SQLite))
```

### WithTable

`func WithTable(name string) Option`

What it is: overrides the table name (default `cache_entries`).

Use cases:

- Multiple independent caches in one database (`sessions`, `idempotency`).
- Conform to a team table-naming convention.

```go
sessions := pgcache.New(db, pgcache.WithTable("session_cache"))
idem := pgcache.New(db, pgcache.WithTable("idempotency_cache"))
_ = sessions.Migrate(ctx)
_ = idem.Migrate(ctx)
```

### WithClock

`func WithClock(fn func() time.Time) Option`

What it is: overrides the time source (`expires_at` is stored/compared as unix
nanoseconds). Tests only.

Use cases:

- Deterministically test TTL expiry and `Vacuum` without sleeping.

```go
now := time.Now()
c := pgcache.New(db, pgcache.WithClock(func() time.Time { return now }))
_ = c.Set(ctx, "k", []byte("v"), time.Minute)
now = now.Add(2 * time.Minute) // "k" is now expired deterministically
```
