import { compactFailureText, type CompactStatus } from "./compact-status"
import type { Words } from "./words"

/**
 * The row above the composer that answers a "Compact now" press: compacting
 * with a spinner, the summarized sizes, or a plain reason with Try again.
 */
export function CompactStatusRow({status, w, onRetry, onDismiss}: {
  status: CompactStatus
  w: Words
  onRetry: () => void
  onDismiss: () => void
}) {
  const busy = status.state === "sending" || status.state === "running"
  return <div className="pa-compact-status" data-state={busy ? "running" : status.state}
    role={status.state === "failed" ? "alert" : "status"} aria-live="polite" aria-busy={busy}>
    {busy && <span className="pa-send-spinner" aria-hidden="true"/>}
    <p>
      {busy ? <strong>{w.compactRunning}</strong>
        : status.state === "done" ? <><strong>{w.compactDone}</strong> {status.text}</>
        : <><strong>{w.compactFailed}</strong> {compactFailureText(status.code, w)} <small>({status.code})</small></>}
    </p>
    {status.state === "failed" && <button type="button" onClick={onRetry}>{w.compactRetry}</button>}
    {!busy && <button type="button" className="pa-compact-dismiss" aria-label={w.compactDismiss} onClick={onDismiss}>×</button>}
  </div>
}
