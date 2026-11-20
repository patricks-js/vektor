import * as stylex from '@stylexjs/stylex'
import { Activity, ArrowDown, ArrowUp, CheckCircle2, CircleAlert, Cpu, HardDrive, MemoryStick, Network } from 'lucide-react'
import { Facts, MetricCard, PageHeading } from '../../components/ui/dashboard'
import { Badge, DetailLink, Empty, ErrorState, Loading, Panel, layout } from '../../components/ui/ui'
import { useHealth, useHost, useResources } from '../../lib/api/queries'
import type { ResourceKind } from '../../lib/api/client'
import { displayName, metadataText } from '../host/resourceMetadata'
import { ChartPanel } from '../metrics/ChartPanel'
import { metric } from '../metrics/catalog'
import { ChartCursorProvider } from '../metrics/cursor'
import { dateTime, duration } from '../metrics/format'
import { TimeRangeSelector, usePeriodLink } from '../metrics/range'
import { LiveValue, PersistedValue } from '../metrics/readings'

const s = stylex.create({
  tower: { display: 'grid', placeItems: 'center', width: 40, height: 40, borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-strong)', color: 'var(--vk-accent)', backgroundColor: 'var(--vk-surface-2)', flexShrink: 0 },
  hostName: { fontFamily: 'var(--vk-mono)', fontWeight: 600, overflowWrap: 'anywhere', fontSize: 16 },
  item: { display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 },
  traffic: { display: 'grid', gap: 6, fontSize: 16, lineHeight: '20px', letterSpacing: '0' },
  list: { listStyle: 'none', margin: 0, padding: 0, display: 'grid', gap: 0 },
  resource: { display: 'flex', gap: 12, alignItems: 'center', justifyContent: 'space-between', minHeight: 44, flexWrap: 'wrap', borderBlockStartWidth: 1, borderBlockStartStyle: 'solid', borderBlockStartColor: 'var(--vk-border-subtle)', paddingBlock: 8 },
})

function HostSummary() {
  const host = useHost()
  const diagnostics = useHealth()
  const inventory = host.data?.inventory
  const uptime = host.data?.uptime_seconds
  const health = diagnostics.isError ? undefined : diagnostics.data
  return (
    <Panel emphasized>
      <div {...stylex.props(layout.split)}>
        <div {...stylex.props(layout.row)}>
          <span {...stylex.props(s.tower)}><Activity size={20} aria-hidden /></span>
          <div {...stylex.props(layout.compact)}>
            <p {...stylex.props(layout.small)}>Host monitorado</p>
            <p {...stylex.props(s.hostName)}>{inventory?.hostname || 'Identidade indisponível'}</p>
          </div>
        </div>
        {health?.status === 'ok'
          ? <Badge tone="success" icon={<CheckCircle2 size={15} aria-hidden />}>Daemon operacional</Badge>
          : <Badge tone={health ? 'warning' : 'neutral'} icon={<CircleAlert size={15} aria-hidden />}>{health ? 'Daemon degradado' : 'Diagnóstico indisponível'}</Badge>}
      </div>
      {inventory && <Facts items={[
        ['Sistema', inventory.os_pretty || inventory.os_name || undefined],
        ['Kernel', inventory.kernel || undefined],
        ['Uptime', uptime === undefined ? undefined : duration(uptime)],
      ]} />}
      {host.isPending && <Loading label="Consultando inventário…" />}
      {host.isError && <ErrorState error={host.error} retry={() => { void host.refetch() }} title="Inventário indisponível" />}
    </Panel>
  )
}

function ResourceSummary({ kind }: { kind: Extract<ResourceKind, 'filesystem' | 'nic'> }) {
  const query = useResources(kind)
  const link = usePeriodLink()
  const filesystem = kind === 'filesystem'
  const active = query.data?.filter(resource => resource.active)
  return (
    <Panel title={filesystem ? 'Sistemas de arquivos' : 'Interfaces de rede'} action={
      <DetailLink to={link(filesystem ? '/host/storage' : '/host/network')}>Ver detalhes</DetailLink>
    }>
      {query.isPending && <Loading />}
      {query.isError && <ErrorState error={query.error} retry={() => { void query.refetch() }} />}
      {active && (active.length ? <>
        <ul {...stylex.props(s.list)}>
          {active.slice(0, 4).map(resource => (
            <li key={resource.id} {...stylex.props(s.resource)}>
              <span {...stylex.props(layout.technical)}>{displayName(resource)}</span>
              {filesystem ? <span {...stylex.props(layout.small)}>{metadataText(resource, 'fs_type') ?? 'Tipo indisponível'}</span>
                : <span {...stylex.props(layout.small)}>{metadataText(resource, 'operstate') ?? 'Estado desconhecido'}</span>}
            </li>
          ))}
        </ul>
        <p {...stylex.props(layout.small)}>{active.length} {active.length === 1 ? 'recurso ativo' : 'recursos ativos'}</p>
      </> : <Empty>Nenhum recurso ativo encontrado.</Empty>)}
    </Panel>
  )
}

