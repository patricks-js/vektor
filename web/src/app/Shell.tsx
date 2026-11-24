import * as stylex from '@stylexjs/stylex'
import { Boxes, CircleAlert, LayoutGrid, Menu, Moon, Server, Settings, Sun, UserRound, X } from 'lucide-react'
import { useEffect, useId, useRef, useState } from 'react'
import { Link, NavLink, Outlet, useLocation } from 'react-router-dom'
import { Badge, Brand, Button, StatusDot, layout } from '../components/ui/ui'
import { useSession } from '../features/auth/session'
import { usePeriodLink } from '../features/metrics/range'
import { useHost, useUpdateStatus } from '../lib/api/queries'
import { isDemoMode } from '../lib/demo/mock'
import { usePartialSnapshot, useTransport } from '../lib/realtime/hooks'
import { usePreferences } from './preferences'

const s = stylex.create({
  shell: {
    minHeight: '100dvh',
    paddingInline: { default: 12, '@media (min-width: 768px)': 24 },
    paddingBlockStart: { default: 12, '@media (min-width: 768px)': 16 },
    paddingBlockEnd: 48,
  },
  header: {
    maxWidth: 1440, marginInline: 'auto', minHeight: 58,
    display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap',
    gap: '8px 16px', paddingBlock: 8, paddingInline: { default: 12, '@media (min-width: 768px)': 18 },
    borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-subtle)',
    borderRadius: 'var(--vk-radius-3)', backgroundColor: 'var(--vk-surface-1)',
    boxShadow: 'var(--vk-shadow-sm)',
  },
  brandLink: { textDecoration: 'none', display: 'inline-flex', alignItems: 'center' },
  desktopNav: { display: { default: 'none', '@media (min-width: 768px)': 'flex' }, alignItems: 'center', gap: 4 },
  navStack: { display: 'grid', gap: 4 },
  navItem: {
    minHeight: { default: 36, '@media (pointer: coarse)': 44 }, paddingInline: 12,
    display: 'inline-flex', alignItems: 'center', gap: 8, minWidth: 0,
    color: { default: 'var(--vk-text-secondary)', ':hover': 'var(--vk-text-primary)' },
    fontSize: 13, fontWeight: 500, textDecoration: 'none', borderRadius: 'var(--vk-radius-2)',
    backgroundColor: { default: 'transparent', ':hover': 'var(--vk-surface-2)' },
  },
  active: { color: 'var(--vk-text-primary)', backgroundColor: 'var(--vk-surface-3)', fontWeight: 600 },
  icon: { flexShrink: 0 },
  mobileOnly: { display: { default: 'inline-flex', '@media (min-width: 768px)': 'none' }, alignItems: 'center', gap: 8 },
  desktopLabel: { display: { default: 'none', '@media (min-width: 1024px)': 'inline' } },
  divider: {
    display: { default: 'none', '@media (min-width: 1024px)': 'block' },
    width: 1, height: 18, backgroundColor: 'var(--vk-border-subtle)',
  },
  hostPill: {
    display: { default: 'none', '@media (min-width: 1024px)': 'inline-flex' }, alignItems: 'center', gap: 6,
    fontFamily: 'var(--vk-mono)', fontSize: 12, color: 'var(--vk-text-secondary)',
    paddingBlock: 4, paddingInline: 8, borderRadius: 'var(--vk-radius-1)',
    backgroundColor: 'var(--vk-surface-2)', borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-subtle)',
  },
  main: { maxWidth: 1440, minWidth: 0, marginInline: 'auto', paddingBlockStart: 16 },
  drawer: {
    position: 'fixed', inset: '0 auto 0 0', height: '100dvh', maxHeight: '100dvh', width: 'min(300px, 90vw)', maxWidth: 'none',
    margin: 0, borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-strong)',
    backgroundColor: 'var(--vk-surface-1)', color: 'var(--vk-text-primary)', padding: 16,
  },
  skip: {
    position: 'fixed', zIndex: 4, top: 8, left: 8, paddingBlock: 10, paddingInline: 16,
    borderRadius: 'var(--vk-radius-2)', color: 'var(--vk-accent-ink)', backgroundColor: 'var(--vk-accent)',
    transform: { default: 'translateY(-160%)', ':focus': 'translateY(0)' },
  },
  sessionLink: {
    display: 'inline-flex', alignItems: 'center', minHeight: { default: 36, '@media (pointer: coarse)': 44 },
    paddingInline: 10, gap: 7, borderRadius: 'var(--vk-radius-2)', fontSize: 13, fontWeight: 500,
    color: { default: 'var(--vk-text-secondary)', ':hover': 'var(--vk-text-primary)' },
    backgroundColor: { default: 'transparent', ':hover': 'var(--vk-surface-2)' },
    textDecoration: 'none', transition: 'background-color 150ms, color 150ms',
  },
  iconBtn: {
    display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
    minWidth: { default: 36, '@media (pointer: coarse)': 44 },
    minHeight: { default: 36, '@media (pointer: coarse)': 44 },
    borderWidth: 0, borderRadius: 'var(--vk-radius-2)', cursor: 'pointer',
    backgroundColor: { default: 'transparent', ':hover': 'var(--vk-surface-2)' },
    color: { default: 'var(--vk-text-secondary)', ':hover': 'var(--vk-text-primary)' },
    transition: 'background-color 150ms, color 150ms',
  },
})

