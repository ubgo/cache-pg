// pgcache_test.go — tests for the pgcache adapter (conformance suite on in-memory SQLite, Migrate idempotency, Vacuum, Close).

package pgcache_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ubgo/cache"
	pgcache "github.com/ubgo/cache-pg"
	"github.com/ubgo/cache/cachetest"
	_ "modernc.org/sqlite"
)

// newSQLite gives each subtest an isolated in-memory database. shared cache +
// a kept-open connection keep the in-memory DB alive for the test's lifetime.
func newSQLite(t testing.TB) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?cache=shared&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1) // serialize writes; avoids in-memory lock churn
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func factory(t *testing.T) cache.Cache {
	c := pgcache.New(newSQLite(t), pgcache.WithDialect(pgcache.SQLite))
	if err := c.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return c
}

func TestConformance(t *testing.T) {
	cachetest.Run(t, factory)
}

func TestMigrateIdempotent(t *testing.T) {
	ctx := context.Background()
	c := pgcache.New(newSQLite(t), pgcache.WithDialect(pgcache.SQLite))
	if err := c.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate must be a no-op, got %v", err)
	}
}

func TestVacuumRemovesExpired(t *testing.T) {
	ctx := context.Background()
	c := factory(t)
	if err := c.Set(ctx, "live", []byte("v"), time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := c.Set(ctx, "dead", []byte("v"), 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)
	if err := c.(interface {
		Vacuum(context.Context) error
	}).Vacuum(ctx); err != nil {
		t.Fatal(err)
	}
	// "dead" is gone (expired + vacuumed); "live" remains.
	if ok, _ := c.Has(ctx, "live"); !ok {
		t.Fatal("live entry wrongly vacuumed")
	}
	it := c.Iterate(ctx, cache.IterateOpts{})
	defer func() { _ = it.Close() }()
	n := 0
	for it.Next() {
		n++
	}
	if it.Err() != nil {
		t.Fatal(it.Err())
	}
	if n != 1 {
		t.Fatalf("want 1 row after vacuum, got %d", n)
	}
}

func TestClosedReturnsErrClosed(t *testing.T) {
	ctx := context.Background()
	c := factory(t)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close must be idempotent, got %v", err)
	}
	if _, err := c.Get(ctx, "k"); err != cache.ErrClosed {
		t.Fatalf("want ErrClosed, got %v", err)
	}
}
