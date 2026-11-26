import * as stylex from '@stylexjs/stylex'
import { useQuery } from '@tanstack/react-query'
import { Check, Copy, FileText, RefreshCw, X } from 'lucide-react'
import { useEffect, useId, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { Badge, Button, Empty, ErrorState, Loading, StatusDot, layout } from '#/components/ui/ui'
import type { DiagnosticCheck, DiagnosticStatus } from '#/lib/api/client'
import { useApi, useDiagnostics, useHealth } from '#/lib/api/queries'
import { dateTime } from '#/features/metrics/format'
import { usePeriodLink } from '#/features/metrics/range'

const s = stylex.create({
  checkItem: {
    display: 'grid', gap: 6, padding: 12, borderRadius: 'var(--vk-radius-2)',
    backgroundColor: 'var(--vk-surface-2)', borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-subtle)',
  },
  checkWarn: { borderColor: 'var(--vk-warning)' },
  checkFail: { borderColor: 'var(--vk-critical)' },
  checkHead: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8, flexWrap: 'wrap' },
  checkTitle: { display: 'inline-flex', alignItems: 'center', gap: 8, fontSize: 13, fontWeight: 600, color: 'var(--vk-text-primary)' },
  checkDetail: { fontSize: 12, color: 'var(--vk-text-secondary)', fontFamily: 'var(--vk-mono)', overflowWrap: 'anywhere' },
  checkHint: { fontSize: 12, color: 'var(--vk-text-secondary)' },
  cmdWrap: {
    display: 'grid', gridTemplateColumns: 'minmax(0, 1fr) auto', alignItems: 'center', gap: 6,
    marginTop: 2, paddingBlock: 5, paddingInline: 8, borderRadius: 'var(--vk-radius-1)',
    backgroundColor: 'var(--vk-bg)', borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-subtle)',
  },
  cmdText: {
    margin: 0, fontFamily: 'var(--vk-mono)', fontSize: 11, lineHeight: '16px',
    color: 'var(--vk-text-primary)', overflowX: 'auto', whiteSpace: 'pre-wrap', overflowWrap: 'anywhere',
  },
  copyBtn: {
    display: 'inline-flex', alignItems: 'center', gap: 4, minHeight: 26, paddingInline: 8,
    borderRadius: 'var(--vk-radius-1)', borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-control)',
    backgroundColor: { default: 'var(--vk-surface-2)', ':hover': 'var(--vk-surface-3)' },
    color: 'var(--vk-text-primary)', fontSize: 11, fontWeight: 500, cursor: 'pointer', flexShrink: 0,
  },
  logBox: {
    display: 'grid', gap: 8, padding: 10, borderRadius: 'var(--vk-radius-2)',
    backgroundColor: 'var(--vk-bg)', borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-subtle)',
  },
  logHead: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8, flexWrap: 'wrap' },
  logFilters: { display: 'flex', alignItems: 'center', gap: 4, flexWrap: 'wrap' },
  pill: {
    minHeight: 26, paddingInline: 8, borderRadius: 'var(--vk-radius-1)', borderWidth: 1, borderStyle: 'solid',
    borderColor: 'var(--vk-border-subtle)', backgroundColor: 'var(--vk-surface-1)', color: 'var(--vk-text-secondary)',
    fontSize: 11, fontWeight: 500, cursor: 'pointer',
  },
  pillActive: {
    backgroundColor: 'var(--vk-surface-3)', color: 'var(--vk-text-primary)', borderColor: 'var(--vk-border-strong)', fontWeight: 600,
  },
  logList: {
    listStyle: 'none', margin: 0, padding: 0, display: 'grid', gap: 4,
    maxHeight: 240, overflowY: 'auto', fontFamily: 'var(--vk-mono)', fontSize: 11, lineHeight: '16px',
  },
  logRow: {
    display: 'grid', gridTemplateColumns: 'auto minmax(0, 1fr)', gap: 8,
    paddingBlock: 3, borderBlockEndWidth: 1, borderBlockEndStyle: 'solid', borderBlockEndColor: 'var(--vk-border-subtle)',
  },
  logTime: { color: 'var(--vk-text-muted)', whiteSpace: 'nowrap' },
  logErr: { color: 'var(--vk-critical)', overflowWrap: 'anywhere' },
  logWarn: { color: 'var(--vk-warning)', overflowWrap: 'anywhere' },
  logInfo: { color: 'var(--vk-text-primary)', overflowWrap: 'anywhere' },
  drawer: {
    position: 'fixed', inset: '0 0 0 auto', height: '100dvh', maxHeight: '100dvh',
    width: 'min(480px, 95vw)', maxWidth: 'none', margin: 0,
    borderWidth: 0, borderInlineStartWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-strong)',
    backgroundColor: 'var(--vk-surface-1)', color: 'var(--vk-text-primary)', padding: 20,
    overflowY: 'auto',
  },
  drawerBody: { display: 'grid', gap: 16, alignContent: 'start' },
  drawerHead: {
    display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 12,
    paddingBlockEnd: 12, borderBlockEndWidth: 1, borderBlockEndStyle: 'solid', borderBlockEndColor: 'var(--vk-border-subtle)',
  },
  drawerFooter: {
    display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, flexWrap: 'wrap',
    paddingBlockStart: 12, borderBlockStartWidth: 1, borderBlockStartStyle: 'solid', borderBlockStartColor: 'var(--vk-border-subtle)',
  },
  footerLink: {
    fontSize: 13, fontWeight: 500, color: 'var(--vk-info)', textDecoration: 'none',
  },
})