const navItems = [
  { to: '/overview', title: 'Overview', icon: LayoutGrid },
  { to: '/host/cpu', title: 'Host', icon: Server },
  { to: '/instances', title: 'Instances', icon: Boxes },
  { to: '/settings', title: 'Settings', icon: Settings },
]

function Navigation({ mobile = false, close }: { mobile?: boolean; close?: () => void }) {
  const link = usePeriodLink()
  const { pathname } = useLocation()
  const update = useUpdateStatus()
  const hasUpdate = Boolean(update.data?.update_available)
  return (
    <nav aria-label="Navegação principal" {...stylex.props(mobile ? s.navStack : s.desktopNav)}>
      {navItems.map(({ to, title, icon: Icon }) => {
        const prefixActive = (title === 'Host' && pathname.startsWith('/host/'))
          || (title === 'Instances' && pathname.startsWith('/instances/'))
        return (
          <NavLink key={to} to={link(to)} aria-label={title} title={title} onClick={close}
            aria-current={prefixActive ? 'location' : undefined}
            className={({ isActive }) => stylex.props(s.navItem, (isActive || prefixActive) && s.active).className ?? ''}>
            <Icon size={15} aria-hidden {...stylex.props(s.icon)} />
            <span>{title}</span>
            {title === 'Settings' && hasUpdate && <StatusDot tone="info" />}
          </NavLink>
        )
      })}
    </nav>
  )
}

function TransportBadge() {
  const state = useTransport()
  const partial = usePartialSnapshot()
  switch (state) {
    case 'live': return <div {...stylex.props(layout.row)}>
      <Badge tone="success" icon={<StatusDot tone="success" />} announce>Ao vivo</Badge>
      {partial && <Badge tone="warning" icon={<CircleAlert size={13} aria-hidden />}>Leitura parcial</Badge>}
    </div>
    case 'stale': return <Badge tone="warning" icon={<StatusDot tone="warning" />} announce>Leituras atrasadas</Badge>
    case 'reconnecting': return <Badge tone="warning" icon={<StatusDot tone="warning" />} announce>Reconectando</Badge>
    case 'offline': return <Badge tone="critical" icon={<StatusDot tone="critical" />} announce>Offline</Badge>
    case 'forbidden': return <Badge tone="critical" icon={<StatusDot tone="critical" />} announce>Stream recusado</Badge>
    default: return <Badge tone="neutral" icon={<StatusDot tone="neutral" />} announce>Conectando</Badge>
  }
}

