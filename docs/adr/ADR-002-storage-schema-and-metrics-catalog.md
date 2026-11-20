# ADR-002: Storage Schema, Metrics Catalog, and Coverage Semantics

**Status:** Accepted  
**Date:** 2026-11-20  
**Scope:** Vektor v0.1 (`internal/metrics`, `internal/history`, `migrations/`)

## Context
Section 12 of `TECHNICAL_DESIGN_V0.1.md` left open:
- Whether the EAV `metric_buckets` schema has an acceptable storage and query footprint.
- Which tiers persist per-core CPU and sensor metrics.
- How coverage should be defined and communicated.

## Decisions
1. **EAV `metric_buckets` (`STRICT, WITHOUT ROWID`)**: Adopted for v0.1. Using a single clustered index `(resource_id, metric_key, resolution_s, bucket_start_ms)` keeps schema evolution simple, avoids sparse columns across heterogeneous resource kinds, and is validated directly by the 24h benchmark in `tests/integration/benchmark_test.go`.
2. **Central versioned metrics catalog**: Every persisted `metric_key` must be registered in `internal/metrics` (`CatalogVersion = "v1"`) with canonical unit, applicable resource kinds, modality (`gauge` or `rate`), and allowed aggregators. Collectors never invent arbitrary dynamic keys.
3. **Per-core CPU and sensor persistence**: Both `cpu.core.busy_pct` (on `cpu_core` resources) and `sensor.temperature_celsius` (on `sensor` resources) are persisted in raw (`30s`) and rolled up through `5m` and `1h` tiers. Both can be disabled via configuration (`IncludePerCoreCPU`, `IncludeSensors`) to tune cardinality on large hosts.
4. **Coverage semantics**: `sample_count` records observed valid samples in a bucket. Query responses compute nominal coverage as `min(1.0, sample_count / expected_samples)` where `expected_samples` is derived from the nominal persistence interval (`30s`, so 1 for `30s`, 10 for `5m`, 120 for `1h`). Averages are unweighted sample averages (`value_sum / sample_count`), NOT duration-weighted averages. Missing buckets remain absent (never zero-filled or interpolated).

## Consequences
- Adding a new metric requires only registering it in the catalog and emitting it from a collector—no SQL migration needed.
- Rollup math stays exact and idempotent across `30s -> 300s -> 3600s`.
- Callers know precisely when a bucket represents partial coverage rather than full window sampling.
