import { expect, it } from 'vitest'
import { returnPath } from './routes'

it('resumes valid deep links and rejects external or unsupported return targets', () => {
  expect(returnPath('/host/network?range=6h&resource=nic%3A1')).toBe('/host/network?range=6h&resource=nic%3A1')
  for (const invalid of ['//example.com', 'https://example.com', '/\\example.com', '/host/unknown', '/login']) {
    expect(returnPath(invalid)).toBe('/overview')
  }
})
