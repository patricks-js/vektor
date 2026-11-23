import * as stylex from '@stylexjs/stylex'
import { ArrowRight, Eye, EyeOff, Terminal } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import type { FormEvent, ReactNode } from 'react'
import { ApiError, createApi } from '../../lib/api/client'
import type { Session } from '../../lib/api/client'
import { createDemoApi, isDemoMode } from '../../lib/demo/mock'
import { Brand, Button, TextField, layout } from '../../components/ui/ui'

const s = stylex.create({
  screen: { display: 'grid', minHeight: '100dvh', placeItems: 'center', padding: { default: 16, '@media (min-width: 768px)': 32 } },
  frame: { display: 'grid', gap: 16, width: 'min(100%, 400px)' },
  box: {
    backgroundColor: 'var(--vk-surface-1)', padding: { default: 24, '@media (min-width: 768px)': 28 },
    borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-border-subtle)',
    borderRadius: 'var(--vk-radius-3)', boxShadow: 'var(--vk-shadow-md)',
    display: 'grid', gap: 22, minWidth: 0,
  },
  brand: { borderBlockEndWidth: 1, borderBlockEndStyle: 'solid', borderBlockEndColor: 'var(--vk-border-subtle)', paddingBlockEnd: 18 },
  footer: { display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 8, color: 'var(--vk-text-muted)' },
  error: {
    color: 'var(--vk-critical)', fontSize: 13,
    backgroundColor: 'var(--vk-surface-2)', borderWidth: 1, borderStyle: 'solid', borderColor: 'var(--vk-critical)',
    borderRadius: 'var(--vk-radius-2)', paddingBlock: 10, paddingInline: 12,
  },
})

export function AuthFrame({ children }: { children: ReactNode }) {
  return (
    <main {...stylex.props(s.screen)}>
      <div {...stylex.props(s.frame)}>
        <div {...stylex.props(s.box)}>
          <div {...stylex.props(s.brand)}><Brand /></div>
          {children}
        </div>
        <div {...stylex.props(s.footer, layout.technical)}><Terminal size={14} aria-hidden /> Monitoramento local</div>
      </div>
    </main>
  )
}

const api = isDemoMode ? createDemoApi() : createApi()

export function Login({ expired, onSuccess }: { expired: boolean; onSuccess: (session: Session) => void }) {
  const [visible, setVisible] = useState(false)
  const [error, setError] = useState('')
  const [pending, setPending] = useState(false)
  const [retryAt, setRetryAt] = useState(0)
  const [now, setNow] = useState(Date.now)
  const username = useRef<HTMLInputElement>(null)
  const message = useRef<HTMLParagraphElement>(null)
  const inFlight = useRef<AbortController | undefined>(undefined)

  useEffect(() => {
    username.current?.focus()
    return () => inFlight.current?.abort()
  }, [])

  useEffect(() => {
    if (error) message.current?.focus()
  }, [error])

  useEffect(() => {
    if (!retryAt) return
    const tick = setInterval(() => {
      const time = Date.now()
      setNow(time)
      if (time >= retryAt) clearInterval(tick)
    }, 1000)
    return () => clearInterval(tick)
  }, [retryAt])

  const retryIn = Math.max(0, Math.ceil((retryAt - now) / 1000))

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (inFlight.current || retryIn) return
    const values = new FormData(event.currentTarget)
    const controller = new AbortController()
    inFlight.current = controller
    setPending(true)
    setError('')
    try {
      const session = await api.login({
        username: String(values.get('username')), password: String(values.get('password')),
      }, controller.signal)
      if (!controller.signal.aborted) onSuccess(session)
    } catch (error) {
      if (controller.signal.aborted) return
      if (error instanceof ApiError && error.status === 429) {
        const time = Date.now()
        setNow(time)
        setRetryAt(time + Math.max(1, error.retryAfter) * 1000)
        setError('Muitas tentativas. Aguarde o prazo informado antes de tentar novamente.')
      } else if (error instanceof ApiError && error.status === 401) {
        setError('Usuário ou senha inválidos.')
      } else if (error instanceof ApiError && [400, 413].includes(error.status)) {
        setError('Dados de login inválidos. Confira o usuário e o tamanho da senha.')
      } else {
        setError('Falha ao entrar. Verifique a conexão com o Vektor e tente novamente.')
      }
    } finally {
      inFlight.current = undefined
      if (!controller.signal.aborted) setPending(false)
    }
  }

  return (
    <AuthFrame>
      <div {...stylex.props(layout.compact)}>
        <h1 {...stylex.props(layout.pageTitle)}>Entrar</h1>
        <p {...stylex.props(layout.small)}>
          {isDemoMode
            ? 'Modo demo ativo. Informe qualquer usuário e senha para entrar (use "erro" como senha para testar falha).'
            : expired ? 'Sua sessão expirou. Entre novamente para continuar.' : 'Acesse o painel de monitoramento do host.'}
        </p>
      </div>
      <form onSubmit={submit} {...stylex.props(layout.stack)}>
        <TextField label="Usuário" name="username" autoComplete="username" autoCapitalize="none" spellCheck={false} maxLength={128} required ref={username} />
        <TextField label="Senha" name="password" autoComplete="current-password" type={visible ? 'text' : 'password'} required
          action={<Button variant="outline" aria-label={visible ? 'Ocultar senha' : 'Mostrar senha'} onClick={() => setVisible(!visible)}>
            {visible ? <EyeOff size={16} aria-hidden /> : <Eye size={16} aria-hidden />}
          </Button>} />
        {error && <p ref={message} tabIndex={-1} role="alert" {...stylex.props(s.error)}>{error}</p>}
        <Button type="submit" variant="primary" busy={pending} disabled={retryIn > 0}>
          {pending ? 'Entrando…' : retryIn ? `Tentar em ${retryIn}s` : 'Entrar'}
          {!pending && !retryIn && <ArrowRight size={15} aria-hidden />}
        </Button>
      </form>
    </AuthFrame>
  )
}
