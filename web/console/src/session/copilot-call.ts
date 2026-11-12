import type { SessionRow } from "@clawdline/contract"
import { realtimeCopilot } from "../realtime/realtime-copilot.js"
import { copilotDock } from "../realtime/copilot-dock.js"
import { assistantLabel } from "./antigravity-ui.js"

/**
 * Calls the voice copilot about one Session: the sheet opens and the call's
 * first words hand that Session over (`originGreetingEvent`). From the menu
 * it runs inside the press; from a hold it runs from a timer, and the lift
 * wakes it (`wakeCopilotCall`).
 */
export function callCopilotAbout(row: SessionRow) {
  copilotDock.open()
  void realtimeCopilot.start({ id: row.id, label: row.label || assistantLabel(row.assistant) || row.id })
}

/** The hold's lift: wakes a call started from its timer (`RealtimeCopilotClient.wake`). */
export function wakeCopilotCall(row: SessionRow | null) {
  realtimeCopilot.wake(row ? { id: row.id, label: row.label || assistantLabel(row.assistant) || row.id } : undefined)
}
