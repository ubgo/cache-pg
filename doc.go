// Package pgcache is the SQL adapter for github.com/ubgo/cache. It targets
// Postgres 14+ in production but is driver-agnostic (it takes a *sql.DB), so
// the conformance suite runs in-process on SQLite with no Docker.
//
//	db, _ := sql.Open("pgx", dsn)
//	c := pgcache.New(db) // Postgres dialect by default
//	_ = c.Migrate(ctx)   // creates the cache_entries table + indexes
//	defer c.Close()
//
// Deviation from PLAN §4.9: the plan specified an Ent schema. This adapter
// uses portable database/sql instead — no code generation, one table, works
// unchanged on Postgres and SQLite. The trade-off is no Ent client surface;
// the cache contract does not need one.
//
// Durable, single-node cache for environments that already run Postgres and
// do not want Redis. Call Vacuum periodically (or via cron) to reclaim rows
// for expired entries.
//
// Design invariants worth knowing before editing this package:
//
//   - Queries are authored ONCE in Postgres "$1,$2" placeholder syntax;
//     rebind rewrites them to "?" for SQLite. Never fork query strings per
//     dialect — extend rebind if a genuine syntax difference appears.
//   - expires_at is unix-nanoseconds, NULL = no expiry. Every read carries
//     "(expires_at IS NULL OR expires_at > now)" so an expired row is never
//     served even before Vacuum physically deletes it. Vacuum is a space
//     concern, never a correctness one.
//   - SetNX, Incr, Decr and SetMulti run inside a transaction. SetNX deletes
//     a logically-expired-but-not-vacuumed row first, so it can be reclaimed.
//   - DeleteByPrefix/Iterate escape LIKE wildcards in the literal prefix and
//     use ESCAPE '\' so a '%' in the caller's prefix matches literally.
//   - Close marks the adapter closed only; it never closes the *sql.DB the
//     caller owns.
package pgcache
