import { ApiError } from '../api/client'
import type { AlertChannel, AlertEvent, AlertRule, Api, AuditEntry, Health, Host, IncusBackup, IncusInstance, IncusResources, IncusSnapshot, LogEntry, ProcessInfo, Resource, ResourceKind, Series, SeriesPoint, ServiceInfo, Session, Step } from '../api/client'
import type { LiveStore, TransportState } from '../realtime/store'
import type { LivePoint } from '../realtime/stream'
import { metric } from '../../features/metrics/catalog'

export const isDemoMode = import.meta.env.VITE_DEMO_MODE === 'true'

const GiB = 1024 ** 3
const MiB = 1024 ** 2
const KiB = 1024

let demoLoggedIn = true
let bootStartedAt = 0
let cachedResources: Resource[] | undefined
let demoRules: AlertRule[] | undefined
let demoChannels: AlertChannel[] | undefined
let demoEvents: AlertEvent[] | undefined
let demoAudit: AuditEntry[] | undefined
let demoIncusInstances: IncusInstance[] | undefined
let demoIncusSnapshots: Record<string, IncusSnapshot[]> | undefined
let demoIncusBackups: Record<string, IncusBackup[]> | undefined
let nextAlertId = 10

function getDemoIncus() {
  if (!demoIncusInstances) {
    const now = Date.now()
    demoIncusInstances = [
      {
        name: 'caddy-edge', type: 'container', status: 'Running', description: 'Reverse proxy & TLS termination',
        architecture: 'x86_64', ephemeral: false, stateful: false, profiles: ['default'],
        created_at: new Date(now - 28 * 86400_000).toISOString(), last_used_at: new Date(now - 3600_000).toISOString(),
        cpu_limit: '2', memory_limit: '1GiB', autostart: true, cpu_usage_ns: 94_000_000_000,
        memory_usage_bytes: 184 * MiB, memory_peak_bytes: 290 * MiB, disk_usage_bytes: 1.4 * GiB,
        processes: 14, pid: 3112, ipv4: ['10.24.0.10'], ipv6: ['fd42:1::10'], snapshots_count: 2,
      },
      {
        name: 'postgres-prod', type: 'container', status: 'Running', description: 'Primary PostgreSQL 16 instance',
        architecture: 'x86_64', ephemeral: false, stateful: false, profiles: ['default'],
        created_at: new Date(now - 45 * 86400_000).toISOString(), last_used_at: new Date(now - 1800_000).toISOString(),
        cpu_limit: '4', memory_limit: '8GiB', autostart: true, cpu_usage_ns: 412_000_000_000,
        memory_usage_bytes: 3.6 * GiB, memory_peak_bytes: 5.1 * GiB, disk_usage_bytes: 18.2 * GiB,
        processes: 29, pid: 3490, ipv4: ['10.24.0.20'], ipv6: [], snapshots_count: 1,
      },
      {
        name: 'ubuntu-lab', type: 'virtual-machine', status: 'Running', description: 'KVM dev & CI runner VM',
        architecture: 'x86_64', ephemeral: false, stateful: false, profiles: ['default'],
        created_at: new Date(now - 14 * 86400_000).toISOString(), last_used_at: new Date(now - 600_000).toISOString(),
        cpu_limit: '4', memory_limit: '4GiB', autostart: false, cpu_usage_ns: 280_000_000_000,
        memory_usage_bytes: 2.1 * GiB, memory_peak_bytes: 3.4 * GiB, disk_usage_bytes: 24.5 * GiB,
        processes: 112, pid: 4120, ipv4: ['10.24.0.35'], ipv6: [], snapshots_count: 1,
      },
      {
        name: 'debian-staging', type: 'virtual-machine', status: 'Stopped', description: 'Pre-production validation VM',
        architecture: 'x86_64', ephemeral: false, stateful: false, profiles: ['default'],
        created_at: new Date(now - 9 * 86400_000).toISOString(), last_used_at: new Date(now - 2 * 86400_000).toISOString(),
        cpu_limit: '2', memory_limit: '2GiB', autostart: false, cpu_usage_ns: 0,
        memory_usage_bytes: 0, memory_peak_bytes: 0, disk_usage_bytes: 12.1 * GiB,
        processes: 0, pid: 0, ipv4: [], ipv6: [], snapshots_count: 0,
      },
      {
        name: 'redis-cache', type: 'container', status: 'Frozen', description: 'In-memory session & queue cache',
        architecture: 'x86_64', ephemeral: false, stateful: false, profiles: ['default'],
        created_at: new Date(now - 6 * 86400_000).toISOString(), last_used_at: new Date(now - 12 * 3600_000).toISOString(),
        cpu_limit: '1', memory_limit: '512MiB', autostart: true, cpu_usage_ns: 12_000_000_000,
        memory_usage_bytes: 96 * MiB, memory_peak_bytes: 140 * MiB, disk_usage_bytes: 310 * MiB,
        processes: 6, pid: 5012, ipv4: ['10.24.0.41'], ipv6: [], snapshots_count: 0,
      },
    ]
  }
  if (!demoIncusSnapshots) {
    const now = Date.now()
    demoIncusSnapshots = {
      'caddy-edge': [
        { name: 'pre-v2.9', created_at: new Date(now - 5 * 86400_000).toISOString(), stateful: false, size_bytes: 42 * MiB },
        { name: 'clean-base', created_at: new Date(now - 20 * 86400_000).toISOString(), stateful: false, size_bytes: 18 * MiB },
      ],
      'postgres-prod': [
        { name: 'nightly-01', created_at: new Date(now - 86400_000).toISOString(), stateful: false, size_bytes: 640 * MiB },
      ],
      'ubuntu-lab': [
        { name: 'fresh-install', created_at: new Date(now - 10 * 86400_000).toISOString(), stateful: false, size_bytes: 1.8 * GiB },
      ],
    }
  }
  if (!demoIncusBackups) {
    const now = Date.now()
    demoIncusBackups = {
      'postgres-prod': [
        { name: 'weekly-2026-11-23', created_at: new Date(now - 86400_000).toISOString(), instance_only: false, optimized_storage: true },
      ],
    }
  }
  return { instances: demoIncusInstances, snapshots: demoIncusSnapshots, backups: demoIncusBackups }
}

