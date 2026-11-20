import { createContext, useContext, useState } from 'react'
import type { ReactNode } from 'react'

const CursorContext = createContext<{ time?: number; setTime: (time?: number) => void }>({ setTime: () => {} })

export function ChartCursorProvider({ children }: { children: ReactNode }) {
  const [time, setTime] = useState<number>()
  return <CursorContext.Provider value={{ time, setTime }}>{children}</CursorContext.Provider>
}

export function useChartCursor() {
  return useContext(CursorContext)
}
