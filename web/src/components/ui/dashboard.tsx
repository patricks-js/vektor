import * as stylex from '@stylexjs/stylex'
import type { LucideIcon } from 'lucide-react'
import type { ReactNode } from 'react'
import { layout } from './ui'

const s = stylex.create({
  title: { outlineStyle: 'none' },
  heading: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 16, minWidth: 0 },
  card: {
    backgroundColor: 'var(--vk-surface-1)', borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-subtle)',
    borderRadius: 'var(--vk-radius-3)', minWidth: 0, padding: { default: 16, '@media (min-width: 1024px)': 18 },
    display: 'grid', alignContent: 'start', gap: 10,
  },
  value: { fontSize: 28, lineHeight: '34px', fontWeight: 600, letterSpacing: '-0.03em', fontVariantNumeric: 'tabular-nums', minHeight: 52 },
  dense: { fontSize: 20, lineHeight: '28px', minHeight: 52 },
  icon: { color: 'var(--vk-text-muted)', flexShrink: 0 },
  label: { fontWeight: 500, fontSize: 13, color: 'var(--vk-text-secondary)' },
  list: { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 155px), 1fr))', gap: '16px 24px' },
  dd: { marginBlockStart: 4, overflowWrap: 'anywhere', color: 'var(--vk-text-primary)', fontWeight: 500 },
  barTrack: {
    width: '100%', height: 7, borderRadius: 'var(--vk-radius-pill)',
    backgroundColor: 'var(--vk-surface-3)', overflow: 'hidden',
  },
  barCompact: { width: 88, flexShrink: 0 },
  barFill: {
    height: '100%', borderRadius: 'var(--vk-radius-pill)',
    transition: 'width 250ms ease, background-color 200ms ease',
  },
  barGood: { backgroundColor: 'var(--vk-success-fill)' },
  barWarn: { backgroundColor: 'var(--vk-warning-fill)' },
  barHigh: { backgroundColor: 'var(--vk-critical-fill)' },
})

export function ProgressBar({ value, compact = false }: { value?: number; compact?: boolean }) {
  const clamped = value === undefined || !Number.isFinite(value) ? 0 : Math.max(0, Math.min(100, value))
  const tone = clamped >= 85 ? s.barHigh : clamped >= 65 ? s.barWarn : s.barGood
  return (
    <span aria-hidden {...stylex.props(s.barTrack, compact && s.barCompact)} style={{ display: 'block' }}>
      <span {...stylex.props(s.barFill, tone)} style={{ display: 'block', width: `${clamped}%` }} />
    </span>
  )
}

export function PageHeading({ title, description, action }: { title: string; description?: ReactNode; action?: ReactNode }) {
  return (
    <div {...stylex.props(s.heading)}>
      <div {...stylex.props(layout.compact)}>
        <h1 {...stylex.props(layout.pageTitle, s.title)} tabIndex={-1}>{title}</h1>
        {description && <p {...stylex.props(layout.small)}>{description}</p>}
      </div>
      {action}
    </div>
  )
}

export function MetricCard({ label, value, context, icon: Icon, dense }: {
  label: string; value: ReactNode; context?: ReactNode; icon: LucideIcon; dense?: boolean
}) {
  return (
    <div {...stylex.props(s.card)}>
      <div {...stylex.props(layout.split, s.label)}><span>{label}</span><Icon size={16} aria-hidden {...stylex.props(s.icon)} /></div>
      <div {...stylex.props(s.value, dense && s.dense)}>{value}</div>
      {context && <p {...stylex.props(layout.small)}>{context}</p>}
    </div>
  )
}

export function Facts({ items }: { items: [string, ReactNode][] }) {
  return (
    <dl {...stylex.props(s.list)}>
      {items.map(([label, value]) => <div key={label}>
        <dt {...stylex.props(layout.small)}>{label}</dt>
        <dd {...stylex.props(s.dd)}>{value ?? '—'}</dd>
      </div>)}
    </dl>
  )
}