function getDemoAudit() {
  if (!demoAudit) {
    const now = Date.now()
    demoAudit = [
      { id: 1, actor: 'admin', action: 'service.restart', target: 'caddy.service', status: 'ok', detail: 'systemctl restart caddy.service completed', created_at_ms: now - 3_600_000 },
    ]
  }
  return demoAudit
}

function getDemoAlertState() {
  if (!demoRules) {
    const now = Date.now()
    demoRules = [
      { id: 1, name: 'CPU acima de 85%', resource_id: 'host:local', metric_key: metric.cpu, operator: 'gt', threshold: 85, duration_seconds: 120, enabled: true, created_at_ms: now - 86_400_000, updated_at_ms: now - 86_400_000 },
      { id: 2, name: 'RAM acima de 90%', resource_id: 'host:local', metric_key: metric.ram, operator: 'gt', threshold: 90, duration_seconds: 60, enabled: true, created_at_ms: now - 86_400_000, updated_at_ms: now - 86_400_000 },
      { id: 3, name: 'Temperatura CPU > 78°C', resource_id: 'sensor:k10temp-tctl', metric_key: metric.temperature, operator: 'gt', threshold: 78, duration_seconds: 60, enabled: false, created_at_ms: now - 43_200_000, updated_at_ms: now - 43_200_000 },
    ]
    demoChannels = [
      { id: 1, name: 'Webhook Homelab', kind: 'webhook', url: 'https://hooks.example.com/vektor', enabled: true, created_at_ms: now - 86_400_000 },
    ]
    demoEvents = [
      { id: 2, rule_id: 1, rule_name: 'CPU acima de 85%', resource_id: 'host:local', metric_key: metric.cpu, status: 'resolved', value: 41.2, threshold: 85, triggered_at_ms: now - 7_200_000, resolved_at_ms: now - 6_900_000 },
      { id: 1, rule_id: 1, rule_name: 'CPU acima de 85%', resource_id: 'host:local', metric_key: metric.cpu, status: 'firing', value: 88.4, threshold: 85, triggered_at_ms: now - 7_200_000 },
    ]
  }
  return { rules: demoRules, channels: demoChannels!, events: demoEvents! }
}

function getBootStartedAt() {
  bootStartedAt ||= Date.now() - (42 * 86_400 + 7 * 3600 + 18 * 60) * 1000
  return bootStartedAt
}

function makeSession(): Session {
  return {
    username: 'admin',
    expires_at: new Date(Date.now() + 12 * 3600 * 1000).toISOString(),
    csrf_token: 'demo-csrf-token',
  }
}

function resource(
  firstSeen: number,
  id: string,
  kind: ResourceKind,
  externalKey: string,
  displayName: string,
  meta: Record<string, unknown>,
  active = true,
): Resource {
  return {
    id,
    kind,
    external_key: externalKey,
    display_name: displayName,
    metadata_json: JSON.stringify(meta),
    active,
    first_seen_ms: firstSeen,
    last_seen_ms: active ? Date.now() - 2_000 : Date.now() - 4 * 86_400 * 1000,
  }
}

function getDemoResources(): Resource[] {
  if (cachedResources) return cachedResources
  const firstSeen = Date.now() - 30 * 86_400 * 1000
  cachedResources = [
    resource(firstSeen, 'host:local', 'host', 'host:local', 'prod-fra-01', {}),

    // CPU cores (8 active + 1 inactive to demonstrate pagination, search & status bars)
    ...Array.from({ length: 8 }, (_, index) =>
      resource(firstSeen, `cpu:${index}`, 'cpu_core', `cpu${index}`, `cpu${index}`, { core_index: index }, true),
    ),
    resource(firstSeen, 'cpu:8', 'cpu_core', 'cpu8', 'cpu8', { core_index: 8 }, false),

    // Filesystems (green, amber, red usage tiers + 1 inactive)
    resource(firstSeen, 'fs:root', 'filesystem', '/', '/', { mountpoint: '/', fs_type: 'ext4', source: '/dev/nvme0n1p2' }),
    resource(firstSeen, 'fs:docker', 'filesystem', '/var/lib/docker', '/var/lib/docker', { mountpoint: '/var/lib/docker', fs_type: 'xfs', source: '/dev/nvme0n1p3' }),
    resource(firstSeen, 'fs:backup', 'filesystem', '/mnt/backup', '/mnt/backup', { mountpoint: '/mnt/backup', fs_type: 'ext4', source: '/dev/sda1' }),
    resource(firstSeen, 'fs:archive', 'filesystem', '/mnt/archive', '/mnt/archive', { mountpoint: '/mnt/archive', fs_type: 'nfs4', source: '10.0.4.12:/export/archive' }, false),

    // Block devices
    resource(firstSeen, 'disk:nvme0n1', 'disk', 'nvme0n1', 'nvme0n1', { kernel_name: 'nvme0n1', model: 'Samsung PM9A3 1.92TB NVMe' }),
    resource(firstSeen, 'disk:sda', 'disk', 'sda', 'sda', { kernel_name: 'sda', model: 'WDC WD40EFZX-68AWUN0' }),
    resource(firstSeen, 'disk:sdb', 'disk', 'sdb', 'sdb', { kernel_name: 'sdb', model: 'Crucial MX500' }, false),

    // Network interfaces
    resource(firstSeen, 'nic:ens18', 'nic', 'ens18', 'ens18', { operstate: 'up', mac: 'bc:24:11:8a:4f:10', pci_addr: '0000:00:12.0', include_in_host_aggregate: true }),
    resource(firstSeen, 'nic:wg0', 'nic', 'wg0', 'wg0', { operstate: 'up', include_in_host_aggregate: true }),
    resource(firstSeen, 'nic:enp2s0', 'nic', 'enp2s0', 'enp2s0', { operstate: 'down', mac: 'bc:24:11:8a:4f:11', pci_addr: '0000:00:13.0', include_in_host_aggregate: false }),

    // Temperature sensors
    resource(firstSeen, 'sensor:k10temp-tctl', 'sensor', 'k10temp:temp1', 'CPU Package (Tctl)', { chip: 'k10temp-pci-00c3', channel: 'temp1', dev_id: 'hwmon1' }),
    resource(firstSeen, 'sensor:nvme-composite', 'sensor', 'nvme:temp1', 'NVMe Composite', { chip: 'nvme-pci-0100', channel: 'temp1', dev_id: 'hwmon2' }),
    resource(firstSeen, 'sensor:acpi-tz0', 'sensor', 'acpitz:temp1', 'Motherboard ACPI', { chip: 'acpitz-acpi-0', channel: 'temp1', dev_id: 'hwmon0' }),
    resource(firstSeen, 'sensor:nic-temp', 'sensor', 'igc:temp1', 'NIC Controller', { chip: 'igc-pci-0200', channel: 'temp1', dev_id: 'hwmon3' }),
  ]
  return cachedResources
}

