import * as stylex from '@stylexjs/stylex'
import { LogOut } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { ranges, isRange, usePreferences } from '../../app/preferences'
import { Facts, PageHeading } from '../../components/ui/dashboard'
import { Button, ErrorState, Modal, Panel, SelectField, layout } from '../../components/ui/ui'
import { dateTime } from '../metrics/format'
import { useSession } from '../auth/session'
import fontLicenses from '../../styles/geist-license.txt?url'

const s = stylex.create({
  checkbox: { display: 'flex', width: 'fit-content', gap: 8, minHeight: 44, alignItems: 'center', cursor: 'pointer' },
  check: { width: 18, height: 18, accentColor: 'var(--vk-accent)' },
  preference: { maxWidth: 300 },
  license: { color: 'var(--vk-accent)', textUnderlineOffset: 4 },
  feedback: { minHeight: '19px' },
})

export function SettingsPage() {
  const { collapsed, setCollapsed, defaultRange, setDefaultRange } = usePreferences()
  const { session, logout } = useSession()
  const [feedback, setFeedback] = useState('')
  const [dialog, setDialog] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>()
  const request = useRef<AbortController | undefined>(undefined)
  useEffect(() => () => request.current?.abort(), [])

  function saved(success: boolean) {
    setFeedback(success ? 'Preferência salva neste navegador.' : 'Preferência aplicada à sessão. O navegador bloqueou o armazenamento.')
  }

  async function signOut() {
    if (request.current) return
    const current = new AbortController()
    request.current = current
    setBusy(true)
    setError(undefined)
    try {
      await logout(current.signal)
    } catch (error) {
      if (!current.signal.aborted) setError(error)
    } finally {
      request.current = undefined
      if (!current.signal.aborted) setBusy(false)
    }
  }

  return (
    <div {...stylex.props(layout.page)}>
      <PageHeading title="Configurações" description="Preferências locais e sessão administrativa" />
      <div {...stylex.props(layout.grid2)}>
        <Panel title="Interface">
          <div {...stylex.props(s.preference)}>
            <SelectField label="Período padrão" value={defaultRange} onChange={event => {
              if (isRange(event.currentTarget.value)) saved(setDefaultRange(event.currentTarget.value))
            }}>
              {ranges.map(range => <option key={range}>{range}</option>)}
            </SelectField>
          </div>
          <label {...stylex.props(s.checkbox)}>
            <input type="checkbox" {...stylex.props(s.check)} checked={collapsed} onChange={event => saved(setCollapsed(event.currentTarget.checked))} />
            Recolher a barra lateral no desktop
          </label>
          <p role="status" {...stylex.props(layout.small, s.feedback)}>{feedback}</p>
        </Panel>
        <Panel title="Sobre o Vektor">
          <Facts items={[['Interface', __APP_VERSION__], ['Contrato da API', 'v1'], ['Fuso dos gráficos', 'UTC']]} />
          <p><a href={fontLicenses} {...stylex.props(s.license)}>Licença da tipografia</a></p>
        </Panel>
      </div>
      <Panel title="Sessão">
        <Facts items={[['Usuário', session.username], ['Expiração', dateTime(session.expires_at)]]} />
        <div><Button variant="danger" onClick={() => setDialog(true)}><LogOut size={16} aria-hidden /> Encerrar sessão</Button></div>
      </Panel>
      <Modal title="Encerrar sessão" open={dialog} onClose={() => {
        if (busy) return
        setError(undefined)
        setDialog(false)
      }} preventClose={busy}>
        <p {...stylex.props(layout.text)}>Você voltará à tela de login.</p>
        {error != null && <ErrorState error={error} title="Não foi possível encerrar a sessão" />}
        <div {...stylex.props(layout.row)}>
          <Button disabled={busy} onClick={() => { setDialog(false); setError(undefined) }}>Cancelar</Button>
          <Button variant="danger" busy={busy} onClick={() => { void signOut() }}>Sair</Button>
        </div>
      </Modal>
    </div>
  )
}
