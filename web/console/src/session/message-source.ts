import type { SessionRow, TranscriptEntry } from "@clawdline/contract"
import { detailSessionName } from "../brand-mark.js"

export interface MessageSenderView {
  messageSenders?: ReadonlyMap<string, SessionRow>
  onOpenMessageSender?: (row: SessionRow) => void
  messageSenderNeedsGeneration?: boolean
}

/** Index the current machine's existing Session rows once for the whole transcript. */
export function indexMessageSenders(rows: readonly SessionRow[]): ReadonlyMap<string, SessionRow> {
  const senders = new Map<string, SessionRow>()
  const seen = new Set<string>()
  for (const row of rows) {
    if (!row.assistant || !row.sessionId) continue
    const key = row.assistant + ":" + row.sessionId
    if (seen.has(key)) senders.delete(key)
    else if (!row.source || row.source.freshness === "current") senders.set(key, row)
    seen.add(key)
  }
  return senders
}

export function messageSender(entry: TranscriptEntry, view: MessageSenderView): { label: string; open?: () => void } {
  const row = entry.sourceAssistant && entry.sourceConversation
    ? view.messageSenders?.get(entry.sourceAssistant + ":" + entry.sourceConversation)
    : undefined
  if (!row) return { label: entry.source || "session" }
  const label = detailSessionName(row)
  const open = view.onOpenMessageSender
  if (!open || (view.messageSenderNeedsGeneration && !row.execution_generation)) return { label }
  return { label, open: () => open(row) }
}
