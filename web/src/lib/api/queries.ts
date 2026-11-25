import { createContext, useContext } from 'react'
import { useQuery } from '@tanstack/react-query'
import type { Api, ResourceKind } from './client'

export const ApiContext = createContext<Api | null>(null)

export function useApi() {
  const api = useContext(ApiContext)
  if (!api) throw new Error('API fora da sessão')
  return api
}

export function useHost() {
  const api = useApi()
  return useQuery({ queryKey: ['host'], queryFn: ({ signal }) => api.host(signal), staleTime: 45_000, refetchInterval: 60_000 })
}

export function useHealth() {
  const api = useApi()
  return useQuery({ queryKey: ['health'], queryFn: ({ signal }) => api.health(signal), staleTime: 20_000, refetchInterval: 30_000 })
}

export function useDiagnostics() {
  const api = useApi()
  return useQuery({
    queryKey: ['diagnostics'],
    queryFn: ({ signal }) => api.diagnostics(signal),
    staleTime: 25_000,
    refetchInterval: 60_000,
    retry: false,
  })
}

export function useResources(kind: ResourceKind) {
  const api = useApi()
  return useQuery({ queryKey: ['resources', kind], queryFn: ({ signal }) => api.resources(kind, signal), staleTime: 45_000, refetchInterval: 60_000 })
}

export function useUpdateStatus() {
  const api = useApi()
  return useQuery({
    queryKey: ['update'],
    queryFn: ({ signal }) => api.update(signal),
    staleTime: 30 * 60_000,
    refetchOnWindowFocus: false,
    retry: false,
  })
}