function seedOf(text: string) {
  let hash = 2166136261
  for (let i = 0; i < text.length; i++) {
    hash ^= text.charCodeAt(i)
    hash = Math.imul(hash, 16777619)
  }
  return (hash >>> 0) / 4294967295
}

// Smooth deterministic organic signal based on timestamp (seconds) and resource seed.
function wave(tsMs: number, seed: number) {
  const t = tsMs / 1000
  const phase = seed * Math.PI * 2
  const slow = Math.sin(t / 900 + phase) * 0.45
  const mid = Math.sin(t / 180 + phase * 1.7) * 0.3
  const fast = Math.sin(t / 37 + phase * 2.9) * 0.15
  const burst = Math.max(0, Math.sin(t / 410 + phase * 3.1) - 0.68) * 2.2
  return slow + mid + fast + burst
}

function metricUnit(key: string): string {
  if (key.endsWith('_pct')) return '%'
  if (key.endsWith('_bytes_per_sec')) return 'bytes/s'
  if (key.endsWith('_bytes')) return 'bytes'
  if (key.startsWith('load.')) return 'load'
  if (key.endsWith('_celsius')) return '°C'
  if (key.endsWith('_per_sec') && key.includes('errors')) return 'errors/s'
  if (key.endsWith('_per_sec') && key.includes('drops')) return 'drops/s'
  if (key.endsWith('_per_sec')) return 'ops/s'
  return ''
}

const fsProfile: Record<string, { total: number; pct: number }> = {
  'fs:root': { total: 240 * GiB, pct: 46.4 },
  'fs:docker': { total: 800 * GiB, pct: 71.8 },
  'fs:backup': { total: 3600 * GiB, pct: 88.2 },
}

function sampleValue(id: string, key: string, tsMs: number): number {
  const s = seedOf(`${id}:${key}`)
  const w = wave(tsMs, s)
  const clamp = (value: number, min: number, max: number) => Math.max(min, Math.min(max, value))

  switch (key) {
    case metric.cpu:
      return clamp(24 + w * 18, 3, 94)
    case metric.core: {
      const base = 16 + s * 42
      return clamp(base + w * 22, 1, 98)
    }
    case metric.load1:
      return clamp(1.15 + w * 0.85, 0.15, 7.5)
    case metric.load5:
      return clamp(1.05 + wave(tsMs, s + 0.1) * 0.55, 0.2, 6.0)
    case metric.load15:
      return clamp(0.95 + wave(tsMs, s + 0.2) * 0.35, 0.25, 5.0)
    case metric.ram:
      return clamp(56 + wave(tsMs, 0.42) * 7, 42, 86)
    case metric.ramTotal:
      return 32 * GiB
    case metric.ramUsed:
      return Math.round((sampleValue(id, metric.ram, tsMs) / 100) * 32 * GiB)
    case metric.ramAvailable:
      return 32 * GiB - sampleValue(id, metric.ramUsed, tsMs)
    case metric.cache:
      return Math.round((6.4 + wave(tsMs, 0.77) * 0.6) * GiB)
    case metric.buffers:
      return Math.round((1.15 + wave(tsMs, 0.19) * 0.15) * GiB)
    case metric.swapTotal:
      return 8 * GiB
    case metric.swap:
      return clamp(12 + wave(tsMs, 0.61) * 3.5, 4, 38)
    case metric.swapUsed:
      return Math.round((sampleValue(id, metric.swap, tsMs) / 100) * 8 * GiB)
    case metric.fs: {
      const profile = fsProfile[id] ?? { total: 500 * GiB, pct: 42 }
      return clamp(profile.pct + Math.sin(tsMs / 3_600_000) * 0.8, 1, 99)
    }
    case metric.fsTotal:
      return (fsProfile[id] ?? { total: 500 * GiB }).total
    case metric.fsUsed: {
      const total = sampleValue(id, metric.fsTotal, tsMs)
      return Math.round((sampleValue(id, metric.fs, tsMs) / 100) * total)
    }
    case metric.fsAvailable:
      return sampleValue(id, metric.fsTotal, tsMs) - sampleValue(id, metric.fsUsed, tsMs)
    case metric.diskRead: {
      const scale = id === 'disk:nvme0n1' ? 18 * MiB : 4.5 * MiB
      return Math.max(24 * KiB, scale * (0.28 + Math.max(0, w * 0.9)))
    }
    case metric.diskWrite: {
      const scale = id === 'disk:nvme0n1' ? 11 * MiB : 2.8 * MiB
      return Math.max(12 * KiB, scale * (0.22 + Math.max(0, wave(tsMs, s + 0.33) * 0.85)))
    }
    case metric.diskReads:
      return Math.max(4, Math.round(sampleValue(id, metric.diskRead, tsMs) / (64 * KiB)))
    case metric.diskWrites:
      return Math.max(2, Math.round(sampleValue(id, metric.diskWrite, tsMs) / (64 * KiB)))
    case metric.rx: {
      if (id === 'nic:enp2s0') return 0
      const base = id === 'host:local' ? 6.4 * MiB : id === 'nic:ens18' ? 5.1 * MiB : 1.3 * MiB
      return Math.max(8 * KiB, base * (0.26 + Math.max(0, w * 0.95)))
    }
    case metric.tx: {
      if (id === 'nic:enp2s0') return 0
      const base = id === 'host:local' ? 3.2 * MiB : id === 'nic:ens18' ? 2.4 * MiB : 0.8 * MiB
      return Math.max(4 * KiB, base * (0.22 + Math.max(0, wave(tsMs, s + 0.41) * 0.9)))
    }
    case metric.rxErrors:
    case metric.txErrors:
    case metric.rxDrops:
    case metric.txDrops:
      return 0
    case metric.temperature: {
      const base = id.includes('k10temp') ? 51 : id.includes('nvme') ? 41 : id.includes('acpi') ? 45 : 39
      return clamp(base + w * 6.5, 28, 84)
    }
    default:
      return clamp(20 + w * 10, 0, 100)
  }
}

