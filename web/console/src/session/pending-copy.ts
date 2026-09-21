import * as L from "../legacy/bridge.js"
import { writeIsOff } from "./outcome.js"

/** A failed send's catalogued sentence and code tag. */
export function pendingFailureSentence(code: string): string {
  return L.failureSentence({ code }, L.strings.sendFailed)
}

/** Whether the composer would still accept the same write. */
export function pendingFailureCanRetry(code: string): boolean {
  return !writeIsOff(code)
}
