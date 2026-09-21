import * as L from "../legacy/bridge.js"
import type { DoorFailure } from "./api.js"

/** The password press in catalogued words, never the daemon's prose. */
export function passwordFailureSentence(error: DoorFailure): string {
  return L.failureSentence(error, {
    sentence: error.code === "unauthorized" ? L.strings.webDoorWrongPassword : "",
    fallback: L.strings.webDoorAskFailed,
  })
}
