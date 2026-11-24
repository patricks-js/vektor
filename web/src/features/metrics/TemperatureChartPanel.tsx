import * as stylex from '@stylexjs/stylex'
import { useQueries } from '@tanstack/react-query'
import { useEffect, useMemo, useRef, useState } from 'react'
import { usePreferences } from '../../app/preferences'
import { Empty, ErrorState, Loading, Panel, Source, layout } from '../../components/ui/ui'
import type { Resource, Series } from '../../lib/api/client'
import { useApi } from '../../lib/api/queries'
import { displayName } from '../host/resourceMetadata'
import { metric } from './catalog'
import { useChartCursor } from './cursor'
import { axisTime, dateTime, duration, formatValue, timeZoneLabel } from './format'
import { rangeMs, useTimeRange } from './range'
import { bucketAt, niceCeiling, plotPath } from './series'

const palette = ['#f97316', '#22c55e', '#3b82f6', '#a855f7', '#ec4899', '#eab308']

const s = stylex.create({
  stage: { position: 'relative', minHeight: 196, minWidth: 0 },
  graph: { display: 'block', width: '100%', color: 'var(--vk-text-muted)', fontFamily: 'var(--vk-mono)', fontSize: 10, touchAction: 'pan-y' },
  grid: { stroke: 'var(--vk-border-subtle)', strokeOpacity: 0.75 },
  cursor: { stroke: 'var(--vk-text-muted)', strokeDasharray: '3 3', strokeWidth: 1 },
  text: { fill: 'currentColor' },
  tooltip: {
    position: 'absolute', top: 8, pointerEvents: 'none', zIndex: 2,
    backgroundColor: 'var(--vk-popover)', borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-strong)',
    borderRadius: 'var(--vk-radius-2)', paddingBlock: 8, paddingInline: 10,
    boxShadow: 'var(--vk-shadow-md)', minWidth: 180, maxWidth: 260, display: 'grid', gap: 5,
  },
  tipTime: { fontFamily: 'var(--vk-mono)', fontSize: 11, color: 'var(--vk-text-secondary)' },
  tipRow: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 10, fontSize: 12 },
  tipLabel: { display: 'inline-flex', alignItems: 'center', gap: 6, minWidth: 0, overflowWrap: 'anywhere' },
  tipSwatch: { width: 3, height: 12, borderRadius: 'var(--vk-radius-pill)', flexShrink: 0 },
  tipValue: { fontFamily: 'var(--vk-mono)', fontWeight: 600, color: 'var(--vk-text-primary)', fontVariantNumeric: 'tabular-nums', flexShrink: 0 },
  slider: {
    appearance: { default: 'none', '::-webkit-slider-thumb': 'none' },
    width: { default: '100%', '::-webkit-slider-thumb': 12, '::-moz-range-thumb': 12 },
    height: { default: 20, '::-webkit-slider-runnable-track': 3, '::-moz-range-track': 3, '::-webkit-slider-thumb': 12, '::-moz-range-thumb': 12 },
    minHeight: { default: 20, '@media (pointer: coarse)': 44 },
    margin: '0', marginBlockStart: { default: 0, '::-webkit-slider-thumb': -4.5 }, cursor: 'pointer',
    borderWidth: { default: 0, '::-moz-range-thumb': 0 },
    borderRadius: { default: 'var(--vk-radius-pill)', '::-webkit-slider-runnable-track': 'var(--vk-radius-pill)', '::-moz-range-track': 'var(--vk-radius-pill)', '::-webkit-slider-thumb': 'var(--vk-radius-pill)', '::-moz-range-thumb': 'var(--vk-radius-pill)' },
    backgroundColor: {
      default: 'transparent',
      '::-webkit-slider-runnable-track': 'var(--vk-surface-3)', '::-moz-range-track': 'var(--vk-surface-3)',
      '::-webkit-slider-thumb': 'var(--vk-text-primary)', '::-moz-range-thumb': 'var(--vk-text-primary)',
    },
  },
  legend: {
    display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: '6px 14px',
    borderBlockStartWidth: 1, borderBlockStartStyle: 'solid', borderBlockStartColor: 'var(--vk-border-subtle)',
    paddingBlockStart: 10, fontSize: 12, color: 'var(--vk-text-secondary)',
  },
  legendItem: { display: 'inline-flex', alignItems: 'center', gap: 6 },
  legendDot: { width: 8, height: 8, borderRadius: 2, flexShrink: 0 },
  legendVal: { fontFamily: 'var(--vk-mono)', color: 'var(--vk-text-primary)', fontWeight: 500, fontVariantNumeric: 'tabular-nums' },
  empty: {
    display: 'grid', alignContent: 'center', minHeight: 196,
    borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-subtle)',
    borderRadius: 'var(--vk-radius-2)', backgroundColor: 'var(--vk-surface-2)', padding: 16,
  },
  foot: { display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: '4px 14px', color: 'var(--vk-text-muted)', fontSize: 12 },
})

