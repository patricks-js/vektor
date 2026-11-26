import { useQuery } from '@tanstack/react-query'
import { useApi } from '#/lib/api/queries'
import type { Series, Step } from '#/lib/api/client'
import type { HistoricalRange } from './range'
import { rangeMs } from './range'

export function useHistory(id: string | undefined, metric: string, range: HistoricalRange, step: Step = 'auto') {
  const api = useApi()
  const shortWindow = range === '15m' || range === '1h'
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
    staleTime: shortWindow ? 10_000 : 50_000,
    refetchInterval: range === '7d' || typeof range !== 'string' ? 300_000 : shortWindow ? 15_000 : 60_000,
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
    if (!point) return
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
    const current = segments[segments.length - 1]
    if (!contiguous || !current) {
      segments.push({ start: px, end: px, parts: [`M${px.toFixed(2)} ${py.toFixed(2)}`] })
    } else {
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

const percentSteps = [4, 8, 12, 16, 20, 28, 36, 40, 48, 60, 80, 100]
const binarySteps = [1, 2, 4, 8, 12, 16, 20, 24, 32, 40, 48, 64, 80, 100, 128, 160, 200, 256, 320, 400, 512, 640, 800, 1024]
const genericSteps = [1, 1.2, 1.6, 2, 2.4, 3.2, 4, 4.8, 6, 8, 10]

export function niceCeiling(peak: number, unit: string, cap?: number): number {
  const clampCap = (value: number) => cap !== undefined ? Math.min(cap, value) : value

  if (unit === '%') {
    if (peak <= 0) return 4
    const target = peak * 1.15
    const step = percentSteps.find(candidate => candidate >= target) ?? 100
    return clampCap(step)
  }

  if (unit === '°C') {
    if (peak <= 0) return 40
    const target = peak * 1.15
    const step = Math.ceil(target / 20) * 20
    return clampCap(Math.max(40, step))
  }

  if (unit === 'bytes' || unit === 'bytes/s') {
    if (peak <= 0) return 1024
    const target = peak * 1.15
    const level = Math.max(0, Math.floor(Math.log2(target) / 10))
    const scale = 1024 ** level
    const normalized = target / scale
    const step = binarySteps.find(candidate => candidate >= normalized) ?? Math.ceil(normalized / 4) * 4
    return clampCap(step * scale)
  }

  if (peak <= 0) return 1
  const target = peak * 1.15
  const mag = 10 ** Math.floor(Math.log10(target))
  const normalized = target / mag
  const step = genericSteps.find(candidate => candidate >= normalized) ?? 10
  return clampCap(Number((step * mag).toPrecision(4)))
}
