# ADR-001: Single-Process Architecture and SQLite Driver/Concurrency Policy

**Status:** Accepted  
**Date:** 2026-11-20  
**Scope:** Vektor v0.1 (`cmd/vektord`)

## Context
`TECHNICAL_DESIGN_V0.1.md` requires a single unprivileged Go process responsible for host metric collection, rate derivation, historical persistence, rollups, and retention, surviving restarts and `SIGTERM` cleanly while remaining easy to build and distribute for `amd64` and `arm64`.

## Decisions
1. **Binary & module naming**: The Go module is `vektor` and the daemon binary lives at `cmd/vektord/` (replacing the draft placeholder `homelabd` in `TECHNICAL_DESIGN_V0.1.md`).
2. **No IPC split in v0.1**: Collection and storage run inside a single unprivileged process (`vektord`, intended to run as a dedicated `homelab` system user). Any future privileged host-management operations will live in a separate helper without moving host monitoring into a privileged daemon.
3. **Pure-Go SQLite (`modernc.org/sqlite`)**: Chosen to eliminate CGO dependencies, simplify cross-compilation (`amd64`/`arm64`), and guarantee consistent SQLite 3 behavior (`STRICT`, `WITHOUT ROWID`, window functions, `VACUUM INTO`).
4. **Separate SQLite WAL connection pools**:
   - **Writer pool**: `MaxOpenConns(1)` with `PRAGMA foreign_keys = ON`, `PRAGMA journal_mode = WAL`, `PRAGMA busy_timeout = 5000`, and configurable `PRAGMA synchronous = NORMAL` (default) or `FULL`.
   - **Reader pool**: `MaxOpenConns(4)` opened with `mode=ro` and `PRAGMA query_only = 1` so read queries never contend on writer mutexes.
5. **Durability & backups**:
   - `synchronous=NORMAL` is the default for WAL throughput, with `FULL` available via configuration for stricter power-loss durability.
   - Consistent live backups MUST use `VACUUM INTO <dest>` (via `database.DB.Backup`), never naive file copies of an active WAL database.

## Consequences
- Zero external runtime or C-library dependencies.
- Single serialized writer eliminates `SQLITE_BUSY` inside the process while allowing concurrent read queries.
- Transactions remain short and bounded, and `VACUUM INTO` produces self-contained verified backup files.
