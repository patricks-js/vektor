import * as stylex from '@stylexjs/stylex'
import { ChevronLeft, ChevronRight, FileText, Play, RefreshCw, RotateCw, Skull, Square, X } from 'lucide-react'
import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Badge, Button, Empty, ErrorState, Loading, Modal, Panel, SelectField, StatusDot, TextField, layout, tableStyles } from '#/components/ui/ui'
import type { ProcessInfo, ServiceAction } from '#/lib/api/client'
import { useApi } from '#/lib/api/queries'
import { useSession } from '#/features/auth/session'
import { dateTime, formatValue } from '#/features/metrics/format'

const s = stylex.create({
  wideTable: { minWidth: 620 },
  center: { textAlign: 'center' },
  num: { fontFamily: 'var(--vk-mono)', fontVariantNumeric: 'tabular-nums', whiteSpace: 'nowrap' },
  cmd: { fontFamily: 'var(--vk-mono)', fontSize: 11, color: 'var(--vk-text-muted)', maxWidth: 360, overflowWrap: 'anywhere' },
  actions: { display: 'inline-flex', alignItems: 'center', gap: 6, flexWrap: 'nowrap' },
  logBox: {
    maxHeight: 420, overflowY: 'auto', borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-subtle)',
    borderRadius: 'var(--vk-radius-2)', backgroundColor: 'var(--vk-bg)', padding: 12, display: 'grid', gap: 6,
    fontFamily: 'var(--vk-mono)', fontSize: 12, lineHeight: '18px',
  },
  logLine: { display: 'grid', gridTemplateColumns: 'auto auto minmax(0, 1fr)', gap: '4px 10px', alignItems: 'baseline' },
  logTime: { color: 'var(--vk-text-muted)', whiteSpace: 'nowrap', fontSize: 11 },
  logMsg: { color: 'var(--vk-text-primary)', overflowWrap: 'anywhere' },
  prioErr: { color: 'var(--vk-critical)', fontWeight: 600 },
  prioWarn: { color: 'var(--vk-warning)', fontWeight: 600 },
  prioInfo: { color: 'var(--vk-text-secondary)' },
})

const pageSize = 15

const actionLabels: Record<ServiceAction, string> = {
  start: 'Iniciar',
  stop: 'Parar',
  restart: 'Reiniciar',
  reload: 'Recarregar',
  enable: 'Habilitar',
  disable: 'Desabilitar',
}

function ServiceLogsPanel({ unit, onClose }: { unit: string; onClose: () => void }) {
  const api = useApi()
  const [priority, setPriority] = useState<'all' | 'info' | 'warning' | 'err'>('all')
  const [lines, setLines] = useState(100)
  const query = useQuery({
    queryKey: ['service-logs', unit, lines, priority],
    queryFn: ({ signal }) => api.serviceLogs(unit, lines, priority, signal),
    staleTime: 10_000,
  })

  return (
    <Panel title={`Logs · ${unit}`} subtitle="Entradas recentes do journald" emphasized action={
      <div {...stylex.props(layout.row)}>
        <Button variant="outline" onClick={() => { void query.refetch() }} busy={query.isFetching}>
          <RefreshCw size={14} aria-hidden /> Atualizar
        </Button>
        <Button variant="quiet" aria-label="Fechar logs" onClick={onClose}><X size={16} aria-hidden /></Button>
      </div>
    }>
      <div {...stylex.props(tableStyles.filters)}>
        <SelectField label="Severidade mínima" value={priority} onChange={e => setPriority(e.currentTarget.value as 'all' | 'info' | 'warning' | 'err')}>
          <option value="all">Todas</option>
          <option value="info">Info</option>
          <option value="warning">Avisos e erros</option>
          <option value="err">Apenas erros</option>
        </SelectField>
        <SelectField label="Linhas" value={String(lines)} onChange={e => setLines(Number(e.currentTarget.value))}>
          <option value="50">50 linhas</option>
          <option value="100">100 linhas</option>
          <option value="250">250 linhas</option>
        </SelectField>
      </div>
      {query.isPending && <Loading label="Consultando journald…" />}
      {query.isError && <ErrorState error={query.error} retry={() => { void query.refetch() }} title="Logs indisponíveis" />}
      {query.data && (query.data.length === 0 ? <Empty>Nenhuma entrada encontrada para os filtros selecionados.</Empty> : (
        <div {...stylex.props(s.logBox)} role="log" aria-label={`Logs de ${unit}`}>
          {query.data.map((entry, idx) => (
            <div key={`${entry.timestamp_ms}-${idx}`} {...stylex.props(s.logLine)}>
              <span {...stylex.props(s.logTime)}>{entry.timestamp_ms ? dateTime(entry.timestamp_ms) : '—'}</span>
              <span {...stylex.props(entry.priority === 'err' ? s.prioErr : entry.priority === 'warning' ? s.prioWarn : s.prioInfo)}>
                {entry.priority.toUpperCase()}
              </span>
              <span {...stylex.props(s.logMsg)}>{entry.message}</span>
            </div>
          ))}
        </div>
      ))}
    </Panel>
  )
}

