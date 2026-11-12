import type { SessionRow } from "@clawdline/contract"
import { realtimeCopilot } from "../realtime/realtime-copilot.js"
import { copilotDock } from "../realtime/copilot-dock.js"
import { assistantLabel } from "./antigravity-ui.js"

/**
 * Calls the voice copilot about one Session: the sheet opens and the call's
 * first words hand that Session over (`originGreetingEvent`). Called inside
 * the press itself, because iOS only lets audio start from a user activation.
 */
export function callCopilotAbout(row: SessionRow) {
  copilotDock.open()
  void realtimeCopilot.start({ id: row.id, label: row.label || assistantLabel(row.assistant) || row.id })
}
