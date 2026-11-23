import * as stylex from '@stylexjs/stylex'
import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { AlertTriangle } from 'lucide-react'
import { Badge, Button, Empty, ErrorState, Loading, Panel, Source, layout } from '../../components/ui/ui'
import { ApiError } from '../../lib/api/client'
import type { Series, Step } from '../../lib/api/client'
import { useChartCursor } from './cursor'
import { axisTime, dateTime, duration, formatValue } from './format'
import { useTimeRange } from './range'
import { areaPath, bucketAt, historyCoverage, plotPath, useHistory } from './series'

type SeriesColor = 'cpu' | 'memory' | 'storage' | 'disk-io' | 'rx' | 'tx' | 'sensor'

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
    boxShadow: 'var(--vk-shadow-md)', minWidth: 164, maxWidth: 230, display: 'grid', gap: 4,
  },
  tipTime: { fontFamily: 'var(--vk-mono)', fontSize: 11, color: 'var(--vk-text-secondary)' },
  tipRow: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 10, fontSize: 12 },
  tipSwatch: { width: 3, height: 14, borderRadius: 'var(--vk-radius-pill)', flexShrink: 0 },
  tipValue: { fontFamily: 'var(--vk-mono)', fontWeight: 600, color: 'var(--vk-text-primary)', fontVariantNumeric: 'tabular-nums' },
  key: {
    display: 'flex', flexWrap: 'wrap', alignItems: 'center', justifyContent: 'space-between', gap: '4px 12px',
    borderBlockStartWidth: 1, borderBlockStartStyle: 'solid', borderBlockStartColor: 'var(--vk-border-subtle)',
    paddingBlockStart: 10, minHeight: 36,
  },
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
  empty: {
    display: 'grid', alignContent: 'center', minHeight: 196,
    borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-subtle)',
    borderRadius: 'var(--vk-radius-2)', backgroundColor: 'var(--vk-surface-2)', padding: 16,
  },
  foot: {
    display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: '4px 14px',
    color: 'var(--vk-text-muted)', fontSize: 12,
  },
  metaBar: { display: 'flex', flexWrap: 'wrap', alignItems: 'center', justifyContent: 'space-between', gap: 8 },
})

const h = 196
const top = 12
const bottom = h - 26
const coveragePercent = new Intl.NumberFormat('pt-BR', { style: 'percent', maximumFractionDigits: 1 })

