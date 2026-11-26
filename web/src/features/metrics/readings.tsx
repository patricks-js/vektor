import * as stylex from '@stylexjs/stylex'
import { useSyncExternalStore } from 'react'
import { ProgressBar } from '#/components/ui/dashboard'
import { useLiveReading } from '#/lib/realtime/hooks'
import { dateTime, formatValue } from './format'
import { useHistory } from './series'

const s = stylex.create({
  block: { display: 'grid', gap: 6 },
  inlineBar: { display: 'inline-flex', alignItems: 'center', gap: 10, minWidth: 0 },
  number: { fontVariantNumeric: 'tabular-nums', fontFamily: 'var(--vk-mono)', overflowWrap: 'anywhere' },
  warning: { color: 'var(--vk-warning)' },
  context: { display: 'block', fontSize: 12, lineHeight: '16px', fontWeight: 400, letterSpacing: 'normal', color: 'var(--vk-text-secondary)' },
})

const pollingClock = 30_000
const readTime = () => Math.floor(Date.now() / pollingClock) * pollingClock
const clockListeners = new Set<() => void>()
let timer: ReturnType<typeof setInterval> | undefined

function subscribeClock(listener: () => void) {
  clockListeners.add(listener)
  timer ??= setInterval(() => clockListeners.forEach(notify => notify()), pollingClock)
  return () => {
    clockListeners.delete(listener)
    if (!clockListeners.size) {
      clearInterval(timer)
      timer = undefined
    }
  }
}

export function LiveValue({ id, metric, unit, detail = false, bar = false, compactBar = false }: {
  id: string; metric: string; unit: string; detail?: boolean; bar?: boolean; compactBar?: boolean
}) {
  const { point, state } = useLiveReading(id, metric)
  const valid = point && (state === 'valid' || state === 'stale')
  const numeric = valid ? point.value : undefined
  return (
    <span {...stylex.props(bar && !compactBar && s.block)}>
      <span {...stylex.props(compactBar && s.inlineBar)}>
        <span {...stylex.props(s.number)}>{valid ? formatValue(point.value, unit) : '—'}</span>
        {compactBar && unit === '%' && <ProgressBar value={numeric} compact />}
      </span>
      {bar && !compactBar && unit === '%' && <ProgressBar value={numeric} />}
      {(detail || state !== 'valid') && (
        <span {...stylex.props(s.context, state === 'stale' && s.warning)}>
          {state === 'waiting' ? 'Aguardando leitura' : state === 'missing' ? 'Sem amostra' : state === 'unavailable' ? 'Indisponível' : state === 'stale' ? 'Leitura atrasada' : 'Ao vivo'}
          {point && (detail || state !== 'valid') && ` · ${dateTime(point.timestamp_ms)}`}
        </span>
      )}
    </span>
  )
}

export function PersistedValue({ id, metric, unit, active = true, detail = false, bar = false }: {
  id: string; metric: string; unit: string; active?: boolean; detail?: boolean; bar?: boolean
}) {
  const query = useHistory(active ? id : undefined, metric, '15m', '30')
  const now = useSyncExternalStore(subscribeClock, readTime)
  const point = query.data?.points.at(-1)
  const sensor = metric.startsWith('sensor.')
  const threshold = sensor ? 360_000 : 420_000
  const age = point ? now - point.bucket_start_ms : Infinity
  const available = active && point && age <= threshold * 2
  const stale = Boolean(available && (age > threshold || query.isError))
  const state = !active ? 'Recurso inativo' : query.isPending ? 'Consultando histórico…' :
    stale ? 'Última leitura atrasada' : query.isError ? 'Consulta indisponível' :
      !point ? 'Sem leitura recente' : !available ? 'Leitura indisponível' :
        point.coverage < 1 ? 'Histórico parcial' : 'Último bucket persistido'
  return (
    <span {...stylex.props(bar && s.block)}>
      <span {...stylex.props(s.number)}>{available ? formatValue(point.last, unit) : '—'}</span>
      {bar && unit === '%' && <ProgressBar value={available ? point.last : undefined} />}
      {(detail || stale || !available || point.coverage < 1) && (
        <span {...stylex.props(s.context, Boolean(stale || (point && point.coverage < 1)) && s.warning)}>
          {state}{point && ` · ${dateTime(point.bucket_start_ms)}`}
        </span>
      )}
    </span>
  )
}
