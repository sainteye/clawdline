import { useEffect, useRef, useState } from "react"
import type { TranscriptToolResult } from "@clawdline/contract"
import { RefusalError } from "@clawdline/core"
import { failureParts, failureSentence } from "../refusals/refusal-text.js"
import { ActionProgress, ActionSpinner } from "../ui/ActionProgress.js"
import { nextWord } from "../next-strings.js"

interface Detail { text: string; truncated: boolean; sourceTruncated: boolean }

/** Stored-byte refusals are terminal; transport failures can still be retried. */
export function storedImageFailure(error: unknown) {
  const unavailable = error instanceof RefusalError && error.code === "tool_image_unavailable" && (error.status === 404 || error.status === 422)
  const detail = failureParts(error, { sentence: nextWord("toolResultImageUnavailable") })
  return { ...detail, text: unavailable ? nextWord("toolResultImageNotRetained") : detail.text, unavailable }
}

function StoredResult({ sessionID, result, representation }: {
  sessionID: string; result: TranscriptToolResult; representation: "text" | "raw"
}) {
  const [detail, setDetail] = useState<Detail | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const flight = useRef<AbortController | null>(null)
  useEffect(() => () => flight.current?.abort(), [])
  const read = async () => {
    if (flight.current || detail) return
    const controller = new AbortController()
    flight.current = controller
    setBusy(true); setError("")
    try {
      const view = `${typeof DecompressionStream === "function" ? "tool-v1z" : "tool-v1"}:${result.reference}:${representation}`
      const response = await fetch(`/v1/transcript?session=${encodeURIComponent(sessionID)}&view=${encodeURIComponent(view)}`, { signal: controller.signal, cache: "no-store" })
      const body = await response.json()
      if (!response.ok) throw new RefusalError(response.status, body, "/v1/transcript")
      if (body.reference !== result.reference || body.representation !== representation || typeof body.text !== "string") throw Object.assign(new Error("tool_result_unavailable"), {code:"bad_payload"})
      if (!controller.signal.aborted) setDetail(body)
    } catch (error) { if (!controller.signal.aborted) setError(failureSentence(error, {sentence:nextWord("toolResultFailed")})) }
    finally { if (!controller.signal.aborted) setBusy(false); if (flight.current === controller) flight.current = null }
  }
  return <details className="tool-result-details" aria-busy={busy} onToggle={event => { if (event.currentTarget.open) void read() }}>
    <summary>{nextWord(representation === "raw" ? "toolResultRaw" : "toolResultExpand")}</summary>
    {busy && <p className="tool-result-loading" role="status"><ActionSpinner />{nextWord("toolResultLoading")}</p>}
    {error && <p role="alert">{error} <button type="button" onClick={() => void read()}>{nextWord("toolResultRetry")}</button></p>}
    {detail && <>{(detail.truncated || detail.sourceTruncated) && <p className="tool-result-note">{nextWord(detail.truncated ? "toolResultBudget" : "toolResultSourceClipped")}</p>}<pre className="activity-result">{detail.text}</pre></>}
  </details>
}

function StoredImage({ sessionID, result }: { sessionID: string; result: TranscriptToolResult }) {
  const [index, setIndex] = useState(0)
  const [picture, setPicture] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [failed, setFailed] = useState<{ text: string; tag: string; unavailable: boolean } | null>(null)
  const flight = useRef<AbortController | null>(null)
  useEffect(() => () => flight.current?.abort(), [])
  const read = async () => {
    if (flight.current) return
    const controller = new AbortController()
    flight.current = controller; setBusy(true); setFailed(null)
    try {
      const representation = `image-${index}-original`
      const view = `tool-v1:${result.reference}:${representation}`
      const response = await fetch(`/v1/transcript?session=${encodeURIComponent(sessionID)}&view=${encodeURIComponent(view)}`, { signal: controller.signal, cache: "no-store" })
      const body = await response.json()
      if (!response.ok) throw new RefusalError(response.status, body, "/v1/transcript")
      if (body.reference !== result.reference || body.representation !== representation ||
        typeof body.dataURL !== "string" || !/^data:image\/(jpeg|png|gif);base64,/u.test(body.dataURL)) throw Object.assign(new Error("tool_image_unavailable"), {code:"bad_payload"})
      const decoded = new Image()
      decoded.src = body.dataURL
      await decoded.decode()
      if (!controller.signal.aborted) setPicture(body.dataURL)
    } catch (error) {
      if (!controller.signal.aborted) {
        setFailed(storedImageFailure(error))
      }
    }
    finally { if (!controller.signal.aborted) setBusy(false); if (flight.current === controller) flight.current = null }
  }
  const move = (delta: number) => { if (busy) return; setIndex(index + delta); setPicture(null); setFailed(null) }
  return <div className="tool-result-image" aria-busy={busy}>
    {result.images > 1 && <div className="tool-result-image-nav">
      <button type="button" disabled={busy || index === 0} onClick={() => move(-1)}>{nextWord("toolResultPrevious")}</button>
      <span>{index + 1} / {result.images}</span>
      <button type="button" disabled={busy || index + 1 >= result.images} onClick={() => move(1)}>{nextWord("toolResultNext")}</button>
    </div>}
    {!picture && !failed?.unavailable && <button className="tool-result-read" type="button" disabled={busy} onClick={() => void read()}>
      <ActionProgress busy={busy} label={<span role="status">{nextWord("toolResultLoading")}</span>}>{nextWord(failed ? "toolResultRetry" : "toolResultOriginal")}</ActionProgress>
    </button>}
    {failed && <div className="tool-result-image-error" role={failed.unavailable ? "status" : "alert"}>
      <p>{failed.text}</p>
      <details className="tool-result-details"><summary>{nextWord("toolResultErrorDetails")}</summary><p>{failed.tag}</p></details>
    </div>}
    {picture && <img src={picture} alt={nextWord("toolResultImageAlt")} />}
  </div>
}

/** Short output stays inline; only deliberate expansion reads stored detail. */
export function ToolResult({ sessionID, result }: { sessionID?: string; result: TranscriptToolResult }) {
  const long = result.textBytes > new TextEncoder().encode(result.preview).length
  return <div className="tool-result">
    {result.preview && <pre className="activity-result tool-result-preview">{result.preview}{long ? "…" : ""}</pre>}
    <div className="tool-result-body">
      {(result.images > 0 || result.unknownBlocks > 0) && <p className="tool-result-note">
        {result.images > 0 && <span>{nextWord("toolResultImages", { count: result.images })}</span>}
        {result.unknownBlocks > 0 && <span>{nextWord("toolResultOther", { count: result.unknownBlocks })}</span>}
      </p>}
      {result.sourceTruncated && <p className="tool-result-note">{nextWord("toolResultSourceClipped")}</p>}
      {sessionID && result.images > 0 && <StoredImage sessionID={sessionID} result={result} />}
      {sessionID && long && <StoredResult sessionID={sessionID} result={result} representation="text" />}
      {sessionID && <StoredResult sessionID={sessionID} result={result} representation="raw" />}
    </div>
  </div>
}
