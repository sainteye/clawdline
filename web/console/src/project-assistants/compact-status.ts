import type * as W from "@clawdline/contract"
// @ts-expect-error Node's strip-types runner loads source extensions.
import { compactedNoticeText } from "./notice-text.ts"
import type { Words } from "./words"

/**
 * What the chat shows for one "Compact now" press. Sending covers the
 * request; running is the compaction turn the receipt named, until that turn
 * leaves its notice (done) or ends without one (failed).
 */
export type CompactStatus =
  | {state: "sending"}
  | {state: "running", turnID: string}
  | {state: "done", turnID: string, text: string}
  | {state: "failed", code: string}

/** Follows the tracked compaction turn through the chat's current rows. */
export function followCompaction(status: CompactStatus | null, turns: readonly Pick<W.ProjectAssistantReadTurnSummary, "id" | "state" | "failure_code" | "failure_field">[],
  messages: readonly Pick<W.ProjectAssistantMessage, "id" | "kind" | "text" | "notice">[], w: Words): CompactStatus | null {
  if (status?.state !== "running") return status
  const notice = messages.find(m => m.id === "compacted-" + status.turnID)
  if (notice) return {state: "done", turnID: status.turnID, text: compactedNoticeText(notice, w) ?? w.compactedNotice}
  const turn = turns.find(t => t.id === status.turnID)
  if (turn && ["failed", "cancelled", "unknown"].includes(turn.state)) return {state: "failed", code: turn.failure_field || turn.failure_code || turn.state}
  if (turn?.state === "succeeded") return {state: "done", turnID: status.turnID, text: w.compactedNotice}
  return status
}

/** A plain reason and next step for a refused or failed compaction. */
export function compactFailureText(code: string, w: Words): string {
  switch (code) {
  case "compact_busy": case "pa_capacity_full": return w.compactFailBusy
  case "compact_no_session": return w.compactFailNoSession
  case "compact_agent": return w.compactFailAgent
  case "compaction_not_reported": return w.compactFailNotReported
  case "pa_read_only": case "pa_scope_denied": case "pa_revoked": case "pa_unpaired": return w.compactFailReadOnly
  case "pa_offline": case "pa_outcome_unknown": case "cloud_not_carried": return w.compactFailOffline
  case "cancelled": return w.compactFailCancelled
  default: return w.compactFailOther
  }
}

/** The most specific code a refusal carries: a compaction's own field, else its code. */
export function compactRefusalCode(error: unknown): string {
  const value = error as Partial<W.ProjectAssistantErrorResponse> | null
  const field = value?.error?.field ?? ""
  return /^compact(ion)?_/.test(field) ? field : value?.error?.code ?? "unavailable"
}
