# ADR-003: Resource Identity, Discovery Reconciliation, and Network Aggregation

**Status:** Accepted  
**Date:** 2026-11-20  
**Scope:** Vektor v0.1 (`internal/host`, `internal/metrics/collector`, `internal/metrics/tracker`)

## Context
Linux device names (`sda`, `eth0`, `hwmon3`) are unstable across reboots and hotplugs, while bridges, bonds, and `veth` pairs cause double counting if summed naively.

## Decisions
1. **Persistent internal `resource_id` reconciled by `(kind, external_key)`**:
   - `host`: fixed `id = "host:local"`, `external_key = "local"`.
   - `cpu_core`: deterministic `id = "cpu:<n>"`, `external_key = "cpu<n>"`.
   - `disk`: priority `dm:<uuid>` (LVM/device-mapper) > `wwid:<wwid>` > `serial:<model>:<serial>` > `block:<name>`. Partitions and ephemeral devices (`loop*`, `ram*`, `zram*`, `sr*`, `fd*`) are excluded from disk I/O tracking.
   - `filesystem`: priority `uuid:<uuid>:<mountpoint>` > `dev:<source>:<mountpoint>`, parsing `/proc/self/mountinfo` (with octal unescaping), excluding pseudo/remote/FUSE types by default, and deduplicating bind mounts by `(major:minor, root)` keeping the shortest mountpoint.
   - `nic`: priority `pci:<pci_addr>` > `mac:<mac>` (physical non-zero MAC) > `virtual:<name>`.
   - `sensor`: `hwmon:<chip>:<dev_id>:<channel>`.
2. **Retention of inactive resources**: When a resource disappears, discovery marks `active = 0` in `resources` without deleting rows, preserving historical buckets. Reappearance matching `(kind, external_key)` reactivates the same `resource_id`.
3. **Virtual NICs & host network aggregate rule**:
   - Virtual NICs (`lo`, bridges, bonds, `veth*`, tunnels) are inventoried and remain inspectable individually.
   - Host-level aggregate rates (`net.rx_bytes_per_sec` and `net.tx_bytes_per_sec` on `host:local`) sum valid per-NIC rates **only** for NICs with `include_in_host_aggregate = true`:
     - Included: non-loopback physical NICs not enslaved to a bond/bridge master (`!is_virtual && !has_master`), plus top-level bond masters (`is_bond && !has_master`).
     - Excluded: loopback (`lo`), bridges, `veth*`/virtual devices, and bond/bridge slave interfaces.

## Consequences
- Device renames (`eth0` -> `enp1s0`) or LVM minor swaps do not split historical series when stable hardware/DM keys are available.
- Host network totals avoid counting bond slaves + bond master or container veth + bridge + physical NIC multiple times.
