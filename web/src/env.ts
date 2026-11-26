import { createEnv } from '@t3-oss/env-core'
import { z } from 'zod'

export const env = createEnv({
  clientPrefix: 'VITE_',
  client: {
    VITE_DEMO_MODE: z
      .enum(['true', 'false'])
      .optional()
      .transform(value => value === 'true'),
  },
  runtimeEnv: {
    VITE_DEMO_MODE: import.meta.env.VITE_DEMO_MODE,
  },
  emptyStringAsUndefined: true,
})
