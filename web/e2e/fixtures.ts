import type { Page } from '@playwright/test'
import type { Host, Resource, ResourceKind, Series } from '../src/lib/api/client'

function resource(id: string, kind: ResourceKind, display_name: string, meta: object = {}, active = true): Resource {
  return { id, kind, display_name, metadata_json: JSON.stringify(meta), external_key: id, active,
    first_seen_ms: 1_790_000_000_000, last_seen_ms: 1_790_000_000_000 }
}

const host: Host = {
  resource: resource('host:local', 'host', 'edge-01'),
  inventory: {
    hostname: 'edge-01', os_name: 'Ubuntu', os_pretty: 'Ubuntu Server', os_version: '26.04.1',
    kernel: '6.0.0', arch: 'x86_64', cpu_model: 'Processador de teste', cpu_cores: 2, cpu_threads: 2,
    memory_total_bytes: 8 * 1024 ** 3, boot_id: 'test', updated_at_ms: 1_790_000_000_000,
  },
  uptime_seconds: 90061,
}

const resources = [
  resource('cpu:2', 'cpu_core', 'cpu2'),
  resource('cpu:9', 'cpu_core', 'cpu9'),
  resource('cpu:22', 'cpu_core', 'cpu22', {}, false),
  resource('fs:root', 'filesystem', '/', { mountpoint: '/', fs_type: 'ext4' }),
  resource('fs:data', 'filesystem', '/data', { mountpoint: '/data', fs_type: 'ext4' }),
  resource('disk:1', 'disk', 'sda'),
  resource('disk:2', 'disk', 'nvme0n1'),
  resource('nic:1', 'nic', 'enp1s0', { operstate: 'up', pci_addr: '0000:01:00.0' }),
  resource('nic:2', 'nic', 'porta-renomeada', { operstate: 'down', pci_addr: '0000:01:00.0' }),
  resource('sensor:0', 'sensor', 'Canal zero', { chip: 'hwmon', channel: 'temp1' }),
  resource('sensor:missing', 'sensor', 'Canal sem leitura', { chip: 'hwmon', channel: 'temp2' }),
]

