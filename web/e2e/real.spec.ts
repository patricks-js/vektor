import { expect, test } from '@playwright/test'

test('autentica no daemon, acompanha o SSE, restaura a conexão e revoga a sessão', async ({ page, context }) => {
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  page.on('console', message => {
    if (message.type() === 'error' && /content security policy/i.test(message.text())) errors.push(message.text())
  })

  await page.goto('/host/memory?range=6h')
  await expect(page).toHaveURL('/login')
  await expect(page.getByLabel('Usuário')).toBeFocused()
  await page.getByLabel('Usuário').fill('admin')
  await page.getByLabel('Senha', { exact: true }).fill('senha-incorreta')
  await page.getByRole('button', { name: 'Entrar' }).click()
  await expect(page.getByRole('alert')).toHaveText('Usuário ou senha inválidos.')
  await expect(page.getByRole('alert')).toBeFocused()
  await page.getByLabel('Senha', { exact: true }).fill(process.env.VEKTOR_E2E_PASSWORD || 'vektor-browser-test-password')
  await page.getByRole('button', { name: 'Mostrar senha' }).click()
  await expect(page.getByLabel('Senha', { exact: true })).toHaveAttribute('type', 'text')
  await page.getByLabel('Senha', { exact: true }).press('Enter')

  await expect(page).toHaveURL('/host/memory?range=6h')
  await expect(page.getByRole('tab', { name: 'Memória' })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByRole('banner').getByRole('status')).toHaveText('Ao vivo', { timeout: 20_000 })
  await expect(page.getByRole('region', { name: 'Distribuição da memória' }).getByText(/GiB|MiB/).first()).toBeVisible()
  const cookies = await context.cookies(new URL('/api/v1/auth/me', page.url()).href)
  expect(cookies.find(cookie => cookie.name === 'vektor_session')).toMatchObject({ httpOnly: true, sameSite: 'Strict', path: '/api/v1' })

  await page.getByRole('link', { name: 'Overview' }).click()
  await expect(page.getByRole('heading', { name: 'Visão geral' })).toBeVisible()
  await expect(page.getByText('Daemon operacional')).toBeVisible()
  await expect(page.getByRole('button', { name: '6h' })).toHaveAttribute('aria-pressed', 'true')

  // The API-only buckets are created by the actual persistence scheduler.
  await expect.poll(async () => {
    const to = Math.floor(Date.now() / 30_000) * 30_000
    const params = new URLSearchParams({ resource_id: 'host:local', metric: 'cpu.busy_pct',
      from: String(to - 900_000), to: String(to), step: 'auto' })
    const response = await context.request.get('/api/v1/metrics?' + params.toString())
    return response.ok() && (await response.json()).points.length > 0
  }, { timeout: 45_000, intervals: [500, 1000, 2000] }).toBe(true)
  await page.getByRole('button', { name: '15m' }).click()
  await expect(page.getByRole('region', { name: 'Utilização da CPU' }).getByRole('slider')).toBeVisible()

  await context.setOffline(true)
  await expect(page.getByRole('banner').getByRole('status')).toHaveText('Offline')
  await context.setOffline(false)
  await expect(page.getByRole('banner').getByRole('status')).toHaveText('Ao vivo', { timeout: 20_000 })

  await page.getByRole('link', { name: 'Settings' }).click()
  const opener = page.getByRole('button', { name: 'Encerrar sessão' })
  await opener.click()
  await expect(page.getByRole('dialog')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(opener).toBeFocused()
  await opener.click()
  await page.getByRole('button', { name: 'Sair', exact: true }).click()
  await expect(page).toHaveURL('/login')
  await page.goBack()
  await expect(page).toHaveURL('/login')
  const unauthorized = await context.request.get('/api/v1/host')
  expect(unauthorized.status()).toBe(401)
  const unknown = await context.request.get('/api/v1/unsupported')
  expect(unknown.status()).toBe(404)
  expect(unknown.headers()['content-type']).toContain('application/json')
  expect(errors).toEqual([])
})
