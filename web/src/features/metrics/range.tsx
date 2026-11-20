import * as stylex from '@stylexjs/stylex'
import { CalendarClock } from 'lucide-react'
import { useState } from 'react'
import type { FormEvent } from 'react'
import { useSearchParams } from 'react-router-dom'
import { isRange, ranges, usePreferences } from '../../app/preferences'
import type { TimeRange } from '../../app/preferences'
import { Button, Modal, TextField, layout } from '../../components/ui/ui'
import { dateTime } from './format'

export const rangeMs: Record<TimeRange, number> = {
  '15m': 900_000, '1h': 3_600_000, '6h': 21_600_000, '24h': 86_400_000, '7d': 604_800_000,
}

const s = stylex.create({
  group: { display: 'flex', width: 'fit-content', maxWidth: '100%', flexWrap: 'wrap', gap: 2, padding: 3, borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-control)', backgroundColor: 'var(--vk-surface-1)' },
  icon: { alignSelf: 'center', color: 'var(--vk-text-muted)', marginInline: 8, display: { default: 'none', '@media (min-width: 560px)': 'block' } },
  option: {
    cursor: 'pointer', borderWidth: 1, borderStyle: 'solid', borderColor: 'transparent', color: 'var(--vk-text-secondary)',
    backgroundColor: { default: 'transparent', ':hover': 'var(--vk-surface-3)' },
    minWidth: 38, minHeight: { default: 34, '@media (pointer: coarse)': 44 },
    fontWeight: 600, fontVariantNumeric: 'tabular-nums', paddingInline: 6,
  },
  selected: {
    backgroundColor: 'var(--vk-surface-3)', borderColor: 'var(--vk-accent)', color: 'var(--vk-accent)',
  },
})

export type HistoricalRange = TimeRange | { from: number; to: number }
const maxWindow = 90 * 86_400_000
const lastYearMs = 253_402_300_799_999

function validBounds(from: number, to: number) {
  return Number.isSafeInteger(from) && Number.isSafeInteger(to) &&
    from >= 0 && to <= lastYearMs && to > from && to - from <= maxWindow
}

export function useTimeRange() {
  const [search, setSearch] = useSearchParams()
  const { defaultRange } = usePreferences()
  const value = search.get('range')
  const from = Number(search.get('from') ?? NaN)
  const to = Number(search.get('to') ?? NaN)
  const range: HistoricalRange = value === 'custom' && validBounds(from, to)
    ? { from, to } : isRange(value) ? value : defaultRange

  function setRange(next: HistoricalRange) {
    setSearch(current => {
      const updated = new URLSearchParams(current)
      for (const key of ['range', 'from', 'to']) updated.delete(key)
      for (const [key, value] of rangeParams(next)) updated.set(key, value)
      return updated
    })
  }
  return { range, setRange }
}

function rangeParams(range: HistoricalRange) {
  return new URLSearchParams(typeof range === 'string' ? { range } : {
    range: 'custom', from: String(range.from), to: String(range.to),
  })
}

export function usePeriodLink() {
  const { range } = useTimeRange()
  return (path: string, resource?: string) => {
    const search = rangeParams(range)
    if (resource) search.set('resource', resource)
    return path + '?' + search
  }
}

export function TimeRangeSelector() {
  const { range, setRange } = useTimeRange()
  const [draft, setDraft] = useState<{ from: number; to: number }>()
  const [error, setError] = useState('')
  const custom = typeof range !== 'string'
  const input = (time: number) => new Date(time).toISOString().slice(0, 16)

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const values = new FormData(event.currentTarget)
    const from = Date.parse(String(values.get('from')) + 'Z')
    const to = Date.parse(String(values.get('to')) + 'Z')
    if (!validBounds(from, to)) {
      setError('Informe início e fim em ordem cronológica, com intervalo de até 90 dias.')
      return
    }
    setRange({ from, to })
    setDraft(undefined)
  }

  return (
    <div {...stylex.props(layout.compact)}>
      <div {...stylex.props(layout.row)} role="group" aria-label="Período histórico">
        <span {...stylex.props(layout.small)}>Período</span>
        <div {...stylex.props(s.group)}>
          <CalendarClock size={15} aria-hidden {...stylex.props(s.icon)} />
          {ranges.map(option => (
            <button type="button" key={option} aria-pressed={range === option}
              {...stylex.props(s.option, range === option && s.selected)}
              onClick={() => { if (option !== range) setRange(option) }}>
              {option}
            </button>
          ))}
          <button type="button" aria-haspopup="dialog" aria-pressed={custom} {...stylex.props(s.option, custom && s.selected)}
            onClick={() => {
              setError('')
              const to = Date.now()
              setDraft(custom ? range : { from: to - rangeMs[range], to })
            }}>Personalizado</button>
        </div>
      </div>
      {custom && <p {...stylex.props(layout.small)}>{dateTime(range.from)} a {dateTime(range.to)}</p>}
      <Modal title="Período personalizado" open={Boolean(draft)} onClose={() => setDraft(undefined)}>
        {draft && <form {...stylex.props(layout.stack)} onSubmit={submit}>
          <TextField label="Início (UTC)" name="from" type="datetime-local" defaultValue={input(draft.from)} required />
          <TextField label="Fim (UTC)" name="to" type="datetime-local" defaultValue={input(draft.to)} required />
          {error && <p role="alert">{error}</p>}
          <div {...stylex.props(layout.row)}>
            <Button onClick={() => setDraft(undefined)}>Cancelar</Button>
            <Button variant="primary" type="submit">Aplicar</Button>
          </div>
        </form>}
      </Modal>
    </div>
  )
}
