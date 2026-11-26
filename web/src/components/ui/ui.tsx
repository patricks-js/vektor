import * as stylex from '@stylexjs/stylex'
import { AlertCircle, ArrowUpRight, Box, Database, LoaderCircle, X } from 'lucide-react'
import { Link } from 'react-router-dom'
import { useEffect, useId, useRef } from 'react'
import type { ButtonHTMLAttributes, ComponentPropsWithRef, ReactNode, SelectHTMLAttributes } from 'react'
import { ApiError } from '#/lib/api/client'

export const layout = stylex.create({
  page: { display: 'grid', gap: 16, minWidth: 0 },
  stack: { display: 'grid', gap: 16, minWidth: 0 },
  compact: { display: 'grid', gap: 6, minWidth: 0 },
  row: { display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 10, minWidth: 0 },
  split: { display: 'flex', flexWrap: 'wrap', justifyContent: 'space-between', alignItems: 'center', gap: 12 },
  text: { color: 'var(--vk-text-secondary)', overflowWrap: 'anywhere' },
  small: { color: 'var(--vk-text-secondary)', fontSize: 13, lineHeight: '19px', overflowWrap: 'anywhere' },
  technical: { fontFamily: 'var(--vk-mono)', fontSize: 12, lineHeight: '16px', fontWeight: 500, overflowWrap: 'anywhere' },
  muted: { color: 'var(--vk-text-muted)' },
  grid2: { display: 'grid', gap: 16, gridTemplateColumns: { default: 'minmax(0, 1fr)', '@media (min-width: 1024px)': 'repeat(2, minmax(0, 1fr))' } },
  grid1: { display: 'grid', gap: 16, gridTemplateColumns: 'minmax(0, 1fr)' },
  grid4: { display: 'grid', gap: 16, gridTemplateColumns: { default: 'minmax(0, 1fr)', '@media (min-width: 560px)': 'repeat(2, minmax(0, 1fr))', '@media (min-width: 1200px)': 'repeat(4, minmax(0, 1fr))' } },
  sectionTitle: { fontSize: 17, lineHeight: '24px', fontWeight: 600, letterSpacing: '-0.015em', color: 'var(--vk-text-primary)' },
  pageTitle: { fontSize: 24, lineHeight: '30px', fontWeight: 600, letterSpacing: '-0.025em', overflowWrap: 'anywhere', color: 'var(--vk-text-primary)' },
})

export const tableStyles = stylex.create({
  scroll: {
    overflowX: 'auto', minWidth: 0, borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-subtle)',
    borderRadius: 'var(--vk-radius-2)', backgroundColor: 'var(--vk-surface-1)',
  },
  table: { borderCollapse: 'collapse', minWidth: 460, width: '100%', textAlign: 'start', tableLayout: 'auto' },
  th: {
    color: 'var(--vk-text-secondary)', fontSize: 12, lineHeight: '16px', backgroundColor: 'var(--vk-surface-2)',
    textAlign: 'start', paddingBlock: 8, paddingInline: 14, fontWeight: 500,
    borderBlockEndWidth: 1, borderBlockEndStyle: 'solid', borderBlockEndColor: 'var(--vk-border-subtle)',
  },
  row: {
    backgroundColor: { default: 'transparent', ':hover': 'var(--vk-surface-2)' },
    transition: 'background-color 150ms',
  },
  td: {
    borderBlockStartWidth: 1, borderBlockStartStyle: 'solid', borderBlockStartColor: 'var(--vk-border-subtle)',
    paddingBlock: 10, paddingInline: 14, minHeight: 44, verticalAlign: 'middle',
    overflowWrap: 'anywhere', fontSize: 13, textAlign: 'start',
  },
  filters: { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 170px), 1fr))', gap: 12, maxWidth: 540 },
})

