import * as stylex from '@stylexjs/stylex'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Archive, ArrowLeft, Camera, Cpu, HardDrive, Monitor, Pause, Play, Power, RefreshCw, RotateCw, Settings2, Terminal, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { Facts } from '../../components/ui/dashboard'
import { Badge, Button, Empty, ErrorState, Loading, Modal, Panel, StatusDot, TextField, layout, tableStyles } from '../../components/ui/ui'
import { useSession } from '../auth/session'
import { dateTime, formatValue } from '../metrics/format'
import type { IncusStateAction } from '../../lib/api/client'
import { useApi } from '../../lib/api/queries'

type DetailTab = 'overview' | 'terminal' | 'snapshots' | 'backups'

type TerminalEntry = {
  id: number
  command: string
  exitCode: number
  stdout: string
  stderr: string
  ts: number
}

const QUICK_COMMANDS = ['uptime', 'free -h', 'df -h', 'ip -br a', 'uname -a']

const s = stylex.create({
  backLink: {
    display: 'inline-flex', alignItems: 'center', gap: 6, fontSize: 13, fontWeight: 500,
    color: { default: 'var(--vk-text-secondary)', ':hover': 'var(--vk-text-primary)' },
    textDecoration: 'none',
  },
  typePill: {
    display: 'inline-flex', alignItems: 'center', gap: 5, fontSize: 11, fontWeight: 600,
    paddingBlock: 3, paddingInline: 8, borderRadius: 'var(--vk-radius-1)',
    backgroundColor: 'var(--vk-surface-2)', borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-subtle)',
    color: 'var(--vk-text-secondary)', textTransform: 'uppercase', letterSpacing: '0.03em',
  },
  tabs: { display: 'flex', flexWrap: 'wrap', gap: 6 },
  tab: {
    minHeight: 34, paddingInline: 12, borderRadius: 'var(--vk-radius-2)', borderWidth: 1, borderStyle: 'solid',
    borderColor: 'var(--vk-border-subtle)', backgroundColor: 'var(--vk-surface-1)', color: 'var(--vk-text-secondary)',
    fontSize: 13, fontWeight: 500, cursor: 'pointer', display: 'inline-flex', alignItems: 'center', gap: 6,
  },
  tabActive: {
    backgroundColor: 'var(--vk-surface-3)', color: 'var(--vk-text-primary)', borderColor: 'var(--vk-border-strong)', fontWeight: 600,
  },
  snapForm: { display: 'flex', flexWrap: 'wrap', gap: 10, alignItems: 'flex-end' },
  monoCell: { fontFamily: 'var(--vk-mono)', fontSize: 12, fontVariantNumeric: 'tabular-nums' },
  feedbackBox: {
    display: 'flex', alignItems: 'flex-start', gap: 10, paddingBlock: 10, paddingInline: 14,
    borderRadius: 'var(--vk-radius-2)', borderWidth: 1, borderStyle: 'solid', fontSize: 13, lineHeight: '19px',
  },
  feedbackError: { borderColor: 'var(--vk-critical)', backgroundColor: 'var(--vk-surface-1)', color: 'var(--vk-critical)' },
  feedbackSuccess: { borderColor: 'var(--vk-success)', backgroundColor: 'var(--vk-surface-1)', color: 'var(--vk-success)' },
  formGrid: {
    display: 'grid', gap: 12,
    gridTemplateColumns: { default: 'minmax(0, 1fr)', '@media (min-width: 720px)': 'repeat(3, minmax(0, 1fr))' },
  },
  unitSelect: {
    minHeight: { default: 38, '@media (pointer: coarse)': 44 }, paddingBlock: 7, paddingInline: 10,
    borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-control)',
    borderRadius: 'var(--vk-radius-2)', backgroundColor: 'var(--vk-surface-2)', color: 'var(--vk-text-primary)',
    fontSize: 13, fontWeight: 500, cursor: 'pointer',
  },
  checkboxRow: {
    display: 'inline-flex', alignItems: 'center', gap: 8, fontSize: 13, color: 'var(--vk-text-primary)', cursor: 'pointer',
  },
  barTrack: {
    width: '100%', height: 8, borderRadius: 'var(--vk-radius-pill)', backgroundColor: 'var(--vk-surface-3)', overflow: 'hidden',
  },
  barFill: {
    height: '100%', borderRadius: 'var(--vk-radius-pill)', backgroundColor: 'var(--vk-accent)', transition: 'width 200ms',
  },
  terminalBox: {
    backgroundColor: '#0a0a0d', color: '#e4e4e7', fontFamily: 'var(--vk-mono)', fontSize: 12, lineHeight: '18px',
    borderRadius: 'var(--vk-radius-2)', borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-strong)',
    padding: 14, minHeight: 240, maxHeight: 420, overflowY: 'auto', display: 'grid', gap: 12, alignContent: 'start',
  },
  termEntry: { display: 'grid', gap: 4 },
  termPrompt: { color: '#4ade80', fontWeight: 600 },
  termStdout: { margin: 0, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', color: '#e4e4e7' },
  termStderr: { margin: 0, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', color: '#f87171' },
  termInputRow: { display: 'grid', gridTemplateColumns: 'minmax(0, 1fr) auto', gap: 8, alignItems: 'end' },
  chip: {
    fontFamily: 'var(--vk-mono)', fontSize: 11, paddingBlock: 4, paddingInline: 8,
    borderRadius: 'var(--vk-radius-1)', borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-subtle)',
    backgroundColor: 'var(--vk-surface-2)', color: 'var(--vk-text-secondary)', cursor: 'pointer',
  },
  consolePre: {
    margin: 0, padding: 12, borderRadius: 'var(--vk-radius-2)', backgroundColor: '#0a0a0d', color: '#d4d4d8',
    fontFamily: 'var(--vk-mono)', fontSize: 12, lineHeight: '18px', maxHeight: 280, overflowY: 'auto',
    whiteSpace: 'pre-wrap', overflowWrap: 'anywhere',
  },
})

