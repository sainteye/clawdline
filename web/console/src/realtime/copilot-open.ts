import { COPILOT_MACHINES, COPILOT_OPEN } from "../overlays/events.js"
import { OPEN_SNIPPETS } from "../legacy/snippets-bridge.js"
import type { SnippetsOutcome } from "../session/snippets-request.js"

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

/**
 * Opens the Session on this page, then its Snippet / Playbook sheet on `tab`,
 * starting `playbook` there when asked (`session/snippets-request.ts`).
 * Resolves with the sheet's outcome, or why it was never reached.
 */
export async function openSnippetsOnThisPage(sessionID: string, request: { tab: "snippet" | "playbook"; playbook?: string; start?: boolean }): Promise<SnippetsOutcome | "not_opened" | "timeout"> {
  if (!(await openOnThisPage(sessionID))) return "not_opened"
  // The opened Session's detail mounts its sheet listener on the next frames.
  await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)))
  return new Promise((resolve) => {
    const timer = setTimeout(() => resolve("timeout"), COPILOT_OPEN_WAIT_MS)
    document.dispatchEvent(new CustomEvent(OPEN_SNIPPETS, { detail: { ...request, answer: (outcome: SnippetsOutcome) => { clearTimeout(timer); resolve(outcome) } } }))
  })
}
