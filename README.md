<p align="center">
  <img src="assets/vektor-banner.png" alt="Vektor" width="100%" />
</p>

# Vektor

Lightweight, native-first Linux host monitoring, operations, and **Incus** virtualization platform. Vektor serves a modern embedded dashboard, authenticated REST API, and live SSE stream from a single static Go binary (`vektord`), paired with a minimal socket-activated privileged helper (`vektor-ctl`).

## Highlights

- **Host Monitoring (Phase 1):** Direct Linux telemetry from `/proc`, `/sys`, and `statfs` with monotonic-clock rate derivation, multi-resolution SQLite WAL history (`30s` raw, `5m` medium, `1h` long), sensor temperatures, threshold alerts with webhooks, UTC/Local timezone toggle, and GitHub Releases update checker.
- **Host Management (Phase 2):** Live process explorer (`/proc`), `systemd` service status and `journald` log viewer, controlled actions (`start`/`stop`/`restart`/`reload`/`enable`/`disable`, process signals, reboot/poweroff) through `vektor-ctl.socket`, and persistent SQLite audit trail (`audit_log`).
- **Incus VMs & System Containers (Phase 3):** Pure Go stdlib Unix-socket integration with Incus REST API 1.0 (`/var/lib/incus/unix.socket`) via `vektor-ctl.socket` — create and manage LXC system containers and KVM/QEMU virtual machines, snapshots (create/restore/delete), storage pools, and networks without adding `vektord` to `incus-admin`.
- **Privilege Separation ([ADR-006](docs/adr/ADR-006-privilege-separation-and-host-management.md)):** `vektord` runs permanently unprivileged (`vektor` user, strict `systemd` hardening); privileged operations go through an allowlisted Unix socket authenticated via kernel `SO_PEERCRED`.

## Quick start (from source)

Build the dashboard with Bun and embed it in the Go binary:

```bash
bun install --cwd web --frozen-lockfile
bun run --cwd web build
CGO_ENABLED=0 go build -tags webui -o ./bin/vektord ./cmd/vektord
CGO_ENABLED=0 go build -o ./bin/vektor-ctl ./cmd/vektor-ctl
./bin/vektord bootstrap-admin --data-dir ./data
./bin/vektord --data-dir ./data
```

Open `http://127.0.0.1:8080` to sign in.

## Native Linux installation (`.deb` & `systemd`)

Download the latest `.deb` from [GitHub Releases](https://github.com/patricks-js/vektor/releases) (or build locally with `./packaging/build-deb.sh <version>`):

```bash
sudo dpkg -i vektor_<version>_amd64.deb
sudo -u vektor /usr/bin/vektord bootstrap-admin --data-dir /var/lib/vektor
sudo systemctl enable --now vektord
```

This installs both `/usr/bin/vektord` and `/usr/bin/vektor-ctl`, configures the `vektor` system user (`systemd-journal` group), enables `vektor-ctl.socket` (`/run/vektor/ctl.sock`), and stores state in `/var/lib/vektor` and environment overrides in `/etc/vektor/vektord.env`.

Or install manually from compiled binaries:

```bash
sudo ./packaging/install.sh ./bin/vektord ./bin/vektor-ctl
```

## Frontend development & Demo mode

Preview the full dashboard (including Host Management and Incus VMs/Containers) without running a daemon:

```bash
bun run --cwd web dev:demo
```

Or develop against a local `vektord` on `http://127.0.0.1:8080` (override with `VEKTOR_DEV_API`):

```bash
bun run --cwd web dev
```

## Documentation

- [Changelog](CHANGELOG.md) — release history (`v0.1.0` → `v0.7.1`)
- [Development, Versioning & Distribution Guide](docs/DEVELOPMENT.md) — workflow, SemVer, CI/CD, and release checklist
- [API and operations guide](docs/API_V0.2.md) · [OpenAPI](docs/openapi-v1.yaml) · [Design system catalog](docs/WEB_DESIGN_SYSTEM.md)
- **Architecture Decision Records:**
  - [ADR-001: Process and SQLite Architecture](docs/adr/ADR-001-process-and-sqlite-architecture.md)
  - [ADR-002: Storage Schema, Metrics Catalog, and Coverage Semantics](docs/adr/ADR-002-storage-schema-and-metrics-catalog.md)
  - [ADR-003: Resource Identity, Discovery Reconciliation, and Network Aggregation](docs/adr/ADR-003-resource-identity-and-network-aggregation.md)
  - [ADR-004: HTTP API and realtime streaming](docs/adr/ADR-004-http-api-and-realtime.md)
  - [ADR-005: Local administrator and SQLite sessions](docs/adr/ADR-005-local-administrator-and-sessions.md)
  - [ADR-006: Privilege Separation, Host Management, and Incus](docs/adr/ADR-006-privilege-separation-and-host-management.md)

## Package Layout

```text
cmd/
  vektord/                   # Unprivileged monitoring & web daemon
  vektor-ctl/                # Socket-activated privileged control helper (SO_PEERCRED)
internal/
  alerts/                    # Threshold rules, SQLite events & webhook notifier
  audit/                     # Persistent SQLite audit trail for privileged actions
  auth/                      # Local administrator and opaque SQLite sessions
  config/                    # Defaults, bounds validation, excluded FS policy
  ctl/                       # Typed allowlisted Unix-socket IPC protocol & executor
  database/                  # Pure-Go SQLite WAL writer/reader pools & migrations
  history/                   # Idempotent RawWriter, RollupEngine, RetentionEngine, QueryService
  host/                      # Host inventory & persistent resource reconciliation registry
  hostmgmt/                  # Read-only processes (/proc), systemd services & journald logs
  httpapi/                   # Authenticated REST, CSRF, SSE, alerts, hostmgmt & Incus routes
  incus/                     # Zero-dependency Incus 1.0 REST-over-Unix-socket client
  metrics/                   # Catalog, procfs/sysfs collectors, rate trackers & scheduler
  observability/             # Structured JSON slog logger & internal HealthTracker
migrations/                  # Embedded SQL migrations (001..005)
packaging/                   # systemd units (vektord, vektor-ctl.socket/.service), install.sh & build-deb.sh
docs/                        # ADRs, API docs, design system & DEVELOPMENT.md
web/                         # React + StyleX UI, Vitest/Playwright tests & Go webui embed handler
tests/integration/           # End-to-end pipeline, HTTP, alerts, hostmgmt & Incus tests
```

## Verification

```bash
go vet ./...
go test -race -count=1 ./...
bun run --cwd web typecheck && bun run --cwd web lint && bun run --cwd web test && bun run --cwd web build
go test -tags webui ./web/...
```