const h = 196
const top = 12
const bottom = h - 26

type Track = { id: string; label: string; color: string; series: Series }

function MultiSensorChart({ tracks }: { tracks: Track[] }) {
  const { timezone } = usePreferences()
  const element = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(340)
  const { time, setTime } = useChartCursor()
  useEffect(() => {
    if (!element.current) return
    const observer = new ResizeObserver(([entry]) => setWidth(Math.max(1, Math.floor(entry.contentRect.width))))
    observer.observe(element.current)
    return () => observer.disconnect()
  }, [])

  const refSeries = tracks[0].series
  const step = refSeries.resolution_s * 1000
  const start = Math.floor(refSeries.from_ms / step) * step
  const span = Math.max(step, refSeries.to_ms - start)
  const count = Math.ceil(span / step)
  const left = width < 350 ? 48 : 56
  const right = Math.max(left + 1, width - 10)

  const geometry = useMemo(() => {
    const allValues = tracks.flatMap(track => track.series.points.map(point => point.avg))
    const rawMin = allValues.length ? Math.min(...allValues) : 0
    const peak = allValues.length ? Math.max(...allValues) : 0
    const min = rawMin >= 16 ? Math.floor((rawMin - 4) / 8) * 8 : Math.min(0, rawMin)
    const spanNeeded = Math.max(16, (peak - min) * 1.15)
    const max = min > 0
      ? min + Math.ceil(spanNeeded / 16) * 16
      : niceCeiling(peak, '°C')
    const x = (ts: number) => left + (ts - start) / span * (right - left)
    const y = (value: number) => bottom - (value - min) / (max - min) * (bottom - top)
    const lines = tracks.map(track => ({ ...track, d: plotPath(track.series, x, y) }))
    return { min, max, x, y, lines }
  }, [tracks, left, right, start, span])

  const ticks = width < 430 ? [0, 0.5, 1] : [0, 0.25, 0.5, 0.75, 1]
  const cursor = time !== undefined && time >= start && time < refSeries.to_ms ? time : undefined
  const cursorIndex = cursor === undefined ? count - 1 : Math.floor((cursor - start) / step)
  const cursorX = cursor === undefined ? undefined : geometry.x(cursor)
  const flipTooltip = cursorX !== undefined && cursorX > (left + right) / 2
  const cursorPoints = cursor === undefined ? [] : tracks.map(track => ({
    track,
    point: bucketAt(track.series, cursor),
  }))

  return (
    <div {...stylex.props(layout.compact)}>
      <div {...stylex.props(layout.small)}>
        <span>Média por intervalo · °C · {timeZoneLabel(timezone)}</span>
      </div>
      <div ref={element} {...stylex.props(s.stage)}>
        <svg width={width} height={h} {...stylex.props(s.graph)} aria-hidden="true"
          onPointerMove={event => {
            if (event.pointerType === 'touch') return
            const ratio = (event.clientX - event.currentTarget.getBoundingClientRect().left - left) / (right - left)
            setTime(start + Math.max(0, Math.min(count - 1, Math.floor(ratio * span / step))) * step)
          }}
          onPointerLeave={() => setTime()}>
          {[0, 0.25, 0.5, 0.75, 1].map(tick => {
            const value = geometry.min + (geometry.max - geometry.min) * tick
            const y = geometry.y(value)
            return <g key={tick}>
              <line x1={left} x2={right} y1={y} y2={y} {...stylex.props(s.grid)} />
              <text x={left - 8} y={y + 3} textAnchor="end" {...stylex.props(s.text)}>{formatValue(value, '°C')}</text>
            </g>
          })}
          {ticks.map(tick => {
            const ts = start + span * tick
            const x = geometry.x(ts)
            return <text key={tick} x={x} y={h - 6} textAnchor={tick === 0 ? 'start' : tick === 1 ? 'end' : 'middle'} {...stylex.props(s.text)}>{axisTime(ts, span)}</text>
          })}
          {geometry.lines.map(track => (
            <path key={track.id} d={track.d} stroke={track.color} strokeWidth={1.75} fill="none" strokeLinejoin="round" />
          ))}
          {cursorX !== undefined && <line x1={cursorX} x2={cursorX} y1={top} y2={bottom} {...stylex.props(s.cursor)} />}
          {cursorPoints.map(({ track, point }) => point && (
            <circle key={track.id} cx={geometry.x(point.bucket_start_ms)} cy={geometry.y(point.avg)} r={3.5} fill={track.color} stroke="var(--vk-surface-1)" strokeWidth={1.5} />
          ))}
        </svg>
        {cursor !== undefined && cursorX !== undefined && (
          <div aria-hidden {...stylex.props(s.tooltip)} style={flipTooltip
            ? { right: Math.max(10, width - cursorX + 12) }
            : { left: Math.min(Math.max(left, cursorX + 12), Math.max(left, width - 196)) }}>
            <span {...stylex.props(s.tipTime)}>{dateTime(cursor)}</span>
            {cursorPoints.map(({ track, point }) => (
              <div key={track.id} {...stylex.props(s.tipRow)}>
                <span {...stylex.props(s.tipLabel)}>
                  <span {...stylex.props(s.tipSwatch)} style={{ backgroundColor: track.color }} />
                  <span>{track.label}</span>
                </span>
                <span {...stylex.props(s.tipValue)}>{point ? formatValue(point.avg, '°C') : '—'}</span>
              </div>
            ))}
          </div>
        )}
      </div>
      <input type="range" {...stylex.props(s.slider)} aria-label="Explorar histórico de temperatura dos sensores" min={0} max={Math.max(0, count - 1)} value={cursorIndex}
        aria-valuetext={`${dateTime(start + cursorIndex * step)} · ${cursorPoints.map(({ track, point }) => `${track.label}: ${point ? formatValue(point.avg, '°C') : '—'}`).join(', ')}`}
        onFocus={() => setTime(start + cursorIndex * step)} onBlur={() => setTime()}
        onChange={event => setTime(start + event.currentTarget.valueAsNumber * step)} />
      <div {...stylex.props(s.legend)}>
        {tracks.map(track => {
          const activePoint = cursor !== undefined ? bucketAt(track.series, cursor) : track.series.points.at(-1)
          return (
            <span key={track.id} {...stylex.props(s.legendItem)}>
              <span aria-hidden {...stylex.props(s.legendDot)} style={{ backgroundColor: track.color }} />
              <span>{track.label}</span>
              <span {...stylex.props(s.legendVal)}>{activePoint ? formatValue(activePoint.avg, '°C') : '—'}</span>
            </span>
          )
        })}
      </div>
      <div {...stylex.props(s.foot)}>
        <span>Resolução {duration(refSeries.resolution_s)}</span>
        <span>{tracks.length} {tracks.length === 1 ? 'sensor' : 'sensores'}</span>
      </div>
    </div>
  )
}

