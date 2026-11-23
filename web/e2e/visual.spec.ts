import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import { fixture } from './fixtures'

const tags = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa']

for (const width of [1440, 1024, 768, 390, 360]) {
  test(`layout e foco a ${width}px`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 960 })
    await fixture(page)
    await page.goto('/overview')
    await expect(page.getByRole('heading', { name: 'Visão geral' })).toBeVisible()
    await expect(page.getByText('Histórico parcial').first()).toBeVisible()
    await expect(page.getByRole('banner').getByRole('status')).toHaveText('Ao vivo')
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth)).toBe(width)
    const cpu = page.getByRole('region', { name: 'Utilização da CPU' })
    await expect(cpu).toHaveCSS('background-color', 'rgb(18, 18, 20)')
    await expect(cpu).toHaveCSS('border-top-width', '1px')
    if (width >= 768) {
      const active = page.getByRole('link', { name: 'Overview' })
      await expect(active).toHaveCSS('background-color', 'rgb(39, 39, 42)')
      await expect(active).toHaveCSS('color', 'rgb(250, 250, 250)')
    }
    await page.screenshot({ path: testInfo.outputPath('overview.png'), fullPage: true })
    const accessibility = await new AxeBuilder({ page }).withTags(tags).analyze()
    expect(accessibility.violations).toEqual([])

    if (width < 768) {
      const opener = page.getByRole('button', { name: 'Abrir navegação' })
      await opener.focus()
      await page.keyboard.press('Enter')
      const drawer = page.getByRole('dialog')
      await expect(drawer).toBeVisible()
      await expect(drawer.getByRole('button', { name: 'Fechar navegação' })).toBeFocused()
      await page.keyboard.press('Escape')
      await expect(drawer).toBeHidden()
      await expect(opener).toBeFocused()
    }

    await page.goto('/host/network?resource=nic%3A2&range=1h')
    await expect(page.getByRole('table', { name: 'Interfaces de rede' })).toBeVisible()
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth)).toBe(width)
    if (width === 390 || width === 1440) {
      await expect(page.getByRole('heading', { name: 'porta-renomeada' })).toBeVisible()
      expect((await new AxeBuilder({ page }).withTags(tags).analyze()).violations).toEqual([])
      await page.screenshot({ path: testInfo.outputPath('network.png'), fullPage: true })
    }
  })
}

test('audita contraste e semântica no login, Settings e confirmação de sessão', async ({ page }) => {
  const mock = await fixture(page)
  mock.expire()
  await page.goto('/login')
  await expect(page.getByRole('heading', { name: 'Entrar' })).toBeVisible()
  expect((await new AxeBuilder({ page }).withTags(tags).analyze()).violations).toEqual([])
  await page.screenshot({ path: test.info().outputPath('login.png') })
  await page.getByLabel('Usuário').fill('admin')
  await page.getByLabel('Senha', { exact: true }).fill('vektor-test')
  await page.getByRole('button', { name: 'Entrar' }).click()
  await expect(page.getByRole('button', { name: /Tentar em/ })).toBeDisabled()

  await page.unrouteAll()
  await fixture(page)
  await page.goto('/settings')
  await expect(page.getByRole('heading', { name: 'Configurações' })).toBeVisible()
  await page.getByLabel('Período padrão').selectOption('24h')
  await page.getByLabel('Exibir gráficos em coluna única').check()
  await expect(page.getByText('Preferência salva neste navegador.')).toBeVisible()
  await page.reload()
  await expect(page.getByLabel('Período padrão')).toHaveValue('24h')
  await expect(page.getByLabel('Exibir gráficos em coluna única')).toBeChecked()
  await page.goto('/overview')
  await expect(page.getByRole('button', { name: '24h' })).toHaveAttribute('aria-pressed', 'true')
  await page.getByRole('link', { name: 'Settings' }).click()
  await page.screenshot({ path: test.info().outputPath('settings.png'), fullPage: true })
  await page.getByRole('button', { name: 'Encerrar sessão' }).click()
  await expect(page.getByRole('dialog')).toBeVisible()
  expect((await new AxeBuilder({ page }).withTags(tags).analyze()).violations).toEqual([])
})