export function ServicesView() {
  const api = useApi()
  const { session } = useSession()
  const cache = useQueryClient()
  const [stateFilter, setStateFilter] = useState<'all' | 'active' | 'failed' | 'inactive'>('all')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(0)
  const [selectedUnit, setSelectedUnit] = useState<string>()
  const [pendingAction, setPendingAction] = useState<{ unit: string; action: ServiceAction }>()
  const [busy, setBusy] = useState(false)
  const [feedback, setFeedback] = useState('')
  const [actionError, setActionError] = useState<unknown>()

  const query = useQuery({
    queryKey: ['services', stateFilter],
    queryFn: ({ signal }) => api.services(stateFilter, signal),
    staleTime: 15_000,
    refetchInterval: 30_000,
  })

  const term = search.trim().toLocaleLowerCase()
  const items = (query.data ?? []).filter(svc =>
    `${svc.name} ${svc.description}`.toLocaleLowerCase().includes(term),
  )
  const pages = Math.ceil(items.length / pageSize)
  const currentPage = Math.min(page, Math.max(0, pages - 1))
  const visible = items.slice(currentPage * pageSize, (currentPage + 1) * pageSize)

  async function confirmServiceAction() {
    if (!pendingAction) return
    setBusy(true)
    setActionError(undefined)
    try {
      await api.serviceAction(session, pendingAction.unit, pendingAction.action)
      setFeedback(`${actionLabels[pendingAction.action]} executado em ${pendingAction.unit}.`)
      setPendingAction(undefined)
      await cache.invalidateQueries({ queryKey: ['services'] })
      await cache.invalidateQueries({ queryKey: ['audit'] })
    } catch (err) {
      setActionError(err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div {...stylex.props(layout.stack)}>
      {selectedUnit && <ServiceLogsPanel unit={selectedUnit} onClose={() => setSelectedUnit(undefined)} />}
      <Panel title="Serviços systemd" subtitle="Estado e controle das units via vektor-ctl.socket" action={
        <Button variant="outline" onClick={() => { void query.refetch() }} busy={query.isFetching}>
          <RefreshCw size={14} aria-hidden /> Atualizar
        </Button>
      }>
        <div {...stylex.props(tableStyles.filters)}>
          <TextField label="Buscar serviço" placeholder="Filtrar por nome ou descrição…" value={search} onChange={e => { setSearch(e.currentTarget.value); setPage(0) }} type="search" />
          <SelectField label="Estado" value={stateFilter} onChange={e => { setStateFilter(e.currentTarget.value as 'all' | 'active' | 'failed' | 'inactive'); setPage(0) }}>
            <option value="all">Todos</option>
            <option value="active">Ativos</option>
            <option value="failed">Com falha</option>
            <option value="inactive">Inativos</option>
          </SelectField>
        </div>
        {feedback && <p role="status" {...stylex.props(layout.small)}>{feedback}</p>}
        {query.isPending && <Loading label="Listando serviços…" />}
        {query.isError && <ErrorState error={query.error} retry={() => { void query.refetch() }} title="Não foi possível listar serviços" />}
        {query.data && (visible.length === 0 ? <Empty>Nenhum serviço corresponde aos filtros.</Empty> : <>
          <div tabIndex={0} role="region" aria-label="Tabela de serviços systemd" {...stylex.props(tableStyles.scroll)}>
            <table {...stylex.props(tableStyles.table, s.wideTable)} aria-label="Serviços systemd">
              <thead><tr>
                <th scope="col" {...stylex.props(tableStyles.th)}>Serviço</th>
                <th scope="col" {...stylex.props(tableStyles.th)}>Estado</th>
                <th scope="col" {...stylex.props(tableStyles.th)}>Boot</th>
                <th scope="col" {...stylex.props(tableStyles.th)}>Descrição</th>
                <th scope="col" {...stylex.props(tableStyles.th)}>Ações</th>
              </tr></thead>
              <tbody>
                {visible.map(svc => {
                  const tone = svc.active_state === 'failed' ? 'critical' : svc.active_state === 'active' ? 'success' : 'neutral'
                  const isSelf = svc.name === 'vektord.service'
                  return (
                    <tr key={svc.name} {...stylex.props(tableStyles.row)}>
                      <th scope="row" {...stylex.props(tableStyles.td, s.num)}>{svc.name}</th>
                      <td {...stylex.props(tableStyles.td)}>
                        <Badge tone={tone} icon={<StatusDot tone={tone} />}>
                          {svc.active_state} ({svc.sub_state})
                        </Badge>
                      </td>
                      <td {...stylex.props(tableStyles.td, layout.small)}>{svc.unit_file_state || '—'}</td>
                      <td {...stylex.props(tableStyles.td, layout.small)}>{svc.description || '—'}</td>
                      <td {...stylex.props(tableStyles.td)}>
                        <div {...stylex.props(s.actions)}>
                          <Button variant="outline" title="Reiniciar" aria-label={`Reiniciar ${svc.name}`}
                            onClick={() => { setActionError(undefined); setPendingAction({ unit: svc.name, action: 'restart' }) }}>
                            <RotateCw size={13} aria-hidden />
                          </Button>
                          {svc.active_state === 'active' ? (
                            <Button variant="outline" title={isSelf ? 'Protegido' : 'Parar'} aria-label={`Parar ${svc.name}`} disabled={isSelf}
                              onClick={() => { setActionError(undefined); setPendingAction({ unit: svc.name, action: 'stop' }) }}>
                              <Square size={13} aria-hidden />
                            </Button>
                          ) : (
                            <Button variant="outline" title="Iniciar" aria-label={`Iniciar ${svc.name}`}
                              onClick={() => { setActionError(undefined); setPendingAction({ unit: svc.name, action: 'start' }) }}>
                              <Play size={13} aria-hidden />
                            </Button>
                          )}
                          <Button variant="outline" onClick={() => setSelectedUnit(svc.name)}>
                            <FileText size={13} aria-hidden /> Logs
                          </Button>
                        </div>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
          {pages > 1 && <div {...stylex.props(layout.split)}>
            <p {...stylex.props(layout.small)}>Página {currentPage + 1} de {pages} · {items.length} serviços</p>
            <div {...stylex.props(layout.row)}>
              <Button aria-label="Página anterior" disabled={currentPage === 0} onClick={() => setPage(currentPage - 1)}><ChevronLeft size={16} aria-hidden /></Button>
              <Button aria-label="Próxima página" disabled={currentPage + 1 === pages} onClick={() => setPage(currentPage + 1)}><ChevronRight size={16} aria-hidden /></Button>
            </div>
          </div>}
        </>)}
      </Panel>

      <Modal title={pendingAction ? `${actionLabels[pendingAction.action]} serviço` : 'Confirmar ação'}
        open={Boolean(pendingAction)} preventClose={busy}
        onClose={() => { if (!busy) { setPendingAction(undefined); setActionError(undefined) } }}>
        {pendingAction && <>
          <p {...stylex.props(layout.text)}>
            Deseja executar <strong>{actionLabels[pendingAction.action].toLowerCase()}</strong> em <span {...stylex.props(layout.technical)}>{pendingAction.unit}</span>?
          </p>
          {actionError != null && <ErrorState error={actionError} title="Não foi possível executar a ação" />}
          <div {...stylex.props(layout.row)}>
            <Button disabled={busy} onClick={() => { setPendingAction(undefined); setActionError(undefined) }}>Cancelar</Button>
            <Button variant={pendingAction.action === 'stop' ? 'danger' : 'primary'} busy={busy} onClick={() => { void confirmServiceAction() }}>
              Confirmar
            </Button>
          </div>
        </>}
      </Modal>
    </div>
  )
}

export function ProcessesView() {
  const api = useApi()
  const { session } = useSession()
  const cache = useQueryClient()
  const [sort, setSort] = useState<'cpu' | 'memory'>('cpu')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(0)
  const [target, setTarget] = useState<ProcessInfo>()
  const [busy, setBusy] = useState(false)
  const [feedback, setFeedback] = useState('')
  const [actionError, setActionError] = useState<unknown>()

  const query = useQuery({
    queryKey: ['processes', sort],
    queryFn: ({ signal }) => api.processes(sort, 50, signal),
    staleTime: 4_000,
    refetchInterval: 5_000,
  })

  const term = search.trim().toLocaleLowerCase()
  const items = (query.data ?? []).filter(proc =>
    `${proc.pid} ${proc.name} ${proc.command} ${proc.user}`.toLocaleLowerCase().includes(term),
  )
  const pages = Math.ceil(items.length / pageSize)
  const currentPage = Math.min(page, Math.max(0, pages - 1))
  const visible = items.slice(currentPage * pageSize, (currentPage + 1) * pageSize)

  async function sendSignal(sig: 'TERM' | 'KILL') {
    if (!target) return
    setBusy(true)
    setActionError(undefined)
    try {
      await api.processSignal(session, target.pid, sig)
      setFeedback(`Sinal SIG${sig} enviado para PID ${target.pid} (${target.name}).`)
      setTarget(undefined)
      await cache.invalidateQueries({ queryKey: ['processes'] })
      await cache.invalidateQueries({ queryKey: ['audit'] })
    } catch (err) {
      setActionError(err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <Panel title="Processos ativos" subtitle="Top 50 processos lidos de /proc (atualização a cada 5 s)" action={
        <Button variant="outline" onClick={() => { void query.refetch() }} busy={query.isFetching}>
          <RefreshCw size={14} aria-hidden /> Atualizar
        </Button>
      }>
        <div {...stylex.props(tableStyles.filters)}>
          <TextField label="Buscar processo" placeholder="PID, nome, comando ou usuário…" value={search} onChange={e => { setSearch(e.currentTarget.value); setPage(0) }} type="search" />
          <SelectField label="Ordenar por" value={sort} onChange={e => { setSort(e.currentTarget.value === 'memory' ? 'memory' : 'cpu'); setPage(0) }}>
            <option value="cpu">Uso de CPU</option>
            <option value="memory">Memória (RSS)</option>
          </SelectField>
        </div>
        {feedback && <p role="status" {...stylex.props(layout.small)}>{feedback}</p>}
        {query.isPending && <Loading label="Lendo processos…" />}
        {query.isError && <ErrorState error={query.error} retry={() => { void query.refetch() }} title="Não foi possível listar processos" />}
        {query.data && (visible.length === 0 ? <Empty>Nenhum processo corresponde à busca.</Empty> : <>
          <div tabIndex={0} role="region" aria-label="Tabela de processos" {...stylex.props(tableStyles.scroll)}>
            <table {...stylex.props(tableStyles.table, s.wideTable)} aria-label="Processos ativos">
              <thead><tr>
                <th scope="col" {...stylex.props(tableStyles.th)}>PID</th>
                <th scope="col" {...stylex.props(tableStyles.th)}>Processo</th>
                <th scope="col" {...stylex.props(tableStyles.th)}>Usuário</th>
                <th scope="col" {...stylex.props(tableStyles.th)}>CPU</th>
                <th scope="col" {...stylex.props(tableStyles.th)}>RAM (RSS)</th>
                <th scope="col" {...stylex.props(tableStyles.th, s.center)}>Threads</th>
                <th scope="col" {...stylex.props(tableStyles.th)}>Ações</th>
              </tr></thead>
              <tbody>
                {visible.map(proc => (
                  <tr key={proc.pid} {...stylex.props(tableStyles.row)}>
                    <td {...stylex.props(tableStyles.td, s.num)}>{proc.pid}</td>
                    <th scope="row" {...stylex.props(tableStyles.td)}>
                      <div>{proc.name}</div>
                      <div {...stylex.props(s.cmd)}>{proc.command}</div>
                    </th>
                    <td {...stylex.props(tableStyles.td, layout.small)}>{proc.user || '—'}</td>
                    <td {...stylex.props(tableStyles.td, s.num)}>{formatValue(proc.cpu_percent, '%')}</td>
                    <td {...stylex.props(tableStyles.td, s.num)}>{formatValue(proc.rss_bytes, 'bytes')}</td>
                    <td {...stylex.props(tableStyles.td, s.num, s.center)}>{proc.threads}</td>
                    <td {...stylex.props(tableStyles.td)}>
                      <Button variant="quiet" disabled={proc.pid <= 1} aria-label={`Encerrar processo ${proc.name} (${proc.pid})`}
                        title={proc.pid <= 1 ? 'PID protegido' : 'Enviar sinal'}
                        onClick={() => { setActionError(undefined); setTarget(proc) }}>
                        <Skull size={14} aria-hidden />
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {pages > 1 && <div {...stylex.props(layout.split)}>
            <p {...stylex.props(layout.small)}>Página {currentPage + 1} de {pages} · {items.length} processos</p>
            <div {...stylex.props(layout.row)}>
              <Button aria-label="Página anterior" disabled={currentPage === 0} onClick={() => setPage(currentPage - 1)}><ChevronLeft size={16} aria-hidden /></Button>
              <Button aria-label="Próxima página" disabled={currentPage + 1 === pages} onClick={() => setPage(currentPage + 1)}><ChevronRight size={16} aria-hidden /></Button>
            </div>
          </div>}
        </>)}
      </Panel>

      <Modal title="Encerrar processo" open={Boolean(target)} preventClose={busy}
        onClose={() => { if (!busy) { setTarget(undefined); setActionError(undefined) } }}>
        {target && <>
          <p {...stylex.props(layout.text)}>
            Enviar sinal para <strong>{target.name}</strong> (PID <span {...stylex.props(layout.technical)}>{target.pid}</span>, usuário {target.user})?
          </p>
          {actionError != null && <ErrorState error={actionError} title="Não foi possível enviar sinal" />}
          <div {...stylex.props(layout.row)}>
            <Button disabled={busy} onClick={() => { setTarget(undefined); setActionError(undefined) }}>Cancelar</Button>
            <Button variant="outline" busy={busy} onClick={() => { void sendSignal('TERM') }}>SIGTERM</Button>
            <Button variant="danger" busy={busy} onClick={() => { void sendSignal('KILL') }}>SIGKILL</Button>
          </div>
        </>}
      </Modal>
    </>
  )
}
