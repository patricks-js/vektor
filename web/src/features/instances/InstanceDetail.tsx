import * as stylex from '@stylexjs/stylex'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Activity, Archive, ArrowLeft, Camera, Cpu, Download, HardDrive, Monitor, Network, Pause, Play, Plus, Power, RefreshCw, RotateCw, Settings2, Terminal, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { Facts } from '#/components/ui/dashboard'
import { Badge, Button, Empty, ErrorState, Loading, Modal, Panel, SelectField, StatusDot, TextField, layout, tableStyles } from '#/components/ui/ui'
import { useSession } from '#/features/auth/session'
import { ChartPanel } from '#/features/metrics/ChartPanel'
import { metric } from '#/features/metrics/catalog'
import { ChartCursorProvider } from '#/features/metrics/cursor'
import { dateTime, formatValue } from '#/features/metrics/format'
import { TimeRangeSelector } from '#/features/metrics/range'
import type { IncusStateAction } from '#/lib/api/client'
import { useApi } from '#/lib/api/queries'

type DetailTab = 'overview' | 'metrics' | 'network' | 'terminal' | 'snapshots' | 'backups'

type TerminalEntry = {
  id: number
  command: string
  exitCode: number
  stdout: string
  stderr: string
  ts: number
}

const QUICK_COMMANDS = ['uptime', 'free -h', 'df -h', 'ip -br a', 'uname -a']
const ENV_KEY_RE = /^[A-Za-z_][A-Za-z0-9_]{0,127}$/
const DEVICE_NAME_RE = /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$/

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
  typePillOCI: {
    color: 'var(--vk-info)', borderColor: 'var(--vk-info)',
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
  proxyGrid: {
    display: 'grid', gap: 10, alignItems: 'end',
    gridTemplateColumns: { default: 'minmax(0, 1fr)', '@media (min-width: 900px)': '1.2fr 0.7fr 1.1fr 0.9fr 0.9fr auto' },
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
  downloadLink: {
    display: 'inline-flex', alignItems: 'center', justifyContent: 'center', gap: 6, paddingInline: 12,
    minHeight: { default: 36, '@media (pointer: coarse)': 44 }, borderWidth: 1, borderStyle: 'solid',
    borderColor: 'var(--vk-border-control)', borderRadius: 'var(--vk-radius-2)',
    backgroundColor: { default: 'var(--vk-surface-1)', ':hover': 'var(--vk-surface-3)' },
    color: 'var(--vk-text-primary)', fontSize: 13, fontWeight: 500, textDecoration: 'none',
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
  if (!m || !m[1] || !m[2]) return null
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
  if (!m || !m[1] || !m[2]) return { value: '', unit: 'GiB' }
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
  const [confirmRemoveProxy, setConfirmRemoveProxy] = useState<string | null>(null)
  const [confirmRemoveEnv, setConfirmRemoveEnv] = useState<string | null>(null)
  const [feedback, setFeedback] = useState<{ tone: 'success' | 'critical'; text: string } | null>(null)

  // Resource edit state
  const [editing, setEditing] = useState(false)
  const [editCPU, setEditCPU] = useState('')
  const [editMemValue, setEditMemValue] = useState('')
  const [editMemUnit, setEditMemUnit] = useState<'MiB' | 'GiB'>('GiB')
  const [editDesc, setEditDesc] = useState('')
  const [editAutostart, setEditAutostart] = useState(false)

  // Proxy form state
  const [proxyName, setProxyName] = useState('')
  const [proxyProto, setProxyProto] = useState<'tcp' | 'udp'>('tcp')
  const [proxyBind, setProxyBind] = useState<'0.0.0.0' | '127.0.0.1'>('0.0.0.0')
  const [proxyHostPort, setProxyHostPort] = useState('')
  const [proxyInstPort, setProxyInstPort] = useState('')

  // Env form state
  const [envKey, setEnvKey] = useState('')
  const [envVal, setEnvVal] = useState('')

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

  const envSaveMutation = useMutation({
    mutationFn: (payload: { env?: Record<string, string>; unset_env?: string[] }) =>
      api.incusUpdateInstance(session, name, payload),
    onSuccess: () => {
      setEnvKey('')
      setEnvVal('')
      setFeedback({ tone: 'success', text: 'Variáveis de ambiente atualizadas.' })
      void queryClient.invalidateQueries({ queryKey: ['incus', 'instance', name] })
    },
    onError: (err) => setFeedback({ tone: 'critical', text: err instanceof Error ? err.message : 'Falha ao atualizar variável.' }),
  })

  const addProxyMutation = useMutation({
    mutationFn: () => api.incusAddProxy(session, name, {
      name: proxyName.trim() || `port-${proxyHostPort.trim()}`,
      listen: `${proxyProto}:${proxyBind}:${proxyHostPort.trim()}`,
      connect: `${proxyProto}:127.0.0.1:${proxyInstPort.trim()}`,
    }),
    onSuccess: (res) => {
      setProxyName('')
      setProxyHostPort('')
      setProxyInstPort('')
      setFeedback({ tone: 'success', text: res.message || 'Encaminhamento de porta adicionado.' })
      void queryClient.invalidateQueries({ queryKey: ['incus', 'instance', name] })
    },
    onError: (err) => setFeedback({ tone: 'critical', text: err instanceof Error ? err.message : 'Falha ao adicionar porta.' }),
  })

  const removeProxyMutation = useMutation({
    mutationFn: (device: string) => api.incusRemoveProxy(session, name, device),
    onSuccess: (res) => {
      setFeedback({ tone: 'success', text: res.message || 'Porta removida.' })
      void queryClient.invalidateQueries({ queryKey: ['incus', 'instance', name] })
    },
    onError: (err) => setFeedback({ tone: 'critical', text: err instanceof Error ? err.message : 'Falha ao remover porta.' }),
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
  const resourceId = `incus:${name}`

  const hpNum = Number(proxyHostPort.trim())
  const ipNum = Number(proxyInstPort.trim())
  const proxyNameTrim = proxyName.trim()
  const proxyValid = (!proxyNameTrim || DEVICE_NAME_RE.test(proxyNameTrim))
    && Number.isInteger(hpNum) && hpNum >= 1 && hpNum <= 65535
    && Number.isInteger(ipNum) && ipNum >= 1 && ipNum <= 65535

  const envKeyValid = ENV_KEY_RE.test(envKey.trim())
  const envEntries = Object.entries(inst?.env ?? {}).sort(([a], [b]) => a.localeCompare(b))
  const proxies = inst?.proxies ?? []

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
                <span {...stylex.props(s.typePill, inst.oci && s.typePillOCI)}>
                  {inst.type === 'virtual-machine' ? <Monitor size={12} aria-hidden /> : <Cpu size={12} aria-hidden />}
                  {inst.type === 'virtual-machine' ? 'VM' : inst.oci ? 'App OCI' : 'Container'}
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
            <button type="button" role="tab" aria-selected={tab === 'metrics'}
              {...stylex.props(s.tab, tab === 'metrics' && s.tabActive)} onClick={() => setTab('metrics')}>
              <Activity size={14} aria-hidden /> Métricas
            </button>
            <button type="button" role="tab" aria-selected={tab === 'network'}
              {...stylex.props(s.tab, tab === 'network' && s.tabActive)} onClick={() => setTab('network')}>
              <Network size={14} aria-hidden /> Portas & Env ({proxies.length})
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
                    ['Portas expostas', proxies.length > 0 ? proxies.map(p => `${p.listen} → ${p.connect}`).join(', ') : 'Nenhuma'],
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

          {tab === 'metrics' && (
            <div {...stylex.props(layout.stack)}>
              <div {...stylex.props(layout.split)}>
                <p {...stylex.props(layout.small)}>
                  Histórico persistido no SQLite multi-resolução para <code>{resourceId}</code>
                </p>
                <TimeRangeSelector />
              </div>
              <ChartCursorProvider>
                <div {...stylex.props(layout.grid2)}>
                  <ChartPanel title="CPU da instância" subtitle="Uso percentual derivado do tempo de CPU" resourceId={resourceId} metric={metric.incusCpu} color="cpu" />
                  <ChartPanel title="Memória RAM" subtitle="Consumo residente da instância" resourceId={resourceId} metric={metric.incusMemory} color="memory" />
                  <ChartPanel title="Uso de Disco (root)" subtitle="Espaço alocado no volume raiz" resourceId={resourceId} metric={metric.incusDisk} color="storage" />
                  <ChartPanel title="Processos ativos" subtitle="Contagem de processos em execução" resourceId={resourceId} metric={metric.incusProcesses} color="rx" />
                </div>
              </ChartCursorProvider>
            </div>
          )}

          {tab === 'network' && (
            <div {...stylex.props(layout.stack)}>
              <Panel title="Encaminhamento de Portas (Proxy Devices)" subtitle="Expõe portas da instância diretamente no host sem editar YAML">
                <form {...stylex.props(s.proxyGrid)} onSubmit={e => { e.preventDefault(); if (proxyValid) addProxyMutation.mutate() }}>
                  <TextField label="Nome (opcional)" value={proxyName} onChange={e => setProxyName(e.target.value)} placeholder="ex: http" />
                  <SelectField label="Proto" value={proxyProto} onChange={e => setProxyProto(e.target.value as 'tcp' | 'udp')}>
                    <option value="tcp">TCP</option>
                    <option value="udp">UDP</option>
                  </SelectField>
                  <SelectField label="Escuta no host" value={proxyBind} onChange={e => setProxyBind(e.target.value as '0.0.0.0' | '127.0.0.1')}>
                    <option value="0.0.0.0">0.0.0.0 (Todas interfaces)</option>
                    <option value="127.0.0.1">127.0.0.1 (Apenas local)</option>
                  </SelectField>
                  <TextField label="Porta Host" type="number" inputMode="numeric" min={1} max={65535}
                    value={proxyHostPort} onChange={e => setProxyHostPort(e.target.value.replace(/[^0-9]/g, ''))} placeholder="8080" />
                  <TextField label="Porta Instância" type="number" inputMode="numeric" min={1} max={65535}
                    value={proxyInstPort} onChange={e => setProxyInstPort(e.target.value.replace(/[^0-9]/g, ''))} placeholder="80" />
                  <Button type="submit" variant="primary" busy={addProxyMutation.isPending} disabled={!proxyValid}>
                    <Plus size={14} aria-hidden /> Adicionar
                  </Button>
                </form>

                {proxies.length === 0 ? (
                  <Empty>Nenhum encaminhamento de porta configurado para esta instância.</Empty>
                ) : (
                  <div {...stylex.props(tableStyles.scroll)}>
                    <table {...stylex.props(tableStyles.table)}>
                      <thead>
                        <tr>
                          <th {...stylex.props(tableStyles.th)}>Device</th>
                          <th {...stylex.props(tableStyles.th)}>Host (Listen)</th>
                          <th {...stylex.props(tableStyles.th)}>Instância (Connect)</th>
                          <th {...stylex.props(tableStyles.th)}>Ações</th>
                        </tr>
                      </thead>
                      <tbody>
                        {proxies.map(p => (
                          <tr key={p.name} {...stylex.props(tableStyles.row)}>
                            <td {...stylex.props(tableStyles.td, s.monoCell)}>{p.name}</td>
                            <td {...stylex.props(tableStyles.td, s.monoCell)}>{p.listen}</td>
                            <td {...stylex.props(tableStyles.td, s.monoCell)}>{p.connect}</td>
                            <td {...stylex.props(tableStyles.td)}>
                              <Button variant="danger" disabled={removeProxyMutation.isPending}
                                aria-label={`Remover porta ${p.name}`}
                                onClick={() => setConfirmRemoveProxy(p.name)}>
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

              <Panel title="Variáveis de Ambiente" subtitle="Configuração environment.* (útil especialmente para containers OCI)">
                <form {...stylex.props(s.snapForm)} onSubmit={e => {
                  e.preventDefault()
                  if (envKeyValid) envSaveMutation.mutate({ env: { [envKey.trim()]: envVal } })
                }}>
                  <TextField label="Chave" value={envKey} onChange={e => setEnvKey(e.target.value)} placeholder="ex: TZ" />
                  <TextField label="Valor" value={envVal} onChange={e => setEnvVal(e.target.value)} placeholder="ex: America/Sao_Paulo" />
                  <Button type="submit" variant="primary" busy={envSaveMutation.isPending} disabled={!envKeyValid}>
                    <Plus size={14} aria-hidden /> Salvar variável
                  </Button>
                </form>

                {envEntries.length === 0 ? (
                  <Empty>Nenhuma variável de ambiente personalizada definida.</Empty>
                ) : (
                  <div {...stylex.props(tableStyles.scroll)}>
                    <table {...stylex.props(tableStyles.table)}>
                      <thead>
                        <tr>
                          <th {...stylex.props(tableStyles.th)}>Chave</th>
                          <th {...stylex.props(tableStyles.th)}>Valor</th>
                          <th {...stylex.props(tableStyles.th)}>Ações</th>
                        </tr>
                      </thead>
                      <tbody>
                        {envEntries.map(([k, v]) => (
                          <tr key={k} {...stylex.props(tableStyles.row)}>
                            <td {...stylex.props(tableStyles.td, s.monoCell)}>{k}</td>
                            <td {...stylex.props(tableStyles.td, s.monoCell)}>{v}</td>
                            <td {...stylex.props(tableStyles.td)}>
                              <Button variant="danger" disabled={envSaveMutation.isPending}
                                aria-label={`Remover variável ${k}`}
                                onClick={() => setConfirmRemoveEnv(k)}>
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
            </div>
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
                            setCmdInput(termEntries[next]?.command ?? '')
                          } else if (e.key === 'ArrowDown' && historyIdx >= 0) {
                            e.preventDefault()
                            if (historyIdx >= termEntries.length - 1) {
                              setHistoryIdx(-1)
                              setCmdInput('')
                            } else {
                              const next = historyIdx + 1
                              setHistoryIdx(next)
                              setCmdInput(termEntries[next]?.command ?? '')
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
              subtitle="Arquivos de backup nativos gerenciados pelo Incus com download direto via streaming"
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
                            <div {...stylex.props(layout.row)}>
                              <a
                                href={api.incusBackupExportUrl(name, b.name)}
                                download={`${name}-${b.name}.tar.gz`}
                                {...stylex.props(s.downloadLink)}
                              >
                                <Download size={13} aria-hidden /> Baixar
                              </a>
                              <Button variant="danger" disabled={deleteBackupMutation.isPending}
                                aria-label={`Excluir backup ${b.name}`}
                                onClick={() => setConfirmDeleteBackup(b.name)}>
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

      <Modal open={Boolean(confirmRemoveProxy)} title={`Remover encaminhamento "${confirmRemoveProxy ?? ''}"?`} preventClose={removeProxyMutation.isPending} onClose={() => setConfirmRemoveProxy(null)}>
        <p {...stylex.props(layout.small)}>A porta exposta no host deixará de encaminhar tráfego para esta instância.</p>
        <div {...stylex.props(layout.row)}>
          <Button variant="outline" disabled={removeProxyMutation.isPending} onClick={() => setConfirmRemoveProxy(null)}>Cancelar</Button>
          <Button variant="danger" busy={removeProxyMutation.isPending} onClick={() => {
            const target = confirmRemoveProxy
            setConfirmRemoveProxy(null)
            if (target) removeProxyMutation.mutate(target)
          }}>
            Remover porta
          </Button>
        </div>
      </Modal>

      <Modal open={Boolean(confirmRemoveEnv)} title={`Remover variável "${confirmRemoveEnv ?? ''}"?`} preventClose={envSaveMutation.isPending} onClose={() => setConfirmRemoveEnv(null)}>
        <p {...stylex.props(layout.small)}>A variável de ambiente será removida da configuração da instância.</p>
        <div {...stylex.props(layout.row)}>
          <Button variant="outline" disabled={envSaveMutation.isPending} onClick={() => setConfirmRemoveEnv(null)}>Cancelar</Button>
          <Button variant="danger" busy={envSaveMutation.isPending} onClick={() => {
            const target = confirmRemoveEnv
            setConfirmRemoveEnv(null)
            if (target) envSaveMutation.mutate({ unset_env: [target] })
          }}>
            Remover variável
          </Button>
        </div>
      </Modal>
    </div>
  )
}
