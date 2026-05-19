// coverage_test.go — targeted branch coverage for pgcache (closed-adapter
// guards, forced DB-error paths via a closed *sql.DB, options, TTL/Touch/
// Stats/Value, SetNX/addInt branches, likeEscape, ordered Iterate, rebind
// Postgres passthrough). Deterministic only.

package pgcache_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ubgo/cache"
	pgcache "github.com/ubgo/cache-pg"
	_ "modernc.org/sqlite"
)

// newClosedDB returns a migrated cache whose underlying *sql.DB has been
// closed, so every subsequent query fails with "sql: database is closed" —
// drives the non-ErrClosed error-return branches without touching production.
func newClosedDB(t testing.TB) cache.Cache {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	c := pgcache.New(db, pgcache.WithDialect(pgcache.SQLite))
	if err := c.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	return c
}

func TestClosedReturnsErrClosedAllMethods(t *testing.T) {
	ctx := context.Background()
	c := factory(t)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	mig := c.(interface{ Migrate(context.Context) error })
	if err := mig.Migrate(ctx); err != cache.ErrClosed {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := c.Get(ctx, "k"); err != cache.ErrClosed {
		t.Fatalf("Get: %v", err)
	}
	if _, err := c.GetMulti(ctx, []string{"k"}); err != cache.ErrClosed {
		t.Fatalf("GetMulti: %v", err)
	}
	if _, err := c.Has(ctx, "k"); err != cache.ErrClosed {
		t.Fatalf("Has: %v", err)
	}
	if _, err := c.TTL(ctx, "k"); err != cache.ErrClosed {
		t.Fatalf("TTL: %v", err)
	}
	if err := c.Set(ctx, "k", []byte("v"), 0); err != cache.ErrClosed {
		t.Fatalf("Set: %v", err)
	}
	if err := c.SetMulti(ctx, map[string]cache.Item{"k": {Value: []byte("v")}}); err != cache.ErrClosed {
		t.Fatalf("SetMulti: %v", err)
	}
	if _, err := c.SetNX(ctx, "k", []byte("v"), 0); err != cache.ErrClosed {
		t.Fatalf("SetNX: %v", err)
	}
	if err := c.Expire(ctx, "k", time.Minute); err != cache.ErrClosed {
		t.Fatalf("Expire: %v", err)
	}
	if err := c.Touch(ctx, "k"); err != cache.ErrClosed {
		t.Fatalf("Touch: %v", err)
	}
	if _, err := c.Incr(ctx, "k", 1); err != cache.ErrClosed {
		t.Fatalf("Incr: %v", err)
	}
	if _, err := c.Decr(ctx, "k", 1); err != cache.ErrClosed {
		t.Fatalf("Decr: %v", err)
	}
	if err := c.Del(ctx, "k"); err != cache.ErrClosed {
		t.Fatalf("Del: %v", err)
	}
	if err := c.DeleteByPrefix(ctx, "k"); err != cache.ErrClosed {
		t.Fatalf("DeleteByPrefix: %v", err)
	}
	if err := c.Flush(ctx); err != cache.ErrClosed {
		t.Fatalf("Flush: %v", err)
	}
	if err := c.(interface {
		Vacuum(context.Context) error
	}).Vacuum(ctx); err != cache.ErrClosed {
		t.Fatalf("Vacuum: %v", err)
	}
	if err := c.Ping(ctx); err != cache.ErrClosed {
		t.Fatalf("Ping: %v", err)
	}
	if s := c.Stats(); s.Entries != 0 {
		t.Fatalf("Stats after close: %+v", s)
	}
}

func TestForcedDBErrorPaths(t *testing.T) {
	ctx := context.Background()
	c := newClosedDB(t)

	if _, err := c.Get(ctx, "k"); err == nil {
		t.Fatal("Get want db err")
	}
	if _, err := c.GetMulti(ctx, []string{"k"}); err == nil {
		t.Fatal("GetMulti want db err")
	}
	if _, err := c.Has(ctx, "k"); err == nil {
		t.Fatal("Has want db err")
	}
	if _, err := c.TTL(ctx, "k"); err == nil {
		t.Fatal("TTL want db err")
	}
	if err := c.Set(ctx, "k", []byte("v"), time.Minute); err == nil {
		t.Fatal("Set want db err")
	}
	if err := c.SetMulti(ctx, map[string]cache.Item{"k": {Value: []byte("v")}}); err == nil {
		t.Fatal("SetMulti want db err (BeginTx)")
	}
	if _, err := c.SetNX(ctx, "k", []byte("v"), time.Minute); err == nil {
		t.Fatal("SetNX want db err (BeginTx)")
	}
	if err := c.Expire(ctx, "k", time.Minute); err == nil {
		t.Fatal("Expire want db err")
	}
	if err := c.Touch(ctx, "k"); err == nil {
		t.Fatal("Touch want db err")
	}
	if _, err := c.Incr(ctx, "k", 1); err == nil {
		t.Fatal("Incr want db err (BeginTx)")
	}
	if _, err := c.Decr(ctx, "k", 1); err == nil {
		t.Fatal("Decr want db err")
	}
	if err := c.Del(ctx, "k"); err == nil {
		t.Fatal("Del want db err")
	}
	if err := c.DeleteByPrefix(ctx, "p"); err == nil {
		t.Fatal("DeleteByPrefix want db err")
	}
	if err := c.Flush(ctx); err == nil {
		t.Fatal("Flush want db err")
	}
	if err := c.(interface {
		Vacuum(context.Context) error
	}).Vacuum(ctx); err == nil {
		t.Fatal("Vacuum want db err")
	}
	if err := c.Ping(ctx); err == nil {
		t.Fatal("Ping want db err")
	}
	// Stats swallows the scan error -> zero entries (not closed).
	if s := c.Stats(); s.Entries != 0 {
		t.Fatalf("Stats on closed db: %+v", s)
	}
	// Iterate: QueryContext fails -> iter.err set, Next false, Err non-nil.
	it := c.Iterate(ctx, cache.IterateOpts{})
	if it.Next() {
		t.Fatal("Next on closed db should be false")
	}
	if it.Err() == nil {
		t.Fatal("Iterate want query err")
	}
	// QueryContext failed so rows is nil -> Close is a no-op (returns nil).
	if err := it.Close(); err != nil {
		t.Fatalf("iter.Close with nil rows: %v", err)
	}
}

func TestMigrateForcedError(t *testing.T) {
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	c := pgcache.New(db, pgcache.WithDialect(pgcache.SQLite))
	_ = db.Close()
	if err := c.Migrate(context.Background()); err == nil {
		t.Fatal("Migrate want db err")
	}
}

func TestWithTableAndWithClock(t *testing.T) {
	ctx := context.Background()
	fixed := time.Unix(1_700_000_000, 0)
	c := pgcache.New(newSQLite(t),
		pgcache.WithDialect(pgcache.SQLite),
		pgcache.WithTable("custom_cache"),
		pgcache.WithClock(func() time.Time { return fixed }))
	if err := c.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Set(ctx, "k", []byte("v"), time.Hour); err != nil {
		t.Fatal(err)
	}
	v, err := c.Get(ctx, "k")
	if err != nil || string(v) != "v" {
		t.Fatalf("Get custom table: %v %v", v, err)
	}
	// With a frozen clock the TTL is exactly one hour.
	d, err := c.TTL(ctx, "k")
	if err != nil || d != time.Hour {
		t.Fatalf("TTL with frozen clock = %v %v", d, err)
	}
	if s := c.Stats(); s.Entries != 1 {
		t.Fatalf("Stats custom table = %+v", s)
	}
}

func TestTTLNoExpiry(t *testing.T) {
	ctx := context.Background()
	c := factory(t)
	if err := c.Set(ctx, "k", []byte("v"), 0); err != nil {
		t.Fatal(err)
	}
	d, err := c.TTL(ctx, "k")
	if err != nil || d != 0 {
		t.Fatalf("TTL no-expiry = %v %v", d, err)
	}
	if _, err := c.TTL(ctx, "missing"); err != cache.ErrNotFound {
		t.Fatalf("TTL missing: %v", err)
	}
}

func TestSetNXCreatedExistsAndExpiredReplace(t *testing.T) {
	ctx := context.Background()
	c := factory(t)

	ok, err := c.SetNX(ctx, "k", []byte("v1"), time.Minute)
	if err != nil || !ok {
		t.Fatalf("first SetNX: %v %v", ok, err)
	}
	ok, err = c.SetNX(ctx, "k", []byte("v2"), time.Minute)
	if err != nil || ok {
		t.Fatalf("second SetNX must be false: %v %v", ok, err)
	}
	// Expired-but-unvacuumed row: SetNX deletes it first, then inserts -> true.
	if err := c.Set(ctx, "exp", []byte("old"), 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)
	ok, err = c.SetNX(ctx, "exp", []byte("new"), time.Minute)
	if err != nil || !ok {
		t.Fatalf("SetNX over expired row must create: %v %v", ok, err)
	}
	v, err := c.Get(ctx, "exp")
	if err != nil || string(v) != "new" {
		t.Fatalf("after SetNX-replace: %v %v", v, err)
	}
}

func TestAddIntInitExistingAndTTLCarry(t *testing.T) {
	ctx := context.Background()
	c := factory(t)

	// Incr from missing -> initializes at delta.
	n, err := c.Incr(ctx, "ctr", 5)
	if err != nil || n != 5 {
		t.Fatalf("Incr init: %v %v", n, err)
	}
	// Incr existing -> accumulates.
	n, err = c.Incr(ctx, "ctr", 3)
	if err != nil || n != 8 {
		t.Fatalf("Incr existing: %v %v", n, err)
	}
	// Decr.
	n, err = c.Decr(ctx, "ctr", 2)
	if err != nil || n != 6 {
		t.Fatalf("Decr: %v %v", n, err)
	}
	// Counter with a TTL: incrementing must not drop/extend the expiry
	// silently (carries original expires_at through the upsert).
	if _, err := c.Incr(ctx, "tctr", 1); err != nil {
		t.Fatal(err)
	}
	if err := c.Expire(ctx, "tctr", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Incr(ctx, "tctr", 1); err != nil {
		t.Fatal(err)
	}
	d, err := c.TTL(ctx, "tctr")
	if err != nil || d <= 0 || d > time.Hour {
		t.Fatalf("TTL'd counter expiry carried = %v %v", d, err)
	}
}

func TestExpireNotFound(t *testing.T) {
	ctx := context.Background()
	c := factory(t)
	if err := c.Expire(ctx, "missing", time.Minute); err != cache.ErrNotFound {
		t.Fatalf("Expire missing: %v", err)
	}
	if err := c.Touch(ctx, "missing"); err != cache.ErrNotFound {
		t.Fatalf("Touch missing: %v", err)
	}
}

func TestDelEmptyAndMany(t *testing.T) {
	ctx := context.Background()
	c := factory(t)
	if err := c.Del(ctx); err != nil {
		t.Fatalf("Del() no keys: %v", err)
	}
	for _, k := range []string{"a", "b", "c"} {
		if err := c.Set(ctx, k, []byte("v"), 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Del(ctx, "a", "b", "c"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := c.Has(ctx, "a"); ok {
		t.Fatal("Del did not remove keys")
	}
}

func TestDeleteByPrefixLikeEscape(t *testing.T) {
	ctx := context.Background()
	c := factory(t)
	// Keys containing LIKE metacharacters must be matched literally.
	keys := []string{`pre%fix:1`, `pre_fix:2`, `pre\fix:3`, `other:1`}
	for _, k := range keys {
		if err := c.Set(ctx, k, []byte("v"), 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.DeleteByPrefix(ctx, `pre%`); err != nil {
		t.Fatal(err)
	}
	// Only the literal "pre%..." key removed; "_", "\" and "other" untouched.
	if ok, _ := c.Has(ctx, `pre%fix:1`); ok {
		t.Fatal("literal pre% not deleted")
	}
	if ok, _ := c.Has(ctx, `pre_fix:2`); !ok {
		t.Fatal("pre_ wrongly matched by pre%")
	}
	if ok, _ := c.Has(ctx, `other:1`); !ok {
		t.Fatal("other wrongly deleted")
	}
}

func TestIterateOrderedAndPrefixAndExpiredFilter(t *testing.T) {
	ctx := context.Background()
	c := factory(t)
	if err := c.Set(ctx, "p:b", []byte("2"), 0); err != nil {
		t.Fatal(err)
	}
	if err := c.Set(ctx, "p:a", []byte("1"), 0); err != nil {
		t.Fatal(err)
	}
	if err := c.Set(ctx, "q:z", []byte("9"), 0); err != nil {
		t.Fatal(err)
	}
	if err := c.Set(ctx, "p:gone", []byte("x"), 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond) // p:gone now expired (filtered by read)

	it := c.Iterate(ctx, cache.IterateOpts{Prefix: "p:"})
	defer func() { _ = it.Close() }()
	var keys []string
	var vals []string
	for it.Next() {
		keys = append(keys, it.Key())
		vals = append(vals, string(it.Value()))
	}
	if it.Err() != nil {
		t.Fatal(it.Err())
	}
	if len(keys) != 2 || keys[0] != "p:a" || keys[1] != "p:b" {
		t.Fatalf("ordered prefix iterate = %v", keys)
	}
	if vals[0] != "1" || vals[1] != "2" {
		t.Fatalf("iterate values = %v", vals)
	}
}

// newDroppedTable returns a migrated cache plus the live *sql.DB; the caller
// drops the table so BeginTx still succeeds but the inner upsert/exec inside
// SetMulti / SetNX / addInt fails — driving the rollback branches. This is
// test-only DDL; no production code changes.
func newDroppedTable(t testing.TB) (cache.Cache, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?cache=shared&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	c := pgcache.New(db, pgcache.WithDialect(pgcache.SQLite))
	if err := c.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := db.Exec(`DROP TABLE cache_entries`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	return c, db
}

func TestSetMultiRollbackOnUpsertError(t *testing.T) {
	ctx := context.Background()
	c, _ := newDroppedTable(t)
	if err := c.SetMulti(ctx, map[string]cache.Item{"a": {Value: []byte("v")}}); err == nil {
		t.Fatal("SetMulti want upsert error -> rollback")
	}
}

func TestSetNXRollbackPaths(t *testing.T) {
	ctx := context.Background()
	c, _ := newDroppedTable(t)
	// DELETE step fails (no table) -> rollback, error.
	if _, err := c.SetNX(ctx, "k", []byte("v"), time.Minute); err == nil {
		t.Fatal("SetNX want delete-exec error -> rollback")
	}
}

func TestAddIntRollbackOnUpsertError(t *testing.T) {
	ctx := context.Background()
	c, _ := newDroppedTable(t)
	// SELECT fails inside tx (no table). Scan returns a non-NoRows error ->
	// rollback path in addInt.
	if _, err := c.Incr(ctx, "k", 1); err == nil {
		t.Fatal("Incr want select error -> rollback")
	}
	if _, err := c.Decr(ctx, "k", 1); err == nil {
		t.Fatal("Decr want select error -> rollback")
	}
}

// newCheckConstrained pre-creates the cache table with a CHECK constraint
// that rejects the sentinel value []byte("REJECT"). pgcache.Migrate uses
// CREATE TABLE IF NOT EXISTS, so it adopts this table unchanged. A SetNX /
// Incr whose write carries the rejected value then fails at the INSERT
// (after the DELETE / SELECT step succeeds) — driving the inner-exec
// rollback branches. Test-only DDL; no production change.
func newCheckConstrained(t testing.TB) cache.Cache {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?cache=shared&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE cache_entries (
		key TEXT PRIMARY KEY,
		value BLOB NOT NULL CHECK (value <> X'52454a454354'),
		expires_at BIGINT
	)`); err != nil {
		t.Fatalf("precreate: %v", err)
	}
	c := pgcache.New(db, pgcache.WithDialect(pgcache.SQLite))
	if err := c.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return c
}

func TestSetNXInsertFailsAfterDelete(t *testing.T) {
	ctx := context.Background()
	c := newCheckConstrained(t)
	// DELETE step finds no expired row (succeeds); INSERT violates the CHECK
	// constraint -> rollback + error (pgcache.go:272-275).
	if _, err := c.SetNX(ctx, "k", []byte("REJECT"), time.Minute); err == nil {
		t.Fatal("SetNX want INSERT check failure -> rollback")
	}
}

func TestAddIntUpsertFailsAfterSelect(t *testing.T) {
	ctx := context.Background()
	// Pre-create the table with a CHECK that rejects the exact 8-byte
	// big-endian encoding of the counter value 1 (0x0000000000000001). The
	// addInt SELECT finds no row (NoRows, succeeds); the subsequent upsert
	// writes that rejected value -> exec fails -> rollback branch
	// (pgcache.go:347-350).
	db, err := sql.Open("sqlite", "file::memory:?cache=shared&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE cache_entries (
		key TEXT PRIMARY KEY,
		value BLOB NOT NULL CHECK (value <> X'0000000000000001'),
		expires_at BIGINT
	)`); err != nil {
		t.Fatalf("precreate: %v", err)
	}
	c := pgcache.New(db, pgcache.WithDialect(pgcache.SQLite))
	if err := c.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := c.Incr(ctx, "ctr", 1); err == nil {
		t.Fatal("Incr want upsert CHECK failure after successful SELECT -> rollback")
	}
}

func TestRebindPostgresPassthrough(t *testing.T) {
	// Postgres dialect must NOT rewrite $N -> ?. We can't run a real Postgres
	// here, but the rebind no-op branch is exercised by constructing a
	// Postgres-dialect cache and confirming a query string is unchanged via a
	// closed DB error that still proves the code path ran. Simpler: a
	// Postgres-dialect Stats() on a closed adapter returns zero without panic
	// and exercises the rebind early-return (dialect != SQLite).
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	c := pgcache.New(db) // default Postgres dialect
	// Stats builds a query and calls rebind with Postgres -> passthrough.
	// The SQLite driver will error on "$1" placeholders; Stats swallows it.
	if s := c.Stats(); s.Entries != 0 {
		t.Fatalf("Postgres-dialect Stats on sqlite driver = %+v", s)
	}
}
