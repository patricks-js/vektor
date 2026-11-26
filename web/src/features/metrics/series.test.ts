import { describe, expect, it } from 'vitest'
import type { Series } from '#/lib/api/client'
import { areaPath, bucketAt, historyCoverage, niceCeiling, plotPath } from './series'
import { formatValue } from './format'

const series: Series = {
  resource_id: 'host:local', metric_key: 'cpu.busy_pct', unit: '%', resolution_s: 30,
  from_ms: 1000, to_ms: 120000,
  points: [
    { bucket_start_ms: 0, avg: 0, min: 0, max: 0, last: 0, sample_count: 1, expected_samples: 2, coverage: 0.5 },
    { bucket_start_ms: 60000, avg: 4, min: 4, max: 4, last: 4, sample_count: 2, expected_samples: 2, coverage: 1 },
    { bucket_start_ms: 90000, avg: 6, min: 6, max: 6, last: 6, sample_count: 2, expected_samples: 2, coverage: 1 },
  ],
}

describe('historical buckets', () => {
  it('preserves zero and breaks the path at a missing bucket', () => {
    expect(bucketAt(series, 25000)?.avg).toBe(0)
    expect(bucketAt(series, 35000)).toBeUndefined()
    expect(plotPath(series, time => time / 1000, value => value)).toEqual('M0.00 0.00 M60.00 4.00 L90.00 6.00')
    expect(areaPath(series, time => time / 1000, value => value, 100)).toEqual('M60.00 4.00 L90.00 6.00 L90.00 100.00 L60.00 100.00 Z')
  })

  it('reports API coverage and counts missing aligned buckets', () => {
    expect(historyCoverage(series)).toEqual({ low: 0.5, high: 1, gaps: 1 })
    expect(historyCoverage({ ...series, points: [] })).toBeUndefined()
  })

  it('keeps rates, capacity, and real zeros distinct', () => {
    expect(formatValue(1024 ** 3, 'bytes')).toBe('1 GiB')
    expect(formatValue(1024 ** 2, 'bytes/s')).toBe('1 MiB/s')
    expect(formatValue(0, '°C')).toBe('0 °C')
  })

  it('computes dynamic Y-axis ceilings with headroom', () => {
    expect(niceCeiling(6.7, '%', 100)).toBe(8)
    expect(niceCeiling(0, '%', 100)).toBe(4)
    expect(niceCeiling(95, '%', 100)).toBe(100)
    expect(niceCeiling(55, '°C')).toBe(80)
    expect(niceCeiling(1.5, 'load')).toBe(2)
    expect(niceCeiling(6 * 1024 ** 2, 'bytes/s')).toBe(8 * 1024 ** 2)
  })
})
