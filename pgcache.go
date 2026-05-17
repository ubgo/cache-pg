package pgcache

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ubgo/cache"
)

// Dialect selects SQL placeholder + DDL syntax.
type Dialect int

const (
	// Postgres uses $1 placeholders (default).
	Postgres Dialect = iota
	// SQLite uses ? placeholders (used by the in-process test suite).
	SQLite
)

// Cache is the database/sql adapter. Construct with New.
type Cache struct {
	db      *sql.DB
	table   string
	dialect Dialect
	clock   func() time.Time
	closed  atomic.Bool
}

// Option configures New.
type Option func(*Cache)

// WithDialect selects the SQL dialect (default Postgres).
func WithDialect(d Dialect) Option { return func(c *Cache) { c.dialect = d } }

// WithTable overrides the table name (default "cache_entries").
func WithTable(name string) Option { return func(c *Cache) { c.table = name } }

// WithClock overrides the time source (tests only).
func WithClock(fn func() time.Time) Option { return func(c *Cache) { c.clock = fn } }

// New wraps an open *sql.DB. Call Migrate once before use.
func New(db *sql.DB, opts ...Option) *Cache {
	c := &Cache{db: db, table: "cache_entries", dialect: Postgres, clock: time.Now}
	for _, o := range opts {
		o(c)
	}
	return c
}

// rebind turns a query written with $1,$2,… into ? for SQLite. It is a pure
// string transform run once per call (queries are short): on '$' it emits '?'
// and skips the following digits. This is the single mechanism that lets every
// query be authored in one Postgres dialect yet run unchanged on SQLite —
// there is deliberately no second, SQLite-specific query set to keep in sync.
func (c *Cache) rebind(q string) string {
	if c.dialect != SQLite {
		return q
	}
	var b strings.Builder
	for i := 0; i < len(q); i++ {
		if q[i] == '$' {
			b.WriteByte('?')
			for i+1 < len(q) && q[i+1] >= '0' && q[i+1] <= '9' {
				i++
			}
			continue
		}
		b.WriteByte(q[i])
	}
	return b.String()
}

// now is the single time source. expires_at is stored and compared as unix
// nanoseconds (a plain BIGINT) rather than a timestamp type so the arithmetic
// is byte-identical on Postgres and SQLite and immune to driver timezone
// handling. WithClock swaps this for deterministic tests.
func (c *Cache) now() int64 { return c.clock().UnixNano() }

// Migrate creates the cache table and supporting indexes if absent.
func (c *Cache) Migrate(ctx context.Context) error {
	if c.closed.Load() {
		return cache.ErrClosed
	}
	stmts := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			key        TEXT PRIMARY KEY,
			value      BLOB NOT NULL,
			expires_at BIGINT
		)`, c.table),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s_expires_at ON %s (expires_at)`, c.table, c.table),
	}
	for _, s := range stmts {
		if _, err := c.db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("pgcache migrate: %w", err)
		}
	}
	return nil
}

// expiresParam maps a TTL to the expires_at column value. ttl<=0 means "no
// expiry" per the cache contract, encoded as SQL NULL; otherwise it is the
// absolute deadline in unix nanoseconds. Returning `any` so a nil flows
// through database/sql as NULL.
func expiresParam(ttl time.Duration, nowNanos int64) any {
	if ttl <= 0 {
		return nil
	}
	return nowNanos + int64(ttl)
}

// Get implements cache.Cache.
func (c *Cache) Get(ctx context.Context, key string) ([]byte, error) {
	if c.closed.Load() {
		return nil, cache.ErrClosed
	}
	q := c.rebind(fmt.Sprintf(
		`SELECT value FROM %s WHERE key=$1 AND (expires_at IS NULL OR expires_at>$2)`, c.table))
	var v []byte
	err := c.db.QueryRowContext(ctx, q, key, c.now()).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cache.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return v, nil
}

// GetMulti implements cache.Cache.
func (c *Cache) GetMulti(ctx context.Context, keys []string) (map[string][]byte, error) {
	if c.closed.Load() {
		return nil, cache.ErrClosed
	}
	out := make(map[string][]byte, len(keys))
	for _, k := range keys {
		v, err := c.Get(ctx, k)
		if err == nil {
			out[k] = v
		} else if !errors.Is(err, cache.ErrNotFound) {
			return nil, err
		}
	}
	return out, nil
}