function chooseResolution(from: number, to: number, step: Step): 30 | 300 | 3600 {
  if (step === '30' || step === '300' || step === '3600') return Number(step) as 30 | 300 | 3600
  const spanSec = Math.max(1, (to - from) / 1000)
  if (spanSec / 30 <= 360) return 30
  if (spanSec / 300 <= 360) return 300
  return 3600
}

function buildSeries(id: string, key: string, from: number, to: number, step: Step): Series {
  const resolution = chooseResolution(from, to, step)
  const stepMs = resolution * 1000
  const start = Math.floor(from / stepMs) * stepMs
  const count = Math.ceil((to - start) / stepMs)
  if (count > 600) throw new ApiError(400, 'point_limit')

  const points: SeriesPoint[] = []
  const expected = resolution === 30 ? 3 : resolution === 300 ? 30 : 360
  for (let ts = start; ts < to; ts += stepMs) {
    const mid = sampleValue(id, key, ts)
    const a = sampleValue(id, key, ts + stepMs * 0.3)
    const b = sampleValue(id, key, ts + stepMs * 0.7)
    const avg = (mid + a + b) / 3
    points.push({
      bucket_start_ms: ts,
      avg,
      min: Math.min(mid, a, b),
      max: Math.max(mid, a, b),
      last: b,
      sample_count: expected,
      expected_samples: expected,
      coverage: 1,
    })
  }
  return {
    resource_id: id,
    metric_key: key,
    unit: metricUnit(key),
    resolution_s: resolution,
    from_ms: from,
    to_ms: to,
    points,
  }
}

function checkAbort(signal?: AbortSignal) {
  signal?.throwIfAborted()
}

