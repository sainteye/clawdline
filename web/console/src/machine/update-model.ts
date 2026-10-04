import type { UpdateStatus } from "@clawdline/contract"
import type { NextWord, nextWord } from "../next-strings.js"

/**
 * What the Settings page's update notice says about `/v1/update`
 * (docs/updates.md), with no DOM and no fetch, so every state can be checked
 * without either.
 */

/** No more often than this does an open console ask again. */
export const UPDATE_READ_EVERY_MS = 10 * 60 * 1000

/**
 * The status a read answered, or null when the answer is not one to speak
 * about. A refusal of any kind is null: a daemon predating the route answers
 * 404, and one predating the Cloud word answers `unknown_command` through the
 * relay. Neither is this machine's fault for the person to read about.
 */
export function settleUpdateRead(ok: boolean, parsed: unknown): UpdateStatus | null {
  if (!ok || !parsed || typeof parsed !== "object") return null
  const status = parsed as Partial<UpdateStatus>
  if (typeof status.state !== "string" || !status.running || !status.latest) return null
  return status as UpdateStatus
}

/** A stamp as the notice prints it: its first eight characters. */
export function shortStamp(stamp: string | undefined): string {
  return (stamp ?? "").slice(0, 8)
}

/**
 * The one line, or null. Only `update_available` and `differs` speak; a
 * machine that is current, ahead, or could not tell says nothing.
 */
export function updateNotice(status: UpdateStatus | null, say: typeof nextWord): string | null {
  if (!status) return null
  let key: NextWord
  if (status.state === "update_available") key = "machineUpdateAvailable"
  else if (status.state === "differs") key = "machineUpdateDiffers"
  else return null
  const running = shortStamp(status.running.stamp)
  const latest = shortStamp(status.latest.stamp)
  if (!running || !latest) return null
  return say(key, { running, latest })
}
