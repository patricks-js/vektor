import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useCallback, useEffect, useState } from 'react'
import { flushSync } from 'react-dom'
import { BrowserRouter, Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { ErrorState, Loading } from '../components/ui/ui'
import { AuthFrame, Login } from '../features/auth/Login'
import { SessionContext } from '../features/auth/session'
import { HostPage } from '../features/host/HostPage'
import { InstanceDetail } from '../features/instances/InstanceDetail'
import { InstancesPage } from '../features/instances/InstancesPage'
import { usePeriodLink } from '../features/metrics/range'
import { Overview } from '../features/overview/Overview'
import { SettingsPage } from '../features/settings/SettingsPage'
import { ApiError, createApi } from '../lib/api/client'
import type { Session } from '../lib/api/client'
import { ApiContext } from '../lib/api/queries'
import { createDemoApi, createDemoLiveStore, isDemoMode } from '../lib/demo/mock'
import { LiveContext } from '../lib/realtime/hooks'
import { createLiveStore } from '../lib/realtime/store'
import { PreferencesContext, isRange, isTheme, isTimeZone, readPreference, writePreference } from './preferences'
import type { Theme, TimeRange, TimeZone } from './preferences'
import { setDisplayTimeZone } from '../features/metrics/format'
import { returnPath } from './routes'
import { Shell } from './Shell'

type Auth = { state: 'checking' } | { state: 'guest'; expired: boolean }
  | { state: 'active'; session: Session } | { state: 'error'; error: unknown }

const authApi = isDemoMode ? createDemoApi() : createApi()

function OverviewRedirect() {
  const link = usePeriodLink()
  return <Navigate replace to={link('/overview')} />
}

function ActiveRoutes({ session, expire, logout }: {
  session: Session
  expire: () => void
  logout: () => void
}) {
  const [api] = useState(() => isDemoMode ? createDemoApi(expire) : createApi(expire))
  const [stream] = useState(() => isDemoMode ? createDemoLiveStore() : createLiveStore())
  const [cache] = useState(() => new QueryClient({
    defaultOptions: {
      queries: {
        gcTime: 180_000,
        retry: (count, error) => count < 1 && error instanceof ApiError && (error.status === 0 || error.status === 429),
        retryDelay: (_, error) => error instanceof ApiError ? Math.max(1000, error.retryAfter * 1000) : 1000,
      },
    },
  }))

  useEffect(() => {
    const stop = stream.start({
      unauthorized: expire,
      recovered: () => { void cache.invalidateQueries({ type: 'active' }, { cancelRefetch: false }) },
    })
    return () => {
      stop()
      cache.clear()
    }
  }, [cache, expire, stream])

  useEffect(() => {
    const remaining = Date.parse(session.expires_at) - Date.now()
    const expiry = setTimeout(expire, Math.max(0, remaining))
    return () => clearTimeout(expiry)
  }, [expire, session.expires_at])

  return (
    <ApiContext.Provider value={api}>
      <QueryClientProvider client={cache}>
        <LiveContext.Provider value={stream}>
          <SessionContext.Provider value={{
            session,
            logout: async signal => {
              try {
                await api.logout(session, signal)
              } catch (error) {
                if (!(error instanceof ApiError) || error.code !== 'csrf_denied') throw error
                const current = await api.me(signal)
                await api.logout(current, signal)
              }
              if (!signal.aborted) logout()
            },
          }}>
            <Routes>
              <Route element={<Shell />}>
                <Route index element={<OverviewRedirect />} />
                <Route path="/overview" element={<Overview />} />
                <Route path="/host" element={<HostPage />} />
                <Route path="/host/:subsystem" element={<HostPage />} />
                <Route path="/instances" element={<InstancesPage />} />
                <Route path="/instances/:name" element={<InstanceDetail />} />
                <Route path="/settings" element={<SettingsPage />} />
                <Route path="*" element={<OverviewRedirect />} />
              </Route>
            </Routes>
          </SessionContext.Provider>
        </LiveContext.Provider>
      </QueryClientProvider>
    </ApiContext.Provider>
  )
}

function Application() {
  const [auth, setAuth] = useState<Auth>({ state: 'checking' })
  const [validation, setValidation] = useState(0)
  const navigate = useNavigate()
  const location = useLocation()
  const signIn = location.pathname === '/login'
  const target = returnPath(location.state?.returnTo)

  useEffect(() => {
    const request = new AbortController()
    void authApi.me(request.signal).then(session => {
      if (!request.signal.aborted) setAuth({ state: 'active', session })
    }).catch(error => {
      if (request.signal.aborted) return
      setAuth(error instanceof ApiError && error.status === 401 ? { state: 'guest', expired: false } : { state: 'error', error })
    })
    return () => request.abort()
  }, [validation])

  // Clear private DOM before freezing the document and recheck the cookie on restore.
  useEffect(() => {
    const hide = (event: PageTransitionEvent) => {
      if (event.persisted) flushSync(() => setAuth({ state: 'checking' }))
    }
    const show = (event: PageTransitionEvent) => {
      if (event.persisted) setValidation(value => value + 1)
    }
    window.addEventListener('pagehide', hide)
    window.addEventListener('pageshow', show)
    return () => {
      window.removeEventListener('pagehide', hide)
      window.removeEventListener('pageshow', show)
    }
  }, [])

  const expire = useCallback(() => {
    setAuth(current => current === auth ? { state: 'guest', expired: true } : current)
  }, [auth])

  if (auth.state === 'active') {
    if (signIn) return <Navigate replace to={target} />
    return <ActiveRoutes key={auth.session.expires_at} session={auth.session} expire={expire} logout={() => {
      setAuth({ state: 'guest', expired: false })
      void navigate('/login', { replace: true })
    }} />
  }

  if (auth.state === 'checking') return <AuthFrame><Loading label="Verificando sessão…" /></AuthFrame>
  if (signIn) return <Login expired={auth.state === 'guest' && auth.expired} onSuccess={session => setAuth({ state: 'active', session })} />
  if (auth.state === 'error') return <AuthFrame><ErrorState error={auth.error} title="Não foi possível verificar a sessão" retry={() => {
    setAuth({ state: 'checking' })
    setValidation(value => value + 1)
  }} /></AuthFrame>

  return <Navigate replace to="/login" state={{ returnTo: returnPath(location.pathname + location.search) }} />
}

export function App() {
  const [singleColumn, setSingleColumn] = useState(() => readPreference('layout') === 'single')
  const [theme, setTheme] = useState<Theme>(() => {
    const saved = readPreference('theme')
    return isTheme(saved) ? saved : 'dark'
  })
  const [timezone, setTimezone] = useState<TimeZone>(() => {
    const saved = readPreference('timezone')
    const initial = isTimeZone(saved) ? saved : 'utc'
    setDisplayTimeZone(initial)
    return initial
  })
  const [defaultRange, setDefaultRange] = useState<TimeRange>(() => {
    const saved = readPreference('range')
    return isRange(saved) ? saved : '1h'
  })

  useEffect(() => {
    if (theme === 'light') document.documentElement.dataset.theme = 'light'
    else delete document.documentElement.dataset.theme
  }, [theme])

  return (
    <BrowserRouter>
      <PreferencesContext.Provider value={{
        singleColumn, theme, timezone, defaultRange,
        setSingleColumn: value => { setSingleColumn(value); return writePreference('layout', value ? 'single' : 'grid') },
        setTheme: value => { setTheme(value); return writePreference('theme', value) },
        setTimezone: value => { setDisplayTimeZone(value); setTimezone(value); return writePreference('timezone', value) },
        setDefaultRange: value => { setDefaultRange(value); return writePreference('range', value) },
      }}>
        <Application />
      </PreferencesContext.Provider>
    </BrowserRouter>
  )
}
