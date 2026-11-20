import * as stylex from '@stylexjs/stylex'
import { Cpu, Gauge, MemoryStick } from 'lucide-react'
import { useEffect, useId, useRef } from 'react'
import { Navigate, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { hostTabs, isHostTab } from '../../app/routes'
import type { HostTab } from '../../app/routes'
import { Facts, MetricCard, PageHeading } from '../../components/ui/dashboard'
import { Empty, ErrorState, Loading, Panel, layout } from '../../components/ui/ui'
import type { Host, Resource, ResourceKind } from '../../lib/api/client'
import { useLiveReading } from '../../lib/realtime/hooks'
import { useApi, useHost } from '../../lib/api/queries'
import { ChartPanel } from '../metrics/ChartPanel'
import { metric } from '../metrics/catalog'
import { ChartCursorProvider } from '../metrics/cursor'
import { dateTime } from '../metrics/format'
import { TimeRangeSelector, usePeriodLink } from '../metrics/range'
import { LiveValue, PersistedValue } from '../metrics/readings'
import { ResourceTable } from './ResourceTable'
import type { ResourceColumn } from './ResourceTable'
import { displayName, metadata, metadataText } from './resourceMetadata'

const s = stylex.create({
  tabs: { display: 'flex', flexWrap: 'wrap', gap: '4px 8px', borderBlockEndWidth: 1, borderBlockEndStyle: 'solid', borderBlockEndColor: 'var(--vk-border-subtle)', paddingBlockEnd: 4 },
  tab: {
    display: 'inline-flex', alignItems: 'center', backgroundColor: { default: 'transparent', ':hover': 'var(--vk-surface-3)' },
    color: 'var(--vk-text-secondary)', borderTopWidth: 0, borderLeftWidth: 0, borderRightWidth: 0, borderBlockEndWidth: 2, borderBlockEndStyle: 'solid', borderBlockEndColor: 'transparent', borderRadius: 0,
    fontWeight: 600, paddingInline: { default: 8, '@media (min-width: 768px)': 12 }, minHeight: 44, cursor: 'pointer',
  },
  active: { color: 'var(--vk-accent)', borderBlockEndColor: 'var(--vk-accent)', backgroundColor: 'var(--vk-surface-2)' },
})

function live(id: string | undefined, key: string, unit: string, detail = true) {
  return id ? <LiveValue id={id} metric={key} unit={unit} detail={detail} /> : '—'
}

function liveColumn(label: string, key: string, unit: string): ResourceColumn {
  return { label, cell: resource => resource.active ? live(resource.id, key, unit, false) : '—' }
}

function persistedColumn(label: string, key: string, unit: string): ResourceColumn {
  return { label, cell: resource => resource.active ? <PersistedValue id={resource.id} metric={key} unit={unit} /> : '—' }
}

const coreColumns = [liveColumn('Utilização', metric.core, '%')]
const fsColumns = [
  { label: 'Tipo', cell: (r: Resource) => metadataText(r, 'fs_type') ?? '—' },
  persistedColumn('Capacidade', metric.fsTotal, 'bytes'),
  persistedColumn('Em uso', metric.fsUsed, 'bytes'),
  persistedColumn('Disponível', metric.fsAvailable, 'bytes'),
]
const diskColumns = [
  liveColumn('Leitura', metric.diskRead, 'bytes/s'), liveColumn('Escrita', metric.diskWrite, 'bytes/s'),
  liveColumn('Leituras/s', metric.diskReads, 'ops/s'), liveColumn('Escritas/s', metric.diskWrites, 'ops/s'),
]
const nicColumns = [
  { label: 'Estado da NIC', cell: (r: Resource) => metadataText(r, 'operstate') ?? '—' },
  liveColumn('Recepção', metric.rx, 'bytes/s'), liveColumn('Transmissão', metric.tx, 'bytes/s'),
  liveColumn('Erros RX', metric.rxErrors, 'errors/s'), liveColumn('Erros TX', metric.txErrors, 'errors/s'),
  liveColumn('Descartes RX', metric.rxDrops, 'drops/s'), liveColumn('Descartes TX', metric.txDrops, 'drops/s'),
]
const sensorColumns = [
  { label: 'Chip', cell: (r: Resource) => metadataText(r, 'chip') ?? '—' },
  { label: 'Canal', cell: (r: Resource) => metadataText(r, 'channel') ?? '—' },
  persistedColumn('Temperatura', metric.temperature, '°C'),
]

function CpuView({ host }: { host?: Host }) {
  const id = host?.resource.id
  return <>
    <div {...stylex.props(layout.grid2)}>
      <MetricCard label="CPU total" icon={Cpu} value={live(id, metric.cpu, '%')} context="Utilização ao vivo" />
      <Panel title="Processador">
        <Facts items={[
          ['Modelo', host?.inventory.cpu_model || undefined],
          ['Núcleos', host?.inventory.cpu_cores || undefined],
          ['Threads', host?.inventory.cpu_threads || undefined],
          ['Arquitetura', host?.inventory.arch || undefined],
        ]} />
      </Panel>
    </div>
    <Panel title="Carga média">
      <Facts items={[['1 minuto', live(id, metric.load1, 'load')], ['5 minutos', live(id, metric.load5, 'load')], ['15 minutos', live(id, metric.load15, 'load')]]} />
    </Panel>
    <div {...stylex.props(layout.grid2)}>
      <ChartPanel title="Utilização da CPU" resourceId={id} metric={metric.cpu} />
      <ChartPanel title="Carga média de 1 minuto" resourceId={id} metric={metric.load1} />
    </div>
    <ResourceTable kind="cpu_core" title="Núcleos da CPU" columns={coreColumns} empty="Nenhum núcleo foi identificado pelo coletor." />
  </>
}

function SwapUtilization({ id }: { id: string }) {
  const total = useLiveReading(id, metric.swapTotal)
  return total.state === 'valid' && total.point?.value === 0
    ? <span>Não configurado</span>
    : <LiveValue id={id} metric={metric.swap} unit="%" detail />
}

function MemoryView({ host }: { host?: Host }) {
  const id = host?.resource.id
  return <>
    <div {...stylex.props(layout.grid2)}>
      <MetricCard label="Memória em uso" icon={MemoryStick} value={live(id, metric.ram, '%')} context="RAM ao vivo" />
      <MetricCard label="Utilização de swap" icon={Gauge} dense value={id ? <SwapUtilization id={id} /> : '—'} context="Swap ao vivo" />
    </div>
    <Panel title="Distribuição da memória">
      <Facts items={[
        ['RAM total', live(id, metric.ramTotal, 'bytes')], ['RAM usada', live(id, metric.ramUsed, 'bytes')],
        ['RAM disponível', live(id, metric.ramAvailable, 'bytes')], ['Cache', live(id, metric.cache, 'bytes')],
        ['Buffers', live(id, metric.buffers, 'bytes')], ['Swap total', live(id, metric.swapTotal, 'bytes')],
        ['Swap usada', live(id, metric.swapUsed, 'bytes')],
      ]} />
    </Panel>
    <div {...stylex.props(layout.grid2)}>
      <ChartPanel title="Uso de memória" resourceId={id} metric={metric.ram} color="memory" />
      <ChartPanel title="Uso de swap" resourceId={id} metric={metric.swap} color="memory" />
    </div>
  </>
}

const metadataLabels: Partial<Record<ResourceKind, [string, string][]>> = {
  filesystem: [['mountpoint', 'Montagem'], ['fs_type', 'Tipo'], ['source', 'Origem']],
  disk: [['kernel_name', 'Dispositivo'], ['model', 'Modelo'], ['dm_name', 'Device mapper']],
  nic: [['operstate', 'Estado da NIC'], ['mac', 'MAC'], ['pci_addr', 'PCI'], ['master', 'Interface mestre']],
  sensor: [['chip', 'Chip'], ['channel', 'Canal'], ['dev_id', 'Dispositivo']],
}

function ResourceDetails({ id, allowed }: { id: string; allowed: ResourceKind[] }) {
  const api = useApi()
  const query = useQuery({ queryKey: ['resource', id], queryFn: ({ signal }) => api.resource(id, signal), staleTime: 45_000, refetchInterval: 60_000 })
  if (query.isPending) return <Panel title="Detalhe do recurso"><Loading /></Panel>
  if (!query.data) return <Panel title="Detalhe do recurso"><ErrorState error={query.error} retry={() => { void query.refetch() }} /></Panel>
  const resource = query.data
  if (!allowed.includes(resource.kind)) return <Panel title="Detalhe do recurso"><Empty>Este recurso pertence a outro subsistema.</Empty></Panel>

  const extra = (metadataLabels[resource.kind] ?? []).map(([key, label]): [string, string | undefined] => [label, metadataText(resource, key)])
  const meta = metadata(resource)
  return <>
    <Panel title={displayName(resource)} emphasized>
      {query.isRefetchError && <ErrorState error={query.error} retry={() => { void query.refetch() }} />}
      <Facts items={[
        ['ID persistente', <span {...stylex.props(layout.technical)}>{resource.id}</span>],
        ['Última descoberta', dateTime(resource.last_seen_ms)],
        ['Estado no inventário', resource.active ? 'Ativo' : 'Inativo'],
        ...extra,
        ...(resource.kind === 'nic' && typeof meta.include_in_host_aggregate === 'boolean'
          ? [['Agregado do host', meta.include_in_host_aggregate ? 'Incluída' : 'Excluída'] as [string, string]] : []),
      ]} />
      {resource.kind === 'sensor' && <Facts items={sensorColumns.slice(-1).map(column => [column.label, column.cell(resource)])} />}
    </Panel>
    {resource.kind === 'filesystem' && <ChartPanel title="Utilização da montagem" resourceId={id} metric={metric.fs} color="storage" />}
    {resource.kind === 'disk' && <div {...stylex.props(layout.grid2)}>
      <ChartPanel title="Leitura por segundo" resourceId={id} metric={metric.diskRead} color="storage" />
      <ChartPanel title="Escrita por segundo" resourceId={id} metric={metric.diskWrite} color="tx" />
    </div>}
    {resource.kind === 'nic' && <div {...stylex.props(layout.grid2)}>
      <ChartPanel title="Recepção por segundo" resourceId={id} metric={metric.rx} color="rx" />
      <ChartPanel title="Transmissão por segundo" resourceId={id} metric={metric.tx} color="tx" />
    </div>}
    {resource.kind === 'sensor' && <ChartPanel title="Histórico de temperatura" resourceId={id} metric={metric.temperature} color="storage" />}
  </>
}

function HostResources({ tab, host }: { tab: Exclude<HostTab, 'cpu' | 'memory'>; host?: Host }) {
  const [search] = useSearchParams()
  const link = usePeriodLink()
  const selected = search.get('resource') ?? undefined
  const url = (id: string) => link('/host/' + tab, id)
  const detail = useRef<HTMLElement>(null)
  useEffect(() => {
    if (selected) detail.current?.focus()
  }, [selected])
  const allowed: ResourceKind[] = tab === 'storage' ? ['filesystem', 'disk'] : tab === 'network' ? ['nic'] : ['sensor']

  return <>
    {tab === 'network' && <Panel title="Tráfego agregado do host">
      <p {...stylex.props(layout.text)}>Taxas por segundo das interfaces incluídas pelo coletor no agregado do host.</p>
      <Facts items={[
        ['Recepção', live(host?.resource.id, metric.rx, 'bytes/s')],
        ['Transmissão', live(host?.resource.id, metric.tx, 'bytes/s')],
      ]} />
    </Panel>}
    {selected && <section aria-label="Recurso selecionado" tabIndex={-1} ref={detail} {...stylex.props(layout.stack)}>
      {/^[A-Za-z0-9:_-]{1,128}$/.test(selected)
        ? <ResourceDetails key={selected} id={selected} allowed={allowed} />
        : <Panel title="Detalhe do recurso"><Empty>O link contém um identificador inválido.</Empty></Panel>}
    </section>}
    {!selected && tab === 'network' && <div {...stylex.props(layout.grid2)}>
      <ChartPanel title="Histórico de recepção do host" resourceId={host?.resource.id} metric={metric.rx} color="rx" />
      <ChartPanel title="Histórico de transmissão do host" resourceId={host?.resource.id} metric={metric.tx} color="tx" />
    </div>}
    {!selected && tab !== 'network' && <p {...stylex.props(layout.text)}>Selecione um recurso na tabela para abrir seu histórico.</p>}
    {tab === 'storage' && <>
      <ResourceTable kind="filesystem" title="Capacidade dos sistemas de arquivos" columns={fsColumns} selected={selected} href={url} />
      <ResourceTable kind="disk" title="I/O dos dispositivos de bloco" columns={diskColumns} selected={selected} href={url} />
    </>}
    {tab === 'network' && <ResourceTable kind="nic" title="Interfaces de rede" columns={nicColumns} selected={selected} href={url} />}
    {tab === 'sensors' && <ResourceTable kind="sensor" title="Sensores de temperatura" empty="Nenhum sensor encontrado." columns={sensorColumns} selected={selected} href={url} />}
  </>
}

const descriptions: Record<HostTab, string> = {
  cpu: 'Utilização, carga média e topologia do processador',
  memory: 'Uso de RAM e swap',
  storage: 'Capacidade por montagem e atividade por dispositivo',
  network: 'Tráfego, erros e descartes por interface',
  sensors: 'Leituras do coletor de temperatura',
}

export function HostPage() {
  const { subsystem } = useParams()
  const nav = useNavigate()
  const link = usePeriodLink()
  const host = useHost()
  const id = useId()
  if (!isHostTab(subsystem)) return <Navigate replace to={link('/host/cpu')} />

  return (
    <div {...stylex.props(layout.page)}>
      <PageHeading title="Host" description={descriptions[subsystem]} action={<TimeRangeSelector />} />
      <div role="tablist" aria-label="Subsistemas do host" {...stylex.props(s.tabs)}
        onKeyDown={event => {
          const index = hostTabs.findIndex(tab => tab.id === subsystem)
          const move = event.key === 'ArrowLeft' ? index - 1 : event.key === 'ArrowRight' ? index + 1 :
            event.key === 'Home' ? 0 : event.key === 'End' ? hostTabs.length - 1 : undefined
          if (move === undefined) return
          event.preventDefault()
          const next = hostTabs[(move + hostTabs.length) % hostTabs.length].id
          void nav(link('/host/' + next))
          event.currentTarget.querySelector<HTMLButtonElement>(`[data-tab="${next}"]`)?.focus()
        }}>
        {hostTabs.map(tab => <button type="button" role="tab" key={tab.id} data-tab={tab.id} id={id + tab.id}
          aria-controls={id + '-panel'} aria-selected={tab.id === subsystem} tabIndex={tab.id === subsystem ? 0 : -1}
          onClick={() => { if (tab.id !== subsystem) void nav(link('/host/' + tab.id)) }}
          {...stylex.props(s.tab, tab.id === subsystem && s.active)}>{tab.label}</button>)}
      </div>
      <div role="tabpanel" id={id + '-panel'} aria-labelledby={id + subsystem} {...stylex.props(layout.stack)}>
        {host.isPending && <Loading label="Consultando inventário do host…" />}
        {host.isError && <ErrorState error={host.error} retry={() => { void host.refetch() }} title="Inventário do host indisponível" />}
        <ChartCursorProvider key={subsystem}>
          {subsystem === 'cpu' ? <CpuView host={host.data} /> : subsystem === 'memory' ? <MemoryView host={host.data} /> :
            <HostResources key={subsystem} tab={subsystem} host={host.data} />}
        </ChartCursorProvider>
      </div>
    </div>
  )
}
