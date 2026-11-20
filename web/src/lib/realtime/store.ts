import { ApiError } from '../api/client'
import { decodeSnapshot, openStream, readStream } from './stream'
import type { LivePoint, Snapshot } from './stream'

export type TransportState = 'connecting' | 'live' | 'reconnecting' | 'stale' | 'offline' | 'forbidden'
type Listener = () => void
type Reading = { state: 'waiting' | 'valid' | 'missing' | 'stale' | 'unavailable'; point?: LivePoint }
const waiting: Reading = { state: 'waiting' }
const missing: Reading = { state: 'missing' }
const unavailable: Reading = { state: 'unavailable' }

// The configured live interval is not published. Its documented maximum is 10 s.
// 15 s heartbeats and collector budgets leave room for another frame before abort.
const liveStaleMs = 22_000
const heartbeatTimeoutMs = 35_000
const readingExpiresMs = 75_000
const index = (id: string, metric: string) => `${id}/${metric}`

export function createLiveStore() {
  const readings = new Map<string, { reading: Reading; measured: number; received: number }>()
  const listeners = new Map<string, Set<Listener>>()
  const statusListeners = new Set<Listener>()
  const qualityListeners = new Set<Listener>()
  let partial = false
  let status: TransportState = 'connecting'
  let firstSnapshot = 0
  let lastSnapshot = 0
  let lastFrame = 0

  function notify(key: string) {
    listeners.get(key)?.forEach(listener => listener())
  }

  function freshness(now: number) {
    for (const [key, entry] of readings) {
      const age = now - entry.measured
      const state = age >= readingExpiresMs ? 'unavailable' : age >= liveStaleMs || status !== 'live' ? 'stale' : 'valid'
      if (state !== entry.reading.state) {
        entry.reading = { point: entry.reading.point, state }
        notify(key)
      }
      if (!listeners.has(key) && now - entry.received > readingExpiresMs * 2) readings.delete(key)
    }
  }

  function setStatus(next: TransportState) {
    if (status === next) return
    status = next
    statusListeners.forEach(listener => listener())
    listeners.forEach((_, key) => { if (!readings.has(key)) notify(key) })
    freshness(performance.now())
  }

  function accept(snapshot: Snapshot, now: number) {
    const incomplete = snapshot.truncated || snapshot.warning_count > 0
    if (partial !== incomplete) {
      partial = incomplete
      qualityListeners.forEach(listener => listener())
    }
    const collected = Date.parse(snapshot.collected_at)
    const first = !firstSnapshot
    firstSnapshot ||= now
    lastSnapshot = now

    for (const point of snapshot.points) {
      const key = index(point.resource_id, point.metric)
      const old = readings.get(key)
      if (old?.reading.point?.timestamp_ms === point.timestamp_ms && old.reading.point.value === point.value) continue
      const measured = now - Math.max(0, collected - point.timestamp_ms)
      const age = now - measured
      const state = age >= readingExpiresMs ? 'unavailable' : age >= liveStaleMs ? 'stale' : 'valid'
      readings.set(key, { reading: { point, state }, measured, received: now })
      notify(key)
    }
    setStatus('live')
    if (first) listeners.forEach((_, key) => { if (!readings.has(key)) notify(key) })
  }

  return {
    getStatus: () => status,
    getPartial: () => partial,
    subscribePartial: (listener: Listener) => {
      qualityListeners.add(listener)
      return () => { qualityListeners.delete(listener) }
    },
    subscribeStatus: (listener: Listener) => {
      statusListeners.add(listener)
      return () => { statusListeners.delete(listener) }
    },
    getReading: (id: string, metric: string) => readings.get(index(id, metric))?.reading ??
      (status === 'offline' || status === 'forbidden' || status === 'stale' ? unavailable : firstSnapshot ? missing : waiting),
    subscribe: (id: string, metric: string, listener: Listener) => {
      const key = index(id, metric)
      const group = listeners.get(key) ?? new Set<Listener>()
      listeners.set(key, group)
      group.add(listener)
      return () => {
        group.delete(listener)
        if (group.size === 0) listeners.delete(key)
      }
    },
    start({ unauthorized, recovered }: { unauthorized: () => void; recovered: () => void }) {
      let alive = true
      let blocked = false
      let failures = 0
      let interrupted = false
      let attempt: AbortController | undefined
      let retry: ReturnType<typeof setTimeout> | undefined

      async function connect() {
        if (!alive || blocked || attempt || !navigator.onLine) return
        const current = new AbortController()
        attempt = current
        lastFrame = performance.now()
        const openedAt = lastFrame
        let retryAfter = 0
        try {
          const body = await openStream(current.signal)
          await readStream(body, frame => {
            if (!alive || current.signal.aborted) return
            lastFrame = performance.now()
            const snapshot = decodeSnapshot(frame)
            if (!snapshot || snapshot.points.length === 0) {
              if (status === 'connecting' && lastFrame - openedAt >= liveStaleMs) setStatus('stale')
              return
            }
            accept(snapshot, lastFrame)
            failures = 0
            if (interrupted) {
              interrupted = false
              recovered()
            }
          })
        } catch (error) {
          if (!alive) return
          if (error instanceof ApiError && error.status === 401) {
            blocked = true
            unauthorized()
          } else if (error instanceof ApiError && error.status === 403) {
            blocked = true
            setStatus('forbidden')
          } else if (error instanceof ApiError) {
            retryAfter = error.retryAfter * 1000
          }
        } finally {
          attempt = undefined
          if (alive && !blocked) {
            interrupted ||= Boolean(lastSnapshot)
            if (!navigator.onLine) {
              setStatus('offline')
            } else {
              failures++
              setStatus(failures >= 4 ? 'offline' : 'reconnecting')
              const delay = Math.min(30_000, 1000 * 2 ** Math.min(5, failures - 1))
              retry = setTimeout(connect, Math.max(retryAfter, delay * (0.75 + Math.random() * 0.5)))
            }
          }
        }
      }

      function offline() {
        clearTimeout(retry)
        setStatus('offline')
        attempt?.abort()
      }

      function online() {
        if (blocked) return
        clearTimeout(retry)
        failures = 0
        setStatus('reconnecting')
        void connect()
      }

      const interval = setInterval(() => {
        const now = performance.now()
        if (attempt && now - lastFrame >= heartbeatTimeoutMs) attempt.abort()
        if (status === 'live' && now - lastSnapshot >= liveStaleMs) {
          interrupted = true
          setStatus('stale')
        }
        freshness(now)
      }, 1000)

      window.addEventListener('online', online)
      window.addEventListener('offline', offline)
      if (navigator.onLine) void connect()
      else setStatus('offline')
      return () => {
        alive = false
        attempt?.abort()
        clearInterval(interval)
        clearTimeout(retry)
        window.removeEventListener('online', online)
        window.removeEventListener('offline', offline)
        readings.clear()
        firstSnapshot = lastSnapshot = 0
        partial = false
        setStatus('connecting')
      }
    },
  }
}

export type LiveStore = ReturnType<typeof createLiveStore>
