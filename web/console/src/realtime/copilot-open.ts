import { COPILOT_DEVSTACK, COPILOT_FLEET, COPILOT_MACHINES, COPILOT_OPEN } from "../overlays/events.js"
import type { CopilotFleetRow } from "./copilot-tools.js"
import { OPEN_SNIPPETS } from "../legacy/snippets-bridge.js"
import type { SnippetsOutcome } from "../session/snippets-request.js"

/** What a page answers when the copilot asks it to open a Session. */
export interface CopilotOpenDetail {
  sessionID: string
  /** The Session's machine, from every machine's list; the reading machine when absent. */
  machineID?: string
  /** Set by the page that opened it; left unset when no page handles the ask. */
  answer?: (opened: boolean) => void
}

/** How long the copilot waits for the page to say it opened the Session. */
export const COPILOT_OPEN_WAIT_MS = 8000

/** Asks this page to open the Session; true once it is open on screen. */
export function openOnThisPage(sessionID: string, machineID?: string): Promise<boolean> {
  return new Promise((resolve) => {
    const timer = setTimeout(() => resolve(false), COPILOT_OPEN_WAIT_MS)
    const detail: CopilotOpenDetail = { sessionID, ...(machineID ? { machineID } : {}), answer: (opened) => { clearTimeout(timer); resolve(opened) } }
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
export async function openSnippetsOnThisPage(sessionID: string, request: { tab: "snippet" | "playbook"; playbook?: string; start?: boolean }, machineID?: string): Promise<SnippetsOutcome | "not_opened" | "timeout"> {
  if (!(await openOnThisPage(sessionID, machineID))) return "not_opened"
  // The opened Session's detail mounts its sheet listener on the next frames.
  await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)))
  return new Promise((resolve) => {
    const timer = setTimeout(() => resolve("timeout"), COPILOT_OPEN_WAIT_MS)
    document.dispatchEvent(new CustomEvent(OPEN_SNIPPETS, { detail: { ...request, answer: (outcome: SnippetsOutcome) => { clearTimeout(timer); resolve(outcome) } } }))
  })
}

/**
 * Every machine's Session rows, when this page shows all machines (CloudGate
 * answers `COPILOT_FLEET` only there); null on a page that shows one.
 */
export function fleetOnThisPage(): Promise<CopilotFleetRow[] | null> {
  return new Promise((resolve) => {
    const timer = setTimeout(() => resolve(null), COPILOT_OPEN_WAIT_MS)
    const detail: { answer?: (rows: CopilotFleetRow[]) => void } = { answer: (rows) => { clearTimeout(timer); resolve(Array.isArray(rows) ? rows : null) } }
    document.dispatchEvent(new CustomEvent(COPILOT_FLEET, { detail }))
    // A page that does not answer at once is not showing every machine.
    queueMicrotask(() => { if (detail.answer) { clearTimeout(timer); resolve(null) } })
  })
}

/**
 * Opens one Project's Dev Stack links in a modal over the page the person is
 * on (`DevStackModal`, held at the page's root), without leaving that page.
 */
export function openDevStackOnThisPage(place: { id: string; label: string }): Promise<boolean> {
  return new Promise((resolve) => {
    const detail: { place: { id: string; label: string }; answer?: (opened: boolean) => void } = { place, answer: (opened) => resolve(opened) }
    document.dispatchEvent(new CustomEvent(COPILOT_DEVSTACK, { detail }))
    if (detail.answer) resolve(false)
  })
}
