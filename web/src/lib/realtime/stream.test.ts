import { expect, it } from 'vitest'
import { decodeSnapshot, readStream } from './stream'

it('decodes chunk boundaries, heartbeat comments and a multiline snapshot including zero', async () => {
  const encoder = new TextEncoder()
  const pieces = [': connected\r\n\r', '\n: heartbeat\n\nevent: snap', 'shot\r\ndata: {"collected_at":"2026-11-20T12:00:00Z",\r\n',
    'data: "points":[{"resource_id":"cpu:9","metric":"cpu.core.busy_pct","timestamp_ms":123,"value":0}],"warning_count":0,"truncated":false}\r\n\r\n']
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const piece of pieces) controller.enqueue(encoder.encode(piece))
      controller.close()
    },
  })
  const frames: string[] = []
  await readStream(stream, frame => frames.push(frame))
  expect(frames.slice(0, 2)).toEqual([': connected', ': heartbeat'])
  expect(decodeSnapshot(frames[2] ?? '')?.points[0]).toMatchObject({ resource_id: 'cpu:9', value: 0 })
  expect(decodeSnapshot('event: snapshot\ndata: {broken')).toBeUndefined()
  expect(stream.locked).toBe(false)
})
