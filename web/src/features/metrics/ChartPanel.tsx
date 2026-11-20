import * as stylex from '@stylexjs/stylex'
import { useEffect, useMemo, useRef, useState } from 'react'
import { AlertTriangle } from 'lucide-react'
import { Badge, Button, Empty, ErrorState, Loading, Panel, Source, layout } from '../../components/ui/ui'
import { ApiError } from '../../lib/api/client'
import type { Series, Step } from '../../lib/api/client'
import { useChartCursor } from './cursor'
import { axisTime, dateTime, duration, formatValue } from './format'
import { useTimeRange } from './range'
import { bucketAt, historyCoverage, plotPath, useHistory } from './series'

type SeriesColor = 'cpu' | 'memory' | 'storage' | 'rx' | 'tx'

const s = stylex.create({
  stage: { position: 'relative', minHeight: 184, minWidth: 0 },
  graph: { display: 'block', width: '100%', color: 'var(--vk-text-muted)', fontFamily: 'var(--vk-mono)', fontSize: 10, touchAction: 'pan-y' },
  grid: { stroke: 'var(--vk-border-subtle)' },
  cursor: { stroke: 'var(--vk-text-secondary)', strokeDasharray: '4 4' },
  text: { fill: 'currentColor' },
  key: { borderBlockStartWidth: 1, borderBlockStartStyle: 'solid', borderBlockStartColor: 'var(--vk-border-subtle)', paddingBlockStart: 12, minHeight: 57 },
  slider: {
    appearance: { default: 'none', '::-webkit-slider-thumb': 'none' },
    width: { default: '100%', '::-webkit-slider-thumb': 14, '::-moz-range-thumb': 14 },
    height: { default: 24, '::-webkit-slider-runnable-track': 4, '::-moz-range-track': 4, '::-webkit-slider-thumb': 14, '::-moz-range-thumb': 14 },
    minHeight: { default: 24, '@media (pointer: coarse)': 44 },
    margin: '0', marginBlockStart: { default: 0, '::-webkit-slider-thumb': -5 }, cursor: 'pointer',
    borderWidth: { default: 0, '::-moz-range-thumb': 0 },
    borderRadius: { default: 0, '::-webkit-slider-thumb': 0, '::-moz-range-thumb': 0 },
    backgroundColor: {
      default: 'transparent',
      '::-webkit-slider-runnable-track': 'var(--vk-border-control)', '::-moz-range-track': 'var(--vk-border-control)',
      '::-webkit-slider-thumb': 'var(--vk-accent)', '::-moz-range-thumb': 'var(--vk-accent)',
    },
  },
  empty: { display: 'grid', alignContent: 'center', minHeight: 184, borderBlockWidth: 1, borderBlockStyle: 'solid', borderBlockColor: 'var(--vk-border-subtle)' },
  foot: { display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: '4px 16px', borderBlockStartWidth: 1, borderBlockStartStyle: 'solid', borderBlockStartColor: 'var(--vk-border-subtle)', paddingBlockStart: 12 },
})

const h = 184
const top = 10
const bottom = h - 25
const coveragePercent = new Intl.NumberFormat('pt-BR', { style: 'percent', maximumFractionDigits: 1 })

