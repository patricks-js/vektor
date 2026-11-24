import { createContext, useContext } from 'react'

export const ranges = ['15m', '1h', '6h', '24h', '7d'] as const
export type TimeRange = typeof ranges[number]
export type Theme = 'dark' | 'light'
export type TimeZone = 'utc' | 'local'

export function isRange(value: unknown): value is TimeRange {
  return ranges.some(range => range === value)
}

export function isTheme(value: unknown): value is Theme {
  return value === 'dark' || value === 'light'
}

export function isTimeZone(value: unknown): value is TimeZone {
  return value === 'utc' || value === 'local'
}

export function readPreference(key: string) {
  try { return localStorage.getItem('vektor.' + key) } catch { return null }
}

export function writePreference(key: string, value: string) {
  try { localStorage.setItem('vektor.' + key, value); return true } catch { return false }
}

export const PreferencesContext = createContext<{
  singleColumn: boolean
  setSingleColumn: (value: boolean) => boolean
  theme: Theme
  setTheme: (value: Theme) => boolean
  timezone: TimeZone
  setTimezone: (value: TimeZone) => boolean
  defaultRange: TimeRange
  setDefaultRange: (value: TimeRange) => boolean
} | null>(null)

export function usePreferences() {
  const preferences = useContext(PreferencesContext)
  if (!preferences) throw new Error('Preferências indisponíveis')
  return preferences
}