// Has implements cache.Cache.
func (c *Cache) Has(ctx context.Context, key string) (bool, error) {
	_, err := c.Get(ctx, key)
	if errors.Is(err, cache.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// TTL implements cache.Cache.
func (c *Cache) TTL(ctx context.Context, key string) (time.Duration, error) {
	if c.closed.Load() {
		return 0, cache.ErrClosed
	}
	q := c.rebind(fmt.Sprintf(
		`SELECT expires_at FROM %s WHERE key=$1 AND (expires_at IS NULL OR expires_at>$2)`, c.table))
	var exp sql.NullInt64
	err := c.db.QueryRowContext(ctx, q, key, c.now()).Scan(&exp)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, cache.ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if !exp.Valid {
		return 0, nil
	}
	return time.Duration(exp.Int64 - c.now()), nil
}

func (c *Cache) upsert(ctx context.Context, ex execer, key string, val []byte, ttl time.Duration) error {
	q := c.rebind(fmt.Sprintf(
		`INSERT INTO %s (key,value,expires_at) VALUES ($1,$2,$3)
		 ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, expires_at=EXCLUDED.expires_at`,
		c.table))
	_, err := ex.ExecContext(ctx, q, key, val, expiresParam(ttl, c.now()))
	return err
}

type execer interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
}

// Set implements cache.Cache.
func (c *Cache) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	if c.closed.Load() {
		return cache.ErrClosed
	}
	return c.upsert(ctx, c.db, key, val, ttl)
}

// SetMulti implements cache.Cache.
func (c *Cache) SetMulti(ctx context.Context, items map[string]cache.Item) error {
	if c.closed.Load() {
		return cache.ErrClosed
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for k, it := range items {
		if err := c.upsert(ctx, tx, k, it.Value, it.TTL); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// SetNX implements cache.Cache.
func (c *Cache) SetNX(ctx context.Context, key string, val []byte, ttl time.Duration) (bool, error) {
	if c.closed.Load() {
		return false, cache.ErrClosed
	}
	// Insert-or-replace-if-expired: delete any expired row first, then try a
	// plain insert and report whether it created the row. The delete step is
	// essential — a logically expired but not-yet-vacuumed row would otherwise
	// satisfy ON CONFLICT DO NOTHING and make SetNX wrongly return false for a
	// key the contract considers absent. Whole thing is one transaction so a
	// concurrent SetNX cannot interleave between the delete and the insert.
	delQ := c.rebind(fmt.Sprintf(
		`DELETE FROM %s WHERE key=$1 AND expires_at IS NOT NULL AND expires_at<=$2`, c.table))
	insQ := c.rebind(fmt.Sprintf(
		`INSERT INTO %s (key,value,expires_at) VALUES ($1,$2,$3) ON CONFLICT (key) DO NOTHING`,
		c.table))
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, delQ, key, c.now()); err != nil {
		_ = tx.Rollback()
		return false, err
	}
	res, err := tx.ExecContext(ctx, insQ, key, val, expiresParam(ttl, c.now()))
	if err != nil {
		_ = tx.Rollback()
		return false, err
	}
	n, _ := res.RowsAffected()
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return n > 0, nil
}

// Expire implements cache.Cache.
func (c *Cache) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if c.closed.Load() {
		return cache.ErrClosed
	}
	q := c.rebind(fmt.Sprintf(
		`UPDATE %s SET expires_at=$1 WHERE key=$2 AND (expires_at IS NULL OR expires_at>$3)`,
		c.table))
	res, err := c.db.ExecContext(ctx, q, expiresParam(ttl, c.now()), key, c.now())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return cache.ErrNotFound
	}
	return nil
}

// Touch implements cache.Cache.
func (c *Cache) Touch(ctx context.Context, key string) error {
	return c.Expire(ctx, key, time.Hour)
}

func (c *Cache) addInt(ctx context.Context, key string, delta int64) (int64, error) {
	if c.closed.Load() {
		return 0, cache.ErrClosed
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	selQ := c.rebind(fmt.Sprintf(
		`SELECT value,expires_at FROM %s WHERE key=$1 AND (expires_at IS NULL OR expires_at>$2)`,
		c.table))
	var cur int64
	var raw []byte
	var exp sql.NullInt64
	row := tx.QueryRowContext(ctx, selQ, key, c.now())
	switch err := row.Scan(&raw, &exp); {
	case errors.Is(err, sql.ErrNoRows):
		// starts at 0
	case err != nil:
		_ = tx.Rollback()
		return 0, err
	default:
		if len(raw) == 8 {
			cur = int64(binary.BigEndian.Uint64(raw))
		}
	}
	// Counters are stored as a fixed 8-byte big-endian int64 in the value
	// column. A missing or expired row is treated as 0 (contract rule). The
	// original expires_at is carried through the upsert so incrementing a
	// TTL'd counter does not silently extend its lifetime.
	cur += delta
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(cur))
	var expArg any
	if exp.Valid {
		expArg = exp.Int64
	}
	upQ := c.rebind(fmt.Sprintf(
		`INSERT INTO %s (key,value,expires_at) VALUES ($1,$2,$3)
		 ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, expires_at=EXCLUDED.expires_at`,
		c.table))
	if _, err := tx.ExecContext(ctx, upQ, key, b, expArg); err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return cur, nil
}

