import * as stylex from '@stylexjs/stylex'
import { ArrowUpRight, Bell, FileText, History, LogOut, Power, Send, SlidersHorizontal, UserRound } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { isRange, isTheme, isTimeZone, ranges, usePreferences } from '../../app/preferences'
import { Facts } from '../../components/ui/dashboard'
import { Badge, Button, ErrorState, Modal, SelectField, StatusDot, layout } from '../../components/ui/ui'
import { useUpdateStatus } from '../../lib/api/queries'
import { dateTime, timeZoneLabel } from '../metrics/format'
import { useSession } from '../auth/session'
import fontLicenses from '../../styles/geist-license.txt?url'
import { AlertsSection, NotificationsSection } from './AlertsSettings'
import { AuditLogSection, HostPowerSection } from './HostControlSettings'

const s = stylex.create({
  card: {
    display: 'grid', gap: 20, minWidth: 0,
    backgroundColor: 'var(--vk-surface-1)', borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-subtle)',
    borderRadius: 'var(--vk-radius-3)', padding: { default: 18, '@media (min-width: 1024px)': 28 },
  },
  header: {
    display: 'grid', gap: 4, paddingBlockEnd: 20,
    borderBlockEndWidth: 1, borderBlockEndStyle: 'solid', borderBlockEndColor: 'var(--vk-border-subtle)',
  },
  title: { outlineStyle: 'none' },
  body: {
    display: 'grid', gap: 24, minWidth: 0,
    gridTemplateColumns: { default: 'minmax(0, 1fr)', '@media (min-width: 860px)': '210px minmax(0, 1fr)' },
    alignItems: 'start',
  },
  sideNav: {
    display: 'flex', flexWrap: 'wrap', gap: 4,
    flexDirection: { default: 'row', '@media (min-width: 860px)': 'column' },
  },
  sideItem: {
    display: 'inline-flex', alignItems: 'center', gap: 10, paddingBlock: 8, paddingInline: 12,
    minHeight: { default: 38, '@media (pointer: coarse)': 44 }, borderRadius: 'var(--vk-radius-2)',
    fontSize: 13, fontWeight: 500, textDecoration: 'none', cursor: 'pointer',
    borderWidth: 0, textAlign: 'start',
    color: { default: 'var(--vk-text-secondary)', ':hover': 'var(--vk-text-primary)' },
    backgroundColor: { default: 'transparent', ':hover': 'var(--vk-surface-2)' },
    transition: 'background-color 150ms, color 150ms',
  },
  sideItemActive: {
    backgroundColor: 'var(--vk-surface-3)', color: 'var(--vk-text-primary)', fontWeight: 600,
  },
  content: { display: 'grid', gap: 28, minWidth: 0, maxWidth: 680 },
  section: {
    display: 'grid', gap: 16, minWidth: 0,
    paddingBlockEnd: 24, borderBlockEndWidth: 1, borderBlockEndStyle: 'solid', borderBlockEndColor: 'var(--vk-border-subtle)',
  },
  sectionLast: { paddingBlockEnd: 0, borderBlockEndWidth: 0 },
  sectionHead: { display: 'grid', gap: 4 },
  fields: { display: 'grid', gap: 16, gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 220px), 1fr))' },
  checkbox: { display: 'flex', width: 'fit-content', gap: 10, minHeight: 44, alignItems: 'center', cursor: 'pointer', fontSize: 13 },
  check: { width: 16, height: 16, accentColor: 'var(--vk-accent)' },
  license: { color: 'var(--vk-text-primary)', textUnderlineOffset: 4, fontSize: 13 },
  feedback: { minHeight: '19px' },
  updateBox: {
    display: 'grid', gap: 8, padding: 14, borderRadius: 'var(--vk-radius-2)',
    backgroundColor: 'var(--vk-surface-2)', borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-strong)',
  },
  updateLink: {
    display: 'inline-flex', alignItems: 'center', gap: 5, fontSize: 13, fontWeight: 500,
    color: 'var(--vk-info)', textUnderlineOffset: 4, width: 'fit-content',
  },
  updateCmd: {
    fontFamily: 'var(--vk-font-mono)', fontSize: 12,
    paddingBlock: 6, paddingInline: 10, borderRadius: 'var(--vk-radius-1)',
    backgroundColor: 'var(--vk-surface-1)', borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-subtle)',
    color: 'var(--vk-text-primary)', width: 'fit-content', userSelect: 'all',
  },
})

