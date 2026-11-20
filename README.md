# Vektor

Lightweight, native-first Linux host monitoring platform. Vektor v0.2 exposes authenticated REST and realtime SSE on Ubuntu Server 26.04.1 LTS.

The unprivileged Go daemon (`vektord`) collects Linux telemetry directly from `/proc`, `/sys`, and `statfs`. It derives monotonic-clock rates and persists multi-resolution series (`30s` raw, `5m` medium, `1h` long) into SQLite WAL. The API reads that history and connects authenticated live clients to on-demand collection.

## Quick start

```bash
go build -o ./bin/vektord ./cmd/vektord
./bin/vektord bootstrap-admin --data-dir ./data
./bin/vektord --data-dir ./data
```

Run both commands as the owner of the data directory. Bootstrap creates the `admin` account with the password entered at the terminal. The API listens on `http://127.0.0.1:8080` by default. [API and operations guide](docs/API_V0.2.md) · [OpenAPI](docs/openapi-v1.yaml) · [Implementation and acceptance evidence](IMPLEMENTATION_SUMMARY_V0.2.md) · [Ubuntu validation](MANUAL_TESTS_V0.2.md)

## Package Layout

```text
cmd/vektord/                 # Daemon, administrator commands and --check mode
internal/
  auth/                      # Local administrator and opaque SQLite sessions
  config/                    # Defaults, bounds validation, excluded FS policy
  database/                  # Pure-Go SQLite WAL writer/reader pools, migrations, VACUUM INTO backup
  host/                      # Host inventory & persistent resource reconciliation registry
  httpapi/                   # REST, login, CSRF and SSE
  observability/             # Structured JSON slog logger & internal HealthTracker
  metrics/
    types.go                 # RawSample (gauge/counter), CPUSample, Clock, MetricPoint
    catalog.go               # Central versioned metrics catalog & UTC bucket alignment
    collector/               # Linux procfs/sysfs collectors, discovery, slow FS & hwmon probes
    tracker/                 # CounterTracker, CPUTracker, Normalizer (profiles, wraps, boot resets)
    scheduler/               # Bounded concurrency loops, in-flight guards, on-demand LiveHub
  history/                   # Idempotent RawWriter, RollupEngine, RetentionEngine, QueryService
migrations/                  # Embedded SQL migrations (STRICT + WITHOUT ROWID)
docs/adr/                    # Architecture Decision Records (ADR-001..005)
tests/integration/           # Pipeline and HTTP integration tests & 24h benchmarks
```

## Architecture Decisions

- [ADR-001: Process and SQLite Architecture](docs/adr/ADR-001-process-and-sqlite-architecture.md)
- [ADR-002: Storage Schema, Metrics Catalog, and Coverage Semantics](docs/adr/ADR-002-storage-schema-and-metrics-catalog.md)
- [ADR-003: Resource Identity, Discovery Reconciliation, and Network Aggregation](docs/adr/ADR-003-resource-identity-and-network-aggregation.md)
- [ADR-004: HTTP API and realtime streaming](docs/adr/ADR-004-http-api-and-realtime.md)
- [ADR-005: Local administrator and SQLite sessions](docs/adr/ADR-005-local-administrator-and-sessions.md)

## Verification

Run static checks and the full race-enabled test suite:

```bash
go vet ./...
go test -count=1 ./...
go test -race -count=1 ./...
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
