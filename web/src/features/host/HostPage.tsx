import * as stylex from '@stylexjs/stylex'
import { Cpu, Gauge, LayoutGrid, MemoryStick, Rows2 } from 'lucide-react'
import { useEffect, useId, useRef } from 'react'
import { Navigate, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { usePreferences } from '#/app/preferences'
import { hostTabs, isHostTab } from '#/app/routes'
import type { HostTab } from '#/app/routes'
import { Facts, MetricCard, PageHeading } from '#/components/ui/dashboard'
import { Button, Empty, ErrorState, Loading, Panel, layout } from '#/components/ui/ui'
import type { Host, Resource, ResourceKind } from '#/lib/api/client'
import { useLiveReading } from '#/lib/realtime/hooks'
import { useApi, useHost, useResources } from '#/lib/api/queries'
import { ChartPanel } from '#/features/metrics/ChartPanel'
import { TemperatureChartPanel } from '#/features/metrics/TemperatureChartPanel'
import { metric } from '#/features/metrics/catalog'
import { ChartCursorProvider } from '#/features/metrics/cursor'
import { dateTime } from '#/features/metrics/format'
import { TimeRangeSelector, usePeriodLink } from '#/features/metrics/range'
import { LiveValue, PersistedValue } from '#/features/metrics/readings'
import { ResourceTable } from './ResourceTable'
import type { ResourceColumn } from './ResourceTable'
import { ProcessesView, ServicesView } from './HostOperations'
import { displayName, metadata, metadataText } from './resourceMetadata'

const s = stylex.create({
  headerCard: {
    display: 'grid', gap: 14, minWidth: 0,
    backgroundColor: 'var(--vk-surface-1)', borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-subtle)',
    borderRadius: 'var(--vk-radius-3)', padding: { default: 16, '@media (min-width: 1024px)': 20 },
  },
  tabs: {
    display: 'inline-flex', flexWrap: 'wrap', gap: 4, width: 'fit-content', maxWidth: '100%',
    padding: 4, borderRadius: 'var(--vk-radius-2)', backgroundColor: 'var(--vk-surface-2)',
    borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-subtle)',
  },
  tab: {
    display: 'inline-flex', alignItems: 'center',
    backgroundColor: { default: 'transparent', ':hover': 'var(--vk-surface-3)' },
    color: { default: 'var(--vk-text-secondary)', ':hover': 'var(--vk-text-primary)' },
    borderWidth: 1, borderStyle: 'solid', borderColor: 'transparent',
    borderRadius: 'var(--vk-radius-1)', fontSize: 13, fontWeight: 500,
    paddingInline: { default: 10, '@media (min-width: 768px)': 14 }, minHeight: { default: 34, '@media (pointer: coarse)': 44 },
    cursor: 'pointer', transition: 'background-color 150ms, color 150ms, border-color 150ms',
  },
  active: {
    color: 'var(--vk-text-primary)', backgroundColor: 'var(--vk-surface-1)',
    borderColor: 'var(--vk-border-strong)', fontWeight: 600,
  },
  controls: { display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8 },
  layoutBtn: { display: { default: 'none', '@media (min-width: 1024px)': 'inline-flex' } },
})

function live(id: string | undefined, key: string, unit: string, detail = true, bar = false) {
  return id ? <LiveValue id={id} metric={key} unit={unit} detail={detail} bar={bar} /> : '—'
}

function liveColumn(label: string, key: string, unit: string, compactBar = false): ResourceColumn {
  return { label, cell: resource => resource.active ? <LiveValue id={resource.id} metric={key} unit={unit} compactBar={compactBar} /> : '—' }
}

function persistedColumn(label: string, key: string, unit: string): ResourceColumn {
  return { label, cell: resource => resource.active ? <PersistedValue id={resource.id} metric={key} unit={unit} /> : '—' }
}

const coreColumns = [liveColumn('Utilização', metric.core, '%', true)]
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

