import * as stylex from '@stylexjs/stylex'
import { AlertCircle, ArrowUpRight, Box, Database, LoaderCircle, MoveUpRight, X } from 'lucide-react'
import { Link } from 'react-router-dom'
import { useEffect, useId, useRef } from 'react'
import type { ButtonHTMLAttributes, ComponentPropsWithRef, ReactNode, SelectHTMLAttributes } from 'react'
import { ApiError } from '../../lib/api/client'

export const layout = stylex.create({
  page: { display: 'grid', gap: 24, minWidth: 0 },
  stack: { display: 'grid', gap: 16, minWidth: 0 },
  compact: { display: 'grid', gap: 8, minWidth: 0 },
  row: { display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 12, minWidth: 0 },
  split: { display: 'flex', flexWrap: 'wrap', justifyContent: 'space-between', alignItems: 'center', gap: 12 },
  text: { color: 'var(--vk-text-secondary)', overflowWrap: 'anywhere' },
  small: { color: 'var(--vk-text-secondary)', fontSize: 13, lineHeight: '19px', overflowWrap: 'anywhere' },
  technical: { fontFamily: 'var(--vk-mono)', fontSize: 12, lineHeight: '16px', fontWeight: 500, overflowWrap: 'anywhere' },
  muted: { color: 'var(--vk-text-muted)' },
  grid2: { display: 'grid', gap: 16, gridTemplateColumns: { default: 'minmax(0, 1fr)', '@media (min-width: 1024px)': 'repeat(2, minmax(0, 1fr))' } },
  grid4: { display: 'grid', gap: 16, gridTemplateColumns: { default: 'minmax(0, 1fr)', '@media (min-width: 560px)': 'repeat(2, minmax(0, 1fr))', '@media (min-width: 1440px)': 'repeat(4, minmax(0, 1fr))' } },
  sectionTitle: { fontSize: 18, lineHeight: '26px', fontWeight: 600 },
  pageTitle: { fontSize: 28, lineHeight: '34px', fontWeight: 600, letterSpacing: '-0.025em', overflowWrap: 'anywhere' },
})

