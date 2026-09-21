import type { Screen } from "@clawdline/contract"
import type { NextWord } from "../next-strings.js"
import type { ReadState } from "../read-state.js"

/** The next action for the screen's four materially different failure paths. */
export function screenFailureWord(
  reading: Extract<ReadState<Screen | null>, { phase: "refused" | "unanswered" }>,
): NextWord | null {
  if (reading.phase === "unanswered") return "screenUnanswered"
  const failure = reading.error as { code?: unknown } | null | undefined
  const code = typeof failure?.code === "string" ? failure.code : ""
  if (code === "session_not_found" || code === "not_found") return "screenSessionGone"
  if (code === "forbidden") return "screenForbidden"
  if (code === "cloud_not_carried") return "screenCloudNotCarried"
  return null
}
