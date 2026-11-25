import { useEffect, useState } from "react"
import type * as W from "@clawdline/contract"
import type { Words } from "./words"

export interface RosterNotice {
  text: string
  error?: boolean
  open?: W.ProjectAssistantBotKey
}

/**
 * Owns roster toast state and in-chat new-session handoff notifications.
 * Ordinary success notices fade after 5s; errors and actionable "Open Bot"
 * notices stay until dismissed.
 */
export function useRosterFeedback(selected: string | null, w: Words) {
  const [rosterNotice, setRosterNotice] = useState<RosterNotice | null>(null)
  const [newSessionNotice, setNewSessionNotice] = useState<{ id: string; count: number } | null>(null)

  useEffect(() => {
    setNewSessionNotice(notice => notice?.id === selected ? notice : null)
  }, [selected])

  useEffect(() => {
    if (!rosterNotice || rosterNotice.error || rosterNotice.open) return
    const timer = window.setTimeout(() => setRosterNotice(current => current === rosterNotice ? null : current), 5000)
    return () => window.clearTimeout(timer)
  }, [rosterNotice])

  const notifyRosterEdit = (id: string, freshContext?: boolean) => {
    if (freshContext) {
      if (selected === id) {
        setRosterNotice(null)
        setNewSessionNotice(old => ({ id, count: (old?.id === id ? old.count : 0) + 1 }))
      } else {
        setRosterNotice({ text: w.rosterNewSessionDone })
      }
      return
    }
    setRosterNotice({ text: w.rosterUpdated })
  }

  return {
    rosterNotice,
    setRosterNotice,
    newSessionCount: newSessionNotice?.id === selected ? newSessionNotice.count : undefined,
    notifyRosterEdit,
  }
}
