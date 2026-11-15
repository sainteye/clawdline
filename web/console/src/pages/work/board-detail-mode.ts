import { useCallback, useState } from "react"

/**
 * Whether Board cards show their full summary (description, gates, times) or
 * only the project mark and title. The choice belongs to this browser: each
 * device remembers its own, and a device that never chose sees brief cards.
 */
export const BOARD_DETAIL_KEY = "clawdline.board.detail-mode"

interface DetailStore {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
}

function browserStore(): DetailStore | null {
  try {
    return typeof localStorage === "undefined" ? null : localStorage
  } catch {
    return null
  }
}

/** Unreadable or blocked storage reads as the default, brief cards. */
export function readBoardDetailMode(store: DetailStore | null = browserStore()): boolean {
  try {
    return store?.getItem(BOARD_DETAIL_KEY) === "detailed"
  } catch {
    return false
  }
}

/** A failed write keeps the choice for this page only; it never throws into the Board. */
export function writeBoardDetailMode(detailed: boolean, store: DetailStore | null = browserStore()): void {
  try {
    store?.setItem(BOARD_DETAIL_KEY, detailed ? "detailed" : "brief")
  } catch {
    // Private windows and blocked site data keep the in-page choice only.
  }
}

export function useBoardDetailMode(): [boolean, (detailed: boolean) => void] {
  const [detailed, setDetailed] = useState(() => readBoardDetailMode())
  const choose = useCallback((next: boolean) => {
    setDetailed(next)
    writeBoardDetailMode(next)
  }, [])
  return [detailed, choose]
}
