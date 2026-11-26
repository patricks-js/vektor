import * as stylex from '@stylexjs/stylex'
import { Power, RefreshCw, RotateCcw } from 'lucide-react'
import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Badge, Button, Empty, ErrorState, Loading, Modal, StatusDot, layout } from '#/components/ui/ui'
import { useApi } from '#/lib/api/queries'
import { useSession } from '#/features/auth/session'
import { dateTime } from '#/features/metrics/format'

const s = stylex.create({
  list: { display: 'grid', gap: 8 },
  rowCard: {
    display: 'flex', flexWrap: 'wrap', alignItems: 'center', justifyContent: 'space-between', gap: 10,
    paddingBlock: 10, paddingInline: 14, borderRadius: 'var(--vk-radius-2)',
    backgroundColor: 'var(--vk-surface-2)', borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-subtle)',
  },
  rowMain: { display: 'grid', gap: 2, minWidth: 0 },
  rowTitle: { display: 'inline-flex', alignItems: 'center', gap: 8, fontSize: 13, fontWeight: 600, color: 'var(--vk-text-primary)' },
})

export function HostPowerSection() {
  const api = useApi()
  const { session } = useSession()
  const cache = useQueryClient()
  const [pending, setPending] = useState<'reboot' | 'poweroff'>()
  const [busy, setBusy] = useState(false)
  const [feedback, setFeedback] = useState('')
  const [error, setError] = useState<unknown>()

  async function confirm() {
    if (!pending) return
    setBusy(true)
    setError(undefined)
    try {
      await api.hostPower(session, pending)
      setFeedback(pending === 'reboot' ? 'Reinicialização do servidor solicitada.' : 'Desligamento do servidor solicitado.')
      setPending(undefined)
      await cache.invalidateQueries({ queryKey: ['audit'] })
    } catch (err) {
      setError(err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div {...stylex.props(layout.stack)}>
      <div {...stylex.props(layout.row)}>
        <Button variant="outline" onClick={() => { setError(undefined); setPending('reboot') }}>
          <RotateCcw size={15} aria-hidden /> Reiniciar servidor
        </Button>
        <Button variant="danger" onClick={() => { setError(undefined); setPending('poweroff') }}>
          <Power size={15} aria-hidden /> Desligar servidor
        </Button>
      </div>
      {feedback && <p role="status" {...stylex.props(layout.small)}>{feedback}</p>}
      <Modal title={pending === 'poweroff' ? 'Desligar servidor' : 'Reiniciar servidor'} open={Boolean(pending)} preventClose={busy}
        onClose={() => { if (!busy) { setPending(undefined); setError(undefined) } }}>
        <p {...stylex.props(layout.text)}>
          {pending === 'poweroff'
            ? 'O servidor será desligado imediatamente e ficará inacessível até ser ligado fisicamente.'
            : 'O servidor será reiniciado imediatamente. A conexão com o Vektor será interrompida durante o boot.'}
        </p>
        {error != null && <ErrorState error={error} title="Não foi possível executar o comando" />}
        <div {...stylex.props(layout.row)}>
          <Button disabled={busy} onClick={() => { setPending(undefined); setError(undefined) }}>Cancelar</Button>
          <Button variant="danger" busy={busy} onClick={() => { void confirm() }}>
            {pending === 'poweroff' ? 'Confirmar desligamento' : 'Confirmar reinicialização'}
          </Button>
        </div>
      </Modal>
    </div>
  )
}

export function AuditLogSection() {
  const api = useApi()
  const query = useQuery({
    queryKey: ['audit'],
    queryFn: ({ signal }) => api.auditLog(signal, 30),
    staleTime: 15_000,
  })

  return (
    <div {...stylex.props(layout.stack)}>
      <div>
        <Button variant="outline" onClick={() => { void query.refetch() }} busy={query.isFetching}>
          <RefreshCw size={14} aria-hidden /> Atualizar histórico
        </Button>
      </div>
      {query.isPending && <Loading label="Carregando auditoria…" />}
      {query.isError && <ErrorState error={query.error} retry={() => { void query.refetch() }} />}
      {query.data && (query.data.length === 0 ? <Empty>Nenhuma ação administrativa registrada.</Empty> : (
        <div {...stylex.props(s.list)}>
          {query.data.slice(0, 15).map(entry => (
            <div key={entry.id} {...stylex.props(s.rowCard)}>
              <div {...stylex.props(s.rowMain)}>
                <span {...stylex.props(s.rowTitle)}>
                  <Badge tone={entry.status === 'ok' ? 'success' : 'critical'} icon={<StatusDot tone={entry.status === 'ok' ? 'success' : 'critical'} />}>
                    {entry.status === 'ok' ? 'OK' : 'Erro'}
                  </Badge>
                  <span {...stylex.props(layout.technical)}>{entry.action}</span>
                  <span>→ {entry.target}</span>
                </span>
                <span {...stylex.props(layout.small)}>
                  {entry.actor} · {dateTime(entry.created_at_ms)}{entry.detail ? ` · ${entry.detail}` : ''}
                </span>
              </div>
            </div>
          ))}
        </div>
      ))}
    </div>
  )
}