function CpuView({ host, singleColumn }: { host?: Host; singleColumn: boolean }) {
  const id = host?.resource.id
  return <>
    <div {...stylex.props(layout.grid2)}>
      <MetricCard label="CPU total" icon={Cpu} value={live(id, metric.cpu, '%', true, true)} context="Utilização ao vivo" />
      <Panel title="Processador" subtitle="Topologia e modelo identificados">
        <Facts items={[
          ['Modelo', host?.inventory.cpu_model || undefined],
          ['Núcleos', host?.inventory.cpu_cores || undefined],
          ['Threads', host?.inventory.cpu_threads || undefined],
          ['Arquitetura', host?.inventory.arch || undefined],
        ]} />
      </Panel>
    </div>
    <Panel title="Carga média" subtitle="Load average do sistema operacional">
      <Facts items={[['1 minuto', live(id, metric.load1, 'load')], ['5 minutos', live(id, metric.load5, 'load')], ['15 minutos', live(id, metric.load15, 'load')]]} />
    </Panel>
    <div {...stylex.props(singleColumn ? layout.grid1 : layout.grid2)}>
      <ChartPanel title="Utilização da CPU" subtitle="Percentual médio de ocupação" resourceId={id} metric={metric.cpu} color="cpu" />
      <ChartPanel title="Carga média de 1 minuto" subtitle="Pressão de processos em execução" resourceId={id} metric={metric.load1} color="cpu" />
    </div>
    <ResourceTable kind="cpu_core" title="Núcleos da CPU" columns={coreColumns} empty="Nenhum núcleo foi identificado pelo coletor." />
  </>
}

function SwapUtilization({ id }: { id: string }) {
  const total = useLiveReading(id, metric.swapTotal)
  return total.state === 'valid' && total.point?.value === 0
    ? <span>Não configurado</span>
    : <LiveValue id={id} metric={metric.swap} unit="%" detail bar />
}

function MemoryView({ host, singleColumn }: { host?: Host; singleColumn: boolean }) {
  const id = host?.resource.id
  return <>
    <div {...stylex.props(layout.grid2)}>
      <MetricCard label="Memória em uso" icon={MemoryStick} value={live(id, metric.ram, '%', true, true)} context="RAM ao vivo" />
      <MetricCard label="Utilização de swap" icon={Gauge} dense value={id ? <SwapUtilization id={id} /> : '—'} context="Swap ao vivo" />
    </div>
    <Panel title="Distribuição da memória" subtitle="Alocação atual de RAM, cache, buffers e swap">
      <Facts items={[
        ['RAM total', live(id, metric.ramTotal, 'bytes')], ['RAM usada', live(id, metric.ramUsed, 'bytes')],
        ['RAM disponível', live(id, metric.ramAvailable, 'bytes')], ['Cache', live(id, metric.cache, 'bytes')],
        ['Buffers', live(id, metric.buffers, 'bytes')], ['Swap total', live(id, metric.swapTotal, 'bytes')],
        ['Swap usada', live(id, metric.swapUsed, 'bytes')],
      ]} />
    </Panel>
    <div {...stylex.props(singleColumn ? layout.grid1 : layout.grid2)}>
      <ChartPanel title="Uso de memória" subtitle="Percentual de RAM utilizada" resourceId={id} metric={metric.ram} color="memory" scale="full" />
      <ChartPanel title="Uso de swap" subtitle="Percentual de swap utilizada" resourceId={id} metric={metric.swap} color="memory" scale="full" />
    </div>
  </>
}

const metadataLabels: Partial<Record<ResourceKind, [string, string][]>> = {
  filesystem: [['mountpoint', 'Montagem'], ['fs_type', 'Tipo'], ['source', 'Origem']],
  disk: [['kernel_name', 'Dispositivo'], ['model', 'Modelo'], ['dm_name', 'Device mapper']],
  nic: [['operstate', 'Estado da NIC'], ['mac', 'MAC'], ['pci_addr', 'PCI'], ['master', 'Interface mestre']],
  sensor: [['chip', 'Chip'], ['channel', 'Canal'], ['dev_id', 'Dispositivo']],
}

