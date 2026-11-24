export const hostTabs = [
  { id: 'cpu', label: 'CPU' },
  { id: 'memory', label: 'Memória' },
  { id: 'storage', label: 'Armazenamento' },
  { id: 'network', label: 'Rede' },
  { id: 'sensors', label: 'Sensores' },
  { id: 'services', label: 'Serviços' },
  { id: 'processes', label: 'Processos' },
] as const

export type HostTab = typeof hostTabs[number]['id']

export function isHostTab(value: string | undefined): value is HostTab {
  return hostTabs.some(tab => tab.id === value)
}

export function returnPath(value: unknown) {
  if (typeof value !== 'string' || !value.startsWith('/') || value.startsWith('//') || value.includes('\\')) return '/overview'
  const href = new URL(value, 'https://vektor.invalid')
  const hostTab = href.pathname.match(/^\/host\/([^/]+)$/)?.[1]
  if (['/', '/overview', '/host', '/settings'].includes(href.pathname) || isHostTab(hostTab)) {
    return href.pathname + href.search
  }
  return '/overview'
}
