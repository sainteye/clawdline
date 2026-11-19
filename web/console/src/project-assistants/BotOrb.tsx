import { useEffect, useRef, useState } from "react"
import { BotAvatar } from "./BotAvatar"
import { words } from "./words"
import type { BotMood } from "./working"

export interface BotOrbProps {
  onOpen: (opener?: HTMLElement) => void
  language?: string
  unread?: number
  pendingPermissions?: number
  mood?: BotMood
  disabled?: boolean
}

export function BotOrb({ onOpen, language, unread = 0, pendingPermissions = 0, mood = "idle", disabled }: BotOrbProps) {
  const w = words(language), count = unread + pendingPermissions
  const last = useRef(count), [ringing, setRinging] = useState(false)
  useEffect(() => {
    const rose = count > last.current
    last.current = count
    if (!rose) { setRinging(false); return }
    setRinging(true)
    const timer = window.setTimeout(() => setRinging(false), 1200)
    return () => clearTimeout(timer)
  }, [count])
  const label = `${w.allBots}${count ? `, ${w.pending}: ${pendingPermissions}, ${w.unread}: ${unread}` : ""}`

  return <span className="project-assistants pa-entry pa-dock-entry">
    <button type="button" className="pa-dock" disabled={disabled} data-ringing={ringing || undefined}
      aria-label={label} title={w.allBots} onClick={event => onOpen(event.currentTarget)}>
      <BotAvatar appearance={{ shape: "blob", color: "#e39567" }} size={20} mood={mood} />
      {count > 0 && <span key={count} className="pa-dock-badge" aria-hidden="true">{count}</span>}
    </button>
  </span>
}
