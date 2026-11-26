import * as stylex from '@stylexjs/stylex'
import { ArrowDown, ArrowUp, Clock3, Cpu, HardDrive, LayoutGrid, MemoryStick, Monitor, Network, RefreshCw, Rows2, Server } from 'lucide-react'
import { usePreferences } from '#/app/preferences'
import { Facts, MetricCard } from '#/components/ui/dashboard'
import { Badge, Button, DetailLink, Empty, ErrorState, Loading, Panel, StatusDot, layout } from '#/components/ui/ui'
import { useDiagnostics, useHealth, useHost, useResources } from '#/lib/api/queries'
import type { ResourceKind } from '#/lib/api/client'
import { DiagnosticCheckCard } from '#/features/diagnostics/DiagnosticsParts'
import { displayName, metadataText } from '#/features/host/resourceMetadata'
import { ChartPanel } from '#/features/metrics/ChartPanel'
import { TemperatureChartPanel } from '#/features/metrics/TemperatureChartPanel'
import { metric } from '#/features/metrics/catalog'
import { ChartCursorProvider } from '#/features/metrics/cursor'
import { dateTime, duration, timeZoneLabel } from '#/features/metrics/format'
import { TimeRangeSelector, usePeriodLink } from '#/features/metrics/range'
import { LiveValue, PersistedValue } from '#/features/metrics/readings'

const s = stylex.create({
  heroCard: {
    display: 'grid', gap: 14, minWidth: 0,
    backgroundColor: 'var(--vk-surface-1)', borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-subtle)',
    borderRadius: 'var(--vk-radius-3)', padding: { default: 16, '@media (min-width: 1024px)': 20 },
  },
  heroTop: { display: 'flex', flexWrap: 'wrap', alignItems: 'center', justifyContent: 'space-between', gap: 12 },
  heroTitle: { outlineStyle: 'none' },
  metaStrip: {
    display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: '8px 14px',
    paddingBlockStart: 12, borderBlockStartWidth: 1, borderBlockStartStyle: 'solid', borderBlockStartColor: 'var(--vk-border-subtle)',
    fontSize: 13, color: 'var(--vk-text-secondary)',
  },
  metaItem: { display: 'inline-flex', alignItems: 'center', gap: 6, minWidth: 0 },
  metaSep: { width: 1, height: 14, backgroundColor: 'var(--vk-border-subtle)' },
  hostName: { fontFamily: 'var(--vk-mono)', fontWeight: 500, color: 'var(--vk-text-primary)', overflowWrap: 'anywhere' },
  item: { display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 },
  rxArrow: { color: 'var(--vk-series-rx)', flexShrink: 0 },
  txArrow: { color: 'var(--vk-series-tx)', flexShrink: 0 },
  traffic: { display: 'grid', gap: 6, fontSize: 15, lineHeight: '20px', letterSpacing: '0' },
  list: {
    listStyle: 'none', margin: 0, padding: 0, display: 'grid', gap: 0,
    borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-subtle)',
    borderRadius: 'var(--vk-radius-2)', overflow: 'hidden',
  },
  resource: {
    display: 'flex', gap: 12, alignItems: 'center', justifyContent: 'space-between',
    minHeight: 42, flexWrap: 'wrap', paddingBlock: 8, paddingInline: 12,
    borderBlockEndWidth: 1, borderBlockEndStyle: 'solid', borderBlockEndColor: 'var(--vk-border-subtle)',
    backgroundColor: { default: 'transparent', ':hover': 'var(--vk-surface-2)' },
    transition: 'background-color 150ms',
  },
  resourceLast: { borderBlockEndWidth: 0 },
  resName: { display: 'inline-flex', alignItems: 'center', gap: 8, minWidth: 0 },
  controls: { display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8 },
  layoutBtn: { display: { default: 'none', '@media (min-width: 1024px)': 'inline-flex' } },
  checkList: { display: 'grid', gap: 8 },
  checkDetail: { fontSize: 12, color: 'var(--vk-text-secondary)', fontFamily: 'var(--vk-mono)', overflowWrap: 'anywhere' },
})