export function TemperatureChartPanel({ sensors }: { sensors: Resource[] }) {
  const api = useApi()
  const { range } = useTimeRange()
  const active = useMemo(() => sensors.filter(sensor => sensor.active).slice(0, palette.length), [sensors])

  const shortWindow = range === '15m' || range === '1h'
  const queries = useQueries({
    queries: active.map(sensor => ({
      queryKey: ['series', sensor.id, metric.temperature, range, 'auto'],
      queryFn: ({ signal }: { signal: AbortSignal }) => {
        const to = Math.floor(Date.now() / 30_000) * 30_000
        if (typeof range !== 'string') return api.series(sensor.id, metric.temperature, range.from, range.to, 'auto', signal)
        return api.series(sensor.id, metric.temperature, to - rangeMs[range], to, 'auto', signal)
      },
      staleTime: shortWindow ? 10_000 : 50_000,
      refetchInterval: range === '7d' || typeof range !== 'string' ? 300_000 : shortWindow ? 15_000 : 60_000,
    })),
  })

  const pending = queries.some(query => query.isPending)
  const firstError = queries.find(query => query.isError)?.error
  const tracks: Track[] = active.flatMap((sensor, index) => {
    const data = queries[index]?.data
    if (!data?.points.length) return []
    return [{ id: sensor.id, label: displayName(sensor), color: palette[index % palette.length], series: data }]
  })

  return (
    <Panel title="Temperatura" subtitle="Temperaturas dos sensores do sistema" action={<Source>Histórico</Source>}>
      {tracks.length > 0 ? <MultiSensorChart tracks={tracks} /> : (
        <div {...stylex.props(s.empty, layout.stack)}>
          {pending ? <Loading label="Consultando temperaturas…" />
            : firstError ? <ErrorState error={firstError} retry={() => { queries.forEach(q => { void q.refetch() }) }} />
            : <Empty>Sem leituras de temperatura neste período.</Empty>}
        </div>
      )}
    </Panel>
  )
}
