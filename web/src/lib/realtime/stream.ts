import { ApiError } from '#/lib/api/client'

export type LivePoint = { resource_id: string; metric: string; timestamp_ms: number; value: number }
export type Snapshot = { collected_at: string; points: LivePoint[]; warning_count: number; truncated: boolean }

// The daemon emits LF-delimited SSE frames including heartbeat comments. Accept
// CRLF and multiline data as well, and bound the buffer above the 8192-point API cap.
export async function readStream(
  body: ReadableStream<Uint8Array>,
  onFrame: (frame: string) => void,
) {
  const reader = body.getReader()
  const decoder = new TextDecoder()
  let pending = ''
  try {
    for (;;) {
      const { value, done } = await reader.read()
      if (done) return
      pending += decoder.decode(value, { stream: true })
      let match = /\r?\n\r?\n/.exec(pending)
      while (match) {
        onFrame(pending.slice(0, match.index))
        pending = pending.slice(match.index + match[0].length)
        match = /\r?\n\r?\n/.exec(pending)
      }
      if (pending.length > 4 * 1024 * 1024) throw new Error('SSE frame exceeds the supported size')
    }
  } finally {
    await reader.cancel().catch(() => {})
    reader.releaseLock()
  }
}

export function decodeSnapshot(frame: string): Snapshot | undefined {
  const lines = frame.split(/\r?\n/)
  if (!lines.some(line => /^event: ?snapshot$/.test(line))) return
  const data = lines.filter(line => line.startsWith('data:')).map(line => line.slice(5).replace(/^ /, '')).join('\n')
  try {
    const snapshot: Snapshot = JSON.parse(data)
    if (!Number.isFinite(Date.parse(snapshot.collected_at)) || !Array.isArray(snapshot.points)) return
    return {
      ...snapshot,
      points: snapshot.points.filter(point =>
        point && typeof point.resource_id === 'string' && typeof point.metric === 'string' &&
        Number.isFinite(point.timestamp_ms) && Number.isFinite(point.value)),
    }
  } catch {
    return
  }
}

export async function openStream(signal: AbortSignal) {
  const response = await fetch('/api/v1/stream/live', {
    signal, credentials: 'same-origin', cache: 'no-store', redirect: 'error', headers: { Accept: 'text/event-stream' },
  })
  if (!response.ok) {
    await response.body?.cancel()
    throw new ApiError(response.status, 'stream_error', Number(response.headers.get('Retry-After')) || 0)
  }
  if (!response.body || !response.headers.get('Content-Type')?.startsWith('text/event-stream')) {
    await response.body?.cancel()
    throw new ApiError(response.status, 'invalid_stream')
  }
  return response.body
}
