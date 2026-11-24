import type { TimeZone } from '../../app/preferences'

const decimal = new Intl.NumberFormat('en', { maximumFractionDigits: 1 })
const temperature = new Intl.NumberFormat('en', { maximumFractionDigits: 2 })
const rate = new Intl.NumberFormat('en', { maximumFractionDigits: 3 })

export function formatValue(value: number, unit: string) {
  if (unit === 'bytes' || unit === 'bytes/s') {
    const scales = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB']
    const level = Math.min(scales.length - 1, Math.max(0, Math.floor(Math.log2(Math.abs(value) || 1) / 10)))
    const scaled = value / 1024 ** level
    return `${rate.format(scaled)} ${scales[level]}${unit.endsWith('/s') ? '/s' : ''}`
  }
  if (unit === '%') return decimal.format(value) + '%'
  if (unit === 'load') return temperature.format(value)
  return `${unit.endsWith('/s') ? rate.format(value) : temperature.format(value)}${unit ? ' ' + unit : ''}`
}

let activeTimeZone: TimeZone = 'utc'

export function setDisplayTimeZone(tz: TimeZone) {
  activeTimeZone = tz
}

export function timeZoneLabel(tz: TimeZone = activeTimeZone) {
  return tz === 'local' ? 'Local' : 'UTC'
}

const formatters = {
  utc: {
    timestamp: new Intl.DateTimeFormat('pt-BR', { timeZone: 'UTC', dateStyle: 'medium', timeStyle: 'medium' }),
    clock: new Intl.DateTimeFormat('pt-BR', { timeZone: 'UTC', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }),
    day: new Intl.DateTimeFormat('pt-BR', { timeZone: 'UTC', day: '2-digit', month: '2-digit' }),
  },
  local: {
    timestamp: new Intl.DateTimeFormat('pt-BR', { dateStyle: 'medium', timeStyle: 'medium' }),
    clock: new Intl.DateTimeFormat('pt-BR', { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }),
    day: new Intl.DateTimeFormat('pt-BR', { day: '2-digit', month: '2-digit' }),
  },
}

export function dateTime(value: number | string, tz: TimeZone = activeTimeZone) {
  const ms = typeof value === 'string' ? Date.parse(value) : value
  if (!Number.isFinite(ms)) return '—'
  const text = formatters[tz].timestamp.format(ms)
  return tz === 'utc' ? text + ' UTC' : text
}

export function axisTime(value: number, span: number, tz: TimeZone = activeTimeZone) {
  const fmt = formatters[tz]
  if (span > 86_400_000) return fmt.day.format(value) + ' ' + fmt.clock.format(value)
  return fmt.clock.format(value)
}

export function duration(seconds: number) {
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ${minutes % 60}m`
  return `${Math.floor(hours / 24)}d ${hours % 24}h`
}