function ResourceDetails({ id, allowed, singleColumn }: { id: string; allowed: ResourceKind[]; singleColumn: boolean }) {
  const api = useApi()
  const query = useQuery({ queryKey: ['resource', id], queryFn: ({ signal }) => api.resource(id, signal), staleTime: 45_000, refetchInterval: 60_000 })
  if (query.isPending) return <Panel title="Detalhe do recurso"><Loading /></Panel>
  if (!query.data) return <Panel title="Detalhe do recurso"><ErrorState error={query.error} retry={() => { void query.refetch() }} /></Panel>
  const resource = query.data
  if (!allowed.includes(resource.kind)) return <Panel title="Detalhe do recurso"><Empty>Este recurso pertence a outro subsistema.</Empty></Panel>

  const extra = (metadataLabels[resource.kind] ?? []).map(([key, label]): [string, string | undefined] => [label, metadataText(resource, key)])
  const meta = metadata(resource)
  return <>
    <Panel title={displayName(resource)} subtitle="Atributos persistentes do recurso selecionado" emphasized>
      {query.isRefetchError && <ErrorState error={query.error} retry={() => { void query.refetch() }} />}
      <Facts items={[
        ['ID persistente', <span {...stylex.props(layout.technical)}>{resource.id}</span>],
        ['Última descoberta', dateTime(resource.last_seen_ms)],
        ['Estado no inventário', resource.active ? 'Ativo' : 'Inativo'],
        ...extra,
        ...(resource.kind === 'nic' && typeof meta['include_in_host_aggregate'] === 'boolean'
          ? [['Agregado do host', meta['include_in_host_aggregate'] ? 'Incluída' : 'Excluída'] as [string, string]] : []),
      ]} />
      {resource.kind === 'sensor' && <Facts items={sensorColumns.slice(-1).map(column => [column.label, column.cell(resource)])} />}
    </Panel>
    {resource.kind === 'filesystem' && <ChartPanel title="Utilização da montagem" subtitle="Espaço ocupado na partição" resourceId={id} metric={metric.fs} color="storage" scale="full" />}
    {resource.kind === 'disk' && <div {...stylex.props(singleColumn ? layout.grid1 : layout.grid2)}>
      <ChartPanel title="Leitura por segundo" subtitle="Throughput de leitura do bloco" resourceId={id} metric={metric.diskRead} color="disk-io" />
      <ChartPanel title="Escrita por segundo" subtitle="Throughput de escrita do bloco" resourceId={id} metric={metric.diskWrite} color="tx" />
    </div>}
    {resource.kind === 'nic' && <div {...stylex.props(singleColumn ? layout.grid1 : layout.grid2)}>
      <ChartPanel title="Recepção por segundo" subtitle="Tráfego de entrada da interface" resourceId={id} metric={metric.rx} color="rx" />
      <ChartPanel title="Transmissão por segundo" subtitle="Tráfego de saída da interface" resourceId={id} metric={metric.tx} color="tx" />
    </div>}
    {resource.kind === 'sensor' && <ChartPanel title="Histórico de temperatura" subtitle="Leituras térmicas do canal" resourceId={id} metric={metric.temperature} color="sensor" />}
  </>
}

function SensorsOverviewChart() {
  const sensors = useResources('sensor')
  const active = sensors.data?.filter(sensor => sensor.active) ?? []
  if (active.length === 0) return null
  return <TemperatureChartPanel sensors={active} />
}

function HostResources({ tab, host, singleColumn }: { tab: Extract<HostTab, 'storage' | 'network' | 'sensors'>; host?: Host; singleColumn: boolean }) {
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
    {tab === 'network' && <Panel title="Tráfego agregado do host" subtitle="Taxas por segundo das interfaces incluídas pelo coletor no agregado do host.">
      <Facts items={[
        ['Recepção', live(host?.resource.id, metric.rx, 'bytes/s')],
        ['Transmissão', live(host?.resource.id, metric.tx, 'bytes/s')],
      ]} />
    </Panel>}
    {selected && <section aria-label="Recurso selecionado" tabIndex={-1} ref={detail} {...stylex.props(layout.stack)}>
      {/^[A-Za-z0-9:_-]{1,128}$/.test(selected)
        ? <ResourceDetails key={selected} id={selected} allowed={allowed} singleColumn={singleColumn} />
        : <Panel title="Detalhe do recurso"><Empty>O link contém um identificador inválido.</Empty></Panel>}
    </section>}
    {!selected && tab === 'network' && <div {...stylex.props(singleColumn ? layout.grid1 : layout.grid2)}>
      <ChartPanel title="Histórico de recepção do host" subtitle="Volume recebido agregado" resourceId={host?.resource.id} metric={metric.rx} color="rx" />
      <ChartPanel title="Histórico de transmissão do host" subtitle="Volume enviado agregado" resourceId={host?.resource.id} metric={metric.tx} color="tx" />
    </div>}
    {!selected && tab === 'sensors' && <SensorsOverviewChart />}
    {!selected && tab !== 'network' && <p {...stylex.props(layout.small)}>Selecione um recurso na tabela para abrir seu histórico.</p>}
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
  services: 'Serviços systemd e logs recentes do journald',
  processes: 'Processos ativos ordenados por CPU ou memória',
}

