import { describe, expect, it } from 'vitest'
import { createDemoApi, createDemoLiveStore } from './mock'
import { metric } from '../../features/metrics/catalog'

describe('demo mock layer', () => {
  it('serves host, resources, history series, and auth transitions without a backend', async () => {
    const api = createDemoApi()
    const controller = new AbortController()
    const session = await api.me(controller.signal)
    expect(session.username).toBe('admin')

    const host = await api.host(controller.signal)
    expect(host.inventory.hostname).toBe('prod-fra-01')

    const cores = await api.resources('cpu_core', controller.signal)
    expect(cores.length).toBeGreaterThan(6)

    const now = Math.floor(Date.now() / 30_000) * 30_000
    const series = await api.series('host:local', metric.cpu, now - 3_600_000, now, 'auto', controller.signal)
    expect(series.points.length).toBeGreaterThan(100)
    expect(series.unit).toBe('%')

    await api.logout(session, controller.signal)
    await expect(api.me(controller.signal)).rejects.toMatchObject({ status: 401 })

    await expect(api.login({ username: 'admin', password: 'erro' }, controller.signal)).rejects.toMatchObject({ status: 401 })
    const restored = await api.login({ username: 'admin', password: 'ok' }, controller.signal)
    expect(restored.username).toBe('admin')
  })

  it('emits live snapshots immediately when started', () => {
    const store = createDemoLiveStore()
    const stop = store.start({ unauthorized: () => {}, recovered: () => {} })
    try {
      expect(store.getStatus()).toBe('live')
      const reading = store.getReading('host:local', metric.cpu)
      expect(reading.state).toBe('valid')
      expect(reading.point?.value).toBeGreaterThan(0)
    } finally {
      stop()
    }
  })
})
