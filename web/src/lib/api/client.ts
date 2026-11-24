import type { components, paths } from './openapi'

type Schemas = components['schemas']
type ApiPaths = keyof paths
export type Session = Schemas['Session']
export type Health = Schemas['Health']
export type Host = Schemas['Host']
export type Resource = Schemas['Resource']
export type ResourceKind = Schemas['ResourceKind']
export type Series = Schemas['Series']
export type SeriesPoint = Schemas['SeriesPoint']
export type Step = NonNullable<paths['/api/v1/metrics']['get']['parameters']['query']['step']>

export type AlertRule = {
  id: number
  name: string
  resource_id: string
  metric_key: string
  operator: 'gt' | 'lt'
  threshold: number
  duration_seconds: number
  enabled: boolean
  created_at_ms: number
  updated_at_ms: number
}

export type AlertRuleInput = {
  name: string
  resource_id: string
  metric_key: string
  operator: 'gt' | 'lt'
  threshold: number
  duration_seconds: number
  enabled: boolean
}

export type AlertChannel = {
  id: number
  name: string
  kind: 'webhook'
  url: string
  enabled: boolean
  created_at_ms: number
}

export type AlertChannelInput = {
  name: string
  kind?: 'webhook'
  url: string
  enabled: boolean
}

export type AlertEvent = {
  id: number
  rule_id: number
  rule_name: string
  resource_id: string
  metric_key: string
  status: 'firing' | 'resolved'
  value: number
  threshold: number
  triggered_at_ms: number
  resolved_at_ms?: number
  notify_error?: string
}

export type UpdateInfo = {
  current_version: string
  latest_version?: string
  update_available: boolean
  release_url?: string
  checked_at?: string
}

export type ProcessInfo = {
  pid: number
  ppid: number
  name: string
  command: string
  user: string
  state: string
  cpu_percent: number
  rss_bytes: number
  threads: number
}

export type ServiceInfo = {
  name: string
  load_state: string
  active_state: string
  sub_state: string
  unit_file_state?: string
  description: string
}

export type LogEntry = {
  timestamp_ms: number
  priority: 'err' | 'warning' | 'info' | 'debug'
  unit?: string
  message: string
}

export type AuditEntry = {
  id: number
  actor: string
  action: string
  target: string
  status: 'ok' | 'error'
  detail?: string
  created_at_ms: number
}

export type ServiceAction = 'start' | 'stop' | 'restart' | 'reload' | 'enable' | 'disable'

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    readonly retryAfter = 0,
    readonly diagnostics?: Health,
  ) {
    super(code)
  }
}

// A table may need several independently cached histories. Leave room under
// the daemon's eight ordinary HTTP slots for the session and other tabs.
function requestSlots() {
  const waiting: (() => void)[] = []
  let active = 0
  return async function schedule<T>(signal: AbortSignal | null | undefined, run: () => Promise<T>) {
    signal?.throwIfAborted()
    if (active >= 4) {
      await new Promise<void>((resolve, reject) => {
        const grant = () => {
          active++
          signal?.removeEventListener('abort', abort)
          resolve()
        }
        const abort = () => {
          waiting.splice(waiting.indexOf(grant), 1)
          reject(signal?.reason)
        }
        waiting.push(grant)
        signal?.addEventListener('abort', abort, { once: true })
      })
    } else active++
    try {
      signal?.throwIfAborted()
      return await run()
    } finally {
      active--
      waiting.shift()?.()
    }
  }
}

