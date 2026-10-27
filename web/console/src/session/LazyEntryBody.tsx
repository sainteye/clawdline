import { useEffect, useRef, useState, type ReactNode } from "react"
import type { TranscriptEntry, TranscriptEntryDetail } from "@clawdline/contract"
import { RefusalError } from "@clawdline/core"
import { ActionSpinner } from "../ui/ActionProgress.js"
import { failureSentence } from "../refusals/refusal-text.js"
import { nextWord } from "../next-strings.js"

export async function readStoredEntry(sessionID: string, reference: string, signal: AbortSignal, patch = false): Promise<TranscriptEntry> {
  const view = `${typeof DecompressionStream === "function" ? "entry-v1z" : "entry-v1"}:${reference}`
  const response = await fetch(`/v1/transcript?session=${encodeURIComponent(sessionID)}&view=${encodeURIComponent(view)}`, { signal, cache: "no-store" })
  const body = await response.json()
  if (!response.ok) throw new RefusalError(response.status, body, "/v1/transcript")
  const changes = body.entry?.fileChanges
  const invalidChanges = changes !== undefined && (!Array.isArray(changes) || changes.some((change: Record<string, unknown>) =>
    !change || typeof change.path !== "string" || typeof change.kind !== "string" ||
    change.unifiedDiff !== undefined && typeof change.unifiedDiff !== "string" ||
    change.content !== undefined && typeof change.content !== "string"))
  if (body.reference !== reference || body.entry?.role !== "tool" || typeof body.entry.text !== "string" || body.entry.detail || invalidChanges || patch && !changes?.length) {
    throw Object.assign(new Error("tool_result_unavailable"), { code: "bad_payload" })
  }
  return body.entry
}

/** The parent keys this component by execution-bound reference, never by preview text. */
export function LazyEntryBody({ sessionID, detail, open, preview = null, children }: {
  sessionID?: string; detail: TranscriptEntryDetail; open: boolean; preview?: ReactNode
  children: (entry: TranscriptEntry) => ReactNode
}) {
  const [entry, setEntry] = useState<TranscriptEntry | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const flight = useRef<AbortController | null>(null)
  const read = async () => {
    if (!sessionID || entry || flight.current) return
    const controller = new AbortController()
    flight.current = controller; setBusy(true); setError("")
    try {
      const answer = await readStoredEntry(sessionID, detail.reference, controller.signal, detail.lineCount > 0)
      if (!controller.signal.aborted) setEntry(answer)
    } catch (error) {
      if (!controller.signal.aborted) setError(failureSentence(error, { sentence: nextWord("toolResultFailed") }))
    } finally {
      if (flight.current === controller) { flight.current = null; setBusy(false) }
    }
  }
  useEffect(() => {
    if (open) void read()
    else { flight.current?.abort(); flight.current = null; setBusy(false) }
    // The reference is also the React key. Closing retains a successfully read body.
  }, [open])
  useEffect(() => () => flight.current?.abort(), [])
  if (!open) return preview
  if (entry) return children(entry)
  return <>{preview}
    {busy && <p className="tool-result-loading" role="status"><ActionSpinner />{nextWord("toolResultLoading")}</p>}
    {error && <p className="tool-result-note" role="alert">{error} <button type="button" onClick={() => void read()}>{nextWord("toolResultRetry")}</button></p>}
  </>
}
