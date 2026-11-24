import * as stylex from '@stylexjs/stylex'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, Camera, Cpu, HardDrive, Monitor, Pause, Play, Power, RotateCw, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { Facts } from '../../components/ui/dashboard'
import { Badge, Button, Empty, ErrorState, Loading, Modal, Panel, StatusDot, TextField, layout, tableStyles } from '../../components/ui/ui'
import { useSession } from '../auth/session'
import { dateTime, formatValue } from '../metrics/format'
import type { IncusStateAction } from '../../lib/api/client'
import { useApi } from '../../lib/api/queries'

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
  snapForm: {
    display: 'flex', flexWrap: 'wrap', gap: 10, alignItems: 'flex-end',
  },
  monoCell: {
    fontFamily: 'var(--vk-mono)', fontSize: 12, fontVariantNumeric: 'tabular-nums',
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

export function InstanceDetail() {
  const { name = '' } = useParams()
  const api = useApi()
  const { session } = useSession()
  const queryClient = useQueryClient()
  const navigate = useNavigate()

  const [snapName, setSnapName] = useState('')
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [confirmRestoreSnap, setConfirmRestoreSnap] = useState<string | null>(null)
  const [confirmDeleteSnap, setConfirmDeleteSnap] = useState<string | null>(null)
  const [feedback, setFeedback] = useState<{ tone: 'success' | 'critical'; text: string } | null>(null)

  const instanceQuery = useQuery({
    queryKey: ['incus', 'instance', name],
    queryFn: ({ signal }) => api.incusInstance(name, signal),
    enabled: Boolean(name),
    refetchInterval: 5_000,
  })

  const snapsQuery = useQuery({
    queryKey: ['incus', 'snapshots', name],
    queryFn: ({ signal }) => api.incusSnapshots(name, signal),
    enabled: Boolean(name),
    refetchInterval: 15_000,
  })

  const stateMutation = useMutation({
    mutationFn: (action: IncusStateAction) => api.incusInstanceState(session, name, action),
    onSuccess: (res) => {
      setFeedback({ tone: 'success', text: res.message || 'Estado atualizado.' })
      void queryClient.invalidateQueries({ queryKey: ['incus'] })
    },
    onError: (err) => setFeedback({ tone: 'critical', text: err instanceof Error ? err.message : 'Falha na operação.' }),
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

  const inst = instanceQuery.data
  const busy = stateMutation.isPending || deleteMutation.isPending

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

          {feedback && (
            <Badge tone={feedback.tone} icon={<StatusDot tone={feedback.tone} />}>{feedback.text}</Badge>
          )}

          <div {...stylex.props(layout.grid2)}>
            <Panel title="Recursos e estado" subtitle="Consumo atual e limites configurados">
              <Facts items={[
                ['RAM em uso', inst.memory_usage_bytes > 0 ? formatValue(inst.memory_usage_bytes, 'bytes') : '—'],
                ['Pico de RAM', inst.memory_peak_bytes > 0 ? formatValue(inst.memory_peak_bytes, 'bytes') : '—'],
                ['Limite de RAM', inst.memory_limit || 'Sem limite'],
                ['Limite de CPU', inst.cpu_limit || 'Sem limite'],
                ['Disco (root)', inst.disk_usage_bytes > 0 ? formatValue(inst.disk_usage_bytes, 'bytes') : '—'],
                ['Processos', inst.processes > 0 ? String(inst.processes) : '—'],
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

          <Panel
            title="Snapshots"
            subtitle="Pontos de restauração instantâneos no storage pool do Incus"
            action={
              <form {...stylex.props(s.snapForm)} onSubmit={event => {
                event.preventDefault()
                if (snapName.trim()) createSnapMutation.mutate()
              }}>
                <TextField
                  label="Novo snapshot"
                  value={snapName}
                  onChange={e => setSnapName(e.target.value)}
                  placeholder="ex: pre-upgrade"
                />
                <Button type="submit" variant="primary" disabled={!snapName.trim() || createSnapMutation.isPending}>
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
                            <Button
                              variant="outline"
                              disabled={restoreSnapMutation.isPending}
                              onClick={() => setConfirmRestoreSnap(snap.name)}
                            >
                              <HardDrive size={13} aria-hidden /> Restaurar
                            </Button>
                            <Button
                              variant="danger"
                              disabled={deleteSnapMutation.isPending}
                              aria-label={`Excluir snapshot ${snap.name}`}
                              onClick={() => setConfirmDeleteSnap(snap.name)}
                            >
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
    </div>
  )
}
