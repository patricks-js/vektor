// @vitest-environment happy-dom
import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { Login } from './Login'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

it('honors Retry-After after a rate-limited login', async () => {
  const request = vi.fn(async () => new Response(JSON.stringify({ code: 'login_rate_limited' }), {
    status: 429, headers: { 'Retry-After': '20', 'Content-Type': 'application/json' },
  }))
  vi.stubGlobal('fetch', request)
  const onSuccess = vi.fn()
  render(<Login expired={false} onSuccess={onSuccess} />)
  expect(document.activeElement).toBe(screen.getByLabelText('Usuário'))
  fireEvent.input(screen.getByLabelText('Usuário'), { target: { value: 'admin' } })
  fireEvent.input(screen.getByLabelText('Senha'), { target: { value: 'pass' } })
  fireEvent.click(screen.getByRole('button', { name: 'Entrar' }))
  await waitFor(() => expect(screen.getByRole('alert').textContent).toMatch(/Muitas tentativas/))
  expect(document.activeElement).toBe(screen.getByRole('alert'))
  const delayed = screen.getByRole<HTMLButtonElement>('button', { name: /Tentar em/ })
  expect(delayed.disabled).toBe(true)
  expect(request).toHaveBeenCalledTimes(1)
  expect(onSuccess).not.toHaveBeenCalled()
})
