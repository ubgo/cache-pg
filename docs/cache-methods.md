# Cache methods (`cache.Cache` over SQL)

Queries are authored once in Postgres `$1` placeholder syntax and rewritten to
`?` for SQLite. `expires_at` is unix-nanoseconds (`NULL` = no expiry). Snippets
assume:

```go
ctx := context.Background()
c := pgcache.New(db)
_ = c.Migrate(ctx)
defer c.Close()
```

## Read

### Get

`Get(ctx, key)` → `SELECT value FROM t WHERE key=$1 AND (expires_at IS NULL OR
expires_at>$2)`. `sql.ErrNoRows` → `cache.ErrNotFound`. Expired rows are
filtered, so a not-yet-vacuumed expired key reads as a miss.

```go
v, err := c.Get(ctx, "user:42")
if errors.Is(err, cache.ErrNotFound) { /* miss or expired */ }
```

### GetMulti

`GetMulti(ctx, keys)` → one filtered `Get` per key; absent keys omitted. A real
DB error (not a miss) aborts and is returned.

```go
m, _ := c.GetMulti(ctx, []string{"a", "b"})
```

### Has

`Has(ctx, key)` → `Get` then maps `ErrNotFound` → `false`.

```go
ok, _ := c.Has(ctx, "idem:abc")
```

### TTL

`TTL(ctx, key)` → `SELECT expires_at …`. Absent/expired → `cache.ErrNotFound`;
`NULL` expiry → `(0, nil)`; else remaining duration.

```go
d, err := c.TTL(ctx, "session:42")
```

## Write

### Set

`Set(ctx, key, val, ttl)` → `INSERT … ON CONFLICT (key) DO UPDATE SET
value=EXCLUDED.value, expires_at=EXCLUDED.expires_at`. `ttl <= 0` stores `NULL`
(no expiry).

```go
_ = c.Set(ctx, "k", []byte("v"), 10*time.Minute)
```

### SetMulti

`SetMulti(ctx, items)` → all upserts inside one transaction (atomic; rolls back
on any error).

```go
_ = c.SetMulti(ctx, map[string]cache.Item{
	"a": {Value: []byte("1"), TTL: time.Minute},
	"b": {Value: []byte("2")},
})
```

### SetNX

`SetNX(ctx, key, val, ttl)` → one transaction: first `DELETE` any
logically-expired-but-not-vacuumed row for the key, then
`INSERT … ON CONFLICT DO NOTHING`; returns `(true, nil)` iff a row was created.
The delete step is essential — without it an expired row would wrongly make
`SetNX` return `false` for a key the contract considers absent.

Use cases: durable locks, write-once idempotency keys that survive a restart.

```go
ok, _ := c.SetNX(ctx, "idem:order-9", []byte("done"), 24*time.Hour)
if !ok { /* already processed */ }
```

### Expire

`Expire(ctx, key, ttl)` → `UPDATE … SET expires_at=$1 WHERE key=$2 AND
not-expired`. 0 rows affected → `cache.ErrNotFound`. `ttl <= 0` sets `NULL`
(permanent).

```go
_ = c.Expire(ctx, "session:42", time.Hour)
_ = c.Expire(ctx, "k", 0) // make permanent
```

### Touch

`Touch(ctx, key)` → `Expire(ctx, key, time.Hour)`.

```go
_ = c.Touch(ctx, "session:42")
```

## Counters

### Incr / Decr

`Incr`/`Decr` run in a transaction: select the current value (a fixed 8-byte
big-endian int64; missing/expired = 0), apply the delta, upsert — carrying the
original `expires_at` through so a TTL'd counter's lifetime is not silently
extended. `Decr(k,d)` == `Incr(k,-d)`; values may go negative.

Use cases: durable rate-limit / usage counters that survive a restart.

```go
n, _ := c.Incr(ctx, "usage:tenant7", 1)
_, _ = c.Decr(ctx, "quota:tenant7", 1)
```

## Delete

### Del

`Del(ctx, keys...)` → one `DELETE FROM t WHERE key=$1` per key. Empty list is a
no-op. Deleting an absent key is not an error.

```go
_ = c.Del(ctx, "a", "b")
```

### DeleteByPrefix

`DeleteByPrefix(ctx, prefix)` → `DELETE … WHERE key LIKE $1 ESCAPE '\'` with
the literal prefix's `%`, `_`, `\` escaped, so a `%` in your prefix matches
literally.

```go
_ = c.DeleteByPrefix(ctx, "user:42:") // literal, wildcard-safe
```

### Flush

`Flush(ctx)` → `DELETE FROM t` (empties the whole table).

```go
_ = c.Flush(ctx)
```

## Iterate

### Iterate

`Iterate(ctx, cache.IterateOpts)` → `SELECT key,value … WHERE not-expired AND
key LIKE $2 ESCAPE '\' ORDER BY key`. Stable key order. Always `Close()` the
iterator; check `Err()` after the loop.

```go
it := c.Iterate(ctx, cache.IterateOpts{Prefix: "user:"})
defer it.Close()
for it.Next() {
	fmt.Println(it.Key(), string(it.Value()))
}
if err := it.Err(); err != nil { log.Fatal(err) }
```

## Lifecycle

### Ping

`Ping(ctx)` → `db.PingContext`. `cache.ErrClosed` after `Close`.

```go
if err := c.Ping(ctx); err != nil { log.Fatal("db down:", err) }
```

### Close

`Close()` — idempotent; marks the adapter closed. **Does not** close the
`*sql.DB` you own.

```go
defer c.Close()
defer db.Close() // the pool you created
```

### Stats

`Stats()` → `cache.Stats{Entries: live COUNT(*) of non-expired rows}`
(best-effort; other fields zero).

```go
fmt.Println("live entries:", c.Stats().Entries)
```
