// doc.go — canonical package documentation (package pgcache, github.com/ubgo/cache-pg).
//
// Package role: this file is the authoritative overview for the ubgo/cache
// SQL adapter; start here before reading pgcache.go (the adapter).
//
// This file: holds ONLY the package doc comment below — no code. It records
// the deviation from PLAN §4.9 (portable database/sql instead of Ent) and the
// design invariants (one Postgres-syntax query set rebound for SQLite,
// expires_at unix-nanos with NULL = no expiry, tx-guarded SetNX/Incr/Decr,
// LIKE ESCAPE prefix ops, Close marks-closed-only) that pgcache.go implements.
//
// AI-context: the // Package … block below is the godoc package doc; do not
// duplicate it (revive flags duplicate package comments). The blank line
// after this header keeps it a file header, not a second package comment.

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
