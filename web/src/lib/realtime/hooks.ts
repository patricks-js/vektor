import { createContext, useCallback, useContext, useSyncExternalStore } from 'react'
import type { LiveStore } from './store'

export const LiveContext = createContext<LiveStore | null>(null)

function useLiveStore() {
  const store = useContext(LiveContext)
  if (!store) throw new Error('Stream fora da sessão')
  return store
}

export function useTransport() {
  const store = useLiveStore()
  return useSyncExternalStore(store.subscribeStatus, store.getStatus)
}

export function useLiveReading(id: string, metric: string) {
  const store = useLiveStore()
  const subscribe = useCallback((listener: () => void) => store.subscribe(id, metric, listener), [store, id, metric])
  const read = useCallback(() => store.getReading(id, metric), [store, id, metric])
  return useSyncExternalStore(subscribe, read)
}

export function usePartialSnapshot() {
  const store = useLiveStore()
  return useSyncExternalStore(store.subscribePartial, store.getPartial)
}