function statusTone(status: string): 'success' | 'warning' | 'critical' | 'neutral' {
  switch (status) {
    case 'Running': return 'success'
    case 'Frozen': return 'warning'
    case 'Error': return 'critical'
    default: return 'neutral'
  }
}

function parseLimitBytes(limit?: string): number | null {
  if (!limit) return null
  const m = /^(\d+)\s*(MiB|GiB|MB|GB|KiB|B)$/i.exec(limit.trim())
  if (!m) return null
  const num = Number(m[1])
  switch (m[2].toLowerCase()) {
    case 'gib': case 'gb': return num * 1024 ** 3
    case 'mib': case 'mb': return num * 1024 ** 2
    case 'kib': return num * 1024
    default: return num
  }
}

function parseMemLimitFields(limit?: string): { value: string; unit: 'MiB' | 'GiB' } {
  if (!limit) return { value: '', unit: 'GiB' }
  const m = /^(\d+)\s*(MiB|GiB)$/i.exec(limit.trim())
  if (!m) return { value: '', unit: 'GiB' }
  return { value: m[1], unit: m[2].toLowerCase() === 'mib' ? 'MiB' : 'GiB' }
}

export function InstanceDetail() {
  const { name = '' } = useParams()
  const api = useApi()
  const { session } = useSession()
  const queryClient = useQueryClient()
  const navigate = useNavigate()

  const [tab, setTab] = useState<DetailTab>('overview')
  const [snapName, setSnapName] = useState('')
  const [backupName, setBackupName] = useState('')
  const [backupInstanceOnly, setBackupInstanceOnly] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [confirmRestoreSnap, setConfirmRestoreSnap] = useState<string | null>(null)
  const [confirmDeleteSnap, setConfirmDeleteSnap] = useState<string | null>(null)
  const [confirmDeleteBackup, setConfirmDeleteBackup] = useState<string | null>(null)
  const [feedback, setFeedback] = useState<{ tone: 'success' | 'critical'; text: string } | null>(null)

  // Resource edit state
  const [editing, setEditing] = useState(false)
  const [editCPU, setEditCPU] = useState('')
  const [editMemValue, setEditMemValue] = useState('')
  const [editMemUnit, setEditMemUnit] = useState<'MiB' | 'GiB'>('GiB')
  const [editDesc, setEditDesc] = useState('')
  const [editAutostart, setEditAutostart] = useState(false)

  // Terminal state
  const [cmdInput, setCmdInput] = useState('')
  const [termEntries, setTermEntries] = useState<TerminalEntry[]>([])
  const [historyIdx, setHistoryIdx] = useState(-1)

  const instanceQuery = useQuery({
    queryKey: ['incus', 'instance', name],
    queryFn: ({ signal }) => api.incusInstance(name, signal),
    enabled: Boolean(name),
    refetchInterval: 5_000,
  })

  const snapsQuery = useQuery({
    queryKey: ['incus', 'snapshots', name],
    queryFn: ({ signal }) => api.incusSnapshots(name, signal),
    enabled: Boolean(name) && (tab === 'snapshots' || tab === 'overview'),
    refetchInterval: 15_000,
  })

  const backupsQuery = useQuery({
    queryKey: ['incus', 'backups', name],
    queryFn: ({ signal }) => api.incusBackups(name, signal),
    enabled: Boolean(name) && tab === 'backups',
    refetchInterval: 20_000,
  })

  const consoleQuery = useQuery({
    queryKey: ['incus', 'console', name],
    queryFn: ({ signal }) => api.incusConsoleLog(name, signal),
    enabled: Boolean(name) && tab === 'terminal',
    staleTime: 10_000,
  })

  const stateMutation = useMutation({
    mutationFn: (action: IncusStateAction) => api.incusInstanceState(session, name, action),
    onSuccess: (res) => {
      setFeedback({ tone: 'success', text: res.message || 'Estado atualizado.' })
      void queryClient.invalidateQueries({ queryKey: ['incus'] })
    },
    onError: (err) => setFeedback({ tone: 'critical', text: err instanceof Error ? err.message : 'Falha na operação.' }),
  })

  const updateMutation = useMutation({
    mutationFn: () => api.incusUpdateInstance(session, name, {
      description: editDesc.trim(),
      cpu_limit: editCPU.trim(),
      memory_limit: editMemValue.trim() ? `${editMemValue.trim()}${editMemUnit}` : '',
      autostart: editAutostart,
    }),
    onSuccess: (res) => {
      setEditing(false)
      setFeedback({ tone: 'success', text: res.message || 'Configuração atualizada.' })
      void queryClient.invalidateQueries({ queryKey: ['incus'] })
    },
    onError: (err) => setFeedback({ tone: 'critical', text: err instanceof Error ? err.message : 'Falha ao atualizar configuração.' }),
  })

  const execMutation = useMutation({
    mutationFn: (command: string) => api.incusExec(session, name, command),
    onSuccess: (res, command) => {
      setTermEntries(prev => [...prev, {
        id: Date.now(),
        command,
        exitCode: res.exit_code,
        stdout: res.stdout,
        stderr: res.stderr,
        ts: Date.now(),
      }])
      setCmdInput('')
      setHistoryIdx(-1)
    },
    onError: (err) => setFeedback({ tone: 'critical', text: err instanceof Error ? err.message : 'Falha ao executar comando.' }),
  })

  const deleteMutation = useMutation({
    mutationFn: () => api.incusDeleteInstance(session, name),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['incus'] })
      void navigate('/instances', { replace: true })
    },
    onError: (err) => setFeedback({ tone: 'critical', text: err instanceof Error ? err.message : 'Falha ao remover instância.' }),
  })

  const createSnapMutation = useMutation({
    mutationFn: () => api.incusCreateSnapshot(session, name, snapName.trim(), false),
    onSuccess: () => {
      setSnapName('')
      setFeedback({ tone: 'success', text: 'Snapshot criado.' })
      void queryClient.invalidateQueries({ queryKey: ['incus', 'snapshots', name] })
      void queryClient.invalidateQueries({ queryKey: ['incus', 'instance', name] })
    },
    onError: (err) => setFeedback({ tone: 'critical', text: err instanceof Error ? err.message : 'Falha ao criar snapshot.' }),
  })

  const restoreSnapMutation = useMutation({
    mutationFn: (snap: string) => api.incusRestoreSnapshot(session, name, snap),
    onSuccess: () => {
      setFeedback({ tone: 'success', text: 'Snapshot restaurado.' })
      void queryClient.invalidateQueries({ queryKey: ['incus'] })
    },
    onError: (err) => setFeedback({ tone: 'critical', text: err instanceof Error ? err.message : 'Falha ao restaurar snapshot.' }),
  })

  const deleteSnapMutation = useMutation({
    mutationFn: (snap: string) => api.incusDeleteSnapshot(session, name, snap),
    onSuccess: () => {
      setFeedback({ tone: 'success', text: 'Snapshot removido.' })
      void queryClient.invalidateQueries({ queryKey: ['incus', 'snapshots', name] })
      void queryClient.invalidateQueries({ queryKey: ['incus', 'instance', name] })
    },
    onError: (err) => setFeedback({ tone: 'critical', text: err instanceof Error ? err.message : 'Falha ao remover snapshot.' }),
  })

  const createBackupMutation = useMutation({
    mutationFn: () => api.incusCreateBackup(session, name, backupName.trim(), backupInstanceOnly),
    onSuccess: () => {
      setBackupName('')
      setFeedback({ tone: 'success', text: 'Backup criado com sucesso.' })
      void queryClient.invalidateQueries({ queryKey: ['incus', 'backups', name] })
    },
    onError: (err) => setFeedback({ tone: 'critical', text: err instanceof Error ? err.message : 'Falha ao criar backup.' }),
  })

  const deleteBackupMutation = useMutation({
    mutationFn: (backup: string) => api.incusDeleteBackup(session, name, backup),
    onSuccess: () => {
      setFeedback({ tone: 'success', text: 'Backup removido.' })
      void queryClient.invalidateQueries({ queryKey: ['incus', 'backups', name] })
    },
    onError: (err) => setFeedback({ tone: 'critical', text: err instanceof Error ? err.message : 'Falha ao remover backup.' }),
  })

  const inst = instanceQuery.data
  const busy = stateMutation.isPending || deleteMutation.isPending
  const ramLimitBytes = parseLimitBytes(inst?.memory_limit)
  const ramPercent = inst && ramLimitBytes && ramLimitBytes > 0
    ? Math.min(100, Math.round((inst.memory_usage_bytes / ramLimitBytes) * 100))
    : null

  const startEdit = () => {
    if (!inst) return
    const mem = parseMemLimitFields(inst.memory_limit)
    setEditCPU(inst.cpu_limit ?? '')
    setEditMemValue(mem.value)
    setEditMemUnit(mem.unit)
    setEditDesc(inst.description ?? '')
    setEditAutostart(Boolean(inst.autostart))
    setEditing(true)
  }

  const runCommand = (raw: string) => {
    const trimmed = raw.trim()
    if (!trimmed || execMutation.isPending) return
    setFeedback(null)
    execMutation.mutate(trimmed)
  }

  return (
    <div {...stylex.props(layout.page)}>
      <div>
        <Link to="/instances" {...stylex.props(s.backLink)}>
          <ArrowLeft size={14} aria-hidden /> Voltar para instâncias
        </Link>
      </div>

      {instanceQuery.isPending && <Panel><Loading label="Carregando instância…" /></Panel>}
      {instanceQuery.isError && <Panel><ErrorState error={instanceQuery.error} retry={() => { void instanceQuery.refetch() }} /></Panel>}

      {inst && (
        <>
          <div {...stylex.props(layout.split)}>
            <div {...stylex.props(layout.compact)}>
              <div {...stylex.props(layout.row)}>
                <h1 tabIndex={-1} {...stylex.props(layout.pageTitle)}>{inst.name}</h1>
                <span {...stylex.props(s.typePill)}>
                  {inst.type === 'virtual-machine' ? <Monitor size={12} aria-hidden /> : <Cpu size={12} aria-hidden />}
                  {inst.type === 'virtual-machine' ? 'VM' : 'Container'}
                </span>
                <Badge tone={statusTone(inst.status)} icon={<StatusDot tone={statusTone(inst.status)} />}>
                  {inst.status}
                </Badge>
              </div>
              {inst.description && <p {...stylex.props(layout.small)}>{inst.description}</p>}
            </div>

            <div {...stylex.props(layout.row)}>
              {inst.status !== 'Running' && inst.status !== 'Frozen' && (
                <Button variant="primary" disabled={busy} onClick={() => stateMutation.mutate('start')}>
                  <Play size={14} aria-hidden /> Iniciar
                </Button>
              )}
              {inst.status === 'Frozen' && (
                <Button variant="primary" disabled={busy} onClick={() => stateMutation.mutate('unfreeze')}>
                  <Play size={14} aria-hidden /> Retomar
                </Button>
              )}
              {inst.status === 'Running' && (
                <>
                  <Button variant="outline" disabled={busy} onClick={() => stateMutation.mutate('restart')}>
                    <RotateCw size={14} aria-hidden /> Reiniciar
                  </Button>
                  <Button variant="outline" disabled={busy} onClick={() => stateMutation.mutate('freeze')}>
                    <Pause size={14} aria-hidden /> Pausar
                  </Button>
                  <Button variant="outline" disabled={busy} onClick={() => stateMutation.mutate('stop')}>
                    <Power size={14} aria-hidden /> Parar
                  </Button>
                </>
              )}
              {inst.status === 'Stopped' && (
                <Button variant="danger" disabled={busy} onClick={() => setConfirmDelete(true)}>
                  <Trash2 size={14} aria-hidden /> Remover
                </Button>
              )}
            </div>
          </div>

          <div {...stylex.props(s.tabs)} role="tablist">
            <button type="button" role="tab" aria-selected={tab === 'overview'}
              {...stylex.props(s.tab, tab === 'overview' && s.tabActive)} onClick={() => setTab('overview')}>
              <Settings2 size={14} aria-hidden /> Visão Geral
            </button>
            <button type="button" role="tab" aria-selected={tab === 'terminal'}
              {...stylex.props(s.tab, tab === 'terminal' && s.tabActive)} onClick={() => setTab('terminal')}>
              <Terminal size={14} aria-hidden /> Terminal & Console
            </button>
            <button type="button" role="tab" aria-selected={tab === 'snapshots'}
              {...stylex.props(s.tab, tab === 'snapshots' && s.tabActive)} onClick={() => setTab('snapshots')}>
              <Camera size={14} aria-hidden /> Snapshots ({inst.snapshots_count})
            </button>
            <button type="button" role="tab" aria-selected={tab === 'backups'}
              {...stylex.props(s.tab, tab === 'backups' && s.tabActive)} onClick={() => setTab('backups')}>
              <Archive size={14} aria-hidden /> Backups
            </button>
          </div>

          {feedback && (
            <div role={feedback.tone === 'critical' ? 'alert' : 'status'}
              {...stylex.props(s.feedbackBox, feedback.tone === 'critical' ? s.feedbackError : s.feedbackSuccess)}>
              <StatusDot tone={feedback.tone} />
              <span>{feedback.text}</span>
            </div>
          )}

          {tab === 'overview' && (
            <>
              <div {...stylex.props(layout.grid2)}>
                <Panel
                  title="Recursos e limites"
                  subtitle="Consumo atual e limites configurados"
                  action={!editing && <Button variant="outline" onClick={startEdit}><Settings2 size={13} aria-hidden /> Editar</Button>}
                >
                  {ramPercent !== null && (
                    <div {...stylex.props(layout.compact)}>
                      <div {...stylex.props(layout.split)}>
                        <span {...stylex.props(layout.small)}>Uso de RAM</span>
                        <span {...stylex.props(s.monoCell)}>
                          {formatValue(inst.memory_usage_bytes, 'bytes')} / {inst.memory_limit} ({ramPercent}%)
                        </span>
                      </div>
                      <div {...stylex.props(s.barTrack)}>
                        <div {...stylex.props(s.barFill)} style={{ width: `${ramPercent}%` }} />
                      </div>
                    </div>
                  )}
                  <Facts items={[
                    ['RAM em uso', inst.memory_usage_bytes > 0 ? formatValue(inst.memory_usage_bytes, 'bytes') : '—'],
                    ['Pico de RAM', inst.memory_peak_bytes > 0 ? formatValue(inst.memory_peak_bytes, 'bytes') : '—'],
                    ['Limite de RAM', inst.memory_limit || 'Sem limite'],
                    ['Limite de CPU', inst.cpu_limit ? `${inst.cpu_limit} vCPU` : 'Sem limite'],
                    ['Disco (root)', inst.disk_usage_bytes > 0 ? formatValue(inst.disk_usage_bytes, 'bytes') : '—'],
                    ['Processos', inst.processes > 0 ? String(inst.processes) : '—'],
                    ['Início automático', inst.autostart ? 'Ativado' : 'Desativado'],
                  ]} />
                </Panel>

                <Panel title="Rede e metadados" subtitle="Endereços atribuídos e perfis do Incus">
                  <Facts items={[
                    ['IPv4', inst.ipv4.length > 0 ? inst.ipv4.join(', ') : '—'],
                    ['IPv6', inst.ipv6.length > 0 ? inst.ipv6.join(', ') : '—'],
                    ['Perfis', inst.profiles.join(', ') || 'default'],
                    ['Arquitetura', inst.architecture || '—'],
                    ['Criada em', inst.created_at ? dateTime(inst.created_at) : '—'],
                    ['Último uso', inst.last_used_at ? dateTime(inst.last_used_at) : '—'],
                  ]} />
                </Panel>
              </div>

              {editing && (
                <Panel title="Editar recursos e configuração" subtitle="Alterações em containers são aplicadas ao vivo; VMs podem exigir reinício">
                  <form {...stylex.props(layout.stack)} onSubmit={e => { e.preventDefault(); updateMutation.mutate() }}>
                    <div {...stylex.props(s.formGrid)}>
                      <TextField
                        label="Limite de CPU (vCPUs)"
                        type="number"
                        inputMode="numeric"
                        min={1}
                        max={128}
                        value={editCPU}
                        onChange={e => setEditCPU(e.target.value.replace(/[^0-9]/g, ''))}
                        placeholder="Sem limite"
                      />
                      <TextField
                        label="Limite de RAM"
                        type="number"
                        inputMode="numeric"
                        min={1}
                        value={editMemValue}
                        onChange={e => setEditMemValue(e.target.value.replace(/[^0-9]/g, ''))}
                        placeholder="Sem limite"
                        action={
                          <select aria-label="Unidade de RAM" value={editMemUnit}
                            onChange={e => setEditMemUnit(e.target.value as 'MiB' | 'GiB')}
                            {...stylex.props(s.unitSelect)}>
                            <option value="MiB">MiB</option>
                            <option value="GiB">GiB</option>
                          </select>
                        }
                      />
                      <TextField
                        label="Descrição"
                        value={editDesc}
                        onChange={e => setEditDesc(e.target.value)}
                        placeholder="Finalidade da instância"
                      />
                    </div>
                    <div {...stylex.props(layout.split)}>
                      <label {...stylex.props(s.checkboxRow)}>
                        <input type="checkbox" checked={editAutostart} onChange={e => setEditAutostart(e.target.checked)} />
                        Iniciar automaticamente no boot do host (boot.autostart)
                      </label>
                      <div {...stylex.props(layout.row)}>
                        <Button variant="outline" onClick={() => setEditing(false)}>Cancelar</Button>
                        <Button type="submit" variant="primary" busy={updateMutation.isPending}>Salvar alterações</Button>
                      </div>
                    </div>
                  </form>
                </Panel>
              )}
            </>
          )}

          {tab === 'terminal' && (
            <div {...stylex.props(layout.stack)}>
              <Panel
                title="Terminal de comandos"
                subtitle="Executa comandos via Incus API (auditado em audit_log)"
                action={termEntries.length > 0 && <Button variant="outline" onClick={() => setTermEntries([])}>Limpar</Button>}
              >
                {inst.status !== 'Running' ? (
                  <Empty>Inicie a instância para executar comandos no terminal.</Empty>
                ) : (
                  <>
                    <div {...stylex.props(layout.row)}>
                      {QUICK_COMMANDS.map(qc => (
                        <button key={qc} type="button" {...stylex.props(s.chip)} disabled={execMutation.isPending}
                          onClick={() => runCommand(qc)}>
                          {qc}
                        </button>
                      ))}
                    </div>

                    <div {...stylex.props(s.terminalBox)} aria-live="polite">
                      {termEntries.length === 0 ? (
                        <span style={{ color: '#71717a' }}>
                          Pronto. Digite um comando abaixo ou clique em um atalho acima (ex: uptime, free -h, df -h).
                        </span>
                      ) : termEntries.map(entry => (
                        <div key={entry.id} {...stylex.props(s.termEntry)}>
                          <div {...stylex.props(layout.split)}>
                            <span {...stylex.props(s.termPrompt)}>root@{inst.name}:# {entry.command}</span>
                            <span style={{ color: entry.exitCode === 0 ? '#4ade80' : '#f87171', fontSize: 11 }}>
                              exit {entry.exitCode}
                            </span>
                          </div>
                          {entry.stdout && <pre {...stylex.props(s.termStdout)}>{entry.stdout}</pre>}
                          {entry.stderr && <pre {...stylex.props(s.termStderr)}>{entry.stderr}</pre>}
                        </div>
                      ))}
                    </div>

                    <form {...stylex.props(s.termInputRow)} onSubmit={e => { e.preventDefault(); runCommand(cmdInput) }}>
                      <TextField
                        label={`Comando em ${inst.name}`}
                        value={cmdInput}
                        onChange={e => setCmdInput(e.target.value)}
                        onKeyDown={e => {
                          if (e.key === 'ArrowUp' && termEntries.length > 0) {
                            e.preventDefault()
                            const next = historyIdx < 0 ? termEntries.length - 1 : Math.max(0, historyIdx - 1)
                            setHistoryIdx(next)
                            setCmdInput(termEntries[next].command)
                          } else if (e.key === 'ArrowDown' && historyIdx >= 0) {
                            e.preventDefault()
                            if (historyIdx >= termEntries.length - 1) {
                              setHistoryIdx(-1)
                              setCmdInput('')
                            } else {
                              const next = historyIdx + 1
                              setHistoryIdx(next)
                              setCmdInput(termEntries[next].command)
                            }
                          }
                        }}
                        placeholder="ex: ls -la /etc"
                      />
                      <Button type="submit" variant="primary" busy={execMutation.isPending} disabled={!cmdInput.trim()}>
                        Executar
                      </Button>
                    </form>
                  </>
                )}
              </Panel>

              <Panel
                title="Log do Console (boot)"
                subtitle="Buffer de saída do console da instância"
                action={
                  <Button variant="outline" busy={consoleQuery.isFetching} onClick={() => { void consoleQuery.refetch() }}>
                    <RefreshCw size={13} aria-hidden /> Atualizar
                  </Button>
                }
              >
                {consoleQuery.isPending && <Loading label="Lendo console…" />}
                {consoleQuery.isError && <ErrorState error={consoleQuery.error} retry={() => { void consoleQuery.refetch() }} />}
                {consoleQuery.data !== undefined && (
                  consoleQuery.data.trim() === ''
                    ? <Empty>Nenhum log de console disponível para esta instância.</Empty>
                    : <pre {...stylex.props(s.consolePre)}>{consoleQuery.data}</pre>
                )}
              </Panel>
            </div>
          )}

          {tab === 'snapshots' && (
            <Panel
              title="Snapshots"
              subtitle="Pontos de restauração instantâneos no storage pool do Incus"
              action={
                <form {...stylex.props(s.snapForm)} onSubmit={event => {
                  event.preventDefault()
                  if (snapName.trim()) createSnapMutation.mutate()
                }}>
                  <TextField label="Novo snapshot" value={snapName} onChange={e => setSnapName(e.target.value)} placeholder="ex: pre-upgrade" />
                  <Button type="submit" variant="primary" busy={createSnapMutation.isPending} disabled={!snapName.trim()}>
                    <Camera size={14} aria-hidden /> Criar snapshot
                  </Button>
                </form>
              }
            >
              {snapsQuery.isPending && <Loading label="Carregando snapshots…" />}
              {snapsQuery.isError && <ErrorState error={snapsQuery.error} retry={() => { void snapsQuery.refetch() }} />}
              {snapsQuery.data && snapsQuery.data.length === 0 && (
                <Empty>Nenhum snapshot criado ainda. Crie um snapshot antes de alterações críticas para restaurar rapidamente.</Empty>
              )}
              {snapsQuery.data && snapsQuery.data.length > 0 && (
                <div {...stylex.props(tableStyles.scroll)}>
                  <table {...stylex.props(tableStyles.table)}>
                    <thead>
                      <tr>
                        <th {...stylex.props(tableStyles.th)}>Nome</th>
                        <th {...stylex.props(tableStyles.th)}>Criado em</th>
                        <th {...stylex.props(tableStyles.th)}>Tamanho</th>
                        <th {...stylex.props(tableStyles.th)}>Ações</th>
                      </tr>
                    </thead>
                    <tbody>
                      {snapsQuery.data.map(snap => (
                        <tr key={snap.name} {...stylex.props(tableStyles.row)}>
                          <td {...stylex.props(tableStyles.td, s.monoCell)}>{snap.name}</td>
                          <td {...stylex.props(tableStyles.td)}>{snap.created_at ? dateTime(snap.created_at) : '—'}</td>
                          <td {...stylex.props(tableStyles.td, s.monoCell)}>
                            {snap.size_bytes > 0 ? formatValue(snap.size_bytes, 'bytes') : '—'}
                          </td>
                          <td {...stylex.props(tableStyles.td)}>
                            <div {...stylex.props(layout.row)}>
                              <Button variant="outline" disabled={restoreSnapMutation.isPending}
                                onClick={() => setConfirmRestoreSnap(snap.name)}>
                                <HardDrive size={13} aria-hidden /> Restaurar
                              </Button>
                              <Button variant="danger" disabled={deleteSnapMutation.isPending}
                                aria-label={`Excluir snapshot ${snap.name}`}
                                onClick={() => setConfirmDeleteSnap(snap.name)}>
                                <Trash2 size={13} aria-hidden />
                              </Button>
                            </div>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </Panel>
          )}

          {tab === 'backups' && (
            <Panel
              title="Backups compactados"
              subtitle="Arquivos de backup nativos gerenciados pelo Incus"
              action={
                <form {...stylex.props(s.snapForm)} onSubmit={event => {
                  event.preventDefault()
                  if (backupName.trim()) createBackupMutation.mutate()
                }}>
                  <TextField label="Novo backup" value={backupName} onChange={e => setBackupName(e.target.value)} placeholder="ex: backup-2026-11" />
                  <label {...stylex.props(s.checkboxRow)}>
                    <input type="checkbox" checked={backupInstanceOnly} onChange={e => setBackupInstanceOnly(e.target.checked)} />
                    Sem snapshots
                  </label>
                  <Button type="submit" variant="primary" busy={createBackupMutation.isPending} disabled={!backupName.trim()}>
                    <Archive size={14} aria-hidden /> Criar backup
                  </Button>
                </form>
              }
            >
              {backupsQuery.isPending && <Loading label="Carregando backups…" />}
              {backupsQuery.isError && <ErrorState error={backupsQuery.error} retry={() => { void backupsQuery.refetch() }} />}
              {backupsQuery.data && backupsQuery.data.length === 0 && (
                <Empty>Nenhum backup encontrado para esta instância.</Empty>
              )}
              {backupsQuery.data && backupsQuery.data.length > 0 && (
                <div {...stylex.props(tableStyles.scroll)}>
                  <table {...stylex.props(tableStyles.table)}>
                    <thead>
                      <tr>
                        <th {...stylex.props(tableStyles.th)}>Nome</th>
                        <th {...stylex.props(tableStyles.th)}>Criado em</th>
                        <th {...stylex.props(tableStyles.th)}>Escopo</th>
                        <th {...stylex.props(tableStyles.th)}>Ações</th>
                      </tr>
                    </thead>
                    <tbody>
                      {backupsQuery.data.map(b => (
                        <tr key={b.name} {...stylex.props(tableStyles.row)}>
                          <td {...stylex.props(tableStyles.td, s.monoCell)}>{b.name}</td>
                          <td {...stylex.props(tableStyles.td)}>{b.created_at ? dateTime(b.created_at) : '—'}</td>
                          <td {...stylex.props(tableStyles.td)}>{b.instance_only ? 'Somente instância' : 'Instância + snapshots'}</td>
                          <td {...stylex.props(tableStyles.td)}>
                            <Button variant="danger" disabled={deleteBackupMutation.isPending}
                              aria-label={`Excluir backup ${b.name}`}
                              onClick={() => setConfirmDeleteBackup(b.name)}>
                              <Trash2 size={13} aria-hidden />
                            </Button>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </Panel>
          )}
        </>
      )}

      <Modal open={confirmDelete} title={`Remover instância "${name}"?`} preventClose={deleteMutation.isPending} onClose={() => setConfirmDelete(false)}>
        <p {...stylex.props(layout.small)}>Esta ação exclui permanentemente a instância, seus discos e todos os seus snapshots.</p>
        <div {...stylex.props(layout.row)}>
          <Button variant="outline" disabled={deleteMutation.isPending} onClick={() => setConfirmDelete(false)}>Cancelar</Button>
          <Button variant="danger" busy={deleteMutation.isPending} onClick={() => { setConfirmDelete(false); deleteMutation.mutate() }}>
            Remover permanentemente
          </Button>
        </div>
      </Modal>

      <Modal open={Boolean(confirmRestoreSnap)} title={`Restaurar snapshot "${confirmRestoreSnap ?? ''}"?`} preventClose={restoreSnapMutation.isPending} onClose={() => setConfirmRestoreSnap(null)}>
        <p {...stylex.props(layout.small)}>O disco atual da instância será revertido para o estado salvo neste snapshot.</p>
        <div {...stylex.props(layout.row)}>
          <Button variant="outline" disabled={restoreSnapMutation.isPending} onClick={() => setConfirmRestoreSnap(null)}>Cancelar</Button>
          <Button variant="danger" busy={restoreSnapMutation.isPending} onClick={() => {
            const target = confirmRestoreSnap
            setConfirmRestoreSnap(null)
            if (target) restoreSnapMutation.mutate(target)
          }}>
            Restaurar snapshot
          </Button>
        </div>
      </Modal>

      <Modal open={Boolean(confirmDeleteSnap)} title={`Excluir snapshot "${confirmDeleteSnap ?? ''}"?`} preventClose={deleteSnapMutation.isPending} onClose={() => setConfirmDeleteSnap(null)}>
        <p {...stylex.props(layout.small)}>O snapshot será removido definitivamente.</p>
        <div {...stylex.props(layout.row)}>
          <Button variant="outline" disabled={deleteSnapMutation.isPending} onClick={() => setConfirmDeleteSnap(null)}>Cancelar</Button>
          <Button variant="danger" busy={deleteSnapMutation.isPending} onClick={() => {
            const target = confirmDeleteSnap
            setConfirmDeleteSnap(null)
            if (target) deleteSnapMutation.mutate(target)
          }}>
            Excluir
          </Button>
        </div>
      </Modal>

      <Modal open={Boolean(confirmDeleteBackup)} title={`Excluir backup "${confirmDeleteBackup ?? ''}"?`} preventClose={deleteBackupMutation.isPending} onClose={() => setConfirmDeleteBackup(null)}>
        <p {...stylex.props(layout.small)}>O arquivo de backup será removido definitivamente do Incus.</p>
        <div {...stylex.props(layout.row)}>
          <Button variant="outline" disabled={deleteBackupMutation.isPending} onClick={() => setConfirmDeleteBackup(null)}>Cancelar</Button>
          <Button variant="danger" busy={deleteBackupMutation.isPending} onClick={() => {
            const target = confirmDeleteBackup
            setConfirmDeleteBackup(null)
            if (target) deleteBackupMutation.mutate(target)
          }}>
            Excluir
          </Button>
        </div>
      </Modal>
    </div>
  )
}
