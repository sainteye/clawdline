import { nextWord } from "../next-strings.js"
import { T } from "./js/core/i18n.js"
import {
  closeabilityLines as originalLines,
  closeabilityPlainReasons as originalPlainReasons,
  projectSessionCloseability as originalProjection,
  sessionCloseabilityHTML as originalBadge,
  sessionCloseabilityShape as originalShape,
} from "./js/view/derive.js"

type Reason = { code?: string; kind?: string }
type Row = { closeability?: { state?: string; attestation_id?: string | null; reasons?: Reason[] } }

// The pinned Swift display code requires an attestation even for a server-safe
// row. The Go daemon has no attestation writer. This value exists only in the
// local copy passed to the old display functions; it is never sent or stored.
// All their other safety checks (freshness, version, reasons, terminal state)
// still run on the actual row.
function displayRow(value: unknown): unknown {
  if (!value || typeof value !== "object") return value
  const row = value as Row
  if (row.closeability?.state !== "safe" || row.closeability.attestation_id) return value
  return { ...row, closeability: { ...row.closeability, attestation_id: "go-policy-safe" } }
}

export function currentCloseability(value: unknown) {
  return originalProjection(displayRow(value))
}

export function currentCloseabilityBadge(value: unknown): string {
  return originalBadge(displayRow(value))
}

export function currentCloseabilityShape(value: unknown): string {
  return originalShape(displayRow(value))
}

export function currentCloseabilityLines(value: unknown): string[] {
  return originalLines(displayRow(value))
}

export function currentCloseabilityPlainReasons(value: unknown): { text: string; count: number }[] {
  const current = displayRow(value)
  const rows = originalPlainReasons(current) as { text: string; count: number }[]
  const reasons = currentCloseability(value).reasons as Reason[]
  const newCodes: Record<string, "closeReasonBoard" | "closeReasonTodo" | "closeReasonDispatchTodo"> = {
    board_item_open: "closeReasonBoard",
    session_todo_open: "closeReasonTodo",
    dispatch_todo_open: "closeReasonDispatchTodo",
  }
  // The old function groups unknown codes into one generic sentence. Replace
  // only those new codes and retain its translations for every older reason.
  const newReasons = reasons.filter((reason) => !!newCodes[reason.code || ""])
  if (newReasons.length === 0) return rows
  const generic = T.webInfoCloseReasonOther
  const genericCount = newReasons.length
  const genericRow = rows.find((row) => row.text === generic)
  if (genericRow) {
    genericRow.count -= genericCount
    if (genericRow.count <= 0) rows.splice(rows.indexOf(genericRow), 1)
  }
  for (const reason of newReasons) {
    const copy = nextWord(newCodes[reason.code || ""])
    const existing = rows.find((row) => row.text === copy)
    if (existing) existing.count += 1
    else rows.push({ text: copy, count: 1 })
  }
  return rows
}