export function diagTone(status: DiagnosticStatus): 'success' | 'warning' | 'critical' {
  if (status === 'fail') return 'critical'
  if (status === 'warn') return 'warning'
  return 'success'
}

export function serviceUnitForCheck(check: DiagnosticCheck): string | undefined {
  switch (check.id) {
    case 'incus':
      return 'incus.service'
    case 'ctl':
      return 'vektor-ctl.service'
    case 'dns':
      return 'systemd-resolved.service'
    case 'clock':
      if (check.detail?.includes('chronyd.service')) return 'chronyd.service'
      if (check.detail?.includes('chrony.service')) return 'chrony.service'
      return 'systemd-timesyncd.service'
    case 'database':
      return 'vektord.service'
    default:
      return undefined
  }
}

export function CommandBlock({ command }: { command: string }) {
  const [copied, setCopied] = useState(false)
  const timer = useRef<number | undefined>(undefined)
  useEffect(() => () => { if (timer.current) clearTimeout(timer.current) }, [])

  async function copy() {
    try {
      await navigator.clipboard.writeText(command)
      setCopied(true)
      if (timer.current) clearTimeout(timer.current)
      timer.current = window.setTimeout(() => setCopied(false), 1800)
    } catch {
      setCopied(false)
    }
  }

  return (
    <div {...stylex.props(s.cmdWrap)}>
      <pre {...stylex.props(s.cmdText)}>{command}</pre>
      <button type="button" {...stylex.props(s.copyBtn)} onClick={() => { void copy() }} aria-label="Copiar comando">
        {copied ? <Check size={12} aria-hidden /> : <Copy size={12} aria-hidden />}
        <span>{copied ? 'Copiado' : 'Copiar'}</span>
      </button>
    </div>
  )
}

