import * as stylex from '@stylexjs/stylex'
import { Plus, Send, Trash2 } from 'lucide-react'
import { useState } from 'react'
import type { FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Badge, Button, Empty, ErrorState, Loading, SelectField, StatusDot, TextField, layout } from '../../components/ui/ui'
import type { AlertRule } from '../../lib/api/client'
import { useApi, useResources } from '../../lib/api/queries'
import { displayName, metadataText } from '../host/resourceMetadata'
import { useSession } from '../auth/session'
import { metric } from '../metrics/catalog'
import { dateTime } from '../metrics/format'

const s = stylex.create({
  list: { display: 'grid', gap: 10 },
  rowCard: {
    display: 'flex', flexWrap: 'wrap', alignItems: 'center', justifyContent: 'space-between', gap: 12,
    paddingBlock: 12, paddingInline: 14, borderRadius: 'var(--vk-radius-2)',
    backgroundColor: 'var(--vk-surface-2)', borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-subtle)',
  },
  rowMain: { display: 'grid', gap: 2, minWidth: 0 },
  rowTitle: { display: 'inline-flex', alignItems: 'center', gap: 8, fontWeight: 600, fontSize: 13, color: 'var(--vk-text-primary)' },
  urlText: { fontFamily: 'var(--vk-mono)', fontSize: 12, color: 'var(--vk-text-secondary)', overflowWrap: 'anywhere' },
  formBox: {
    display: 'grid', gap: 14, padding: 16, borderRadius: 'var(--vk-radius-2)',
    backgroundColor: 'var(--vk-surface-2)', borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-subtle)',
  },
  formGrid: { display: 'grid', gap: 12, gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 155px), 1fr))' },
  subHead: { fontSize: 14, fontWeight: 600, color: 'var(--vk-text-primary)' },
  feedback: { minHeight: '19px' },
})

const metricOptions = [
  { value: metric.cpu, label: 'CPU total (%)', kind: 'host' as const },
  { value: metric.ram, label: 'Memória RAM (%)', kind: 'host' as const },
  { value: metric.swap, label: 'Swap (%)', kind: 'host' as const },
  { value: metric.load1, label: 'Carga média (1m)', kind: 'host' as const },
  { value: metric.fs, label: 'Sistema de arquivos (%)', kind: 'filesystem' as const },
  { value: metric.temperature, label: 'Temperatura do sensor (°C)', kind: 'sensor' as const },
]

function metricLabel(key: string) {
  return metricOptions.find(opt => opt.value === key)?.label ?? key
}

