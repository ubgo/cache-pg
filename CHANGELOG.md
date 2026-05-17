# Changelog

All notable changes to `github.com/ubgo/cache-pg` are documented here.
Format follows Keep a Changelog; the project follows SemVer (pre-GA in `v0.x`).

## [Unreleased]

### Added

- Driver-agnostic `database/sql` adapter implementing `cache.Cache`
  (Postgres 14+ in production; SQLite for in-process conformance).
- `Migrate()` (idempotent DDL) and `Vacuum()` (expired-row reclamation).
- Transactional `SetNX` / `Incr` / `Decr` / `SetMulti`.
- `WithDialect`, `WithTable`, `WithClock` options.
- Passes the shared `github.com/ubgo/cache/cachetest` suite under `-race` on
  in-memory SQLite (no Docker).

### Notes

- Deviates from PLAN §4.9: uses portable `database/sql` rather than an Ent
  schema. Rationale documented in README.

[Unreleased]: https://github.com/ubgo/cache-pg/commits/main
