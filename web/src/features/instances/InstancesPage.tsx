import * as stylex from '@stylexjs/stylex'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Box, ChevronLeft, ChevronRight, Cpu, FileText, HardDrive, Monitor, Network, Pause, Play, Plus, Power, RotateCw, X } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { Badge, Button, Empty, ErrorState, Loading, Panel, SelectField, StatusDot, TextField, layout, tableStyles } from '#/components/ui/ui'
import { useSession } from '#/features/auth/session'
import { CommandBlock, ServiceLogViewer } from '#/features/diagnostics/DiagnosticsParts'
import { dateTime, formatValue } from '#/features/metrics/format'
import type { IncusCreateInstanceInput, IncusStateAction } from '#/lib/api/client'
import { useApi, useDiagnostics } from '#/lib/api/queries'

const PAGE_SIZE = 15

const IMAGE_PRESETS = [
  { label: 'Ubuntu 24.04 LTS', value: 'images:ubuntu/24.04' },
  { label: 'Debian 12 (Bookworm)', value: 'images:debian/12' },
  { label: 'Alpine 3.21', value: 'images:alpine/3.21' },
  { label: 'Fedora 41', value: 'images:fedora/41' },
  { label: 'Arch Linux', value: 'images:archlinux' },
]

const OCI_PRESETS = [
  { label: 'Nginx (Alpine)', value: 'nginx:alpine', port: '80' },
  { label: 'Caddy 2 (Alpine)', value: 'caddy:2-alpine', port: '80' },
  { label: 'Redis 7 (Alpine)', value: 'redis:7-alpine', port: '6379' },
  { label: 'Valkey 8 (Alpine)', value: 'valkey/valkey:8-alpine', port: '6379' },
  { label: 'PostgreSQL 16 (Alpine)', value: 'postgres:16-alpine', port: '5432' },
]

const OCI_REGISTRIES = [
  { label: 'Docker Hub (registry-1.docker.io)', value: 'https://registry-1.docker.io' },
  { label: 'GitHub Container Registry (ghcr.io)', value: 'https://ghcr.io' },
]

