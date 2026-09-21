import { RefusalError } from "@clawdline/core"
import * as L from "../../../legacy/bridge.js"

/** A settings failure in screen-owned words; local shell prose remains local. */
export function settingsFailureSentence(error: unknown): string {
  if (error instanceof RefusalError) return L.failureSentence(error, L.strings.webRequestFailed)
  if (error instanceof Error) return error.message
  return String(error)
}