const sections = [
  { id: 'geral', label: 'Geral', icon: SlidersHorizontal },
  { id: 'alertas', label: 'Alertas', icon: Bell },
  { id: 'notificacoes', label: 'Notificações', icon: Send },
  { id: 'energia', label: 'Energia', icon: Power },
  { id: 'auditoria', label: 'Auditoria', icon: History },
  { id: 'sessao', label: 'Sessão', icon: UserRound },
  { id: 'sobre', label: 'Sobre', icon: FileText },
] as const

export function SettingsPage() {
  const { singleColumn, setSingleColumn, theme, setTheme, timezone, setTimezone, defaultRange, setDefaultRange } = usePreferences()
  const { session, logout } = useSession()
  const update = useUpdateStatus()
  const [activeSection, setActiveSection] = useState<typeof sections[number]['id']>('geral')
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
      <div {...stylex.props(s.card)}>
        <div {...stylex.props(s.header)}>
          <h1 {...stylex.props(layout.pageTitle, s.title)} tabIndex={-1}>Configurações</h1>
          <p {...stylex.props(layout.small)}>Gerencie preferências de exibição e a sessão administrativa.</p>
        </div>

        <div {...stylex.props(s.body)}>
          <nav aria-label="Seções de configurações" {...stylex.props(s.sideNav)}>
            {sections.map(({ id, label, icon: Icon }) => (
              <a key={id} href={`#${id}`} onClick={() => setActiveSection(id)}
                {...stylex.props(s.sideItem, activeSection === id && s.sideItemActive)}>
                <Icon size={15} aria-hidden />
                <span>{label}</span>
              </a>
            ))}
          </nav>

          <div {...stylex.props(s.content)}>
            <section id="geral" aria-labelledby="settings-general" {...stylex.props(s.section)}>
              <div {...stylex.props(s.sectionHead)}>
                <h2 id="settings-general" {...stylex.props(layout.sectionTitle)}>Interface</h2>
                <p {...stylex.props(layout.small)}>Ajuste o período padrão, o tema e a disposição dos gráficos.</p>
              </div>
              <div {...stylex.props(s.fields)}>
                <SelectField label="Período padrão" value={defaultRange} onChange={event => {
                  if (isRange(event.currentTarget.value)) saved(setDefaultRange(event.currentTarget.value))
                }}>
                  {ranges.map(range => <option key={range}>{range}</option>)}
                </SelectField>
                <SelectField label="Tema da interface" value={theme} onChange={event => {
                  if (isTheme(event.currentTarget.value)) saved(setTheme(event.currentTarget.value))
                }}>
                  <option value="dark">Escuro</option>
                  <option value="light">Claro</option>
                </SelectField>
                <SelectField label="Fuso horário" value={timezone} onChange={event => {
                  if (isTimeZone(event.currentTarget.value)) saved(setTimezone(event.currentTarget.value))
                }}>
                  <option value="utc">UTC</option>
                  <option value="local">Horário local</option>
                </SelectField>
              </div>
              <label {...stylex.props(s.checkbox)}>
                <input type="checkbox" {...stylex.props(s.check)} checked={singleColumn} onChange={event => saved(setSingleColumn(event.currentTarget.checked))} />
                Exibir gráficos em coluna única
              </label>
              <p role="status" {...stylex.props(layout.small, s.feedback)}>{feedback}</p>
            </section>

            <section id="alertas" aria-labelledby="settings-alerts" {...stylex.props(s.section)}>
              <div {...stylex.props(s.sectionHead)}>
                <h2 id="settings-alerts" {...stylex.props(layout.sectionTitle)}>Alertas</h2>
                <p {...stylex.props(layout.small)}>Defina limites para CPU, memória, disco e temperatura.</p>
              </div>
              <AlertsSection />
            </section>

            <section id="notificacoes" aria-labelledby="settings-notifications" {...stylex.props(s.section)}>
              <div {...stylex.props(s.sectionHead)}>
                <h2 id="settings-notifications" {...stylex.props(layout.sectionTitle)}>Notificações</h2>
                <p {...stylex.props(layout.small)}>Envie alertas via Webhook HTTP (POST JSON) para seus canais.</p>
              </div>
              <NotificationsSection />
            </section>

            <section id="energia" aria-labelledby="settings-power" {...stylex.props(s.section)}>
              <div {...stylex.props(s.sectionHead)}>
                <h2 id="settings-power" {...stylex.props(layout.sectionTitle)}>Energia do servidor</h2>
                <p {...stylex.props(layout.small)}>Reinicie ou desligue o host de forma controlada via vektor-ctl.socket.</p>
              </div>
              <HostPowerSection />
            </section>

            <section id="auditoria" aria-labelledby="settings-audit" {...stylex.props(s.section)}>
              <div {...stylex.props(s.sectionHead)}>
                <h2 id="settings-audit" {...stylex.props(layout.sectionTitle)}>Auditoria</h2>
                <p {...stylex.props(layout.small)}>Registro das últimas ações administrativas executadas pelo painel.</p>
              </div>
              <AuditLogSection />
            </section>

            <section id="sessao" aria-labelledby="settings-session" {...stylex.props(s.section)}>
              <div {...stylex.props(s.sectionHead)}>
                <h2 id="settings-session" {...stylex.props(layout.sectionTitle)}>Sessão</h2>
                <p {...stylex.props(layout.small)}>Detalhes da autenticação local ativa neste navegador.</p>
              </div>
              <Facts items={[['Usuário', session.username], ['Expiração', dateTime(session.expires_at)]]} />
              <div>
                <Button variant="danger" onClick={() => setDialog(true)}><LogOut size={15} aria-hidden /> Encerrar sessão</Button>
              </div>
            </section>

            <section id="sobre" aria-labelledby="settings-about" {...stylex.props(s.section, s.sectionLast)}>
              <div {...stylex.props(s.sectionHead)}>
                <h2 id="settings-about" {...stylex.props(layout.sectionTitle)}>Sobre o Vektor</h2>
                <p {...stylex.props(layout.small)}>Informações da interface web, daemon e atualizações.</p>
              </div>
              <Facts items={[
                ['Versão', update.data?.current_version || __APP_VERSION__],
                ['Contrato da API', 'v1'],
                ['Fuso dos gráficos', timeZoneLabel(timezone)],
                ['Status da versão', update.data?.update_available
                  ? <Badge tone="info" icon={<StatusDot tone="info" />}>v{update.data.latest_version} disponível</Badge>
                  : update.data?.latest_version
                    ? <Badge tone="success" icon={<StatusDot tone="success" />}>Atualizado</Badge>
                    : '—'],
              ]} />
              {update.data?.update_available && update.data.latest_version && (
                <div {...stylex.props(s.updateBox)}>
                  <p {...stylex.props(layout.small)}>
                    Uma nova versão (<strong>v{update.data.latest_version}</strong>) está disponível no GitHub Releases. Para atualizar pelo terminal:
                  </p>
                  <code {...stylex.props(s.updateCmd)}>sudo vektord upgrade</code>
                  {update.data.release_url && (
                    <a href={update.data.release_url} target="_blank" rel="noreferrer" {...stylex.props(s.updateLink)}>
                      Ver notas da versão v{update.data.latest_version} <ArrowUpRight size={14} aria-hidden />
                    </a>
                  )}
                </div>
              )}
              <p><a href={fontLicenses} {...stylex.props(s.license)}>Licença da tipografia</a></p>
            </section>
          </div>
        </div>
      </div>

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