export function ServiceLogViewer({ unit, defaultPriority = 'warning' }: {
  unit: string
  defaultPriority?: 'all' | 'warning' | 'err'
}) {
  const api = useApi()
  const [priority, setPriority] = useState<'all' | 'warning' | 'err'>(defaultPriority)
  const query = useQuery({
    queryKey: ['diag-logs', unit, priority],
    queryFn: ({ signal }) => api.serviceLogs(unit, 40, priority, signal),
    staleTime: 10_000,
    retry: false,
  })

  return (
    <div {...stylex.props(s.logBox)}>
      <div {...stylex.props(s.logHead)}>
        <span {...stylex.props(layout.technical)}>{unit}</span>
        <div {...stylex.props(s.logFilters)}>
          {([
            ['warning', 'Avisos e erros'],
            ['err', 'Só erros'],
            ['all', 'Todos'],
          ] as const).map(([key, label]) => (
            <button
              key={key}
              type="button"
              {...stylex.props(s.pill, priority === key && s.pillActive)}
              onClick={() => setPriority(key)}
            >
              {label}
            </button>
          ))}
          <button
            type="button"
            {...stylex.props(s.pill)}
            onClick={() => { void query.refetch() }}
            disabled={query.isFetching}
            aria-label={`Atualizar logs de ${unit}`}
          >
            <RefreshCw size={11} aria-hidden />
          </button>
        </div>
      </div>
      {query.isPending && <Loading label={`Carregando logs de ${unit}…`} />}
      {query.isError && <ErrorState error={query.error} retry={() => { void query.refetch() }} />}
      {query.data && query.data.length === 0 && (
        <Empty>Nenhum registro encontrado para este filtro em {unit}.</Empty>
      )}
      {query.data && query.data.length > 0 && (
        <ul {...stylex.props(s.logList)}>
          {query.data.map((entry, idx) => (
            <li key={`${entry.timestamp_ms}-${idx}`} {...stylex.props(s.logRow)}>
              <span {...stylex.props(s.logTime)}>{dateTime(entry.timestamp_ms)}</span>
              <span {...stylex.props(entry.priority === 'err' ? s.logErr : entry.priority === 'warning' ? s.logWarn : s.logInfo)}>
                {entry.message}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

export function DiagnosticCheckCard({ check }: { check: DiagnosticCheck }) {
  const [showLogs, setShowLogs] = useState(false)
  const unit = serviceUnitForCheck(check)

  return (
    <div
      {...stylex.props(
        s.checkItem,
        check.status === 'warn' && s.checkWarn,
        check.status === 'fail' && s.checkFail,
      )}
    >
      <div {...stylex.props(s.checkHead)}>
        <span {...stylex.props(s.checkTitle)}>
          <StatusDot tone={diagTone(check.status)} />
          {check.title}
        </span>
        <span {...stylex.props(layout.small)}>{check.summary}</span>
      </div>
      {check.detail && <div {...stylex.props(s.checkDetail)}>{check.detail}</div>}
      {check.hint && <div {...stylex.props(s.checkHint)}>{check.hint}</div>}
      {check.command && check.status !== 'ok' && <CommandBlock command={check.command} />}
      {unit && (check.status !== 'ok' || showLogs) && (
        <div {...stylex.props(layout.compact)}>
          <div>
            <Button variant="quiet" onClick={() => setShowLogs(v => !v)}>
              <FileText size={13} aria-hidden />
              {showLogs ? `Ocultar logs (${unit})` : `Ver logs (${unit})`}
            </Button>
          </div>
          {showLogs && <ServiceLogViewer unit={unit} />}
        </div>
      )}
    </div>
  )
}

export function DiagnosticsDrawer({ open, onClose }: { open: boolean; onClose: () => void }) {
  const dialogRef = useRef<HTMLDialogElement>(null)
  const titleId = useId()
  const [showAll, setShowAll] = useState(false)
  const diag = useDiagnostics()
  const health = useHealth()
  const link = usePeriodLink()

  useEffect(() => {
    const dialog = dialogRef.current
    if (open && dialog && !dialog.open) dialog.showModal()
    if (!open && dialog?.open) dialog.close()
  }, [open])

  const checks = diag.data?.checks ?? []
  const issues = checks.filter(c => c.status !== 'ok')
  const visibleChecks = showAll || issues.length === 0 ? checks : issues

  return (
    <dialog
      ref={dialogRef}
      {...stylex.props(s.drawer)}
      aria-labelledby={titleId}
      onCancel={event => {
        event.preventDefault()
        onClose()
      }}
      onClick={event => {
        if (event.target === dialogRef.current) onClose()
      }}
    >
      {open && (
        <div {...stylex.props(s.drawerBody)}>
          <div {...stylex.props(s.drawerHead)}>
            <div {...stylex.props(layout.compact)}>
              <h2 id={titleId} {...stylex.props(layout.sectionTitle)}>Diagnóstico do sistema</h2>
              <p {...stylex.props(layout.small)}>
                {issues.length === 0
                  ? 'Nenhum problema ativo detectado.'
                  : `${issues.length} ${issues.length === 1 ? 'item requer atenção' : 'itens requerem atenção'}.`}
              </p>
            </div>
            <div {...stylex.props(layout.row)}>
              <Button variant="quiet" busy={diag.isFetching} onClick={() => { void diag.refetch(); void health.refetch() }} aria-label="Reexecutar diagnóstico">
                <RefreshCw size={15} aria-hidden />
              </Button>
              <Button variant="quiet" onClick={onClose} aria-label="Fechar diagnóstico">
                <X size={18} aria-hidden />
              </Button>
            </div>
          </div>

          {diag.isPending && <Loading label="Executando verificações…" />}
          {diag.isError && <ErrorState error={diag.error} retry={() => { void diag.refetch() }} />}

          {diag.data && (
            <>
              {issues.length > 0 && (
                <div {...stylex.props(layout.split)}>
                  <Badge tone={issues.some(i => i.status === 'fail') ? 'critical' : 'warning'} icon={<StatusDot tone={issues.some(i => i.status === 'fail') ? 'critical' : 'warning'} />}>
                    {showAll ? `Exibindo todos (${checks.length})` : `Exibindo apenas avisos/falhas (${issues.length})`}
                  </Badge>
                  <Button variant="quiet" onClick={() => setShowAll(v => !v)}>
                    {showAll ? 'Só problemas' : `Ver todos (${checks.length})`}
                  </Button>
                </div>
              )}

              <div {...stylex.props(layout.compact)}>
                {visibleChecks.map(check => (
                  <DiagnosticCheckCard key={check.id} check={check} />
                ))}
              </div>
            </>
          )}

          <div {...stylex.props(s.drawerFooter)}>
            {diag.data?.generated_at && (
              <span {...stylex.props(layout.small)}>Atualizado: {dateTime(diag.data.generated_at)}</span>
            )}
            <Link to={link('/overview') + '#diagnostico'} onClick={onClose} {...stylex.props(s.footerLink)}>
              Abrir painel completo no Overview
            </Link>
          </div>
        </div>
      )}
    </dialog>
  )
}
