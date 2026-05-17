# Schema operations

The table is one row per key:

```
key        TEXT PRIMARY KEY
value      BLOB NOT NULL
expires_at BIGINT          -- unix nanoseconds; NULL = no expiry
```

plus an index on `expires_at`. Every read carries
`(expires_at IS NULL OR expires_at > now)`, so an expired row is **never
served** even before it is physically deleted.

### Migrate

`func (c *Cache) Migrate(ctx context.Context) error`

What it is: creates the cache table and its supporting index
(`CREATE TABLE/INDEX IF NOT EXISTS`). Idempotent — safe to call on every boot.
Returns `cache.ErrClosed` if closed.

Use cases:

- Run once at startup before serving traffic.
- Bootstrap the table in a migration step or an init container.

```go
c := pgcache.New(db, pgcache.WithTable("kv"))
if err := c.Migrate(ctx); err != nil { // creates kv + kv_expires_at
	log.Fatal(err)
}
```

### Vacuum

`func (c *Cache) Vacuum(ctx context.Context) error`

What it is: permanently deletes rows whose TTL has elapsed
(`DELETE … WHERE expires_at IS NOT NULL AND expires_at <= now`). Purely space
reclamation — reads already filter expired rows, so skipping `Vacuum` only
grows the table, never serves stale data. Safe to run concurrently with traffic
(single `DELETE`, row locks only). Returns `cache.ErrClosed` if closed.

Use cases:

- Periodic ticker / cron to bound table size.
- Run after a burst of short-TTL writes to reclaim space promptly.

```go
go func() {
	for range time.Tick(15 * time.Minute) {
		if err := c.Vacuum(ctx); err != nil {
			log.Printf("vacuum failed: %v", err)
		}
	}
}()
```