function HostLabel() {
  const host = useHost()
  return <span {...stylex.props(s.hostPill)}>{host.data?.inventory.hostname || 'Host local'}</span>
}

export function Shell() {
  const { theme, setTheme } = usePreferences()
  const { session } = useSession()
  const link = usePeriodLink()
  const { pathname } = useLocation()
  const drawer = useRef<HTMLDialogElement>(null)
  const main = useRef<HTMLElement>(null)
  const titleId = useId()
  const page = pathname.startsWith('/host/') ? '/host' : pathname
  const [drawerOpen, setDrawerOpen] = useState(false)

  useEffect(() => {
    const heading = main.current?.querySelector('h1')
    heading?.focus()
    if (heading?.textContent) document.title = heading.textContent + ' · Vektor'
  }, [page])

  useEffect(() => {
    const dialog = drawer.current
    if (drawerOpen && dialog && !dialog.open) dialog.showModal()
    if (!drawerOpen && dialog?.open) dialog.close()
  }, [drawerOpen])

  useEffect(() => {
    const viewport = matchMedia('(min-width: 768px)')
    const close = (event: MediaQueryListEvent) => { if (event.matches) setDrawerOpen(false) }
    viewport.addEventListener('change', close)
    return () => viewport.removeEventListener('change', close)
  }, [])

  return (
    <div {...stylex.props(s.shell)}>
      <a href="#main" {...stylex.props(s.skip)} onClick={event => { event.preventDefault(); main.current?.focus() }}>Pular para o conteúdo</a>
      <header {...stylex.props(s.header)}>
        <div {...stylex.props(layout.row)}>
          <div {...stylex.props(s.mobileOnly)}>
            <Button variant="quiet" onClick={() => setDrawerOpen(true)} aria-label="Abrir navegação" aria-expanded={drawerOpen} aria-haspopup="dialog">
              <Menu size={18} aria-hidden />
            </Button>
          </div>
          <Link to={link('/overview')} {...stylex.props(s.brandLink)} aria-label="Vektor início">
            <Brand />
          </Link>
          <span aria-hidden {...stylex.props(s.divider)} />
          <Navigation />
        </div>
        <div {...stylex.props(layout.row)}>
          {isDemoMode && <Badge tone="info" icon={<StatusDot tone="info" />}>Demo</Badge>}
          <HostLabel />
          <TransportBadge />
          <button type="button" {...stylex.props(s.iconBtn)}
            aria-label={theme === 'dark' ? 'Alternar para tema claro' : 'Alternar para tema escuro'}
            title={theme === 'dark' ? 'Tema claro' : 'Tema escuro'}
            onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}>
            {theme === 'dark' ? <Sun size={16} aria-hidden /> : <Moon size={16} aria-hidden />}
          </button>
          <Link to={link('/settings')} aria-label={`Sessão de ${session.username}`} {...stylex.props(s.sessionLink)}>
            <UserRound size={15} aria-hidden /><span {...stylex.props(s.desktopLabel)}>{session.username}</span>
          </Link>
        </div>
      </header>
      <main {...stylex.props(s.main)} id="main" ref={main} tabIndex={-1}><Outlet /></main>
      <dialog {...stylex.props(s.drawer)} ref={drawer} onCancel={() => setDrawerOpen(false)} aria-labelledby={titleId}>
        {drawerOpen && <div {...stylex.props(layout.stack)}>
          <div {...stylex.props(layout.split)}>
            <h2 id={titleId}><Brand /></h2>
            <Button variant="quiet" aria-label="Fechar navegação" onClick={() => setDrawerOpen(false)}><X size={18} aria-hidden /></Button>
          </div>
          <Navigation mobile close={() => setDrawerOpen(false)} />
        </div>}
      </dialog>
    </div>
  )
}
