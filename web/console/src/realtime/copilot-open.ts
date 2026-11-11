import { COPILOT_OPEN } from "../overlays/events.js"

/** What a page answers when the copilot asks it to open a Session. */
export interface CopilotOpenDetail {
  sessionID: string
  /** Set by the page that opened it; left unset when no page handles the ask. */
  answer?: (opened: boolean) => void
}

/** How long the copilot waits for the page to say it opened the Session. */
export const COPILOT_OPEN_WAIT_MS = 8000

/** Asks this page to open the Session; true once it is open on screen. */
export function openOnThisPage(sessionID: string): Promise<boolean> {
  return new Promise((resolve) => {
    const timer = setTimeout(() => resolve(false), COPILOT_OPEN_WAIT_MS)
    const detail: CopilotOpenDetail = { sessionID, answer: (opened) => { clearTimeout(timer); resolve(opened) } }
    document.dispatchEvent(new CustomEvent<CopilotOpenDetail>(COPILOT_OPEN, { detail }))
  })
}