export function HostPage() {
  const { subsystem } = useParams()
  const nav = useNavigate()
  const link = usePeriodLink()
  const host = useHost()
  const { singleColumn, setSingleColumn } = usePreferences()
  const id = useId()
  if (!isHostTab(subsystem)) return <Navigate replace to={link('/host/cpu')} />

  return (
    <div {...stylex.props(layout.page)}>
      <div {...stylex.props(s.headerCard)}>
        <PageHeading title="Host" description={descriptions[subsystem]} action={
          <div {...stylex.props(s.controls)}>
            <TimeRangeSelector />
            <span {...stylex.props(s.layoutBtn)}>
              <Button variant="outline" aria-label={singleColumn ? 'Exibir gráficos em duas colunas' : 'Exibir gráficos em uma coluna'}
                aria-pressed={singleColumn}
                title={singleColumn ? 'Grade de 2 colunas' : 'Coluna única'}
                onClick={() => setSingleColumn(!singleColumn)}>
                {singleColumn ? <Rows2 size={15} aria-hidden /> : <LayoutGrid size={15} aria-hidden />}
              </Button>
            </span>
          </div>
        } />
        <div role="tablist" aria-label="Subsistemas do host" {...stylex.props(s.tabs)}
          onKeyDown={event => {
            const index = hostTabs.findIndex(tab => tab.id === subsystem)
            const move = event.key === 'ArrowLeft' ? index - 1 : event.key === 'ArrowRight' ? index + 1 :
              event.key === 'Home' ? 0 : event.key === 'End' ? hostTabs.length - 1 : undefined
            if (move === undefined) return
            event.preventDefault()
            const nextTab = hostTabs[(move + hostTabs.length) % hostTabs.length]
            if (!nextTab) return
            const next = nextTab.id
            void nav(link('/host/' + next))
            event.currentTarget.querySelector<HTMLButtonElement>(`[data-tab="${next}"]`)?.focus()
          }}>
          {hostTabs.map(tab => <button type="button" role="tab" key={tab.id} data-tab={tab.id} id={id + tab.id}
            aria-controls={id + '-panel'} aria-selected={tab.id === subsystem} tabIndex={tab.id === subsystem ? 0 : -1}
            onClick={() => { if (tab.id !== subsystem) void nav(link('/host/' + tab.id)) }}
            {...stylex.props(s.tab, tab.id === subsystem && s.active)}>{tab.label}</button>)}
        </div>
      </div>
      <div role="tabpanel" id={id + '-panel'} aria-labelledby={id + subsystem} {...stylex.props(layout.stack)}>
        {host.isPending && <Loading label="Consultando inventário do host…" />}
        {host.isError && <ErrorState error={host.error} retry={() => { void host.refetch() }} title="Inventário do host indisponível" />}
        <ChartCursorProvider key={subsystem}>
          {subsystem === 'cpu' ? <CpuView host={host.data} singleColumn={singleColumn} />
            : subsystem === 'memory' ? <MemoryView host={host.data} singleColumn={singleColumn} />
            : subsystem === 'services' ? <ServicesView />
            : subsystem === 'processes' ? <ProcessesView />
            : <HostResources key={subsystem} tab={subsystem} host={host.data} singleColumn={singleColumn} />}
        </ChartCursorProvider>
      </div>
    </div>
  )
}