// Incr implements cache.Cache.
func (c *Cache) Incr(ctx context.Context, key string, delta int64) (int64, error) {
	return c.addInt(ctx, key, delta)
}

// Decr implements cache.Cache.
func (c *Cache) Decr(ctx context.Context, key string, delta int64) (int64, error) {
	return c.addInt(ctx, key, -delta)
}

// Del implements cache.Cache.
func (c *Cache) Del(ctx context.Context, keys ...string) error {
	if c.closed.Load() {
		return cache.ErrClosed
	}
	if len(keys) == 0 {
		return nil
	}
	for _, k := range keys {
		q := c.rebind(fmt.Sprintf(`DELETE FROM %s WHERE key=$1`, c.table))
		if _, err := c.db.ExecContext(ctx, q, k); err != nil {
			return err
		}
	}
	return nil
}

// DeleteByPrefix implements cache.Cache.
func (c *Cache) DeleteByPrefix(ctx context.Context, prefix string) error {
	if c.closed.Load() {
		return cache.ErrClosed
	}
	q := c.rebind(fmt.Sprintf(`DELETE FROM %s WHERE key LIKE $1 ESCAPE '\'`, c.table))
	_, err := c.db.ExecContext(ctx, q, likeEscape(prefix)+"%")
	return err
}

// Flush implements cache.Cache.
func (c *Cache) Flush(ctx context.Context) error {
	if c.closed.Load() {
		return cache.ErrClosed
	}
	_, err := c.db.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s`, c.table))
	return err
}

// Vacuum permanently removes rows whose TTL has elapsed. Run periodically
// (cron / ticker). This is purely space reclamation: reads already filter
// expired rows, so skipping Vacuum only grows the table, never serves stale
// data. Safe to run concurrently with traffic (single DELETE, row locks only).
func (c *Cache) Vacuum(ctx context.Context) error {
	if c.closed.Load() {
		return cache.ErrClosed
	}
	q := c.rebind(fmt.Sprintf(
		`DELETE FROM %s WHERE expires_at IS NOT NULL AND expires_at<=$1`, c.table))
	_, err := c.db.ExecContext(ctx, q, c.now())
	return err
}

// Iterate implements cache.Cache.
func (c *Cache) Iterate(ctx context.Context, opts cache.IterateOpts) cache.Iterator {
	q := c.rebind(fmt.Sprintf(
		`SELECT key,value FROM %s WHERE (expires_at IS NULL OR expires_at>$1) AND key LIKE $2 ESCAPE '\' ORDER BY key`,
		c.table))
	rows, err := c.db.QueryContext(ctx, q, c.now(), likeEscape(opts.Prefix)+"%")
	return &iter{rows: rows, err: err}
}

type iter struct {
	rows *sql.Rows
	err  error
	k    string
	v    []byte
}

func (it *iter) Next() bool {
	if it.err != nil || it.rows == nil {
		return false
	}
	if !it.rows.Next() {
		it.err = it.rows.Err()
		return false
	}
	if err := it.rows.Scan(&it.k, &it.v); err != nil {
		it.err = err
		return false
	}
	return true
}

func (it *iter) Key() string   { return it.k }
func (it *iter) Value() []byte { return it.v }
func (it *iter) Err() error    { return it.err }
func (it *iter) Close() error {
	if it.rows != nil {
		return it.rows.Close()
	}
	return nil
}

// Ping implements cache.Cache.
func (c *Cache) Ping(ctx context.Context) error {
	if c.closed.Load() {
		return cache.ErrClosed
	}
	return c.db.PingContext(ctx)
}

// Close implements cache.Cache. Idempotent; does not close the *sql.DB the
// caller owns — it only marks this adapter closed.
func (c *Cache) Close() error {
	c.closed.Store(true)
	return nil
}

// Stats implements cache.Cache. Entries is a live COUNT(*) (best-effort).
func (c *Cache) Stats() cache.Stats {
	if c.closed.Load() {
		return cache.Stats{}
	}
	var n int64
	q := c.rebind(fmt.Sprintf(
		`SELECT COUNT(*) FROM %s WHERE expires_at IS NULL OR expires_at>$1`, c.table))
	_ = c.db.QueryRowContext(context.Background(), q, c.now()).Scan(&n)
	return cache.Stats{Entries: n}
}

// likeEscape neutralizes LIKE wildcards in a literal prefix so a caller prefix
// containing %, _ or \ matches literally. Paired with `ESCAPE '\'` in every
// LIKE query (DeleteByPrefix, Iterate). Order matters: backslash is escaped
// first so the escape characters introduced for % and _ are not re-escaped.
func likeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

var _ cache.Cache = (*Cache)(nil)