function Chart({ series, color, title }: { series: Series; color: SeriesColor; title: string }) {
  const element = useRef<HTMLDivElement>(null)
  const gradId = useId()
  const [width, setWidth] = useState(340)
  const { time, setTime } = useChartCursor()
  useEffect(() => {
    if (!element.current) return
    const observer = new ResizeObserver(([entry]) => setWidth(Math.max(1, Math.floor(entry.contentRect.width))))
    observer.observe(element.current)
    return () => observer.disconnect()
  }, [])

  const step = series.resolution_s * 1000
  const start = Math.floor(series.from_ms / step) * step
  const span = series.to_ms - start
  const count = Math.ceil(span / step)
  const left = width < 350 ? 52 : 62
  const right = Math.max(left + 1, width - 10)
  const geometry = useMemo(() => {
    const values = series.points.map(point => point.avg)
    const min = Math.min(0, ...values)
    const max = series.unit === '%' ? Math.max(100, ...values) : Math.max(1, ...values)
    const x = (ts: number) => left + (ts - start) / span * (right - left)
    const y = (value: number) => bottom - (value - min) / (max - min) * (bottom - top)
    const marks = series.points.filter((point, index, points) =>
      point.coverage < 1 || ((index === 0 || point.bucket_start_ms - points[index - 1].bucket_start_ms !== step) &&
        (index === points.length - 1 || points[index + 1].bucket_start_ms - point.bucket_start_ms !== step)))
    return {
      min, max, x, y,
      line: plotPath(series, x, y),
      area: areaPath(series, x, y, bottom),
      marks,
    }
  }, [series, left, right, start, span, step])
  const ticks = width < 430 ? [0, 0.5, 1] : [0, 0.25, 0.5, 0.75, 1]
  const cursor = time !== undefined && time >= start && time < series.to_ms ? time : undefined
  const point = cursor === undefined ? undefined : bucketAt(series, cursor)
  const cursorIndex = cursor === undefined ? count - 1 : Math.floor((cursor - start) / step)
  const paint = `var(--vk-series-${color})`
  const coverage = historyCoverage(series)
  const incomplete = coverage && (coverage.gaps > 0 || coverage.low < 1)
  const cursorX = cursor === undefined ? undefined : geometry.x(cursor)
  const flipTooltip = cursorX !== undefined && cursorX > (left + right) / 2

  return (
    <div {...stylex.props(layout.compact)}>
      <div {...stylex.props(s.metaBar, layout.small)}>
        <span>Média por intervalo · {series.unit === 'load' ? 'carga' : series.unit} · UTC</span>
        {incomplete && <Badge tone="warning" icon={<AlertTriangle size={13} aria-hidden />}>Histórico parcial</Badge>}
      </div>
      <div ref={element} {...stylex.props(s.stage)}>
        <svg width={width} height={h} {...stylex.props(s.graph)} aria-hidden="true"
          onPointerMove={event => {
            if (event.pointerType === 'touch') return
            const ratio = (event.clientX - event.currentTarget.getBoundingClientRect().left - left) / (right - left)
            setTime(start + Math.max(0, Math.min(count - 1, Math.floor(ratio * span / step))) * step)
          }}
          onPointerLeave={() => setTime()}>
          <defs>
            <linearGradient id={gradId} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={paint} stopOpacity="0.32" />
              <stop offset="100%" stopColor={paint} stopOpacity="0.02" />
            </linearGradient>
          </defs>
          {[0, 0.25, 0.5, 0.75, 1].map(tick => {
            const value = geometry.min + (geometry.max - geometry.min) * tick
            const y = geometry.y(value)
            return <g key={tick}>
              <line x1={left} x2={right} y1={y} y2={y} {...stylex.props(s.grid)} />
              <text x={left - 8} y={y + 3} textAnchor="end" {...stylex.props(s.text)}>{formatValue(value, series.unit)}</text>
            </g>
          })}
          {ticks.map(tick => {
            const ts = start + span * tick
            const x = geometry.x(ts)
            return <text key={tick} x={x} y={h - 6} textAnchor={tick === 0 ? 'start' : tick === 1 ? 'end' : 'middle'} {...stylex.props(s.text)}>{axisTime(ts, span)}</text>
          })}
          {geometry.area && <path d={geometry.area} fill={`url(#${gradId})`} />}
          <path d={geometry.line} stroke={paint} strokeWidth={1.75} fill="none" strokeLinejoin="round" data-history-line="" />
          {geometry.marks.map(p => (
            <circle key={p.bucket_start_ms} cx={geometry.x(p.bucket_start_ms)} cy={geometry.y(p.avg)}
              r={p.coverage < 1 ? 2.5 : 2} fill={p.coverage < 1 ? 'var(--vk-surface-1)' : paint} stroke={paint} />
          ))}
          {cursorX !== undefined && <line x1={cursorX} x2={cursorX} y1={top} y2={bottom} {...stylex.props(s.cursor)} />}
          {point && <circle cx={geometry.x(point.bucket_start_ms)} cy={geometry.y(point.avg)} r={4} fill={paint} stroke="var(--vk-surface-1)" strokeWidth={1.5} />}
        </svg>
        {cursor !== undefined && cursorX !== undefined && (
          <div aria-hidden {...stylex.props(s.tooltip)} style={flipTooltip
            ? { right: Math.max(10, width - cursorX + 12) }
            : { left: Math.min(Math.max(left, cursorX + 12), Math.max(left, width - 176)) }}>
            <span {...stylex.props(s.tipTime)}>{dateTime(point?.bucket_start_ms ?? cursor)}</span>
            <div {...stylex.props(s.tipRow)}>
              <span {...stylex.props(layout.row)}>
                <span {...stylex.props(s.tipSwatch)} style={{ backgroundColor: paint }} />
                <span>{title}</span>
              </span>
              <span {...stylex.props(s.tipValue)}>{point ? formatValue(point.avg, series.unit) : '—'}</span>
            </div>
          </div>
        )}
      </div>
      <input type="range" {...stylex.props(s.slider)} aria-label={`Explorar histórico de ${title}`} min={0} max={Math.max(0, count - 1)} value={cursorIndex}
        aria-valuetext={`${dateTime(start + cursorIndex * step)} · ${point ? formatValue(point.avg, series.unit) : 'Sem amostra'}`}
        onFocus={() => setTime(start + cursorIndex * step)} onBlur={() => setTime()}
        onChange={event => setTime(start + event.currentTarget.valueAsNumber * step)} />
      <div {...stylex.props(layout.small, s.key)} data-history-detail="">
        {cursor === undefined ? <p>Passe o cursor sobre a série ou use o controle.</p> : (
          <>
            <span {...stylex.props(layout.technical)}>{dateTime(point?.bucket_start_ms ?? cursor)}</span>
            <span>{point ? <><strong>{formatValue(point.avg, series.unit)}</strong> · {point.sample_count}/{point.expected_samples} amostras · {coveragePercent.format(point.coverage)}</> : 'Sem amostra neste intervalo.'}</span>
          </>
        )}
      </div>
      <div {...stylex.props(s.foot)}>
        <span>Resolução {duration(series.resolution_s)}</span>
        {coverage && <span>Cobertura {coveragePercent.format(coverage.low)}{coverage.low !== coverage.high && ` a ${coveragePercent.format(coverage.high)}`}</span>}
        {coverage && coverage.gaps > 0 && <span>{coverage.gaps} {coverage.gaps === 1 ? 'lacuna' : 'lacunas'}</span>}
      </div>
    </div>
  )
}