export function createDemoApi(onUnauthorized?: () => void): Api {
  function requireSession() {
    if (!demoLoggedIn) {
      onUnauthorized?.()
      throw new ApiError(401, 'unauthenticated')
    }
  }

  return {
    async me(signal?: AbortSignal): Promise<Session> {
      checkAbort(signal)
      if (!demoLoggedIn) throw new ApiError(401, 'unauthenticated')
      return makeSession()
    },
    async login(credentials, signal): Promise<Session> {
      checkAbort(signal)
      if (!credentials.username.trim() || !credentials.password) {
        throw new ApiError(400, 'invalid_parameters')
      }
      if (credentials.password === 'erro') {
        throw new ApiError(401, 'invalid_credentials')
      }
      demoLoggedIn = true
      return makeSession()
    },
    async logout(_session, signal) {
      checkAbort(signal)
      demoLoggedIn = false
      return { ok: true as const }
    },
    async host(signal): Promise<Host> {
      checkAbort(signal)
      requireSession()
      const now = Date.now()
      return {
        resource: { ...getDemoResources()[0], last_seen_ms: now },
        inventory: {
          hostname: 'prod-fra-01',
          os_name: 'Ubuntu',
          os_version: '24.04.2 LTS',
          os_pretty: 'Ubuntu 24.04.2 LTS',
          kernel: '6.8.0-52-generic',
          arch: 'x86_64',
          cpu_model: 'AMD EPYC 7543P 32-Core Processor',
          cpu_cores: 8,
          cpu_threads: 16,
          memory_total_bytes: 32 * GiB,
          boot_id: 'demo-boot-8a92c4e1',
          updated_at_ms: now,
        },
        uptime_seconds: Math.floor((now - getBootStartedAt()) / 1000),
      }
    },
    async health(signal): Promise<Health> {
      checkAbort(signal)
      requireSession()
      const nowIso = new Date().toISOString()
      return {
        status: 'ok',
        db_ok: true,
        db_ping_ok: true,
        db_bytes: 48 * MiB,
        wal_bytes: 4 * MiB,
        shm_bytes: 32 * KiB,
        last_collect_at: nowIso,
        last_write_at: nowIso,
        rollup_lag_s: 0,
        collector_error_count: 0,
        dropped_live_events: 0,
        active_live_subscribers: 1,
        wall_clock_jump_count: 0,
      }
    },
    async resource(id, signal): Promise<Resource> {
      checkAbort(signal)
      requireSession()
      const found = getDemoResources().find(item => item.id === id)
      if (!found) throw new ApiError(404, 'not_found')
      return { ...found, last_seen_ms: found.active ? Date.now() - 1000 : found.last_seen_ms }
    },
    async resources(kind, signal): Promise<Resource[]> {
      checkAbort(signal)
      requireSession()
      const now = Date.now()
      return getDemoResources()
        .filter(item => item.kind === kind)
        .map(item => ({ ...item, last_seen_ms: item.active ? now - 1000 : item.last_seen_ms }))
    },
    async series(id, key, from, to, step, signal): Promise<Series> {
      checkAbort(signal)
      requireSession()
      return buildSeries(id, key, from, to, step)
    },
    async alertRules(signal) {
      checkAbort(signal)
      requireSession()
      return [...getDemoAlertState().rules]
    },
    async createAlertRule(_session, input, signal) {
      checkAbort(signal)
      requireSession()
      const now = Date.now()
      const rule: AlertRule = { ...input, id: nextAlertId++, created_at_ms: now, updated_at_ms: now }
      getDemoAlertState().rules.push(rule)
      return rule
    },
    async updateAlertRule(_session, id, input, signal) {
      checkAbort(signal)
      requireSession()
      const state = getDemoAlertState()
      const idx = state.rules.findIndex(r => r.id === id)
      if (idx === -1) throw new ApiError(404, 'not_found')
      const updated: AlertRule = { ...state.rules[idx], ...input, id, updated_at_ms: Date.now() }
      state.rules[idx] = updated
      return updated
    },
    async deleteAlertRule(_session, id, signal) {
      checkAbort(signal)
      requireSession()
      const state = getDemoAlertState()
      state.rules = state.rules.filter(r => r.id !== id)
      demoRules = state.rules
      return { ok: true as const }
    },
    async alertChannels(signal) {
      checkAbort(signal)
      requireSession()
      return [...getDemoAlertState().channels]
    },
    async createAlertChannel(_session, input, signal) {
      checkAbort(signal)
      requireSession()
      const ch: AlertChannel = {
        id: nextAlertId++,
        name: input.name,
        kind: 'webhook',
        url: input.url,
        enabled: input.enabled,
        created_at_ms: Date.now(),
      }
      getDemoAlertState().channels.push(ch)
      return ch
    },
    async deleteAlertChannel(_session, id, signal) {
      checkAbort(signal)
      requireSession()
      const state = getDemoAlertState()
      state.channels = state.channels.filter(c => c.id !== id)
      demoChannels = state.channels
      return { ok: true as const }
    },
    async testAlertChannel(_session, _url, signal) {
      checkAbort(signal)
      requireSession()
      return { ok: true as const }
    },
    async alertEvents(signal) {
      checkAbort(signal)
      requireSession()
      return [...getDemoAlertState().events]
    },
    async update(signal) {
      checkAbort(signal)
      requireSession()
      return {
        current_version: __APP_VERSION__,
        latest_version: __APP_VERSION__,
        update_available: false,
        checked_at: new Date().toISOString(),
      }
    },
    async processes(sort, limit, signal): Promise<ProcessInfo[]> {
      checkAbort(signal)
      requireSession()
      const t = Date.now()
      const items: ProcessInfo[] = [
        { pid: 1420, ppid: 1, name: 'incusd', command: '/usr/libexec/incus/incusd --group incus-admin', user: 'root', state: 'S', cpu_percent: 4.2 + Math.abs(wave(t, 0.2)) * 3, rss_bytes: 412 * MiB, threads: 28 },
        { pid: 984, ppid: 1, name: 'vektord', command: '/usr/bin/vektord --data-dir /var/lib/vektor', user: 'vektor', state: 'S', cpu_percent: 0.4 + Math.abs(wave(t, 0.4)) * 0.4, rss_bytes: 28 * MiB, threads: 9 },
        { pid: 1810, ppid: 1, name: 'postgres', command: '/usr/lib/postgresql/16/bin/postgres -D /var/lib/postgresql/16/main', user: 'postgres', state: 'S', cpu_percent: 3.1 + Math.abs(wave(t, 0.7)) * 4, rss_bytes: 860 * MiB, threads: 12 },
        { pid: 2104, ppid: 1, name: 'caddy', command: '/usr/bin/caddy run --environ --config /etc/caddy/Caddyfile', user: 'caddy', state: 'S', cpu_percent: 1.3 + Math.abs(wave(t, 0.5)) * 1.5, rss_bytes: 94 * MiB, threads: 14 },
        { pid: 612, ppid: 1, name: 'systemd-journald', command: '/usr/lib/systemd/systemd-journald', user: 'root', state: 'S', cpu_percent: 0.2, rss_bytes: 48 * MiB, threads: 1 },
        { pid: 842, ppid: 1, name: 'sshd', command: 'sshd: /usr/sbin/sshd -D', user: 'root', state: 'S', cpu_percent: 0.1, rss_bytes: 14 * MiB, threads: 1 },
      ]
      items.sort((a, b) => sort === 'memory' ? b.rss_bytes - a.rss_bytes : b.cpu_percent - a.cpu_percent)
      return items.slice(0, limit)
    },
    async services(state, signal): Promise<ServiceInfo[]> {
      checkAbort(signal)
      requireSession()
      const all: ServiceInfo[] = [
        { name: 'backup-sync.service', load_state: 'loaded', active_state: 'failed', sub_state: 'failed', unit_file_state: 'enabled', description: 'Nightly Offsite Backup Sync' },
        { name: 'caddy.service', load_state: 'loaded', active_state: 'active', sub_state: 'running', unit_file_state: 'enabled', description: 'Caddy Reverse Proxy' },
        { name: 'incus.service', load_state: 'loaded', active_state: 'active', sub_state: 'running', unit_file_state: 'enabled', description: 'Incus Container and VM Hypervisor' },
        { name: 'postgresql.service', load_state: 'loaded', active_state: 'active', sub_state: 'exited', unit_file_state: 'enabled', description: 'PostgreSQL RDBMS' },
        { name: 'ssh.service', load_state: 'loaded', active_state: 'active', sub_state: 'running', unit_file_state: 'enabled', description: 'OpenBSD Secure Shell server' },
        { name: 'vektord.service', load_state: 'loaded', active_state: 'active', sub_state: 'running', unit_file_state: 'enabled', description: 'Vektor Linux Host Monitoring Daemon' },
        { name: 'apt-daily.service', load_state: 'loaded', active_state: 'inactive', sub_state: 'dead', unit_file_state: 'static', description: 'Daily apt download activities' },
      ]
      if (state === 'active') return all.filter(s => s.active_state === 'active')
      if (state === 'failed') return all.filter(s => s.active_state === 'failed')
      if (state === 'inactive') return all.filter(s => s.active_state !== 'active' && s.active_state !== 'failed')
      return all
    },
    async serviceLogs(unit, _lines, priority, signal): Promise<LogEntry[]> {
      checkAbort(signal)
      requireSession()
      const now = Date.now()
      const entries: LogEntry[] = [
        { timestamp_ms: now - 180_000, priority: 'info', unit, message: `Started ${unit}.` },
        { timestamp_ms: now - 120_000, priority: 'info', unit, message: 'Health check completed in 4ms (status=ok).' },
        { timestamp_ms: now - 60_000, priority: 'warning', unit, message: 'Transient upstream latency spike observed (182ms).' },
        { timestamp_ms: now - 15_000, priority: unit.includes('backup') ? 'err' : 'info', unit, message: unit.includes('backup') ? 'Remote target unreachable: connection timed out after 10s.' : 'Periodic checkpoint completed cleanly.' },
      ]
      if (priority === 'err') return entries.filter(e => e.priority === 'err')
      if (priority === 'warning') return entries.filter(e => e.priority === 'err' || e.priority === 'warning')
      return entries
    },
    async serviceAction(_session, unit, action, signal) {
      checkAbort(signal)
      requireSession()
      getDemoAudit().unshift({
        id: nextAlertId++, actor: 'admin', action: `service.${action}`, target: unit, status: 'ok',
        detail: `systemctl ${action} ${unit} completed (demo)`, created_at_ms: Date.now(),
      })
      return { ok: true, message: `systemctl ${action} ${unit} completed` }
    },
    async processSignal(_session, pid, sig, signal) {
      checkAbort(signal)
      requireSession()
      getDemoAudit().unshift({
        id: nextAlertId++, actor: 'admin', action: 'process.signal', target: `pid:${pid}`, status: 'ok',
        detail: `signal ${sig} sent to pid ${pid} (demo)`, created_at_ms: Date.now(),
      })
      return { ok: true, message: `signal ${sig} sent to pid ${pid}` }
    },
    async hostPower(_session, action, signal) {
      checkAbort(signal)
      requireSession()
      getDemoAudit().unshift({
        id: nextAlertId++, actor: 'admin', action: `host.${action}`, target: 'host:local', status: 'ok',
        detail: `host ${action} initiated (demo)`, created_at_ms: Date.now(),
      })
      return { ok: true, message: `host ${action} initiated` }
    },
    async auditLog(signal, limit = 50) {
      checkAbort(signal)
      requireSession()
      return getDemoAudit().slice(0, limit)
    },
    async incusStatus(signal) {
      checkAbort(signal)
      requireSession()
      const { instances } = getDemoIncus()
      return {
        available: true,
        server_version: '6.8',
        kernel_version: '6.8.0-52-generic',
        drivers: ['lxc', 'qemu'],
        storage_drivers: ['zfs', 'dir'],
        instance_count: instances.length,
        running_count: instances.filter(i => i.status === 'Running').length,
        vm_count: instances.filter(i => i.type === 'virtual-machine').length,
        container_count: instances.filter(i => i.type === 'container').length,
      }
    },
    async incusInstances(signal) {
      checkAbort(signal)
      requireSession()
      const { instances, snapshots } = getDemoIncus()
      return instances.map(i => ({ ...i, snapshots_count: (snapshots[i.name] ?? []).length }))
    },
    async incusInstance(name, signal) {
      checkAbort(signal)
      requireSession()
      const { instances, snapshots } = getDemoIncus()
      const found = instances.find(i => i.name === name)
      if (!found) throw new ApiError(404, 'not_found')
      return { ...found, snapshots_count: (snapshots[found.name] ?? []).length }
    },
    async incusCreateInstance(_session, input, signal) {
      checkAbort(signal)
      requireSession()
      const { instances } = getDemoIncus()
      if (instances.some(i => i.name === input.name)) throw new ApiError(422, 'action_failed')
      const nowIso = new Date().toISOString()
      instances.push({
        name: input.name,
        type: input.type,
        status: input.start ? 'Running' : 'Stopped',
        description: input.description ?? '',
        architecture: 'x86_64',
        ephemeral: false,
        stateful: false,
        profiles: input.profiles?.length ? input.profiles : ['default'],
        created_at: nowIso,
        last_used_at: input.start ? nowIso : undefined,
        cpu_limit: input.cpu_limit,
        memory_limit: input.memory_limit,
        autostart: false,
        cpu_usage_ns: 0,
        memory_usage_bytes: input.start ? 128 * MiB : 0,
        memory_peak_bytes: input.start ? 160 * MiB : 0,
        disk_usage_bytes: 512 * MiB,
        processes: input.start ? 12 : 0,
        pid: input.start ? 6200 : 0,
        ipv4: input.start ? [`10.24.0.${60 + instances.length}`] : [],
        ipv6: [],
        snapshots_count: 0,
      })
      getDemoAudit().unshift({
        id: nextAlertId++, actor: 'admin', action: 'incus.instance.create', target: `incus:${input.name}`, status: 'ok',
        detail: `instance ${input.name} created (demo)`, created_at_ms: Date.now(),
      })
      return { ok: true, message: `instance ${input.name} created` }
    },
    async incusDeleteInstance(_session, name, signal) {
      checkAbort(signal)
      requireSession()
      const state = getDemoIncus()
      demoIncusInstances = state.instances.filter(i => i.name !== name)
      delete state.snapshots[name]
      getDemoAudit().unshift({
        id: nextAlertId++, actor: 'admin', action: 'incus.instance.delete', target: `incus:${name}`, status: 'ok',
        detail: `instance ${name} deleted (demo)`, created_at_ms: Date.now(),
      })
      return { ok: true, message: `instance ${name} deleted` }
    },
    async incusInstanceState(_session, name, action, signal) {
      checkAbort(signal)
      requireSession()
      const { instances } = getDemoIncus()
      const inst = instances.find(i => i.name === name)
      if (!inst) throw new ApiError(404, 'not_found')
      if (action === 'start' || action === 'restart' || action === 'unfreeze') {
        inst.status = 'Running'
        if (!inst.ipv4.length) inst.ipv4 = ['10.24.0.88']
        if (!inst.memory_usage_bytes) inst.memory_usage_bytes = 256 * MiB
        if (!inst.processes) inst.processes = 18
      } else if (action === 'stop' || action === 'force_stop') {
        inst.status = 'Stopped'
        inst.ipv4 = []
        inst.memory_usage_bytes = 0
        inst.processes = 0
      } else if (action === 'freeze') {
        inst.status = 'Frozen'
      }
      getDemoAudit().unshift({
        id: nextAlertId++, actor: 'admin', action: `incus.instance.${action}`, target: `incus:${name}`, status: 'ok',
        detail: `incus ${action} ${name} completed (demo)`, created_at_ms: Date.now(),
      })
      return { ok: true, message: `incus ${action} ${name} completed` }
    },
    async incusSnapshots(name, signal) {
      checkAbort(signal)
      requireSession()
      return [...(getDemoIncus().snapshots[name] ?? [])]
    },
    async incusCreateSnapshot(_session, name, snapName, stateful = false, signal) {
      checkAbort(signal)
      requireSession()
      const { snapshots } = getDemoIncus()
      snapshots[name] = snapshots[name] ?? []
      snapshots[name].unshift({ name: snapName, created_at: new Date().toISOString(), stateful, size_bytes: 64 * MiB })
      getDemoAudit().unshift({
        id: nextAlertId++, actor: 'admin', action: 'incus.snapshot.create', target: `incus:${name}/${snapName}`, status: 'ok',
        detail: `snapshot ${name}/${snapName} created (demo)`, created_at_ms: Date.now(),
      })
      return { ok: true, message: `snapshot ${name}/${snapName} created` }
    },
    async incusRestoreSnapshot(_session, name, snapName, signal) {
      checkAbort(signal)
      requireSession()
      getDemoAudit().unshift({
        id: nextAlertId++, actor: 'admin', action: 'incus.snapshot.restore', target: `incus:${name}/${snapName}`, status: 'ok',
        detail: `snapshot ${name}/${snapName} restored (demo)`, created_at_ms: Date.now(),
      })
      return { ok: true, message: `snapshot ${name}/${snapName} restored` }
    },
    async incusDeleteSnapshot(_session, name, snapName, signal) {
      checkAbort(signal)
      requireSession()
      const { snapshots } = getDemoIncus()
      snapshots[name] = (snapshots[name] ?? []).filter(s => s.name !== snapName)
      getDemoAudit().unshift({
        id: nextAlertId++, actor: 'admin', action: 'incus.snapshot.delete', target: `incus:${name}/${snapName}`, status: 'ok',
        detail: `snapshot ${name}/${snapName} deleted (demo)`, created_at_ms: Date.now(),
      })
      return { ok: true, message: `snapshot ${name}/${snapName} deleted` }
    },
    async incusResources(signal): Promise<IncusResources> {
      checkAbort(signal)
      requireSession()
      return {
        storage_pools: [
          { name: 'default', driver: 'zfs', status: 'Created', description: 'Primary NVMe ZFS pool', used_bytes: 56 * GiB, total_bytes: 500 * GiB, used_by_count: 5 },
        ],
        networks: [
          { name: 'incusbr0', type: 'bridge', managed: true, status: 'Created', ipv4_address: '10.24.0.1/24', ipv6_address: 'fd42:1::1/64', used_by_count: 5 },
        ],
        profiles: [
          { name: 'default', description: 'Default Incus profile', used_by_count: 5 },
        ],
      }
    },
    async incusUpdateInstance(_session, name, input, signal) {
      checkAbort(signal)
      requireSession()
      const inst = getDemoIncus().instances.find(i => i.name === name)
      if (!inst) throw new ApiError(404, 'not_found', 0, undefined, 'Instance not found')
      if (input.description !== undefined) inst.description = input.description
      if (input.cpu_limit !== undefined) inst.cpu_limit = input.cpu_limit || undefined
      if (input.memory_limit !== undefined) inst.memory_limit = input.memory_limit || undefined
      if (input.autostart !== undefined) inst.autostart = input.autostart
      getDemoAudit().unshift({
        id: nextAlertId++, actor: 'admin', action: 'incus.instance.update', target: `incus:${name}`, status: 'ok',
        detail: `instance ${name} updated (demo)`, created_at_ms: Date.now(),
      })
      return { ok: true, message: `instance ${name} updated` }
    },
    async incusExec(_session, name, command, signal) {
      checkAbort(signal)
      requireSession()
      const inst = getDemoIncus().instances.find(i => i.name === name)
      if (!inst) throw new ApiError(404, 'not_found', 0, undefined, 'Instance not found')
      if (inst.status !== 'Running') {
        throw new ApiError(422, 'action_failed', 0, undefined, 'Instance is not running')
      }
      const cmd = command.trim()
      let stdout = `Executed "${cmd}" on ${name}\n`
      if (cmd === 'uptime') stdout = ` 01:55:12 up 14 days,  4:21,  0 users,  load average: 0.14, 0.18, 0.12\n`
      else if (cmd.startsWith('free')) stdout = `               total        used        free      shared  buff/cache   available\nMem:           4.0Gi       1.2Gi       2.1Gi        32Mi       710Mi       2.6Gi\nSwap:             0B          0B          0B\n`
      else if (cmd.startsWith('df')) stdout = `Filesystem      Size  Used Avail Use% Mounted on\n/dev/root        50G  4.2G   46G   9% /\ntmpfs           2.0G     0  2.0G   0% /dev/shm\n`
      else if (cmd.startsWith('ip')) stdout = `lo               UNKNOWN        127.0.0.1/8 ::1/128\neth0             UP             ${inst.ipv4[0] ?? '10.24.0.99'}/24\n`
      else if (cmd === 'whoami') stdout = 'root\n'
      else if (cmd.startsWith('uname')) stdout = `Linux ${name} 6.8.0-52-generic #53-Ubuntu SMP PREEMPT_DYNAMIC x86_64 GNU/Linux\n`
      getDemoAudit().unshift({
        id: nextAlertId++, actor: 'admin', action: 'incus.instance.exec', target: `incus:${name}`, status: 'ok',
        detail: `${cmd} (exit 0)`, created_at_ms: Date.now(),
      })
      return { exit_code: 0, stdout, stderr: '' }
    },
    async incusConsoleLog(name, signal) {
      checkAbort(signal)
      requireSession()
      return `[    0.000000] Linux version 6.8.0-52-generic (buildd@lcy02-amd64)\n[    1.204118] systemd[1]: Hostname set to <${name}>.\n[    2.841902] systemd[1]: Reached target multi-user.target - Multi-User System.\nUbuntu 24.04.2 LTS ${name} console\n`
    },
    async incusBackups(name, signal) {
      checkAbort(signal)
      requireSession()
      return [...(getDemoIncus().backups[name] ?? [])]
    },
    async incusCreateBackup(_session, name, backupName, instanceOnly, signal) {
      checkAbort(signal)
      requireSession()
      const { backups } = getDemoIncus()
      backups[name] = backups[name] ?? []
      backups[name].unshift({
        name: backupName,
        created_at: new Date().toISOString(),
        instance_only: instanceOnly,
        optimized_storage: true,
      })
      getDemoAudit().unshift({
        id: nextAlertId++, actor: 'admin', action: 'incus.backup.create', target: `incus:${name}/backup:${backupName}`, status: 'ok',
        detail: `backup ${name}/${backupName} created (demo)`, created_at_ms: Date.now(),
      })
      return { ok: true, message: `backup ${name}/${backupName} created` }
    },
    async incusDeleteBackup(_session, name, backupName, signal) {
      checkAbort(signal)
      requireSession()
      const { backups } = getDemoIncus()
      backups[name] = (backups[name] ?? []).filter(b => b.name !== backupName)
      getDemoAudit().unshift({
        id: nextAlertId++, actor: 'admin', action: 'incus.backup.delete', target: `incus:${name}/backup:${backupName}`, status: 'ok',
        detail: `backup ${name}/${backupName} deleted (demo)`, created_at_ms: Date.now(),
      })
      return { ok: true, message: `backup ${name}/${backupName} deleted` }
    },
  }
}