const ui = stylex.create({
  button: {
    display: 'inline-flex', alignItems: 'center', justifyContent: 'center', gap: 7, paddingInline: 14,
    minHeight: { default: 36, '@media (pointer: coarse)': 44 }, borderWidth: 1, borderStyle: 'solid',
    fontWeight: 500, fontSize: 13, borderRadius: 'var(--vk-radius-2)', cursor: 'pointer',
    textDecoration: 'none', transition: 'background-color 150ms, border-color 150ms, color 150ms',
    opacity: { default: 1, ':disabled': 0.5 },
  },
  primary: {
    backgroundColor: { default: 'var(--vk-accent)', ':hover': 'var(--vk-accent-hover)' },
    color: 'var(--vk-accent-ink)', borderColor: 'var(--vk-accent)', fontWeight: 600,
  },
  outline: {
    backgroundColor: { default: 'var(--vk-surface-1)', ':hover': 'var(--vk-surface-3)' },
    color: 'var(--vk-text-primary)', borderColor: 'var(--vk-border-control)',
  },
  quiet: {
    backgroundColor: { default: 'transparent', ':hover': 'var(--vk-surface-3)' },
    color: { default: 'var(--vk-text-secondary)', ':hover': 'var(--vk-text-primary)' },
    borderColor: 'transparent',
  },
  danger: {
    backgroundColor: { default: 'transparent', ':hover': 'var(--vk-surface-3)' },
    color: 'var(--vk-critical)', borderColor: 'var(--vk-critical)',
  },
  field: {
    display: 'block', width: '100%', minHeight: { default: 38, '@media (pointer: coarse)': 44 }, paddingBlock: 7, paddingInline: 12,
    borderWidth: 1, borderStyle: 'solid', borderColor: { default: 'var(--vk-border-control)', ':hover': 'var(--vk-border-strong)' },
    borderRadius: 'var(--vk-radius-2)', backgroundColor: 'var(--vk-bg)', color: 'var(--vk-text-primary)',
    fontVariantNumeric: 'tabular-nums', fontSize: 13, minWidth: 0, transition: 'border-color 150ms',
  },
  invalid: { borderColor: 'var(--vk-critical)' },
  fieldGroup: { display: 'grid', gap: 6, minWidth: 0 },
  fieldLabel: { fontWeight: 500, fontSize: 13, color: 'var(--vk-text-primary)' },
  fieldWithAction: { display: 'grid', gridTemplateColumns: 'minmax(0, 1fr) auto', gap: 6 },
  panel: {
    display: 'grid', gap: 16, alignContent: 'start', minWidth: 0,
    backgroundColor: 'var(--vk-surface-1)', borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-subtle)',
    borderRadius: 'var(--vk-radius-3)', padding: { default: 16, '@media (min-width: 1024px)': 20 },
  },
  emphasized: { borderColor: 'var(--vk-border-strong)' },
  panelHead: { display: 'flex', flexWrap: 'wrap', justifyContent: 'space-between', alignItems: 'flex-start', gap: 12 },
  panelTitles: { display: 'grid', gap: 2, minWidth: 0 },
  badge: { display: 'inline-flex', alignItems: 'center', gap: 6, fontSize: 12, fontWeight: 500, lineHeight: '18px' },
  dot: { width: 8, height: 8, borderRadius: 'var(--vk-radius-pill)', flexShrink: 0, display: 'inline-block' },
  dotSuccess: { backgroundColor: 'var(--vk-success-fill)' },
  dotWarning: { backgroundColor: 'var(--vk-warning-fill)' },
  dotCritical: { backgroundColor: 'var(--vk-critical-fill)' },
  dotNeutral: { backgroundColor: 'var(--vk-unknown)' },
  dotInfo: { backgroundColor: 'var(--vk-info)' },
  success: { color: 'var(--vk-success)' },
  warning: { color: 'var(--vk-warning)' },
  critical: { color: 'var(--vk-critical)' },
  neutral: { color: 'var(--vk-text-secondary)' },
  info: { color: 'var(--vk-info)' },
  state: { display: 'flex', alignItems: 'flex-start', gap: 12, color: 'var(--vk-text-secondary)' },
  stateIcon: { flexShrink: 0, marginTop: 2 },
  skeleton: { minHeight: 10, borderRadius: 'var(--vk-radius-pill)', backgroundColor: 'var(--vk-surface-3)', width: '62%', marginBlock: 4 },
  skeletonShort: { width: '38%' },
  brand: {
    display: 'inline-flex', alignItems: 'center', gap: 8, fontSize: 19, fontWeight: 700,
    letterSpacing: '-0.04em', lineHeight: '24px', color: 'var(--vk-text-primary)',
  },
  mark: {
    display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
    width: 24, height: 24, flexShrink: 0,
  },
  dialog: {
    backgroundColor: 'var(--vk-surface-1)', color: 'var(--vk-text-primary)',
    borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-strong)',
    borderRadius: 'var(--vk-radius-3)', width: 'min(440px, calc(100% - 32px))', padding: 24,
    boxShadow: 'var(--vk-shadow-md)',
  },
  link: {
    display: 'inline-flex', alignItems: 'center', gap: 4, minHeight: { default: 28, '@media (pointer: coarse)': 44 },
    color: { default: 'var(--vk-text-secondary)', ':hover': 'var(--vk-text-primary)' },
    fontSize: 13, fontWeight: 500, textDecoration: 'none', transition: 'color 150ms',
  },
  source: {
    display: 'inline-flex', alignItems: 'center', gap: 5, fontSize: 11, fontWeight: 500,
    color: 'var(--vk-text-secondary)', backgroundColor: 'var(--vk-surface-2)',
    borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-subtle)',
    borderRadius: 'var(--vk-radius-1)', paddingBlock: 3, paddingInline: 8,
  },
})

