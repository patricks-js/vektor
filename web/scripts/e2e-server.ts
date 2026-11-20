// Isolated real daemon for Playwright. Only this disposable test account uses a fixed password.
import { mkdtemp, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const temporary = await mkdtemp(join(tmpdir(), 'vektor-e2e-'))
const binary = join(temporary, 'vektord')
const state = join(temporary, 'data')
const port = Bun.env.VEKTOR_E2E_PORT || '18437'
const env = Object.fromEntries(Object.entries(Bun.env).filter(([key]) => !key.startsWith('VEKTOR_')))
const repo = new URL('../..', import.meta.url).pathname

try {
  const build = Bun.spawn(['go', 'build', '-tags', 'webui', '-o', binary, './cmd/vektord'], {
    cwd: repo, stdout: 'inherit', stderr: 'inherit', env,
  })
  if (await build.exited) throw new Error('Falha ao compilar vektord para E2E')

  const bootstrap = Bun.spawn([binary, 'bootstrap-admin', '--data-dir', state], {
    stdin: new Blob(['vektor-browser-test-password\n']), stdout: 'inherit', stderr: 'inherit', env,
  })
  if (await bootstrap.exited) throw new Error('Falha no bootstrap do administrador E2E')

  const daemon = Bun.spawn([binary, '--http-addr', `127.0.0.1:${port}`, '--data-dir', state,
    '--live-interval', '1s', '--persist-interval', '10s'], { stdout: 'inherit', stderr: 'inherit', env })
  const shutdown = () => daemon.kill('SIGTERM')
  process.on('SIGTERM', shutdown)
  process.on('SIGINT', shutdown)
  const exit = await daemon.exited
  process.off('SIGTERM', shutdown)
  process.off('SIGINT', shutdown)
  if (exit) throw new Error(`Daemon E2E encerrou com código ${exit}`)
} finally {
  await rm(temporary, { recursive: true, force: true })
}