function Diagnostics() {
  const query = useHealth()
  const health = query.data
  return (
    <Panel title="Diagnóstico do daemon">
      {query.isPending && <Loading />}
      {query.isError && <ErrorState error={query.error} retry={() => { void query.refetch() }} />}
      {health && <>
        {health.status === 'ok' && health.collector_error_count === 0 && health.wall_clock_jump_count === 0 && health.dropped_live_events === 0 &&
          <p {...stylex.props(layout.text)}>Nenhum problema detectado nos diagnósticos disponíveis.</p>}
        {health.clock_error && <Badge tone="warning" icon={<CircleAlert size={15} aria-hidden />}>Relógio degradado. Escritas históricas pausadas.</Badge>}
        {(!health.db_ok || !health.db_ping_ok) && <Badge tone="critical" icon={<CircleAlert size={15} aria-hidden />}>SQLite degradado. O histórico pode estar indisponível.</Badge>}
        <Facts items={[
          ['Última coleta', health.last_collect_at && dateTime(health.last_collect_at)],
          ['Última escrita', health.last_write_at && dateTime(health.last_write_at)],
          ['Erros de coleta acumulados', health.collector_error_count],
          ['Atraso de agregação', duration(health.rollup_lag_s)],
          ['Eventos descartados', health.dropped_live_events],
          ['Saltos de relógio', health.wall_clock_jump_count],
        ]} />
        {health.collector_errors_by_name && Object.keys(health.collector_errors_by_name).length > 0 &&
          <Facts items={Object.entries(health.collector_errors_by_name)} />}
      </>}
    </Panel>
  )
}

export function Overview() {
  const host = useHost()
  const filesystems = useResources('filesystem')
  const id = host.data?.resource.id
  const root = filesystems.data?.find(resource => resource.active && metadataText(resource, 'mountpoint') === '/')
  return (
    <div {...stylex.props(layout.page)}>
      <PageHeading title="Visão geral" description="Atividade e diagnóstico do host" action={<TimeRangeSelector />} />
      <HostSummary />
      <section {...stylex.props(layout.grid4)} aria-label="Métricas atuais">
        <MetricCard label="CPU" icon={Cpu} value={id ? <LiveValue id={id} metric={metric.cpu} unit="%" detail /> : '—'} context="Utilização total" />
        <MetricCard label="Memória" icon={MemoryStick} value={id ? <LiveValue id={id} metric={metric.ram} unit="%" detail /> : '—'} context="RAM em uso" />
        <MetricCard label="Armazenamento" icon={HardDrive} value={root ? <PersistedValue id={root.id} metric={metric.fs} unit="%" detail /> : '—'}
          context={root ? 'Sistema de arquivos /' : filesystems.isPending ? 'Consultando inventário…' : filesystems.isError ? 'Inventário indisponível' : 'Montagem / não monitorada'} />
        <MetricCard label="Rede" icon={Network} dense value={id ? (
          <span {...stylex.props(s.traffic)}>
            <span {...stylex.props(s.item)}><ArrowDown size={15} aria-hidden /><span>RX</span><LiveValue id={id} metric={metric.rx} unit="bytes/s" /></span>
            <span {...stylex.props(s.item)}><ArrowUp size={15} aria-hidden /><span>TX</span><LiveValue id={id} metric={metric.tx} unit="bytes/s" /></span>
          </span>
        ) : '—'} context="Agregado do host · Ao vivo" />
      </section>
      <section {...stylex.props(layout.stack)} aria-labelledby="timeline">
        <div {...stylex.props(layout.split)}>
          <h2 id="timeline" {...stylex.props(layout.sectionTitle)}>Atividade no período</h2>
          <p {...stylex.props(layout.small)}>Histórico persistido · UTC</p>
        </div>
        <ChartCursorProvider>
          <div {...stylex.props(layout.grid2)}>
            <ChartPanel title="Utilização da CPU" resourceId={id} resourcePending={host.isPending} metric={metric.cpu} />
            <ChartPanel title="Uso de memória" resourceId={id} resourcePending={host.isPending} metric={metric.ram} color="memory" />
          </div>
        </ChartCursorProvider>
      </section>
      <div {...stylex.props(layout.grid2)}><ResourceSummary kind="filesystem" /><ResourceSummary kind="nic" /></div>
      <Diagnostics />
    </div>
  )
}