type Tone = 'success' | 'warning' | 'critical' | 'neutral' | 'info'
type Variant = 'primary' | 'outline' | 'quiet' | 'danger'

const dotTone = {
  success: ui.dotSuccess,
  warning: ui.dotWarning,
  critical: ui.dotCritical,
  neutral: ui.dotNeutral,
  info: ui.dotInfo,
} as const

export function StatusDot({ tone }: { tone: Tone }) {
  return <span aria-hidden {...stylex.props(ui.dot, dotTone[tone])} />
}

export function Button({ variant = 'outline', busy, children, disabled, type = 'button', ...props }: ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: Variant
  busy?: boolean
}) {
  return (
    <button {...stylex.props(ui.button, ui[variant])} {...props} type={type} disabled={busy || disabled} aria-busy={busy || undefined}>
      {busy && <LoaderCircle size={15} aria-hidden />}
      {children}
    </button>
  )
}

export function Brand({ markOnly = false }: { markOnly?: boolean }) {
  return (
    <span {...stylex.props(ui.brand)} role={markOnly ? 'img' : undefined} aria-label={markOnly ? 'Vektor' : undefined}>
      <span {...stylex.props(ui.mark)} aria-hidden>
        <svg viewBox="16 14 100 100" width="24" height="24" fill="none">
          <path fill="currentColor" d="M20 37H33.5L55 72.8L76.5 37H93.5L100.7 49L63 110.5L56.5 99.7L88 47.5H82.5L54.5 94L20 37Z" />
          <path fill="var(--vk-success)" d="M85.5 33.5L111.5 18L101.5 43.5L97 34.5Z" />
        </svg>
      </span>
      {!markOnly && 'Vektor'}
    </span>
  )
}

export function TextField({ label, hint, error, action, ...props }: ComponentPropsWithRef<'input'> & {
  label: string
  hint?: string
  error?: string
  action?: ReactNode
}) {
  const generatedId = useId()
  const id = props.id ?? generatedId
  const input = <input {...stylex.props(ui.field, Boolean(error) && ui.invalid)} {...props} id={id} aria-invalid={error ? true : undefined} aria-describedby={hint || error ? id + '-hint' : undefined} />
  return (
    <div {...stylex.props(ui.fieldGroup)}>
      <label htmlFor={id} {...stylex.props(ui.fieldLabel)}>{label}</label>
      {action ? <div {...stylex.props(ui.fieldWithAction)}>{input}{action}</div> : input}
      {(error || hint) && <span id={id + '-hint'} {...stylex.props(layout.small, Boolean(error) && ui.critical)}>{error ?? hint}</span>}
    </div>
  )
}

export function SelectField({ label, children, ...props }: SelectHTMLAttributes<HTMLSelectElement> & { label: string }) {
  return (
    <label {...stylex.props(ui.fieldGroup)}>
      <span {...stylex.props(ui.fieldLabel)}>{label}</span>
      <select {...stylex.props(ui.field)} {...props}>{children}</select>
    </label>
  )
}