function HostSummary() {
  const host = useHost()
  const diagnostics = useHealth()
  const { singleColumn, setSingleColumn } = usePreferences()
  const inventory = host.data?.inventory
  const uptime = host.data?.uptime_seconds
  const health = diagnostics.isError ? undefined : diagnostics.data
  const osLabel = inventory?.os_pretty || inventory?.os_name
  const cpuSummary = inventory?.cpu_model
    ? `${inventory.cpu_model}${inventory.cpu_cores ? ` (${inventory.cpu_cores}c/${inventory.cpu_threads || inventory.cpu_cores}t)` : ''}`
    : undefined

  return (
    <section {...stylex.props(s.heroCard)} aria-label="Resumo do host">
      <div {...stylex.props(s.heroTop)}>
        <div {...stylex.props(layout.compact)}>
          <h1 {...stylex.props(layout.pageTitle, s.heroTitle)} tabIndex={-1}>Visão geral</h1>
          <p {...stylex.props(layout.small)}>Atividade e diagnóstico do host em tempo real</p>
        </div>
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
      </div>

      <div {...stylex.props(s.metaStrip)}>
        {health?.status === 'ok'
          ? <Badge tone="success" icon={<StatusDot tone="success" />}>Daemon operacional</Badge>
          : <Badge tone={health ? 'warning' : 'neutral'} icon={<StatusDot tone={health ? 'warning' : 'neutral'} />}>{health ? 'Daemon degradado' : 'Diagnóstico indisponível'}</Badge>}
        {inventory && <>
          <span aria-hidden {...stylex.props(s.metaSep)} />
          <span {...stylex.props(s.metaItem)}>
            <Server size={14} aria-hidden />
            <span {...stylex.props(s.hostName)}>{inventory.hostname || 'Identidade indisponível'}</span>
          </span>
          {osLabel && <>
            <span aria-hidden {...stylex.props(s.metaSep)} />
            <span {...stylex.props(s.metaItem)}><Monitor size={14} aria-hidden /><span>{osLabel}</span></span>
          </>}
          {uptime !== undefined && <>
            <span aria-hidden {...stylex.props(s.metaSep)} />
            <span {...stylex.props(s.metaItem)}><Clock3 size={14} aria-hidden /><span>{duration(uptime)}</span></span>
          </>}
          {inventory.kernel && <>
            <span aria-hidden {...stylex.props(s.metaSep)} />
            <span {...stylex.props(s.metaItem, layout.technical)}>{inventory.kernel}</span>
          </>}
          {cpuSummary && <>
            <span aria-hidden {...stylex.props(s.metaSep)} />
            <span {...stylex.props(s.metaItem)}><Cpu size={14} aria-hidden /><span>{cpuSummary}</span></span>
          </>}
        </>}
      </div>
      {host.isPending && <Loading label="Consultando inventário…" />}
      {host.isError && <ErrorState error={host.error} retry={() => { void host.refetch() }} title="Inventário indisponível" />}
    </section>
  )
}

function ResourceSummary({ kind }: { kind: Extract<ResourceKind, 'filesystem' | 'nic'> }) {
  const query = useResources(kind)
  const link = usePeriodLink()
  const filesystem = kind === 'filesystem'
  const active = query.data?.filter(resource => resource.active)
  const shown = active?.slice(0, 4) ?? []
  return (
    <Panel title={filesystem ? 'Sistemas de arquivos' : 'Interfaces de rede'}
      subtitle={filesystem ? 'Montagens ativas monitoradas' : 'Interfaces descobertas no host'}
      action={<DetailLink to={link(filesystem ? '/host/storage' : '/host/network')}>Ver detalhes</DetailLink>}>
      {query.isPending && <Loading />}
      {query.isError && <ErrorState error={query.error} retry={() => { void query.refetch() }} />}
      {active && (shown.length ? <>
        <ul {...stylex.props(s.list)}>
          {shown.map((resource, index) => {
            const state = filesystem ? metadataText(resource, 'fs_type') : metadataText(resource, 'operstate')
            const isUp = filesystem || state === 'up'
            return (
              <li key={resource.id} {...stylex.props(s.resource, index === shown.length - 1 && s.resourceLast)}>
                <span {...stylex.props(s.resName)}>
                  <StatusDot tone={isUp ? 'success' : 'neutral'} />
                  <span {...stylex.props(layout.technical)}>{displayName(resource)}</span>
                </span>
                <span {...stylex.props(layout.small)}>{state ?? (filesystem ? 'Tipo indisponível' : 'Estado desconhecido')}</span>
              </li>
            )
          })}
        </ul>
        <p {...stylex.props(layout.small)}>{active.length} {active.length === 1 ? 'recurso ativo' : 'recursos ativos'}</p>
      </> : <Empty>Nenhum recurso ativo encontrado.</Empty>)}
    </Panel>
  )
}

