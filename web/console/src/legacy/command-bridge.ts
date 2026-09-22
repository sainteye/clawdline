// The command sheet's transport seam. The original `input/command.js` binds
// DOM before React has drawn it, so session/Command.tsx follows that module
// while this file keeps the copied request and spinner vocabulary.
import type { IntentResult } from "@clawdline/contract"
import { makeJSONFetch } from "@clawdline/core/refusal"
import { T } from "./js/core/i18n.js"
import {
  commandSpin,
  drawSpinner,
  setCommandSpin as setCommandSpinOriginal,
  spinPhase,
} from "./js/core/pixels.js"
import { uuid } from "./js/core/util.js"

const jsonFetch = makeJSONFetch({
  words: {
    offline: (T as Record<string, string>).webOffline,
    requestFailed: (T as Record<string, string>).webRequestFailed,
    notJSON: (T as Record<string, string>).webNotJSON,
  },
  refusalFields: [
    { source: "app", target: "app", type: "string" },
    { source: "reason", target: "reason", type: "string" },
  ],
})

export function planIntent(text: string): Promise<IntentResult> {
  return jsonFetch<IntentResult>("/v1/intents", {
    method: "POST",
    headers: { "Content-Type": "application/json", "Idempotency-Key": uuid() },
    body: JSON.stringify({ text }),
  })
}

export function setCommandSpinner(canvas: HTMLCanvasElement | null): void {
  setCommandSpinOriginal(canvas)
  if (canvas) drawSpinner(commandSpin, spinPhase)
}
