import { COPILOT_MACHINES, COPILOT_OPEN } from "../overlays/events.js"

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

export interface CopilotMachine { id: string; name: string; online: boolean; current: boolean }

/** The machines this page can start a Session on; none answered means the console's own machine only. */
export function machinesOnThisPage(): Promise<CopilotMachine[]> {
  return new Promise((resolve) => {
    const timer = setTimeout(() => resolve([]), COPILOT_OPEN_WAIT_MS)
    const detail = { answer: (machines: CopilotMachine[]) => { clearTimeout(timer); resolve(machines) } }
    document.dispatchEvent(new CustomEvent(COPILOT_MACHINES, { detail }))
  })
}