function Diagnostics() {
  const query = useHealth()
  const diagQuery = useDiagnostics()
  const health = query.data
  const report = diagQuery.data
  return (
    <div id="diagnostico">
      <Panel
        title="Diagnóstico do daemon"
        subtitle="Estado da coleta, persistência, relógio, DNS, helper e Incus"
        action={
          <Button variant="quiet" busy={diagQuery.isFetching} onClick={() => { void query.refetch(); void diagQuery.refetch() }}>
            <RefreshCw size={14} aria-hidden />
            Reexecutar
          </Button>
        }
      >
        {query.isPending && <Loading />}
        {query.isError && <ErrorState error={query.error} retry={() => { void query.refetch() }} />}
        {health && <>
          {health.status === 'ok' && health.collector_error_count === 0 && health.wall_clock_jump_count === 0 && health.dropped_live_events === 0 && (!report || report.status === 'ok') &&
            <p {...stylex.props(layout.small)}>Nenhum problema detectado nos diagnósticos disponíveis.</p>}
          {health.clock_error && (
            <div {...stylex.props(layout.compact)}>
              <Badge tone="warning" icon={<StatusDot tone="warning" />}>Relógio degradado. Escritas históricas pausadas.</Badge>
              <span {...stylex.props(s.checkDetail)}>{health.clock_error}</span>
            </div>
          )}
          {(!health.db_ok || !health.db_ping_ok) && (
            <div {...stylex.props(layout.compact)}>
              <Badge tone="critical" icon={<StatusDot tone="critical" />}>SQLite degradado. O histórico pode estar indisponível.</Badge>
              {health.last_db_error && <span {...stylex.props(s.checkDetail)}>{health.last_db_error}</span>}
            </div>
          )}
          {report && report.checks.length > 0 && (
            <div {...stylex.props(s.checkList)}>
              {report.checks.map(check => (
                <DiagnosticCheckCard key={check.id} check={check} />
              ))}
            </div>
          )}
          <Facts items={[
            ['Última coleta', health.last_collect_at && dateTime(health.last_collect_at)],
            ['Última escrita', health.last_write_at && dateTime(health.last_write_at)],
            ['Erros de coleta acumulados', health.collector_error_count],
            ['Atraso de agregação', duration(health.rollup_lag_s)],
            ['Eventos descartados', health.dropped_live_events],
            ['Saltos de relógio', health.wall_clock_jump_count],
          ]} />
          {health.collector_errors_by_name && Object.keys(health.collector_errors_by_name).length > 0 && (
            <Facts items={Object.entries(health.collector_errors_by_name).map(([name, count]) => {
              const last = health.collector_last_errors?.[name]
              return [name, last ? `${count}x · ${last.message}` : `${count}x`]
            })} />
          )}
        </>}
      </Panel>
    </div>
  )
}

export function Overview() {
  const host = useHost()
  const filesystems = useResources('filesystem')
  const sensors = useResources('sensor')
  const { singleColumn, timezone } = usePreferences()
  const id = host.data?.resource.id
  const root = filesystems.data?.find(resource => resource.active && metadataText(resource, 'mountpoint') === '/')
  const activeSensors = sensors.data?.filter(sensor => sensor.active) ?? []
  return (
    <div {...stylex.props(layout.page)}>
      <HostSummary />
      <section {...stylex.props(layout.grid4)} aria-label="Métricas atuais">
        <MetricCard label="CPU" icon={Cpu} value={id ? <LiveValue id={id} metric={metric.cpu} unit="%" detail bar /> : '—'} context="Utilização total" />
        <MetricCard label="Memória" icon={MemoryStick} value={id ? <LiveValue id={id} metric={metric.ram} unit="%" detail bar /> : '—'} context="RAM em uso" />
        <MetricCard label="Armazenamento" icon={HardDrive} value={root ? <PersistedValue id={root.id} metric={metric.fs} unit="%" detail bar /> : '—'}
          context={root ? 'Sistema de arquivos /' : filesystems.isPending ? 'Consultando inventário…' : filesystems.isError ? 'Inventário indisponível' : 'Montagem / não monitorada'} />
        <MetricCard label="Rede" icon={Network} dense value={id ? (
          <span {...stylex.props(s.traffic)}>
            <span {...stylex.props(s.item)}><ArrowDown size={14} aria-hidden {...stylex.props(s.rxArrow)} /><span>RX</span><LiveValue id={id} metric={metric.rx} unit="bytes/s" /></span>
            <span {...stylex.props(s.item)}><ArrowUp size={14} aria-hidden {...stylex.props(s.txArrow)} /><span>TX</span><LiveValue id={id} metric={metric.tx} unit="bytes/s" /></span>
          </span>
        ) : '—'} context="Agregado do host · Ao vivo" />
      </section>
      <section {...stylex.props(layout.stack)} aria-labelledby="timeline">
        <div {...stylex.props(layout.split)}>
          <h2 id="timeline" {...stylex.props(layout.sectionTitle)}>Atividade no período</h2>
          <p {...stylex.props(layout.small)}>Histórico persistido · {timeZoneLabel(timezone)}</p>
        </div>
        <ChartCursorProvider>
          <div {...stylex.props(singleColumn ? layout.grid1 : layout.grid2)}>
            <ChartPanel title="Utilização da CPU" subtitle="Utilização média do processador no host" resourceId={id} resourcePending={host.isPending} metric={metric.cpu} color="cpu" />
            <ChartPanel title="Uso de memória" subtitle="Percentual de RAM alocada no intervalo" resourceId={id} resourcePending={host.isPending} metric={metric.ram} color="memory" scale="full" />
          </div>
          {activeSensors.length > 0 && <TemperatureChartPanel sensors={activeSensors} />}
        </ChartCursorProvider>
      </section>
      <div {...stylex.props(layout.grid2)}><ResourceSummary kind="filesystem" /><ResourceSummary kind="nic" /></div>
      <Diagnostics />
    </div>
  )
}
