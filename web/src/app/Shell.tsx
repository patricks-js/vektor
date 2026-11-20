import * as stylex from '@stylexjs/stylex'
import { Activity, CircleAlert, Clock3, Gauge, Menu, PanelLeftClose, PanelLeftOpen, Radio, Server, Settings, UserRound, WifiOff, X } from 'lucide-react'
import { useEffect, useId, useRef, useState } from 'react'
import { Link, NavLink, Outlet, useLocation } from 'react-router-dom'
import { Badge, Brand, Button, layout } from '../components/ui/ui'
import { useSession } from '../features/auth/session'
import { usePeriodLink } from '../features/metrics/range'
import { useHost } from '../lib/api/queries'
import { usePartialSnapshot, useTransport } from '../lib/realtime/hooks'
import { usePreferences } from './preferences'

const s = stylex.create({
  shell: { display: 'grid', minHeight: '100dvh', gridTemplateColumns: { default: 'minmax(0, 1fr)', '@media (min-width: 768px)': '64px minmax(0, 1fr)', '@media (min-width: 1024px)': '232px minmax(0, 1fr)' } },
  collapsed: { gridTemplateColumns: { default: 'minmax(0, 1fr)', '@media (min-width: 768px)': '64px minmax(0, 1fr)' } },
  sidebar: {
    display: { default: 'none', '@media (min-width: 768px)': 'flex' }, minWidth: 0, flexDirection: 'column',
    position: 'sticky', top: 0, height: '100dvh', borderInlineEndWidth: 1, borderInlineEndStyle: 'solid', borderInlineEndColor: 'var(--vk-border-subtle)',
    backgroundColor: 'var(--vk-surface-1)', padding: 8, gap: 16,
  },
  sideTop: { minHeight: 56, display: 'grid', alignItems: 'center', paddingInline: 8, borderBlockEndWidth: 1, borderBlockEndStyle: 'solid', borderBlockEndColor: 'var(--vk-border-subtle)' },
  sideBottom: { marginBlockStart: 'auto', paddingBlock: 8, borderBlockStartWidth: 1, borderBlockStartStyle: 'solid', borderBlockStartColor: 'var(--vk-border-subtle)', display: 'grid', gap: 8 },
  label: { display: { default: 'none', '@media (min-width: 1024px)': 'inline' } },
  hide: { display: 'none' },
  tabletOnly: { display: { default: 'inline', '@media (min-width: 1024px)': 'none' } },
  desktopOnly: { display: { default: 'none', '@media (min-width: 1024px)': 'block' } },
  mobileOnly: { display: { default: 'block', '@media (min-width: 768px)': 'none' } },
  mobileTools: { display: { default: 'flex', '@media (min-width: 768px)': 'none' }, alignItems: 'center', gap: 8 },
  header: {
    minHeight: 64, display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap',
    gap: '4px 12px', paddingBlock: 8, paddingInline: { default: 16, '@media (min-width: 1024px)': 32 },
    borderBlockEndWidth: 1, borderBlockEndStyle: 'solid', borderBlockEndColor: 'var(--vk-border-subtle)', backgroundColor: 'var(--vk-surface-1)',
  },
  main: { maxWidth: 1680, minWidth: 0, marginInline: 'auto', padding: { default: '20px 16px 32px', '@media (min-width: 768px)': '24px 24px 48px', '@media (min-width: 1024px)': '32px 32px 48px' } },
  nav: { display: 'grid', gap: 4 },
  navItem: {
    minHeight: { default: 42, '@media (pointer: coarse)': 44 }, paddingInline: 12, display: 'flex', alignItems: 'center', gap: 10,
    minWidth: 0, color: 'var(--vk-text-secondary)', fontWeight: 600, textDecoration: 'none',
    borderInlineStartWidth: 2, borderInlineStartStyle: 'solid', borderInlineStartColor: 'transparent',
    backgroundColor: { default: 'transparent', ':hover': 'var(--vk-surface-3)' },
  },
  active: { color: 'var(--vk-accent)', borderInlineStartColor: 'var(--vk-accent)', backgroundColor: 'var(--vk-surface-2)' },
  icon: { flexShrink: 0 },
  drawer: {
    position: 'fixed', inset: '0 auto 0 0', height: '100dvh', maxHeight: '100dvh', width: 'min(300px, 90vw)', maxWidth: 'none',
    margin: 0, borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-strong)', backgroundColor: 'var(--vk-surface-1)', color: 'var(--vk-text-primary)', padding: 16,
  },
  skip: { position: 'fixed', zIndex: 4, top: 8, left: 8, padding: '10px 16px', color: 'var(--vk-accent-ink)', backgroundColor: 'var(--vk-accent)', transform: { default: 'translateY(-160%)', ':focus': 'translateY(0)' } },
  sessionLink: { display: 'inline-flex', alignItems: 'center', minHeight: { default: 40, '@media (pointer: coarse)': 44 }, gap: 8, color: 'var(--vk-text-secondary)', textUnderlineOffset: 4 },
})