function buildSnapshotPoints(now: number): LivePoint[] {
  const points: LivePoint[] = []
  const push = (id: string, key: string) => {
    points.push({ resource_id: id, metric: key, timestamp_ms: now, value: sampleValue(id, key, now) })
  }

  for (const key of [
    metric.cpu, metric.load1, metric.load5, metric.load15,
    metric.ram, metric.ramTotal, metric.ramUsed, metric.ramAvailable,
    metric.cache, metric.buffers, metric.swap, metric.swapTotal, metric.swapUsed,
    metric.rx, metric.tx,
  ]) {
    push('host:local', key)
  }

  for (const item of getDemoResources()) {
    if (!item.active) continue
    if (item.kind === 'cpu_core') push(item.id, metric.core)
    if (item.kind === 'disk') {
      push(item.id, metric.diskRead)
      push(item.id, metric.diskWrite)
      push(item.id, metric.diskReads)
      push(item.id, metric.diskWrites)
    }
    if (item.kind === 'nic') {
      push(item.id, metric.rx)
      push(item.id, metric.tx)
      push(item.id, metric.rxErrors)
      push(item.id, metric.txErrors)
      push(item.id, metric.rxDrops)
      push(item.id, metric.txDrops)
    }
  }
  return points
}

export function createDemoLiveStore(): LiveStore {
  type Listener = () => void
  type Reading = { state: 'waiting' | 'valid' | 'missing' | 'stale' | 'unavailable'; point?: LivePoint }
  const readings = new Map<string, Reading>()
  const listeners = new Map<string, Set<Listener>>()
  const statusListeners = new Set<Listener>()
  const qualityListeners = new Set<Listener>()
  let status: TransportState = 'connecting'
  const waiting: Reading = { state: 'waiting' }
  const missing: Reading = { state: 'missing' }
  const keyOf = (id: string, m: string) => `${id}/${m}`

  function emit() {
    const now = Date.now()
    for (const point of buildSnapshotPoints(now)) {
      const key = keyOf(point.resource_id, point.metric)
      readings.set(key, { state: 'valid', point })
      listeners.get(key)?.forEach(fn => fn())
    }
    if (status !== 'live') {
      status = 'live'
      statusListeners.forEach(fn => fn())
    }
  }

  return {
    getStatus: () => status,
    getPartial: () => false,
    subscribePartial: listener => {
      qualityListeners.add(listener)
      return () => { qualityListeners.delete(listener) }
    },
    subscribeStatus: listener => {
      statusListeners.add(listener)
      return () => { statusListeners.delete(listener) }
    },
    getReading: (id, m) => readings.get(keyOf(id, m)) ?? (status === 'live' ? missing : waiting),
    subscribe: (id, m, listener) => {
      const key = keyOf(id, m)
      const group = listeners.get(key) ?? new Set<Listener>()
      listeners.set(key, group)
      group.add(listener)
      return () => {
        group.delete(listener)
        if (group.size === 0) listeners.delete(key)
      }
    },
    start() {
      emit()
      const timer = setInterval(emit, 1000)
      return () => {
        clearInterval(timer)
        readings.clear()
        status = 'connecting'
      }
    },
  }
}
