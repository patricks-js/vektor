import * as stylex from '@stylexjs/stylex'
import type { LucideIcon } from 'lucide-react'
import type { ReactNode } from 'react'
import { layout } from './ui'

const s = stylex.create({
  title: { outlineStyle: 'none' },
  heading: { display: 'flex', alignItems: 'flex-end', justifyContent: 'space-between', flexWrap: 'wrap', gap: 16, minWidth: 0 },
  card: {
    backgroundColor: 'var(--vk-surface-1)', borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-subtle)',
    minWidth: 0, padding: { default: 16, '@media (min-width: 1024px)': 20 }, display: 'grid', alignContent: 'start', gap: 12,
  },
  value: { fontSize: 36, lineHeight: '40px', fontWeight: 600, letterSpacing: '-0.045em', fontVariantNumeric: 'tabular-nums', minHeight: 58 },
  dense: { fontSize: 24, lineHeight: '32px', minHeight: 56 },
  icon: { color: 'var(--vk-info)', flexShrink: 0 },
  label: { fontWeight: 600, color: 'var(--vk-text-secondary)' },
  list: { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 155px), 1fr))', gap: '16px 24px' },
  dd: { marginBlockStart: 4, overflowWrap: 'anywhere' },
})

export function PageHeading({ title, description, action }: { title: string; description?: ReactNode; action?: ReactNode }) {
  return (
    <div {...stylex.props(s.heading)}>
      <div {...stylex.props(layout.compact)}>
        <h1 {...stylex.props(layout.pageTitle, s.title)} tabIndex={-1}>{title}</h1>
        {description && <p {...stylex.props(layout.text)}>{description}</p>}
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
      <div {...stylex.props(layout.split, s.label)}><span>{label}</span><Icon size={17} aria-hidden {...stylex.props(s.icon)} /></div>
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