const navItems = [
  { to: '/overview', title: 'Overview', icon: Gauge },
  { to: '/host/cpu', title: 'Host', icon: Server },
  { to: '/settings', title: 'Settings', icon: Settings },
]

function Navigation({ collapsed, mobile = false, close }: { collapsed?: boolean; mobile?: boolean; close?: () => void }) {
  const link = usePeriodLink()
  const { pathname } = useLocation()
  return (
    <nav aria-label="Navegação principal" {...stylex.props(s.nav)}>
      {navItems.map(({ to, title, icon: Icon }) => (
        <NavLink key={to} to={link(to)} aria-label={title} title={title} onClick={close}
          aria-current={title === 'Host' && pathname.startsWith('/host/') ? 'location' : undefined}
          className={({ isActive }) => stylex.props(s.navItem, (isActive || (title === 'Host' && pathname.startsWith('/host/'))) && s.active).className ?? ''}>
          <Icon size={18} aria-hidden {...stylex.props(s.icon)} />
          <span {...stylex.props(!mobile && s.label, !mobile && collapsed && s.hide)}>{title}</span>
        </NavLink>
      ))}
    </nav>
  )
}

function TransportBadge() {
  const state = useTransport()
  const partial = usePartialSnapshot()
  switch (state) {
    case 'live': return <div {...stylex.props(layout.row)}>
      <Badge tone="success" icon={<Radio size={15} aria-hidden />} announce>Ao vivo</Badge>
      {partial && <Badge tone="warning" icon={<CircleAlert size={14} aria-hidden />}>Leitura parcial</Badge>}
    </div>
    case 'stale': return <Badge tone="warning" icon={<Clock3 size={15} aria-hidden />} announce>Leituras atrasadas</Badge>
    case 'reconnecting': return <Badge tone="warning" icon={<Activity size={15} aria-hidden />} announce>Reconectando</Badge>
    case 'offline': return <Badge tone="critical" icon={<WifiOff size={15} aria-hidden />} announce>Offline</Badge>
    case 'forbidden': return <Badge tone="critical" icon={<CircleAlert size={15} aria-hidden />} announce>Stream recusado</Badge>
    default: return <Badge tone="neutral" icon={<Radio size={15} aria-hidden />} announce>Conectando</Badge>
  }
}

function HostLabel() {
  const host = useHost()
  return <span {...stylex.props(layout.technical)}>{host.data?.inventory.hostname || 'Host local'}</span>
}

export function Shell() {
  const { collapsed, setCollapsed } = usePreferences()
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
    <div {...stylex.props(s.shell, collapsed && s.collapsed)}>
      <a href="#main" {...stylex.props(s.skip)} onClick={event => { event.preventDefault(); main.current?.focus() }}>Pular para o conteúdo</a>
      <aside {...stylex.props(s.sidebar)}>
        <div {...stylex.props(s.sideTop)}>
          <span {...stylex.props(s.tabletOnly)}><Brand markOnly /></span>
          <span {...stylex.props(s.desktopOnly)}><Brand markOnly={collapsed} /></span>
        </div>
        <Navigation collapsed={collapsed} />
        <div {...stylex.props(s.sideBottom, s.desktopOnly)}>
          <Button variant="quiet" aria-label={collapsed ? 'Expandir barra lateral' : 'Recolher barra lateral'} onClick={() => setCollapsed(!collapsed)}>
            {collapsed ? <PanelLeftOpen size={18} aria-hidden /> : <><PanelLeftClose size={18} aria-hidden /> Recolher</>}
          </Button>
        </div>
      </aside>
      <div>
        <header {...stylex.props(s.header)}>
          <div {...stylex.props(layout.row)}>
            <div {...stylex.props(s.mobileTools)}>
              <Button variant="quiet" onClick={() => setDrawerOpen(true)} aria-label="Abrir navegação" aria-expanded={drawerOpen} aria-haspopup="dialog">
                <Menu size={20} aria-hidden />
              </Button>
              <Brand markOnly />
            </div>
            <HostLabel />
          </div>
          <div {...stylex.props(layout.row)}>
            <TransportBadge />
            <Link to={link('/settings')} aria-label={`Sessão de ${session.username}`} {...stylex.props(s.sessionLink)}>
              <UserRound size={16} aria-hidden /><span {...stylex.props(s.label)}>{session.username}</span>
            </Link>
          </div>
        </header>
        <main {...stylex.props(s.main)} id="main" ref={main} tabIndex={-1}><Outlet /></main>
      </div>
      <dialog {...stylex.props(s.drawer)} ref={drawer} onCancel={() => setDrawerOpen(false)} aria-labelledby={titleId}>
        {drawerOpen && <div {...stylex.props(layout.stack)}>
          <div {...stylex.props(layout.split)}>
            <h2 id={titleId}><Brand /></h2>
            <Button variant="quiet" aria-label="Fechar navegação" onClick={() => setDrawerOpen(false)}><X size={20} aria-hidden /></Button>
          </div>
          <Navigation mobile close={() => setDrawerOpen(false)} />
        </div>}
      </dialog>
    </div>
  )
}