export function ChartPanel({ title, subtitle, resourceId, metric, color = 'cpu', resourcePending }: {
  title: string
  subtitle?: string
  resourceId?: string
  resourcePending?: boolean
  metric: string
  color?: SeriesColor
}) {
  const { range } = useTimeRange()
  const key = typeof range === 'string' ? range : `${range.from}/${range.to}`
  const [resolution, setResolution] = useState<{ key: string; step: Step }>()
  const step = resolution?.key === key ? resolution.step : 'auto'
  const query = useHistory(resourceId, metric, range, step)
  const pointLimit = query.error instanceof ApiError && query.error.code === 'point_limit'

  return (
    <Panel title={title} subtitle={subtitle} action={<Source>Histórico</Source>}>
      {query.data?.points.length ? <Chart series={query.data} title={title} color={color} /> : (
        <div {...stylex.props(s.empty, layout.stack)}>
          {!resourceId ? (resourcePending ? <Loading label="Aguardando inventário…" /> : <Empty>Inventário indisponível para consultar esta série.</Empty>)
            : query.isPending ? <Loading label="Consultando histórico…" />
            : pointLimit ? <>
              <Empty>A janela solicitada excede o limite de pontos. Selecione um período menor ou amplie a resolução.</Empty>
              {step !== '3600' && <div><Button onClick={() => setResolution({ key, step: '3600' })}>Usar resolução de 1h</Button></div>}
            </>
            : query.isError ? <ErrorState error={query.error} retry={() => { void query.refetch() }} />
            : <Empty>Sem amostras neste período. A janela pode anteceder a coleta ou estar fora da retenção disponível.</Empty>}
          {query.data && <p {...stylex.props(layout.small)}>Resolução {duration(query.data.resolution_s)} · Cobertura indisponível · UTC</p>}
        </div>
      )}
      {query.isRefetchError && query.data?.points.length ? <ErrorState title="Última consulta falhou" error={query.error} retry={() => { void query.refetch() }} /> : null}
      {step !== 'auto' && <div><Button variant="quiet" onClick={() => setResolution(undefined)}>Restaurar resolução automática</Button></div>}
    </Panel>
  )
}
