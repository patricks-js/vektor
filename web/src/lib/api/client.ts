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

  async function send<T>(path: ApiPaths | `/api/v1/resources/${string}`, init?: RequestInit, params?: Record<string, string>) {
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

  function request<T>(path: ApiPaths | `/api/v1/resources/${string}`, init?: RequestInit, params?: Record<string, string>) {
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
  }
}

export type Api = ReturnType<typeof createApi>