export function AlertsSection() {
  const api = useApi()
  const { session } = useSession()
  const cache = useQueryClient()
  const rulesQuery = useQuery({ queryKey: ['alerts', 'rules'], queryFn: ({ signal }) => api.alertRules(signal) })
  const eventsQuery = useQuery({ queryKey: ['alerts', 'events'], queryFn: ({ signal }) => api.alertEvents(signal) })
  const filesystems = useResources('filesystem')
  const sensors = useResources('sensor')

  const activeFilesystems = (filesystems.data ?? []).filter(r => r.active)
  const activeSensors = (sensors.data ?? []).filter(r => r.active)

  const [adding, setAdding] = useState(false)
  const [name, setName] = useState('')
  const [metricKey, setMetricKey] = useState<string>(metric.cpu)
  const [resourceId, setResourceId] = useState('host:local')
  const [operator, setOperator] = useState<'gt' | 'lt'>('gt')
  const [threshold, setThreshold] = useState('85')
  const [durationSeconds, setDurationSeconds] = useState('60')
  const [busy, setBusy] = useState(false)
  const [feedback, setFeedback] = useState('')

  const selectedOption = metricOptions.find(opt => opt.value === metricKey) ?? metricOptions[0]
  const resourceChoices = selectedOption.kind === 'filesystem' ? activeFilesystems
    : selectedOption.kind === 'sensor' ? activeSensors : []

  function defaultResourceFor(nextMetric: string) {
    const opt = metricOptions.find(o => o.value === nextMetric)
    if (opt?.kind === 'filesystem') {
      const root = activeFilesystems.find(r => metadataText(r, 'mountpoint') === '/')
      return root?.id ?? activeFilesystems[0]?.id ?? ''
    }
    if (opt?.kind === 'sensor') {
      return activeSensors[0]?.id ?? ''
    }
    return 'host:local'
  }

  function resolveResourceName(id: string) {
    if (id === 'host:local') return 'Host'
    const found = [...activeFilesystems, ...activeSensors].find(r => r.id === id)
    return found ? displayName(found) : id
  }

  async function onCreate(event: FormEvent) {
    event.preventDefault()
    const targetResource = selectedOption.kind === 'host' ? 'host:local' : (resourceId || defaultResourceFor(metricKey))
    const numThreshold = Number(threshold)
    const numDuration = Number(durationSeconds)
    if (!name.trim() || !targetResource || !Number.isFinite(numThreshold) || !Number.isFinite(numDuration) || numDuration < 0) {
      setFeedback('Preencha nome, recurso, limite e duração válidos.')
      return
    }
    setBusy(true)
    setFeedback('')
    try {
      await api.createAlertRule(session, {
        name: name.trim(),
        resource_id: targetResource,
        metric_key: metricKey,
        operator,
        threshold: numThreshold,
        duration_seconds: Math.round(numDuration),
        enabled: true,
      })
      setName('')
      setAdding(false)
      setFeedback('Regra de alerta criada.')
      await cache.invalidateQueries({ queryKey: ['alerts', 'rules'] })
    } catch {
      setFeedback('Não foi possível criar a regra.')
    } finally {
      setBusy(false)
    }
  }

  async function toggleRule(rule: AlertRule) {
    setBusy(true)
    setFeedback('')
    try {
      await api.updateAlertRule(session, rule.id, {
        name: rule.name,
        resource_id: rule.resource_id,
        metric_key: rule.metric_key,
        operator: rule.operator,
        threshold: rule.threshold,
        duration_seconds: rule.duration_seconds,
        enabled: !rule.enabled,
      })
      await cache.invalidateQueries({ queryKey: ['alerts', 'rules'] })
    } catch {
      setFeedback('Não foi possível atualizar a regra.')
    } finally {
      setBusy(false)
    }
  }

  async function removeRule(id: number) {
    setBusy(true)
    setFeedback('')
    try {
      await api.deleteAlertRule(session, id)
      await cache.invalidateQueries({ queryKey: ['alerts', 'rules'] })
    } catch {
      setFeedback('Não foi possível remover a regra.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div {...stylex.props(layout.stack)}>
      {rulesQuery.isPending && <Loading label="Carregando regras…" />}
      {rulesQuery.isError && <ErrorState error={rulesQuery.error} retry={() => { void rulesQuery.refetch() }} />}
      {rulesQuery.data && (
        rulesQuery.data.length === 0 ? <Empty>Nenhuma regra de alerta configurada.</Empty> : (
          <div {...stylex.props(s.list)}>
            {rulesQuery.data.map(rule => (
              <div key={rule.id} {...stylex.props(s.rowCard)}>
                <div {...stylex.props(s.rowMain)}>
                  <span {...stylex.props(s.rowTitle)}>
                    <StatusDot tone={rule.enabled ? 'success' : 'neutral'} />
                    <span>{rule.name}</span>
                  </span>
                  <span {...stylex.props(layout.small)}>
                    {metricLabel(rule.metric_key)} ({resolveResourceName(rule.resource_id)}) {rule.operator === 'gt' ? '>' : '<'} {rule.threshold} · por {rule.duration_seconds}s
                  </span>
                </div>
                <div {...stylex.props(layout.row)}>
                  <Button variant="outline" disabled={busy} onClick={() => { void toggleRule(rule) }}>
                    {rule.enabled ? 'Pausar' : 'Ativar'}
                  </Button>
                  <Button variant="quiet" disabled={busy} aria-label={`Remover regra ${rule.name}`} onClick={() => { void removeRule(rule.id) }}>
                    <Trash2 size={15} aria-hidden />
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )
      )}

      {adding ? (
        <form onSubmit={onCreate} {...stylex.props(s.formBox)}>
          <div {...stylex.props(s.formGrid)}>
            <TextField label="Nome da regra" placeholder="Ex.: CPU acima de 85%" value={name} onChange={e => setName(e.currentTarget.value)} required />
            <SelectField label="Métrica" value={metricKey} onChange={e => {
              const next = e.currentTarget.value
              setMetricKey(next)
              setResourceId(defaultResourceFor(next))
            }}>
              {metricOptions.map(opt => <option key={opt.value} value={opt.value}>{opt.label}</option>)}
            </SelectField>
            {selectedOption.kind !== 'host' && (
              <SelectField label="Recurso" value={resourceId || defaultResourceFor(metricKey)} onChange={e => setResourceId(e.currentTarget.value)}>
                {resourceChoices.length === 0
                  ? <option value="">Nenhum recurso ativo disponível</option>
                  : resourceChoices.map(r => <option key={r.id} value={r.id}>{displayName(r)}</option>)}
              </SelectField>
            )}
            <SelectField label="Condição" value={operator} onChange={e => setOperator(e.currentTarget.value === 'lt' ? 'lt' : 'gt')}>
              <option value="gt">Maior que (&gt;)</option>
              <option value="lt">Menor que (&lt;)</option>
            </SelectField>
            <TextField label="Limite" type="number" step="any" value={threshold} onChange={e => setThreshold(e.currentTarget.value)} required />
            <TextField label="Duração (s)" type="number" min={0} max={86400} value={durationSeconds} onChange={e => setDurationSeconds(e.currentTarget.value)} required />
          </div>
          <div {...stylex.props(layout.row)}>
            <Button type="submit" variant="primary" busy={busy}>Salvar regra</Button>
            <Button variant="outline" disabled={busy} onClick={() => setAdding(false)}>Cancelar</Button>
          </div>
        </form>
      ) : (
        <div>
          <Button variant="outline" onClick={() => setAdding(true)}><Plus size={15} aria-hidden /> Adicionar regra</Button>
        </div>
      )}

      {feedback && <p role="status" {...stylex.props(layout.small, s.feedback)}>{feedback}</p>}

      {eventsQuery.data && eventsQuery.data.length > 0 && (
        <div {...stylex.props(layout.compact)}>
          <h3 {...stylex.props(s.subHead)}>Eventos recentes</h3>
          <div {...stylex.props(s.list)}>
            {eventsQuery.data.slice(0, 5).map(ev => (
              <div key={ev.id} {...stylex.props(s.rowCard)}>
                <div {...stylex.props(s.rowMain)}>
                  <span {...stylex.props(s.rowTitle)}>
                    <Badge tone={ev.status === 'firing' ? 'critical' : 'success'} icon={<StatusDot tone={ev.status === 'firing' ? 'critical' : 'success'} />}>
                      {ev.status === 'firing' ? 'Disparado' : 'Resolvido'}
                    </Badge>
                    <span>{ev.rule_name}</span>
                  </span>
                  <span {...stylex.props(layout.small)}>
                    Valor {ev.value.toFixed(1)} (limite {ev.threshold}) · {dateTime(ev.resolved_at_ms ?? ev.triggered_at_ms)}
                  </span>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

export function NotificationsSection() {
  const api = useApi()
  const { session } = useSession()
  const cache = useQueryClient()
  const channelsQuery = useQuery({ queryKey: ['alerts', 'channels'], queryFn: ({ signal }) => api.alertChannels(signal) })

  const [adding, setAdding] = useState(false)
  const [name, setName] = useState('')
  const [url, setUrl] = useState('')
  const [busy, setBusy] = useState(false)
  const [testingId, setTestingId] = useState<number | null>(null)
  const [feedback, setFeedback] = useState('')

  async function onCreate(event: FormEvent) {
    event.preventDefault()
    if (!name.trim() || !url.trim()) return
    setBusy(true)
    setFeedback('')
    try {
      await api.createAlertChannel(session, { name: name.trim(), kind: 'webhook', url: url.trim(), enabled: true })
      setName('')
      setUrl('')
      setAdding(false)
      setFeedback('Webhook adicionado.')
      await cache.invalidateQueries({ queryKey: ['alerts', 'channels'] })
    } catch {
      setFeedback('URL inválida. Informe um endereço http:// ou https://.')
    } finally {
      setBusy(false)
    }
  }

  async function onTest(id: number, targetUrl: string) {
    setTestingId(id)
    setFeedback('')
    try {
      await api.testAlertChannel(session, targetUrl)
      setFeedback('Notificação de teste enviada com sucesso.')
    } catch {
      setFeedback('Falha ao enviar teste para o webhook informado.')
    } finally {
      setTestingId(null)
    }
  }

  async function onRemove(id: number) {
    setBusy(true)
    setFeedback('')
    try {
      await api.deleteAlertChannel(session, id)
      await cache.invalidateQueries({ queryKey: ['alerts', 'channels'] })
    } catch {
      setFeedback('Não foi possível remover o webhook.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div {...stylex.props(layout.stack)}>
      {channelsQuery.isPending && <Loading label="Carregando canais…" />}
      {channelsQuery.isError && <ErrorState error={channelsQuery.error} retry={() => { void channelsQuery.refetch() }} />}
      {channelsQuery.data && (
        channelsQuery.data.length === 0 ? <Empty>Nenhum webhook configurado.</Empty> : (
          <div {...stylex.props(s.list)}>
            {channelsQuery.data.map(ch => (
              <div key={ch.id} {...stylex.props(s.rowCard)}>
                <div {...stylex.props(s.rowMain)}>
                  <span {...stylex.props(s.rowTitle)}>
                    <StatusDot tone={ch.enabled ? 'success' : 'neutral'} />
                    <span>{ch.name}</span>
                  </span>
                  <span {...stylex.props(s.urlText)}>{ch.url}</span>
                </div>
                <div {...stylex.props(layout.row)}>
                  <Button variant="outline" busy={testingId === ch.id} disabled={busy} onClick={() => { void onTest(ch.id, ch.url) }}>
                    <Send size={13} aria-hidden /> Testar URL
                  </Button>
                  <Button variant="quiet" disabled={busy} aria-label={`Remover webhook ${ch.name}`} onClick={() => { void onRemove(ch.id) }}>
                    <Trash2 size={15} aria-hidden />
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )
      )}

      {adding ? (
        <form onSubmit={onCreate} {...stylex.props(s.formBox)}>
          <div {...stylex.props(s.formGrid)}>
            <TextField label="Nome do canal" placeholder="Ex.: Discord Homelab" value={name} onChange={e => setName(e.currentTarget.value)} required />
            <TextField label="URL do webhook" type="url" placeholder="https://..." value={url} onChange={e => setUrl(e.currentTarget.value)} required />
          </div>
          <div {...stylex.props(layout.row)}>
            <Button type="submit" variant="primary" busy={busy}>Salvar URL</Button>
            <Button variant="outline" disabled={busy} onClick={() => setAdding(false)}>Cancelar</Button>
          </div>
        </form>
      ) : (
        <div>
          <Button variant="outline" onClick={() => setAdding(true)}><Plus size={15} aria-hidden /> Adicionar URL</Button>
        </div>
      )}

      {feedback && <p role="status" {...stylex.props(layout.small, s.feedback)}>{feedback}</p>}
    </div>
  )
}