export function Panel({ title, subtitle, action, children, emphasized }: {
  title?: string
  subtitle?: ReactNode
  action?: ReactNode
  children: ReactNode
  emphasized?: boolean
}) {
  const id = useId()
  return (
    <section {...stylex.props(ui.panel, emphasized && ui.emphasized)} aria-labelledby={title ? id : undefined}>
      {(title || action) && (
        <div {...stylex.props(ui.panelHead)}>
          {title && (
            <div {...stylex.props(ui.panelTitles)}>
              <h2 id={id} {...stylex.props(layout.sectionTitle)}>{title}</h2>
              {subtitle && <p {...stylex.props(layout.small)}>{subtitle}</p>}
            </div>
          )}
          {action}
        </div>
      )}
      {children}
    </section>
  )
}

export function Badge({ tone, icon, children, announce }: { tone: Tone; icon?: ReactNode; children: ReactNode; announce?: boolean }) {
  return <span {...stylex.props(ui.badge, ui[tone])} role={announce ? 'status' : undefined}>{icon}{children}</span>
}

export function Loading({ label = 'Carregando dados…' }: { label?: string }) {
  return (
    <div role="status" {...stylex.props(layout.compact, layout.small)}>
      <span>{label}</span>
      <span aria-hidden {...stylex.props(ui.skeleton)} />
      <span aria-hidden {...stylex.props(ui.skeleton, ui.skeletonShort)} />
    </div>
  )
}

export function Empty({ children }: { children: ReactNode }) {
  return <div {...stylex.props(ui.state)}><Box aria-hidden size={16} {...stylex.props(ui.stateIcon)} /><p>{children}</p></div>
}

export function ErrorState({ error, retry, title = 'Não foi possível carregar os dados' }: { error: unknown; retry?: () => void; title?: string }) {
  const api = error instanceof ApiError ? error : undefined
  const detail = api?.code === 'inventory_unavailable' ? 'O daemon ainda está descobrindo o host.'
    : api?.status === 403 ? 'O servidor recusou o acesso.'
    : api?.status === 429 ? 'O servidor atingiu o limite de requisições. Tente em instantes.'
    : api?.status ? 'O servidor retornou um erro.'
    : 'Verifique a conexão e tente novamente.'
  return (
    <div {...stylex.props(ui.state)}>
      <AlertCircle aria-hidden size={17} {...stylex.props(ui.stateIcon, ui.critical)} />
      <div {...stylex.props(layout.compact)}>
        <p><strong>{title}</strong></p>
        <p>{detail} {api && <span {...stylex.props(layout.technical)}>({api.status || 'rede'} · {api.code})</span>}</p>
        {retry && <div><Button onClick={retry}>Tentar novamente</Button></div>}
      </div>
    </div>
  )
}

export function DetailLink({ to, children }: { to: string; children: ReactNode }) {
  return <Link {...stylex.props(ui.link)} to={to}>{children}<ArrowUpRight size={14} aria-hidden /></Link>
}

export function Modal({ title, open, onClose, children, preventClose }: { title: string; open: boolean; onClose: () => void; children: ReactNode; preventClose?: boolean }) {
  const dialog = useRef<HTMLDialogElement>(null)
  const id = useId()
  useEffect(() => {
    const element = dialog.current
    if (open && element && !element.open) element.showModal()
    if (!open && element?.open) element.close()
  }, [open])
  return (
    <dialog {...stylex.props(ui.dialog)} ref={dialog} aria-labelledby={id} onCancel={event => { if (preventClose) event.preventDefault(); else onClose() }}>
      <div {...stylex.props(layout.stack)}>
        <div {...stylex.props(layout.split)}>
          <h2 {...stylex.props(layout.sectionTitle)} id={id}>{title}</h2>
          <Button variant="quiet" disabled={preventClose} onClick={onClose} aria-label="Fechar"><X size={18} aria-hidden /></Button>
        </div>
        {children}
      </div>
    </dialog>
  )
}

export function Source({ children }: { children: ReactNode }) {
  return <span {...stylex.props(ui.source)}><Database size={12} aria-hidden />{children}</span>
}