export async function fixture(page: Page, options: {
  emptySensors?: boolean
  hostUnavailable?: boolean
  healthUnavailable?: boolean
  pointLimit?: boolean
  delayHost?: Promise<void>
} = {}) {
  let authenticated = true
  const requests: URL[] = []
  const visible = options.emptySensors ? resources.filter(resource => resource.kind !== 'sensor') : resources
  await page.route('**/api/v1/**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname.slice('/api/v1/'.length)
    requests.push(url)
    const session = { username: 'admin', csrf_token: 'fixture-token', expires_at: new Date(Date.now() + 3600_000).toISOString() }
    if (path === 'auth/login') return route.fulfill({ status: 429, headers: { 'Retry-After': '30' }, json: { code: 'rate_limited', message: 'limit' } })
    if (!authenticated) return route.fulfill({ status: 401, json: { code: 'unauthenticated', message: 'unauthorized' } })
    if (path === 'auth/me') return route.fulfill({ json: session })
    if (path === 'auth/logout') return route.fulfill({ json: { ok: true } })
    if (path === 'host') {
      await options.delayHost
      return options.hostUnavailable
        ? route.fulfill({ status: 503, json: { code: 'inventory_unavailable', message: 'pending' } })
        : route.fulfill({ json: host })
    }
    if (path === 'health') {
      if (options.healthUnavailable) return route.fulfill({ status: 503, json: { code: 'unavailable', message: 'unavailable' } })
      return route.fulfill({ json: {
        status: 'ok', db_ok: true, db_ping_ok: true, rollup_lag_s: 0, collector_error_count: 0, dropped_live_events: 0,
        wall_clock_jump_count: 0, db_bytes: 4096, wal_bytes: 0, shm_bytes: 0, active_live_subscribers: 1,
      } })
    }
    if (path === 'resources') return route.fulfill({ json: {
      resources: visible.filter(resource => resource.kind === url.searchParams.get('kind')),
    } })
    if (path.startsWith('resources/')) {
      const id = decodeURIComponent(path.slice('resources/'.length))
      const record = visible.find(resource => resource.id === id)
      return record ? route.fulfill({ json: record }) : route.fulfill({ status: 404, json: { code: 'resource_not_found', message: 'missing' } })
    }
    if (path === 'metrics') {
      const metric_key = url.searchParams.get('metric')!
      const resource_id = url.searchParams.get('resource_id')!
      const step = url.searchParams.get('step')
      const from_ms = Number(url.searchParams.get('from'))
      const to_ms = Number(url.searchParams.get('to'))
      if (options.pointLimit && metric_key === 'cpu.busy_pct' && step === 'auto') {
        return route.fulfill({ status: 422, json: { code: 'point_limit', message: 'point limit' } })
      }
      const resolution_s = step === '3600' ? 3600 : to_ms - from_ms > 86_400_000 ? 3600 : 30
      const seconds = resolution_s * 1000
      const last = Math.floor((to_ms - 1) / seconds) * seconds
      const points = resource_id === 'sensor:missing' || metric_key.startsWith('swap') ? [] :
        (resolution_s === 30 ? [last - 3 * seconds, last - seconds, last] : [last]).map((time, index) => {
          const n = resource_id === 'sensor:0' || index === 0 ? 0 : metric_key.endsWith('_bytes') ? 1024 ** 3 : 24 + index
          return { bucket_start_ms: time, avg: n, min: n, max: n, last: n, sample_count: index ? 2 : 1, expected_samples: 2, coverage: index ? 1 : 0.5 }
        })
      const unit = metric_key.startsWith('sensor') ? '°C' : metric_key.endsWith('_pct') ? '%' :
        metric_key.endsWith('_bytes_per_sec') ? 'bytes/s' : metric_key.endsWith('_bytes') ? 'bytes' : 'load'
      const series: Series = { metric_key, resource_id, from_ms, to_ms, resolution_s, unit, points }
      return route.fulfill({ json: series })
    }
    return route.abort()
  })

  // Controlled browser stream for time and lifecycle assertions. The real suite uses the daemon endpoint.
  await page.addInitScript(() => {
    const nativeFetch = window.fetch.bind(window)
    const stats = { starts: 0, open: 0, max: 0 }
    Object.defineProperty(window, 'fixtureStreamStats', { value: stats })
    const fetchWithStream = (input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input) !== '/api/v1/stream/live') return nativeFetch(input, init)
      const encoder = new TextEncoder()
      const stream = new ReadableStream<Uint8Array>({
        start(controller) {
          stats.starts++
          stats.open++
          stats.max = Math.max(stats.max, stats.open)
          const now = Date.now()
          const points = [
            ['host:local', 'cpu.busy_pct', 31], ['host:local', 'memory.used_pct', 43],
            ['host:local', 'net.rx_bytes_per_sec', 1024], ['host:local', 'net.tx_bytes_per_sec', 2048],
            ['cpu:2', 'cpu.core.busy_pct', 8], ['cpu:9', 'cpu.core.busy_pct', 0],
            ['nic:1', 'net.rx_bytes_per_sec', 8192], ['nic:2', 'net.rx_bytes_per_sec', 512],
          ].map(([resource_id, metric, value]) => ({ resource_id, metric, value, timestamp_ms: now }))
          const close = () => {
            window.removeEventListener('fixture-drop', close)
            init?.signal?.removeEventListener('abort', close)
            stats.open--
            controller.close()
          }
          window.addEventListener('fixture-drop', close)
          init?.signal?.addEventListener('abort', close)
          controller.enqueue(encoder.encode('event: snapshot\ndata: ' + JSON.stringify({
            collected_at: new Date(now).toISOString(), points, warning_count: 0, truncated: false,
          }) + '\n\n'))
        },
      })
      return Promise.resolve(new Response(stream, { headers: { 'Content-Type': 'text/event-stream' } }))
    }
    Object.defineProperty(window, 'fetch', { value: fetchWithStream, writable: true })
  })

  return { requests, expire: () => { authenticated = false } }
}

export async function streamStats(page: Page) {
  return page.evaluate(() => Reflect.get(window, 'fixtureStreamStats') as { starts: number; open: number; max: number })
}
