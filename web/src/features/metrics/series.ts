import { useQuery } from '@tanstack/react-query'
import { useApi } from '../../lib/api/queries'
import type { Series, Step } from '../../lib/api/client'
import type { HistoricalRange } from './range'
import { rangeMs } from './range'

export function useHistory(id: string | undefined, metric: string, range: HistoricalRange, step: Step = 'auto') {
  const api = useApi()
  return useQuery({
    queryKey: ['series', id, metric, range, step],
    enabled: Boolean(id),
    queryFn: ({ signal }) => {
      if (!id) throw new Error('Recurso ausente')
      // Align request times across panels and leave the currently open raw bucket out.
      const to = Math.floor(Date.now() / 30_000) * 30_000
      if (typeof range !== 'string') return api.series(id, metric, range.from, range.to, step, signal)
      return api.series(id, metric, to - rangeMs[range], to, step, signal)
    },
    staleTime: 50_000,
    refetchInterval: range === '7d' || typeof range !== 'string' ? 300_000 : 60_000,
  })
}

export function bucketAt(series: Series, time: number) {
  const step = series.resolution_s * 1000
  const target = Math.floor(time / step) * step
  let low = 0
  let high = series.points.length - 1
  while (low <= high) {
    const middle = Math.floor((low + high) / 2)
    const point = series.points[middle]
    if (point.bucket_start_ms === target) return point
    if (point.bucket_start_ms < target) low = middle + 1
    else high = middle - 1
  }
}

export function plotPath(series: Series, x: (time: number) => number, y: (value: number) => number) {
  let previous: number | undefined
  return series.points.map(point => {
    const contiguous = previous !== undefined && point.bucket_start_ms - previous === series.resolution_s * 1000
    previous = point.bucket_start_ms
    return `${contiguous ? 'L' : 'M'}${x(point.bucket_start_ms).toFixed(2)} ${y(point.avg).toFixed(2)}`
  }).join(' ')
}

export function areaPath(series: Series, x: (time: number) => number, y: (value: number) => number, baseline: number) {
  const step = series.resolution_s * 1000
  const segments: { start: number; end: number; parts: string[] }[] = []
  let previous: number | undefined
  for (const point of series.points) {
    const px = x(point.bucket_start_ms)
    const py = y(point.avg)
    const contiguous = previous !== undefined && point.bucket_start_ms - previous === step
    if (!contiguous) {
      segments.push({ start: px, end: px, parts: [`M${px.toFixed(2)} ${py.toFixed(2)}`] })
    } else {
      const current = segments[segments.length - 1]
      current.end = px
      current.parts.push(`L${px.toFixed(2)} ${py.toFixed(2)}`)
    }
    previous = point.bucket_start_ms
  }
  const base = baseline.toFixed(2)
  return segments
    .filter(segment => segment.parts.length > 1)
    .map(segment => `${segment.parts.join(' ')} L${segment.end.toFixed(2)} ${base} L${segment.start.toFixed(2)} ${base} Z`)
    .join(' ')
}

export function historyCoverage(series: Series) {
  if (!series.points.length) return
  const coverage = series.points.map(point => point.coverage)
  const step = series.resolution_s * 1000
  const from = Math.floor(series.from_ms / step) * step
  const gaps = Math.max(0, Math.ceil((series.to_ms - from) / step) - series.points.length)
  return { low: Math.min(...coverage), high: Math.max(...coverage), gaps }
}