const s = stylex.create({
  kpiGrid: {
    display: 'grid', gap: 12,
    gridTemplateColumns: { default: 'repeat(2, minmax(0, 1fr))', '@media (min-width: 900px)': 'repeat(4, minmax(0, 1fr))' },
  },
  kpiCard: {
    display: 'grid', gap: 4, padding: 14, borderRadius: 'var(--vk-radius-3)',
    backgroundColor: 'var(--vk-surface-1)', borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-subtle)',
  },
  kpiLabel: { fontSize: 12, color: 'var(--vk-text-secondary)', fontWeight: 500 },
  kpiValue: { fontSize: 22, fontWeight: 600, fontVariantNumeric: 'tabular-nums', color: 'var(--vk-text-primary)', letterSpacing: '-0.02em' },
  tabs: { display: 'flex', flexWrap: 'wrap', gap: 6 },
  tab: {
    minHeight: 34, paddingInline: 12, borderRadius: 'var(--vk-radius-2)', borderWidth: 1, borderStyle: 'solid',
    borderColor: 'var(--vk-border-subtle)', backgroundColor: 'var(--vk-surface-1)', color: 'var(--vk-text-secondary)',
    fontSize: 13, fontWeight: 500, cursor: 'pointer', display: 'inline-flex', alignItems: 'center', gap: 6,
  },
  tabActive: {
    backgroundColor: 'var(--vk-surface-3)', color: 'var(--vk-text-primary)', borderColor: 'var(--vk-border-strong)', fontWeight: 600,
  },
  filterBar: {
    display: 'grid', gap: 12,
    gridTemplateColumns: { default: 'minmax(0, 1fr)', '@media (min-width: 720px)': 'minmax(0, 1.4fr) repeat(2, minmax(0, 1fr))' },
  },
  nameLink: {
    color: { default: 'var(--vk-text-primary)', ':hover': 'var(--vk-accent)' },
    fontWeight: 600, textDecoration: 'none', display: 'inline-flex', alignItems: 'center', gap: 6,
  },
  typeTag: {
    fontSize: 10, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.04em',
    paddingBlock: 2, paddingInline: 6, borderRadius: 'var(--vk-radius-1)',
    backgroundColor: 'var(--vk-surface-2)', color: 'var(--vk-text-secondary)',
    borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-subtle)',
  },
  mono: { fontFamily: 'var(--vk-mono)', fontSize: 12, fontVariantNumeric: 'tabular-nums' },
  sub: { fontSize: 12, color: 'var(--vk-text-muted)' },
  formGrid: {
    display: 'grid', gap: 12,
    gridTemplateColumns: { default: 'minmax(0, 1fr)', '@media (min-width: 720px)': 'repeat(3, minmax(0, 1fr))' },
  },
  checkboxRow: {
    display: 'inline-flex', alignItems: 'center', gap: 8, fontSize: 13, color: 'var(--vk-text-primary)', cursor: 'pointer',
  },
  codeBlock: {
    fontFamily: 'var(--vk-mono)', fontSize: 12, padding: 12, borderRadius: 'var(--vk-radius-2)',
    backgroundColor: 'var(--vk-bg)', borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-subtle)',
    overflowX: 'auto', color: 'var(--vk-text-primary)',
  },
  unitSelect: {
    minHeight: { default: 38, '@media (pointer: coarse)': 44 }, paddingBlock: 7, paddingInline: 10,
    borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-control)',
    borderRadius: 'var(--vk-radius-2)', backgroundColor: 'var(--vk-surface-2)', color: 'var(--vk-text-primary)',
    fontSize: 13, fontWeight: 500, cursor: 'pointer',
  },
  feedbackBox: {
    display: 'flex', alignItems: 'flex-start', gap: 10, paddingBlock: 10, paddingInline: 14,
    borderRadius: 'var(--vk-radius-2)', borderWidth: 1, borderStyle: 'solid', fontSize: 13, lineHeight: '19px',
  },
  feedbackError: {
    borderColor: 'var(--vk-critical)', backgroundColor: 'var(--vk-surface-1)', color: 'var(--vk-critical)',
  },
  feedbackSuccess: {
    borderColor: 'var(--vk-success)', backgroundColor: 'var(--vk-surface-1)', color: 'var(--vk-success)',
  },
  typeTagOCI: {
    backgroundColor: 'var(--vk-surface-3)', color: 'var(--vk-info)', borderColor: 'var(--vk-info)',
  },
  textarea: {
    width: '100%', minHeight: 72, paddingBlock: 8, paddingInline: 12,
    borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-control)',
    borderRadius: 'var(--vk-radius-2)', backgroundColor: 'var(--vk-bg)', color: 'var(--vk-text-primary)',
    fontFamily: 'var(--vk-mono)', fontSize: 12, resize: 'vertical',
  },
  pagination: {
    display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 10,
    paddingTop: 4, fontSize: 12, color: 'var(--vk-text-secondary)',
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

const INSTANCE_NAME_RE = /^[a-zA-Z0-9][a-zA-Z0-9-]{0,62}$/
const IMAGE_ALIAS_RE = /^[a-zA-Z0-9._:/@-]{1,256}$/
const ENV_KEY_RE = /^[A-Za-z_][A-Za-z0-9_]{0,127}$/

function parseEnvLines(raw: string): { env?: Record<string, string>; error?: string } {
  const lines = raw.split('\n').map(l => l.trim()).filter(Boolean)
  if (lines.length === 0) return {}
  const env: Record<string, string> = {}
  for (const line of lines) {
    const idx = line.indexOf('=')
    if (idx <= 0) return { error: `Formato inválido em "${line}" (use CHAVE=VALOR).` }
    const k = line.slice(0, idx).trim()
    const v = line.slice(idx + 1)
    if (!ENV_KEY_RE.test(k)) return { error: `Nome de variável inválido: "${k}".` }
    env[k] = v
  }
  return { env }
}

export function InstancesPage() {
  const api = useApi()
  const { session } = useSession()
  const queryClient = useQueryClient()

  const [tab, setTab] = useState<'instances' | 'infrastructure'>('instances')
  const [search, setSearch] = useState('')
  const [typeFilter, setTypeFilter] = useState<'all' | 'container' | 'oci' | 'virtual-machine'>('all')
  const [statusFilter, setStatusFilter] = useState<'all' | 'Running' | 'Stopped'>('all')
  const [page, setPage] = useState(0)
  const [showCreate, setShowCreate] = useState(false)
  const [feedback, setFeedback] = useState<{ tone: 'success' | 'critical'; text: string } | null>(null)

  const [newName, setNewName] = useState('')
  const [newKind, setNewKind] = useState<'container' | 'oci' | 'virtual-machine'>('container')
  const [newImage, setNewImage] = useState(IMAGE_PRESETS[0]?.value ?? '')
  const [newOCIImage, setNewOCIImage] = useState(OCI_PRESETS[0]?.value ?? '')
  const [newOCIRegistry, setNewOCIRegistry] = useState(OCI_REGISTRIES[0]?.value ?? '')
  const [customImage, setCustomImage] = useState('')
  const [newCPU, setNewCPU] = useState('2')
  const [newMemValue, setNewMemValue] = useState('1')
  const [newMemUnit, setNewMemUnit] = useState<'MiB' | 'GiB'>('GiB')
  const [newDesc, setNewDesc] = useState('')
  const [newProfile, setNewProfile] = useState('default')
  const [newEnvText, setNewEnvText] = useState('')
  const [newHostPort, setNewHostPort] = useState('')
  const [newContainerPort, setNewContainerPort] = useState('')
  const [newStart, setNewStart] = useState(true)
  const [submitted, setSubmitted] = useState(false)

  const statusQuery = useQuery({
    queryKey: ['incus', 'status'],
    queryFn: ({ signal }) => api.incusStatus(signal),
    refetchInterval: 15_000,
  })

  const available = Boolean(statusQuery.data?.available)

  const diagQuery = useDiagnostics()
  const incusDiag = diagQuery.data?.checks.find(c => c.id === 'incus')
  const [showUnavailableLogs, setShowUnavailableLogs] = useState(false)

  const instancesQuery = useQuery({
    queryKey: ['incus', 'instances'],
    queryFn: ({ signal }) => api.incusInstances(signal),
    enabled: available,
    refetchInterval: 8_000,
  })

  const resourcesQuery = useQuery({
    queryKey: ['incus', 'resources'],
    queryFn: ({ signal }) => api.incusResources(signal),
    enabled: available && (tab === 'infrastructure' || showCreate),
    staleTime: 30_000,
  })

  const stateMutation = useMutation({
    mutationFn: ({ name, action }: { name: string; action: IncusStateAction }) =>
      api.incusInstanceState(session, name, action),
    onSuccess: (res) => {
      setFeedback({ tone: 'success', text: res.message || 'Ação concluída.' })
      void queryClient.invalidateQueries({ queryKey: ['incus'] })
    },
    onError: (err) => setFeedback({ tone: 'critical', text: err instanceof Error ? err.message : 'Falha na ação.' }),
  })

  const createMutation = useMutation({
    mutationFn: (input: IncusCreateInstanceInput) => api.incusCreateInstance(session, input),
    onSuccess: (res) => {
      setFeedback({ tone: 'success', text: res.message || 'Instância criada com sucesso.' })
      setShowCreate(false)
      setSubmitted(false)
      setNewName('')
      setNewDesc('')
      void queryClient.invalidateQueries({ queryKey: ['incus'] })
    },
    onError: (err) => setFeedback({ tone: 'critical', text: err instanceof Error ? err.message : 'Falha ao criar instância.' }),
  })

  const isOCI = newKind === 'oci'
  const selectedImage = isOCI ? newOCIImage : newImage

  const trimmedName = newName.trim()
  const nameError = (submitted || trimmedName.length > 0) && !INSTANCE_NAME_RE.test(trimmedName)
    ? 'Use apenas letras, números e hífen (começando por letra ou número, máx. 63 caracteres).'
    : undefined

  const trimmedCustomImg = customImage.trim()
  const customImageError = selectedImage === '__custom__' && (submitted || trimmedCustomImg.length > 0) && !IMAGE_ALIAS_RE.test(trimmedCustomImg)
    ? (isOCI ? 'Informe uma imagem OCI válida (ex: nginx:alpine ou owner/app:latest).' : 'Informe um alias válido (ex: images:ubuntu/24.04).')
    : undefined

  const trimmedCPU = newCPU.trim()
  const cpuNum = Number(trimmedCPU)
  const cpuError = trimmedCPU !== '' && (!Number.isInteger(cpuNum) || cpuNum < 1 || cpuNum > 128)
    ? 'Informe um número inteiro de vCPUs entre 1 e 128 (ou deixe vazio).'
    : undefined

  const trimmedMem = newMemValue.trim()
  const memNum = Number(trimmedMem)
  const memMax = newMemUnit === 'GiB' ? 1024 : 1048576
  const memError = trimmedMem !== '' && (!Number.isInteger(memNum) || memNum < 1 || memNum > memMax)
    ? `Informe um inteiro entre 1 e ${memMax} ${newMemUnit} (ou deixe vazio).`
    : undefined

  const parsedEnv = parseEnvLines(newEnvText)
  const envError = (submitted || newEnvText.trim().length > 0) ? parsedEnv.error : undefined

  const hp = Number(newHostPort.trim())
  const cp = Number(newContainerPort.trim())
  const hasAnyPort = newHostPort.trim() !== '' || newContainerPort.trim() !== ''
  const portError = hasAnyPort && (!Number.isInteger(hp) || hp < 1 || hp > 65535 || !Number.isInteger(cp) || cp < 1 || cp > 65535)
    ? 'Informe ambas as portas (host e instância) entre 1 e 65535.'
    : undefined

  const formValid = Boolean(trimmedName) && !nameError && !customImageError && !cpuError && !memError && !envError && !portError
    && (selectedImage !== '__custom__' || Boolean(trimmedCustomImg))

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase()
    return (instancesQuery.data ?? []).filter(inst => {
      if (typeFilter === 'oci' && !inst.oci) return false
      if (typeFilter === 'container' && (inst.type !== 'container' || inst.oci)) return false
      if (typeFilter === 'virtual-machine' && inst.type !== 'virtual-machine') return false
      if (statusFilter !== 'all' && inst.status !== statusFilter) return false
      if (q && !inst.name.toLowerCase().includes(q) && !inst.ipv4.some(ip => ip.includes(q)) && !(inst.description ?? '').toLowerCase().includes(q)) {
        return false
      }
      return true
    })
  }, [instancesQuery.data, search, typeFilter, statusFilter])

  const totalPages = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE))
  const safePage = Math.min(page, totalPages - 1)
  const visible = filtered.slice(safePage * PAGE_SIZE, (safePage + 1) * PAGE_SIZE)

  const handleCreate = (event: React.FormEvent) => {
    event.preventDefault()
    setSubmitted(true)
    if (!formValid) return
    const image = selectedImage === '__custom__' ? trimmedCustomImg : selectedImage
    setFeedback(null)
    createMutation.mutate({
      name: trimmedName,
      type: newKind === 'virtual-machine' ? 'virtual-machine' : 'container',
      protocol: isOCI ? 'oci' : 'simplestreams',
      server: isOCI ? newOCIRegistry : undefined,
      image,
      description: newDesc.trim() || undefined,
      cpu_limit: trimmedCPU || undefined,
      memory_limit: trimmedMem ? `${trimmedMem}${newMemUnit}` : undefined,
      profiles: newProfile ? [newProfile] : ['default'],
      env: parsedEnv.env,
      proxies: hasAnyPort ? [{
        name: `port-${hp}`,
        listen: `tcp:0.0.0.0:${hp}`,
        connect: `tcp:127.0.0.1:${cp}`,
      }] : undefined,
      start: newStart,
    })
  }

  return (
    <div {...stylex.props(layout.page)}>
      <div {...stylex.props(layout.split)}>
        <div {...stylex.props(layout.compact)}>
          <h1 tabIndex={-1} {...stylex.props(layout.pageTitle)}>Instâncias Incus</h1>
          <p {...stylex.props(layout.small)}>
            {statusQuery.data?.available
              ? `Incus ${statusQuery.data.server_version ?? ''} · Drivers: ${(statusQuery.data.drivers ?? []).join(', ') || 'lxc'}${statusQuery.data.oci_supported ? ' · OCI ativo' : ''}`
              : 'Gerenciamento de system containers, apps OCI e máquinas virtuais via Incus'}
          </p>
        </div>
        {available && (
          <div {...stylex.props(layout.row)}>
            <div {...stylex.props(s.tabs)} role="tablist">
              <button type="button" role="tab" aria-selected={tab === 'instances'}
                {...stylex.props(s.tab, tab === 'instances' && s.tabActive)}
                onClick={() => setTab('instances')}>
                <Box size={14} aria-hidden /> Instâncias
              </button>
              <button type="button" role="tab" aria-selected={tab === 'infrastructure'}
                {...stylex.props(s.tab, tab === 'infrastructure' && s.tabActive)}
                onClick={() => setTab('infrastructure')}>
                <HardDrive size={14} aria-hidden /> Storage & Redes
              </button>
            </div>
            <Button variant="primary" onClick={() => { setTab('instances'); setShowCreate(v => !v) }}>
              {showCreate ? <X size={14} aria-hidden /> : <Plus size={14} aria-hidden />}
              {showCreate ? 'Fechar' : 'Nova instância'}
            </Button>
          </div>
        )}
      </div>

      {statusQuery.isPending && <Panel><Loading label="Verificando Incus no host…" /></Panel>}
      {statusQuery.isError && !statusQuery.data && <Panel><ErrorState error={statusQuery.error} retry={() => { void statusQuery.refetch() }} /></Panel>}

      {statusQuery.data && !statusQuery.data.available && (() => {
        const ctlDown = (statusQuery.data.reason ?? '').includes('vektor-ctl')
        const hasRealDiag = Boolean(incusDiag && incusDiag.status !== 'ok')
        const logUnit = ctlDown ? 'vektor-ctl.service' : 'incus.service'
        return (
          <Panel
            title={hasRealDiag && incusDiag ? incusDiag.summary : ctlDown ? 'Helper vektor-ctl indisponível' : 'Incus não detectado neste host'}
            subtitle={statusQuery.data.reason || 'Socket /var/lib/incus/unix.socket indisponível'}
          >
            {hasRealDiag && incusDiag ? (
              <>
                {incusDiag.detail && <pre {...stylex.props(s.codeBlock)}>{incusDiag.detail}</pre>}
                {incusDiag.hint && <p {...stylex.props(layout.small)}>{incusDiag.hint}</p>}
                {incusDiag.command && <CommandBlock command={incusDiag.command} />}
              </>
            ) : ctlDown ? (
              <>
                <p {...stylex.props(layout.small)}>
                  O <code>vektord</code> não conseguiu se comunicar com o helper privilegiado <code>vektor-ctl.socket</code> em <code>/run/vektor/ctl.sock</code>.
                  Verifique se o socket está ativo e se <code>/run/vektor</code> tem permissão <code>0755</code>:
                </p>
                <CommandBlock command={'sudo chmod 0755 /run/vektor\nsudo systemctl enable --now vektor-ctl.socket\nsudo systemctl status vektor-ctl.socket'} />
              </>
            ) : (
              <>
                <p {...stylex.props(layout.small)}>
                  O Vektor integra diretamente com o daemon do <strong>Incus</strong> através do helper isolado <code>vektor-ctl.socket</code> (ADR-006).
                  Para habilitar o gerenciamento de VMs e containers neste servidor, instale e inicialize o Incus:
                </p>
                <CommandBlock command={'sudo apt install incus\nsudo incus admin init --minimal'} />
              </>
            )}
            <div {...stylex.props(layout.row)}>
              <Button variant="outline" busy={statusQuery.isFetching || diagQuery.isFetching} onClick={() => { void statusQuery.refetch(); void diagQuery.refetch() }}>
                {statusQuery.isFetching ? 'Verificando…' : 'Verificar novamente'}
              </Button>
              {(hasRealDiag || ctlDown) && (
                <Button variant="quiet" onClick={() => setShowUnavailableLogs(v => !v)}>
                  <FileText size={14} aria-hidden />
                  {showUnavailableLogs ? `Ocultar logs (${logUnit})` : `Ver logs (${logUnit})`}
                </Button>
              )}
              {statusQuery.dataUpdatedAt > 0 && !statusQuery.isFetching && (
                <span {...stylex.props(layout.small)}>
                  Última verificação: {dateTime(statusQuery.dataUpdatedAt)}
                </span>
              )}
            </div>
            {showUnavailableLogs && <ServiceLogViewer unit={logUnit} />}
          </Panel>
        )
      })()}

      {available && statusQuery.data && (
        <>
          <div {...stylex.props(s.kpiGrid)}>
            <div {...stylex.props(s.kpiCard)}>
              <span {...stylex.props(s.kpiLabel)}>Total de instâncias</span>
              <span {...stylex.props(s.kpiValue)}>{statusQuery.data.instance_count}</span>
            </div>
            <div {...stylex.props(s.kpiCard)}>
              <span {...stylex.props(s.kpiLabel)}>Em execução</span>
              <span {...stylex.props(s.kpiValue)}>{statusQuery.data.running_count}</span>
            </div>
            <div {...stylex.props(s.kpiCard)}>
              <span {...stylex.props(s.kpiLabel)}>Containers (LXC)</span>
              <span {...stylex.props(s.kpiValue)}>{statusQuery.data.container_count}</span>
            </div>
            <div {...stylex.props(s.kpiCard)}>
              <span {...stylex.props(s.kpiLabel)}>Máquinas Virtuais (KVM)</span>
              <span {...stylex.props(s.kpiValue)}>{statusQuery.data.vm_count}</span>
            </div>
          </div>

          {feedback && (
            <div
              role={feedback.tone === 'critical' ? 'alert' : 'status'}
              {...stylex.props(s.feedbackBox, feedback.tone === 'critical' ? s.feedbackError : s.feedbackSuccess)}
            >
              <StatusDot tone={feedback.tone} />
              <span>{feedback.text}</span>
            </div>
          )}

          {showCreate && (
            <Panel
              title="Criar nova instância"
              subtitle={isOCI
                ? 'Pull direto de registro OCI (Docker Hub / GHCR) como Application Container no Incus'
                : 'Pull automático via images.linuxcontainers.org (imagens de VM podem levar alguns minutos no primeiro download)'}
            >
              {isOCI && !statusQuery.data.oci_supported && (
                <Badge tone="warning" icon={<StatusDot tone="warning" />}>
                  Este host reportou Incus {statusQuery.data.server_version} sem extensão instance_oci. Atualize o Incus (6.3+ / 7.x LTS) para criar containers OCI.
                </Badge>
              )}
              <form {...stylex.props(layout.stack)} onSubmit={handleCreate} noValidate>
                <div {...stylex.props(s.formGrid)}>
                  <TextField
                    label="Nome"
                    value={newName}
                    onChange={e => setNewName(e.target.value)}
                    placeholder={isOCI ? 'ex: nginx-web' : 'ex: app-01'}
                    error={nameError}
                    required
                  />
                  <SelectField label="Tipo" value={newKind} onChange={e => setNewKind(e.target.value as 'container' | 'oci' | 'virtual-machine')}>
                    <option value="container">System Container (LXC)</option>
                    <option value="oci">App Container (OCI / Docker Hub)</option>
                    <option value="virtual-machine">Virtual Machine (KVM/QEMU)</option>
                  </SelectField>
                  {isOCI ? (
                    <>
                      <SelectField label="Registro OCI" value={newOCIRegistry} onChange={e => setNewOCIRegistry(e.target.value)}>
                        {OCI_REGISTRIES.map(r => <option key={r.value} value={r.value}>{r.label}</option>)}
                      </SelectField>
                      <SelectField label="Imagem OCI" value={newOCIImage} onChange={e => setNewOCIImage(e.target.value)}>
                        {OCI_PRESETS.map(p => <option key={p.value} value={p.value}>{p.label} ({p.value})</option>)}
                        <option value="__custom__">Outra imagem OCI…</option>
                      </SelectField>
                    </>
                  ) : (
                    <SelectField label="Imagem" value={newImage} onChange={e => setNewImage(e.target.value)}>
                      {IMAGE_PRESETS.map(p => <option key={p.value} value={p.value}>{p.label}</option>)}
                      <option value="__custom__">Outra (remote:alias)…</option>
                    </SelectField>
                  )}
                  {selectedImage === '__custom__' && (
                    <TextField
                      label={isOCI ? 'Imagem OCI customizada' : 'Alias customizado'}
                      value={customImage}
                      onChange={e => setCustomImage(e.target.value)}
                      placeholder={isOCI ? 'ex: nginx:alpine ou owner/app:latest' : 'images:ubuntu/24.04'}
                      error={customImageError}
                      required
                    />
                  )}
                  <TextField
                    label="Limite de CPU (vCPUs)"
                    type="number"
                    inputMode="numeric"
                    min={1}
                    max={128}
                    step={1}
                    value={newCPU}
                    onChange={e => setNewCPU(e.target.value.replace(/[^0-9]/g, ''))}
                    placeholder="Sem limite"
                    error={cpuError}
                  />
                  <TextField
                    label="Limite de RAM"
                    type="number"
                    inputMode="numeric"
                    min={1}
                    max={memMax}
                    step={1}
                    value={newMemValue}
                    onChange={e => setNewMemValue(e.target.value.replace(/[^0-9]/g, ''))}
                    placeholder="Sem limite"
                    error={memError}
                    action={
                      <select
                        aria-label="Unidade de RAM"
                        value={newMemUnit}
                        onChange={e => setNewMemUnit(e.target.value as 'MiB' | 'GiB')}
                        {...stylex.props(s.unitSelect)}
                      >
                        <option value="MiB">MiB</option>
                        <option value="GiB">GiB</option>
                      </select>
                    }
                  />
                  <SelectField label="Perfil" value={newProfile} onChange={e => setNewProfile(e.target.value)}>
                    {(resourcesQuery.data?.profiles.length ? resourcesQuery.data.profiles : [{ name: 'default', used_by_count: 0 }]).map(p => (
                      <option key={p.name} value={p.name}>{p.name}</option>
                    ))}
                  </SelectField>
                  <TextField label="Descrição (opcional)" value={newDesc} onChange={e => setNewDesc(e.target.value)} placeholder="Finalidade da instância" />
                  <TextField
                    label="Porta no Host (opcional)"
                    type="number"
                    inputMode="numeric"
                    min={1}
                    max={65535}
                    value={newHostPort}
                    onChange={e => setNewHostPort(e.target.value.replace(/[^0-9]/g, ''))}
                    placeholder="ex: 8080"
                    error={portError}
                  />
                  <TextField
                    label="Porta na Instância (opcional)"
                    type="number"
                    inputMode="numeric"
                    min={1}
                    max={65535}
                    value={newContainerPort}
                    onChange={e => setNewContainerPort(e.target.value.replace(/[^0-9]/g, ''))}
                    placeholder="ex: 80"
                  />
                </div>
                {isOCI && (
                  <div {...stylex.props(layout.compact)}>
                    <label htmlFor="oci-env-input" style={{ fontSize: 13, fontWeight: 500 }}>
                      Variáveis de ambiente (opcional, uma por linha: <code>CHAVE=VALOR</code>)
                    </label>
                    <textarea
                      id="oci-env-input"
                      value={newEnvText}
                      onChange={e => setNewEnvText(e.target.value)}
                      placeholder={'TZ=America/Sao_Paulo\nPOSTGRES_PASSWORD=secret'}
                      {...stylex.props(s.textarea)}
                    />
                    {envError && <span style={{ color: 'var(--vk-critical)', fontSize: 12 }}>{envError}</span>}
                  </div>
                )}
                <div {...stylex.props(layout.split)}>
                  <label {...stylex.props(s.checkboxRow)}>
                    <input type="checkbox" checked={newStart} onChange={e => setNewStart(e.target.checked)} />
                    Iniciar automaticamente após a criação
                  </label>
                  <div {...stylex.props(layout.row)}>
                    <Button type="button" variant="outline" onClick={() => setShowCreate(false)}>Cancelar</Button>
                    <Button type="submit" variant="primary" busy={createMutation.isPending} disabled={!formValid}>
                      {createMutation.isPending ? 'Criando…' : 'Criar instância'}
                    </Button>
                  </div>
                </div>
              </form>
            </Panel>
          )}

          {tab === 'instances' && (
            <Panel title="Instâncias" subtitle="Clique no nome para detalhes e snapshots">
              <div {...stylex.props(s.filterBar)}>
                <TextField
                  label="Buscar"
                  value={search}
                  onChange={e => { setSearch(e.target.value); setPage(0) }}
                  placeholder="Nome, IP ou descrição…"
                />
                <SelectField label="Tipo" value={typeFilter} onChange={e => { setTypeFilter(e.target.value as typeof typeFilter); setPage(0) }}>
                  <option value="all">Todos os tipos</option>
                  <option value="container">System Containers</option>
                  <option value="oci">Apps OCI</option>
                  <option value="virtual-machine">VMs</option>
                </SelectField>
                <SelectField label="Estado" value={statusFilter} onChange={e => { setStatusFilter(e.target.value as typeof statusFilter); setPage(0) }}>
                  <option value="all">Todos os estados</option>
                  <option value="Running">Rodando</option>
                  <option value="Stopped">Parados</option>
                </SelectField>
              </div>

              {instancesQuery.isPending && <Loading label="Carregando instâncias…" />}
              {instancesQuery.isError && <ErrorState error={instancesQuery.error} retry={() => { void instancesQuery.refetch() }} />}

              {instancesQuery.data && visible.length === 0 && (
                <Empty>Nenhuma instância encontrada. Crie uma nova instância ou ajuste os filtros acima.</Empty>
              )}

              {visible.length > 0 && (
                <>
                  <div {...stylex.props(tableStyles.scroll)}>
                    <table {...stylex.props(tableStyles.table)}>
                      <thead>
                        <tr>
                          <th {...stylex.props(tableStyles.th)}>Instância</th>
                          <th {...stylex.props(tableStyles.th)}>Estado</th>
                          <th {...stylex.props(tableStyles.th)}>IPv4</th>
                          <th {...stylex.props(tableStyles.th)}>RAM / Limite</th>
                          <th {...stylex.props(tableStyles.th)}>CPU</th>
                          <th {...stylex.props(tableStyles.th)}>Snaps</th>
                          <th {...stylex.props(tableStyles.th)}>Ações</th>
                        </tr>
                      </thead>
                      <tbody>
                        {visible.map(inst => (
                          <tr key={inst.name} {...stylex.props(tableStyles.row)}>
                            <td {...stylex.props(tableStyles.td)}>
                              <div>
                                <Link to={`/instances/${encodeURIComponent(inst.name)}`} {...stylex.props(s.nameLink)}>
                                  {inst.type === 'virtual-machine' ? <Monitor size={14} aria-hidden /> : <Cpu size={14} aria-hidden />}
                                  <span>{inst.name}</span>
                                  <span {...stylex.props(s.typeTag, inst.oci && s.typeTagOCI)}>
                                    {inst.type === 'virtual-machine' ? 'VM' : inst.oci ? 'OCI' : 'CT'}
                                  </span>
                                </Link>
                                {inst.description && <div {...stylex.props(s.sub)}>{inst.description}</div>}
                              </div>
                            </td>
                            <td {...stylex.props(tableStyles.td)}>
                              <Badge tone={statusTone(inst.status)} icon={<StatusDot tone={statusTone(inst.status)} />}>
                                {inst.status}
                              </Badge>
                            </td>
                            <td {...stylex.props(tableStyles.td, s.mono)}>
                              {inst.ipv4.length > 0 ? inst.ipv4[0] : '—'}
                            </td>
                            <td {...stylex.props(tableStyles.td, s.mono)}>
                              {inst.memory_usage_bytes > 0 ? formatValue(inst.memory_usage_bytes, 'bytes') : '—'}
                              {inst.memory_limit ? ` / ${inst.memory_limit}` : ''}
                            </td>
                            <td {...stylex.props(tableStyles.td, s.mono)}>
                              {inst.cpu_limit ? `${inst.cpu_limit} vCPU` : '—'}
                            </td>
                            <td {...stylex.props(tableStyles.td, s.mono)}>
                              {inst.snapshots_count}
                            </td>
                            <td {...stylex.props(tableStyles.td)}>
                              <div {...stylex.props(layout.row)}>
                                {inst.status !== 'Running' && inst.status !== 'Frozen' && (
                                  <Button variant="outline" title="Iniciar" aria-label={`Iniciar ${inst.name}`}
                                    disabled={stateMutation.isPending}
                                    onClick={() => stateMutation.mutate({ name: inst.name, action: 'start' })}>
                                    <Play size={13} aria-hidden />
                                  </Button>
                                )}
                                {inst.status === 'Frozen' && (
                                  <Button variant="outline" title="Retomar" aria-label={`Retomar ${inst.name}`}
                                    disabled={stateMutation.isPending}
                                    onClick={() => stateMutation.mutate({ name: inst.name, action: 'unfreeze' })}>
                                    <Play size={13} aria-hidden />
                                  </Button>
                                )}
                                {inst.status === 'Running' && (
                                  <>
                                    <Button variant="outline" title="Reiniciar" aria-label={`Reiniciar ${inst.name}`}
                                      disabled={stateMutation.isPending}
                                      onClick={() => stateMutation.mutate({ name: inst.name, action: 'restart' })}>
                                      <RotateCw size={13} aria-hidden />
                                    </Button>
                                    <Button variant="outline" title="Pausar" aria-label={`Pausar ${inst.name}`}
                                      disabled={stateMutation.isPending}
                                      onClick={() => stateMutation.mutate({ name: inst.name, action: 'freeze' })}>
                                      <Pause size={13} aria-hidden />
                                    </Button>
                                    <Button variant="outline" title="Parar" aria-label={`Parar ${inst.name}`}
                                      disabled={stateMutation.isPending}
                                      onClick={() => stateMutation.mutate({ name: inst.name, action: 'stop' })}>
                                      <Power size={13} aria-hidden />
                                    </Button>
                                  </>
                                )}
                              </div>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>

                  {totalPages > 1 && (
                    <div {...stylex.props(s.pagination)}>
                      <span>Mostrando {safePage * PAGE_SIZE + 1}–{Math.min((safePage + 1) * PAGE_SIZE, filtered.length)} de {filtered.length}</span>
                      <div {...stylex.props(layout.row)}>
                        <Button variant="outline" disabled={safePage === 0} onClick={() => setPage(p => Math.max(0, p - 1))}>
                          <ChevronLeft size={14} aria-hidden /> Anterior
                        </Button>
                        <span>Página {safePage + 1} / {totalPages}</span>
                        <Button variant="outline" disabled={safePage >= totalPages - 1} onClick={() => setPage(p => Math.min(totalPages - 1, p + 1))}>
                          Próxima <ChevronRight size={14} aria-hidden />
                        </Button>
                      </div>
                    </div>
                  )}
                </>
              )}
            </Panel>
          )}

          {tab === 'infrastructure' && (
            <div {...stylex.props(layout.grid2)}>
              <Panel title="Storage Pools" subtitle="Pools configurados no Incus">
                {resourcesQuery.isPending && <Loading />}
                {resourcesQuery.isError && <ErrorState error={resourcesQuery.error} retry={() => { void resourcesQuery.refetch() }} />}
                {resourcesQuery.data && resourcesQuery.data.storage_pools.length === 0 && (
                  <Empty>Nenhum storage pool encontrado. Inicialize o Incus para criar um storage pool.</Empty>
                )}
                {resourcesQuery.data && resourcesQuery.data.storage_pools.length > 0 && (
                  <div {...stylex.props(tableStyles.scroll)}>
                    <table {...stylex.props(tableStyles.table)}>
                      <thead>
                        <tr>
                          <th {...stylex.props(tableStyles.th)}>Pool</th>
                          <th {...stylex.props(tableStyles.th)}>Driver</th>
                          <th {...stylex.props(tableStyles.th)}>Uso</th>
                          <th {...stylex.props(tableStyles.th)}>Itens</th>
                        </tr>
                      </thead>
                      <tbody>
                        {resourcesQuery.data.storage_pools.map(pool => (
                          <tr key={pool.name} {...stylex.props(tableStyles.row)}>
                            <td {...stylex.props(tableStyles.td, s.mono)}>{pool.name}</td>
                            <td {...stylex.props(tableStyles.td)}>{pool.driver}</td>
                            <td {...stylex.props(tableStyles.td, s.mono)}>
                              {pool.total_bytes > 0 ? `${formatValue(pool.used_bytes, 'bytes')} / ${formatValue(pool.total_bytes, 'bytes')}` : '—'}
                            </td>
                            <td {...stylex.props(tableStyles.td, s.mono)}>{pool.used_by_count}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
              </Panel>

              <Panel title="Redes" subtitle="Bridges e redes gerenciadas">
                {resourcesQuery.isPending && <Loading />}
                {resourcesQuery.isError && <ErrorState error={resourcesQuery.error} retry={() => { void resourcesQuery.refetch() }} />}
                {resourcesQuery.data && resourcesQuery.data.networks.length === 0 && (
                  <Empty>Nenhuma rede Incus encontrada.</Empty>
                )}
                {resourcesQuery.data && resourcesQuery.data.networks.length > 0 && (
                  <div {...stylex.props(tableStyles.scroll)}>
                    <table {...stylex.props(tableStyles.table)}>
                      <thead>
                        <tr>
                          <th {...stylex.props(tableStyles.th)}>Rede</th>
                          <th {...stylex.props(tableStyles.th)}>Tipo</th>
                          <th {...stylex.props(tableStyles.th)}>IPv4</th>
                          <th {...stylex.props(tableStyles.th)}>Em uso</th>
                        </tr>
                      </thead>
                      <tbody>
                        {resourcesQuery.data.networks.map(net => (
                          <tr key={net.name} {...stylex.props(tableStyles.row)}>
                            <td {...stylex.props(tableStyles.td, s.mono)}>
                              <span {...stylex.props(layout.row)}>
                                <Network size={13} aria-hidden /> {net.name}
                              </span>
                            </td>
                            <td {...stylex.props(tableStyles.td)}>{net.type}{net.managed ? '' : ' (unmanaged)'}</td>
                            <td {...stylex.props(tableStyles.td, s.mono)}>{net.ipv4_address || '—'}</td>
                            <td {...stylex.props(tableStyles.td, s.mono)}>{net.used_by_count}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
              </Panel>
            </div>
          )}
        </>
      )}
    </div>
  )
}