export function createApi(onUnauthorized?: () => void) {
  const schedule = requestSlots()

  async function send<T>(path: ApiPaths | `/api/v1/resources/${string}` | `/api/v1/alerts/${string}` | '/api/v1/update' | '/api/v1/processes' | `/api/v1/processes/${number}/signal` | '/api/v1/services' | `/api/v1/services/${string}/logs` | `/api/v1/services/${string}/action` | '/api/v1/host/power' | '/api/v1/audit', init?: RequestInit, params?: Record<string, string>) {
    const url = params ? `${path}?${new URLSearchParams(params)}` : path
    let response: Response
    try {
      response = await fetch(url, { credentials: 'same-origin', cache: 'no-store', redirect: 'error', ...init })
    } catch (error) {
      if (init?.signal?.aborted) throw error
      throw new ApiError(0, 'network')
    }
    if (!response.ok) {
      if (response.status === 401 && path !== '/api/v1/auth/login' && !init?.signal?.aborted) onUnauthorized?.()
      const retry = Number(response.headers.get('Retry-After')) || 0
      const body: (Partial<Schemas['Error']> & { diagnostics?: Health }) | null = await response.json().catch(() => null)
      throw new ApiError(response.status, body?.code ?? 'http_error', retry, body?.diagnostics)
    }
    if (!response.headers.get('content-type')?.includes('application/json')) {
      throw new ApiError(response.status, 'invalid_response')
    }
    return response.json() as Promise<T>
  }

  function request<T>(path: ApiPaths | `/api/v1/resources/${string}` | `/api/v1/alerts/${string}` | '/api/v1/update' | '/api/v1/processes' | `/api/v1/processes/${number}/signal` | '/api/v1/services' | `/api/v1/services/${string}/logs` | `/api/v1/services/${string}/action` | '/api/v1/host/power' | '/api/v1/audit', init?: RequestInit, params?: Record<string, string>) {
    return schedule(init?.signal, () => send<T>(path, init, params))
  }

  return {
    me: (signal?: AbortSignal) => request<Session>('/api/v1/auth/me', { signal }),
    login: (credentials: Schemas['Login'], signal: AbortSignal) => request<Session>('/api/v1/auth/login', {
      method: 'POST', signal, body: JSON.stringify(credentials), headers: { 'Content-Type': 'application/json' },
    }),
    logout: (session: Session, signal: AbortSignal) => request<{ok: true}>('/api/v1/auth/logout', {
      method: 'POST', signal, headers: { 'X-CSRF-Token': session.csrf_token },
    }),
    host: (signal: AbortSignal) => request<Host>('/api/v1/host', { signal }),
    health: async (signal: AbortSignal) => {
      try {
        return await request<Health>('/api/v1/health', { signal })
      } catch (error) {
        if (error instanceof ApiError && error.status === 503 && error.diagnostics) return error.diagnostics
        throw error
      }
    },
    resource: (id: string, signal: AbortSignal) => request<Resource>(`/api/v1/resources/${encodeURIComponent(id)}`, { signal }),
    resources: async (kind: ResourceKind, signal: AbortSignal) => {
      const items: Resource[] = []
      let cursor: string | undefined
      do {
        const page: Schemas['ResourcePage'] = await request('/api/v1/resources', { signal }, {
          kind, state: 'all', limit: '100', ...(cursor ? { cursor } : {}),
        })
        items.push(...page.resources)
        if (page.next_cursor && cursor && page.next_cursor <= cursor) throw new ApiError(200, 'invalid_cursor')
        cursor = page.next_cursor
      } while (cursor)
      return items
    },
    series: (id: string, metric: string, from: number, to: number, step: Step, signal: AbortSignal) =>
      request<Series>('/api/v1/metrics', { signal }, {
        resource_id: id, metric, from: String(from), to: String(to), step, max_points: '600',
      }),
    alertRules: async (signal: AbortSignal) => (await request<{ rules: AlertRule[] }>('/api/v1/alerts/rules', { signal })).rules,
    createAlertRule: (session: Session, input: AlertRuleInput, signal?: AbortSignal) =>
      request<AlertRule>('/api/v1/alerts/rules', {
        method: 'POST', signal, body: JSON.stringify(input),
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': session.csrf_token },
      }),
    updateAlertRule: (session: Session, id: number, input: AlertRuleInput, signal?: AbortSignal) =>
      request<AlertRule>(`/api/v1/alerts/rules/${id}`, {
        method: 'PUT', signal, body: JSON.stringify(input),
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': session.csrf_token },
      }),
    deleteAlertRule: (session: Session, id: number, signal?: AbortSignal) =>
      request<{ ok: true }>(`/api/v1/alerts/rules/${id}`, {
        method: 'DELETE', signal, headers: { 'X-CSRF-Token': session.csrf_token },
      }),
    alertChannels: async (signal: AbortSignal) => (await request<{ channels: AlertChannel[] }>('/api/v1/alerts/channels', { signal })).channels,
    createAlertChannel: (session: Session, input: AlertChannelInput, signal?: AbortSignal) =>
      request<AlertChannel>('/api/v1/alerts/channels', {
        method: 'POST', signal, body: JSON.stringify(input),
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': session.csrf_token },
      }),
    deleteAlertChannel: (session: Session, id: number, signal?: AbortSignal) =>
      request<{ ok: true }>(`/api/v1/alerts/channels/${id}`, {
        method: 'DELETE', signal, headers: { 'X-CSRF-Token': session.csrf_token },
      }),
    testAlertChannel: (session: Session, url: string, signal?: AbortSignal) =>
      request<{ ok: true }>('/api/v1/alerts/channels/test', {
        method: 'POST', signal, body: JSON.stringify({ url }),
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': session.csrf_token },
      }),
    alertEvents: async (signal: AbortSignal) => (await request<{ events: AlertEvent[] }>('/api/v1/alerts/events', { signal })).events,
    update: (signal: AbortSignal) => request<UpdateInfo>('/api/v1/update', { signal }),
    processes: async (sort: 'cpu' | 'memory', limit: number, signal: AbortSignal) =>
      (await request<{ processes: ProcessInfo[] }>('/api/v1/processes', { signal }, { sort, limit: String(limit) })).processes,
    services: async (state: 'all' | 'active' | 'failed' | 'inactive', signal: AbortSignal) =>
      (await request<{ services: ServiceInfo[] }>('/api/v1/services', { signal }, { state })).services,
    serviceLogs: async (unit: string, lines: number, priority: 'all' | 'info' | 'warning' | 'err', signal: AbortSignal) =>
      (await request<{ unit: string; entries: LogEntry[] }>(`/api/v1/services/${encodeURIComponent(unit)}/logs`, { signal }, {
        lines: String(lines),
        ...(priority !== 'all' ? { priority } : {}),
      })).entries,
    serviceAction: (session: Session, unit: string, action: ServiceAction, signal?: AbortSignal) =>
      request<{ ok: boolean; message?: string }>(`/api/v1/services/${encodeURIComponent(unit)}/action`, {
        method: 'POST', signal, body: JSON.stringify({ action }),
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': session.csrf_token },
      }),
    processSignal: (session: Session, pid: number, sig: 'TERM' | 'KILL', signal?: AbortSignal) =>
      request<{ ok: boolean; message?: string }>(`/api/v1/processes/${pid}/signal`, {
        method: 'POST', signal, body: JSON.stringify({ signal: sig }),
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': session.csrf_token },
      }),
    hostPower: (session: Session, action: 'reboot' | 'poweroff', signal?: AbortSignal) =>
      request<{ ok: boolean; message?: string }>('/api/v1/host/power', {
        method: 'POST', signal, body: JSON.stringify({ action }),
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': session.csrf_token },
      }),
    auditLog: async (signal: AbortSignal, limit = 50) =>
      (await request<{ entries: AuditEntry[] }>('/api/v1/audit', { signal }, { limit: String(limit) })).entries,
  }
}

export type Api = ReturnType<typeof createApi>
