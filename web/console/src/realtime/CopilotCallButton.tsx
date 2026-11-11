import { useEffect, useState } from "react"
import { nextWord } from "../next-strings.js"
import { realtimeCopilot } from "./realtime-copilot.js"
import { copilotDock } from "./copilot-dock.js"

/**
 * The voice copilot's call button beside the Session list's other actions.
 * It only opens the copilot; the sheet and the floating bubble live at the
 * page's root (`CopilotDockView`), so a call outlasts this toolbar.
 */
export function CopilotCallButton() {
  const [active, setActive] = useState(false)
  useEffect(() => realtimeCopilot.subscribe((s) => setActive(s.status === "connected" || s.status === "connecting")), [])
  const label = nextWord(active ? "copilotInCall" : "copilotCall")
  return <button className={`start copilot-call${active ? " in-call" : ""}`} id="copilot-call" type="button"
    title={label} aria-label={label} aria-pressed={active} onClick={() => copilotDock.open()}>
    <svg className="ico" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" focusable="false">
      <path d="M6.62 10.79a15.05 15.05 0 006.59 6.59l2.2-2.2a1 1 0 011.02-.24c1.12.37 2.33.57 3.57.57a1 1 0 011 1V20a1 1 0 01-1 1A17 17 0 013 4a1 1 0 011-1h3.5a1 1 0 011 1c0 1.25.2 2.45.57 3.57a1 1 0 01-.25 1.02l-2.2 2.2z"/>
    </svg>
  </button>
}