function Chart({ series, color, title }: { series: Series; color: SeriesColor; title: string }) {
  const element = useRef<HTMLDivElement>(null)
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
  const left = width < 350 ? 56 : 68
  const right = Math.max(left + 1, width - 12)
  const geometry = useMemo(() => {
    const values = series.points.map(point => point.avg)
    const min = Math.min(0, ...values)
    const max = series.unit === '%' ? Math.max(100, ...values) : Math.max(1, ...values)
    const x = (ts: number) => left + (ts - start) / span * (right - left)
    const y = (value: number) => bottom - (value - min) / (max - min) * (bottom - top)
    const marks = series.points.filter((point, index, points) =>
      point.coverage < 1 || ((index === 0 || point.bucket_start_ms - points[index - 1].bucket_start_ms !== step) &&
        (index === points.length - 1 || points[index + 1].bucket_start_ms - point.bucket_start_ms !== step)))
    return { min, max, x, y, line: plotPath(series, x, y), marks }
  }, [series, left, right, start, span, step])
  const ticks = width < 430 ? [0, 0.5, 1] : [0, 0.25, 0.5, 0.75, 1]
  const cursor = time !== undefined && time >= start && time < series.to_ms ? time : undefined
  const point = cursor === undefined ? undefined : bucketAt(series, cursor)
  const cursorIndex = cursor === undefined ? count - 1 : Math.floor((cursor - start) / step)
  const paint = `var(--vk-series-${color})`
  const coverage = historyCoverage(series)
  const incomplete = coverage && (coverage.gaps > 0 || coverage.low < 1)

  return (
    <div {...stylex.props(layout.compact)}>
      <div {...stylex.props(layout.split, layout.small)}>
        <span>Média por intervalo · {series.unit === 'load' ? 'carga' : series.unit} · UTC</span>
        {incomplete && <Badge tone="warning" icon={<AlertTriangle size={14} aria-hidden />}>Histórico parcial</Badge>}
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
              <text x={left - 6} y={y + 3} textAnchor="end" {...stylex.props(s.text)}>{formatValue(value, series.unit)}</text>
            </g>
          })}
          {ticks.map(tick => {
            const ts = start + span * tick
            const x = geometry.x(ts)
            return <text key={tick} x={x} y={h - 5} textAnchor={tick === 0 ? 'start' : tick === 1 ? 'end' : 'middle'} {...stylex.props(s.text)}>{axisTime(ts, span)}</text>
          })}
          <path d={geometry.line} stroke={paint} strokeWidth={2} fill="none" strokeLinejoin="round" data-history-line="" />
          {geometry.marks.map(p => (
            <circle key={p.bucket_start_ms} cx={geometry.x(p.bucket_start_ms)} cy={geometry.y(p.avg)}
              r={p.coverage < 1 ? 2.5 : 2} fill={p.coverage < 1 ? 'var(--vk-surface-1)' : paint} stroke={paint} />
          ))}
          {cursor !== undefined && <line x1={geometry.x(cursor)} x2={geometry.x(cursor)} y1={top} y2={bottom} {...stylex.props(s.cursor)} />}
          {point && <circle cx={geometry.x(point.bucket_start_ms)} cy={geometry.y(point.avg)} r={4} fill={paint} />}
        </svg>
      </div>
      <input type="range" {...stylex.props(s.slider)} aria-label={`Explorar histórico de ${title}`} min={0} max={Math.max(0, count - 1)} value={cursorIndex}
        aria-valuetext={`${dateTime(start + cursorIndex * step)} · ${point ? formatValue(point.avg, series.unit) : 'Sem amostra'}`}
        onFocus={() => setTime(start + cursorIndex * step)} onBlur={() => setTime()}
        onChange={event => setTime(start + event.currentTarget.valueAsNumber * step)} />
      <div {...stylex.props(layout.small, s.key)} data-history-detail="">
        {cursor === undefined ? <p>Use o controle do gráfico ou passe o cursor sobre a série.</p> : (
          <>
            <p {...stylex.props(layout.technical)}>{dateTime(point?.bucket_start_ms ?? cursor)}</p>
            <p>{point ? <><strong>{formatValue(point.avg, series.unit)}</strong> · {point.sample_count}/{point.expected_samples} amostras · {coveragePercent.format(point.coverage)} de cobertura</> : 'Sem amostra neste intervalo.'}</p>
          </>
        )}
      </div>
      <div {...stylex.props(layout.small, s.foot)}>
        <span>Resolução {duration(series.resolution_s)}</span>
        {coverage && <span>Cobertura por bucket {coveragePercent.format(coverage.low)}{coverage.low !== coverage.high && ` a ${coveragePercent.format(coverage.high)}`}</span>}
        {coverage && coverage.gaps > 0 && <span>{coverage.gaps} {coverage.gaps === 1 ? 'lacuna' : 'lacunas'}</span>}
      </div>
    </div>
  )
}

export function ChartPanel({ title, resourceId, metric, color = 'cpu', resourcePending }: {
  title: string
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
    <Panel title={title} action={<Source>Histórico</Source>}>
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
