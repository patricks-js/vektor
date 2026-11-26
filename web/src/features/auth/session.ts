import { createContext, useContext } from 'react'
import type { Session } from '#/lib/api/client'

export const SessionContext = createContext<{ session: Session; logout: (signal: AbortSignal) => Promise<void> } | null>(null)

export function useSession() {
  const value = useContext(SessionContext)
  if (!value) throw new Error('Sessão ausente')
  return value
}
