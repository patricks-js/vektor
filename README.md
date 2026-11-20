# Vektor

Lightweight, native-first Linux host monitoring and infrastructure management platform (v0.1: **Metrics Engine & History Engine**).

Vektor v0.1 implements an unprivileged single-process Go daemon (`vektord`) that collects Linux host telemetry directly from `/proc`, `/sys`, and `statfs`, derives monotonic-clock rates, and persists multi-resolution time series (`30s` raw, `5m` medium, `1h` long) into SQLite WAL with weighted idempotent rollups and watermark-safe retention.

## Package Layout

```text
cmd/vektord/                 # Unprivileged daemon entrypoint & --check mode
internal/
  config/                    # Defaults, bounds validation, excluded FS policy
  database/                  # Pure-Go SQLite WAL writer/reader pools, migrations, VACUUM INTO backup
  host/                      # Host inventory & persistent resource reconciliation registry
  observability/             # Structured JSON slog logger & internal HealthTracker
  metrics/
    types.go                 # RawSample (gauge/counter), CPUSample, Clock, MetricPoint
    catalog.go               # Central versioned metrics catalog & UTC bucket alignment
    collector/               # Linux procfs/sysfs collectors, discovery, slow FS & hwmon probes
    tracker/                 # CounterTracker, CPUTracker, Normalizer (profiles, wraps, boot resets)
    scheduler/               # Bounded concurrency loops, in-flight guards, on-demand LiveHub
  history/                   # Idempotent RawWriter, RollupEngine, RetentionEngine, QueryService
migrations/                  # Embedded SQL migrations (STRICT + WITHOUT ROWID)
docs/adr/                    # Architecture Decision Records (ADR-001..003)
tests/integration/           # End-to-end pipeline tests & 24h footprint benchmarks
```

## Architecture Decisions

- [ADR-001: Process and SQLite Architecture](docs/adr/ADR-001-process-and-sqlite-architecture.md)
- [ADR-002: Storage Schema, Metrics Catalog, and Coverage Semantics](docs/adr/ADR-002-storage-schema-and-metrics-catalog.md)
- [ADR-003: Resource Identity, Discovery Reconciliation, and Network Aggregation](docs/adr/ADR-003-resource-identity-and-network-aggregation.md)

## Quick Start

Run static checks and the full race-enabled test suite:

```bash
go vet ./...
go test -race -v ./internal/... ./tests/integration/...
```

Run the 24h storage & query benchmark:

```bash
go test -bench=. -benchmem ./tests/integration/...
```

Build and run a local unprivileged diagnostic check (discovers host hardware, runs one baseline/rate cycle, writes to SQLite, and prints JSON host/resource/health summary):

```bash
go build -o ./bin/vektord ./cmd/vektord
./bin/vektord --data-dir ./data --check
```
