import type { SessionInfo, SessionRow } from "@clawdline/contract"

/** Unknown activity, old receipts and other conversations never imply idle. */
export function rotationReason(row: SessionRow | null, info: SessionInfo | null, now: number): "size" | "idle" | null {
  if (!row?.sessionId || info?.session.sessionId !== row.sessionId || !info.rotation ||
    !["codex", "claude"].includes(row.assistant ?? "") || row.parent_session_id || row.coordinator || row.machine_scope) return null
  if (typeof info.record?.bytes === "number" && Number.isFinite(info.record?.bytes) && info.record.bytes >= info.rotation.recordBytesLimit) return "size"
  const delivered = row.disposition?.scope === "session" &&
    (row.work_state === "milestone_complete" || row.work_state === "work_complete")
  const receipt = row.disposition?.receiptAt
  const activity = row.activity?.known ? row.activity.at : undefined
  if (row.state !== "idle" || !delivered || !receipt || !activity || receipt > now || activity > now) return null
  return now - Math.max(receipt, activity) >= info.rotation.idleSecondsLimit ? "idle" : null
}