const ui = stylex.create({
  button: {
    display: 'inline-flex', alignItems: 'center', justifyContent: 'center', gap: 8, paddingInline: 12,
    minHeight: { default: 40, '@media (pointer: coarse)': 44 }, borderWidth: 1, borderStyle: 'solid',
    fontWeight: 600, borderRadius: 'var(--vk-radius-0)', cursor: 'pointer',
    textDecoration: 'none', transition: 'background-color 150ms, border-color 150ms',
    opacity: { default: 1, ':disabled': 0.6 },
  },
  primary: {
    backgroundColor: { default: 'var(--vk-accent)', ':hover': 'var(--vk-success)' },
    color: 'var(--vk-accent-ink)', borderColor: 'var(--vk-accent)',
  },
  outline: {
    backgroundColor: { default: 'var(--vk-surface-2)', ':hover': 'var(--vk-surface-3)' },
    color: 'var(--vk-text-primary)', borderColor: 'var(--vk-border-control)',
  },
  quiet: {
    backgroundColor: { default: 'transparent', ':hover': 'var(--vk-surface-3)' },
    color: 'var(--vk-text-secondary)', borderColor: 'transparent',
  },
  danger: {
    backgroundColor: { default: 'transparent', ':hover': 'var(--vk-surface-3)' },
    color: 'var(--vk-critical)', borderColor: 'var(--vk-critical)',
  },
  field: {
    display: 'block', width: '100%', minHeight: { default: 40, '@media (pointer: coarse)': 44 }, padding: '8px 12px',
    borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-control)', borderRadius: 'var(--vk-radius-1)',
    backgroundColor: 'var(--vk-bg)', color: 'var(--vk-text-primary)',
    fontVariantNumeric: 'tabular-nums', minWidth: 0,
  },
  invalid: { borderColor: 'var(--vk-critical)' },
  fieldGroup: { display: 'grid', gap: 8, minWidth: 0 },
  fieldLabel: { fontWeight: 600 },
  fieldWithAction: { display: 'grid', gridTemplateColumns: 'minmax(0, 1fr) auto', gap: 4 },
  panel: { display: 'grid', gap: 16, alignContent: 'start', minWidth: 0, backgroundColor: 'var(--vk-surface-1)', borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-subtle)', padding: { default: 16, '@media (min-width: 1024px)': 20 } },
  emphasized: { borderWidth: 2, borderStyle: 'solid', borderColor: 'var(--vk-border-strong)' },
  badge: { display: 'inline-flex', alignItems: 'center', gap: 7, fontSize: 12, fontWeight: 600, lineHeight: '18px' },
  success: { color: 'var(--vk-success)' },
  warning: { color: 'var(--vk-warning)' },
  critical: { color: 'var(--vk-critical)' },
  neutral: { color: 'var(--vk-text-secondary)' },
  info: { color: 'var(--vk-info)' },
  state: { display: 'flex', alignItems: 'flex-start', gap: 12, color: 'var(--vk-text-secondary)' },
  stateIcon: { flexShrink: 0, marginTop: 2 },
  skeleton: { minHeight: 12, backgroundColor: 'var(--vk-surface-3)', width: '62%', marginBlock: 4 },
  skeletonShort: { width: '38%' },
  brand: { display: 'inline-flex', alignItems: 'center', gap: 10, fontSize: 21, fontWeight: 700, letterSpacing: '-0.045em', lineHeight: '26px' },
  mark: { display: 'grid', placeItems: 'center', width: 32, height: 32, backgroundColor: 'var(--vk-accent)', color: 'var(--vk-accent-ink)', flexShrink: 0 },
  dialog: { backgroundColor: 'var(--vk-surface-1)', color: 'var(--vk-text-primary)', borderWidth: 2, borderStyle: 'solid', borderColor: 'var(--vk-border-strong)', borderRadius: 'var(--vk-radius-2)', width: 'min(440px, calc(100% - 32px))', padding: 24 },
  link: {
    display: 'inline-flex', alignItems: 'center', gap: 6, minHeight: { default: 24, '@media (pointer: coarse)': 44 },
    color: 'var(--vk-accent)', textUnderlineOffset: 4,
  },
})

type Tone = 'success' | 'warning' | 'critical' | 'neutral' | 'info'
type Variant = 'primary' | 'outline' | 'quiet' | 'danger'

export function Button({ variant = 'outline', busy, children, disabled, type = 'button', ...props }: ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: Variant
  busy?: boolean
}) {
  return (
    <button {...stylex.props(ui.button, ui[variant])} {...props} type={type} disabled={busy || disabled} aria-busy={busy || undefined}>
      {busy && <LoaderCircle size={16} aria-hidden />}
      {children}
    </button>
  )
}

export function Brand({ markOnly = false }: { markOnly?: boolean }) {
  return (
    <span {...stylex.props(ui.brand)} role={markOnly ? 'img' : undefined} aria-label={markOnly ? 'Vektor' : undefined}>
      <span {...stylex.props(ui.mark)}><MoveUpRight size={21} strokeWidth={2.5} aria-hidden /></span>
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

export function Panel({ title, action, children, emphasized }: { title?: string; action?: ReactNode; children: ReactNode; emphasized?: boolean }) {
  const id = useId()
  return (
    <section {...stylex.props(ui.panel, emphasized && ui.emphasized)} aria-labelledby={title ? id : undefined}>
      {title && <div {...stylex.props(layout.split)}><h2 id={id} {...stylex.props(layout.sectionTitle)}>{title}</h2>{action}</div>}
      {children}
    </section>
  )
}

export function Badge({ tone, icon, children, announce }: { tone: Tone; icon: ReactNode; children: ReactNode; announce?: boolean }) {
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
  return <span {...stylex.props(ui.badge, ui.neutral)}><Database size={13} aria-hidden />{children}</span>
}
