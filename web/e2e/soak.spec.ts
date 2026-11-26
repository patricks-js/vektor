import { expect, test } from '@playwright/test'

const seconds = Number(process.env['VEKTOR_SOAK_SECONDS'] || 0)

test('mede uma sessão prolongada com o SSE real', async ({ page, context }, testInfo) => {
  test.skip(!Number.isFinite(seconds) || seconds < 60, 'Defina VEKTOR_SOAK_SECONDS com pelo menos 60 segundos')
  test.setTimeout((seconds + 60) * 1000)
  await page.goto('/login')
  await page.getByLabel('Usuário').fill('admin')
  await page.getByLabel('Senha', { exact: true }).fill(process.env['VEKTOR_E2E_PASSWORD'] || 'vektor-browser-test-password')
  await page.getByRole('button', { name: 'Entrar' }).click()
  const live = page.getByRole('banner').getByRole('status')
  await expect(live).toHaveText('Ao vivo')
  const cdp = await context.newCDPSession(page)
  let streamStarts = 0
  let historyRequests = 0
  page.on('request', request => {
    const path = new URL(request.url()).pathname
    if (path.endsWith('/stream/live')) streamStarts++
    if (path.endsWith('/metrics')) historyRequests++
  })

  const sample = async () => {
    await cdp.send('HeapProfiler.collectGarbage')
    const heap = await cdp.send('Runtime.getHeapUsage')
    const dom = await cdp.send('Memory.getDOMCounters')
    const health = await context.request.get('/api/v1/health')
    expect(health.ok()).toBe(true)
    const subscribers = (await health.json()).active_live_subscribers
    expect(subscribers).toBe(1)
    return { heapBytes: heap.usedSize, ...dom, subscribers }
  }

  const start = Date.now()
  const samples = [{ seconds: 0, ...(await sample()) }]
  while (Date.now() - start < seconds * 1000) {
    await page.getByRole('link', { name: 'Host' }).click()
    await page.getByRole('tab', { name: 'Memória' }).click()
    await expect(page.getByRole('heading', { name: 'Distribuição da memória' })).toBeVisible()
    await page.getByRole('link', { name: 'Overview' }).click()
    await expect(live).toHaveText('Ao vivo')
    await new Promise(resolve => setTimeout(resolve, Math.min(15_000, Math.max(0, seconds * 1000 - (Date.now() - start)))))
    samples.push({ seconds: Math.round((Date.now() - start) / 1000), ...(await sample()) })
  }
  expect(streamStarts).toBe(0)
  const navigation = await page.evaluate(() => {
    const entry = performance.getEntriesByType('navigation')[0] as PerformanceNavigationTiming
    return { loadMs: Math.round(entry.loadEventEnd), domContentLoadedMs: Math.round(entry.domContentLoadedEventEnd) }
  })
  const evidence = JSON.stringify({ samples, streamRestarts: streamStarts, historyRequests, navigation }, null, 2)
  await testInfo.attach('soak.json', { body: evidence, contentType: 'application/json' })
  console.log(evidence)
})
