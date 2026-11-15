import type { SessionRow } from "@clawdline/contract"
import type { CallOrigin } from "../realtime/copilot-tools.js"
import { realtimeCopilot } from "../realtime/realtime-copilot.js"
import { copilotDock } from "../realtime/copilot-dock.js"
import { assistantLabel } from "./antigravity-ui.js"

function originOf(row: SessionRow): CallOrigin {
  const machine = (row as SessionRow & { machine?: unknown }).machine
  return { id: row.id, label: row.label || assistantLabel(row.assistant) || row.id,
    ...(typeof machine === 'string' && machine ? { machine } : {}) }
}

/**
 * Calls the voice copilot about one Session: the sheet opens and the call's
 * single handover follows actual record reads (`readCallHandover`). From the menu
 * it runs inside the press; from a hold it runs from a timer, and the lift
 * wakes it (`wakeCopilotCall`).
 */
export function callCopilotAbout(row: SessionRow) {
  copilotDock.open()
  void realtimeCopilot.start(originOf(row))
}

/** The hold's lift: wakes a call started from its timer (`RealtimeCopilotClient.wake`). */
export function wakeCopilotCall(row: SessionRow | null) {
  realtimeCopilot.wake(row ? originOf(row) : undefined)
}
