import type * as W from "@clawdline/contract"
import { useEffect, useId, useRef, useState } from "react"
import { errorCode } from "./client"
import type { HeldOutcome } from "./view-state"
import type { Words } from "./words"

export type RetryFailure = { code: string, at: number }
export type RetryOutcome =
  | { kind: "admitted", receipt: W.ProjectAssistantReceipt }
  | { kind: "rejected" }
  | { kind: "failed", failure: RetryFailure, editable: boolean }

/** Codes after which the message may still have arrived, so it stays held for another retry. */
const uncertain = new Set(["pa_outcome_unknown", "execution_effect_unknown", "unavailable", "pa_offline", "pa_revoked"])

/** One retry of a held send; it reports what happened instead of swallowing it. */
export async function retryOnce(send: () => Promise<unknown>,
  accepted: (answer: unknown) => W.ProjectAssistantReceipt | "rejected" | null,
  now: () => number = Date.now): Promise<RetryOutcome> {
  try {
    const answer = await send()
    const result = accepted(answer)
    if (result === "rejected") return { kind: "rejected" }
    if (result) return { kind: "admitted", receipt: result }
    return { kind: "failed", failure: { code: "pa_outcome_unknown", at: now() }, editable: false }
  } catch (error) {
    const code = errorCode(error)
    return { kind: "failed", failure: { code, at: now() }, editable: !uncertain.has(code) }
  }
}

type Kind = "again" | "later" | "reload" | "settings" | "edit"
/** Which next step a failed retry calls for; the person sees it beside the reason. */
export function retryFailureKind(code: string): Kind {
  if (code === "pa_outcome_unknown" || code === "execution_effect_unknown") return "again"
  if (code === "unavailable" || code === "pa_offline" || code === "pa_capacity_full" || code === "sandbox_unavailable") return "later"
  if (code === "pa_revoked" || code === "pa_stale_generation" || code === "pa_stale_version" || code === "pa_context_stale" || code === "pa_expired") return "reload"
  if (code === "pa_disabled" || code === "pa_read_only" || code === "pa_scope_denied" || code === "pa_unpaired" || code === "pa_unsupported" || code === "pa_not_found" ||
    code === "cli_not_installed" || code === "adapter_not_found" || code === "provider_version_unsupported" || code.startsWith("auth_")) return "settings"
  return "edit"
}

export function retryFailureText(failure: RetryFailure, w: Words, language: string) {
  const time = new Intl.DateTimeFormat(language, { hour: "2-digit", minute: "2-digit", hourCycle: "h23" }).format(failure.at)
  const kind = retryFailureKind(failure.code)
  const reason = { again: w.retryReasonUnknown, later: w.retryReasonUnreachable, reload: w.retryReasonChanged, settings: w.retryReasonRefused, edit: w.retryReasonNotAccepted }[kind]
  const next = { again: w.retryNextAgain, later: w.retryNextLater, reload: w.retryNextReload, settings: w.retryNextSettings, edit: w.retryNextEdit }[kind]
  return { title: w.retryFailedAt.replace("{time}", time).replace("{reason}", reason), next }
}

/**
 * How a held send may leave this device. The device's journal is its only
 * record; the source keeps a message only after admitting it. `delete`: the
 * source refused it or answered for another attempt (`needsEdit`), so this
 * text was never delivered. `remove`: the outcome is unknown; removing it stops
 * this device from tracking or retrying it, and a message the source did admit
 * still appears in the transcript from the source's own history. Nothing is
 * offered for an accepted send, which clears itself when it appears, or while
 * its request is open.
 */
export type UnsentDiscard = "delete" | "remove"
export function unsentDiscardKind(row: Pick<HeldOutcome, "needsEdit">, state: { accepted: boolean, transporting: boolean }): UnsentDiscard | null {
  if (state.accepted || state.transporting) return null
  return row.needsEdit ? "delete" : "remove"
}

export function UnsentDiscardConfirm({ kind, disabled, onConfirm, onCancel, w }: {
  kind: UnsentDiscard, disabled?: boolean, onConfirm: () => void, onCancel: () => void, w: Words
}) {
  return <div className="pa-unsent-confirm" role="group" aria-label={w.unsentClear}>
    <p>{kind === "delete" ? w.unsentDeleteQuestion : w.unsentRemoveQuestion}</p>
    <button type="button" className="pa-unsent-confirm-action" disabled={disabled} onClick={onConfirm}>{kind === "delete" ? w.unsentDeleteConfirm : w.unsentRemoveConfirm}</button>
    <button type="button" onClick={onCancel}>{w.cancel}</button>
  </div>
}

/** What the status icon opens: the delivery fact, then the plain next step. */
export function UnsentStatusNote({ id, w }: { id: string, w: Words }) {
  return <p id={id} className="pa-unsent-note" role="note"><strong>{w.sendUnconfirmed}</strong>{w.unsentStatusNext}</p>
}

export function UnsentActions({ retrying, accepted, failure, restore, canRestore = true, canRetry, disabled, onRetry, onRestore, discard = null, discardFailed = false, onDiscard, w, language }: {
  retrying: boolean, accepted: boolean, failure?: RetryFailure, restore: boolean, canRestore?: boolean, canRetry: boolean,
  disabled: boolean, onRetry: () => void, onRestore: () => void, discard?: UnsentDiscard | null, discardFailed?: boolean, onDiscard?: () => void, w: Words, language: string
}) {
  const [confirming, setConfirming] = useState(false)
  const [explained, setExplained] = useState(false)
  const row = useRef<HTMLDivElement>(null), noteID = useId()
  // The explanation closes on a second tap of the icon, a tap elsewhere, or Escape.
  useEffect(() => {
    if (!explained) return
    const outside = (e: PointerEvent) => { if (!row.current?.querySelector(".pa-unsent-info-wrap")?.contains(e.target as Node)) setExplained(false) }
    const escape = (e: KeyboardEvent) => { if (e.key === "Escape") setExplained(false) }
    document.addEventListener("pointerdown", outside); document.addEventListener("keydown", escape)
    return () => { document.removeEventListener("pointerdown", outside); document.removeEventListener("keydown", escape) }
  }, [explained])
  const notice = !retrying && !accepted && failure ? retryFailureText(failure, w, language) : null
  const offer = !retrying && !!discard && !!onDiscard
  const unconfirmed = !notice && !retrying && !accepted
  return <div className="pa-unsent-actions" ref={row}>
    {notice
      ? <p className="pa-unsent-failure" role="alert">{notice.title} {notice.next} <small>({failure!.code})</small></p>
      : accepted && <span role="status">{w.sendAccepted}</span>}
    {unconfirmed && <span className="pa-unsent-info-wrap">
      <button type="button" className="pa-unsent-info" aria-label={w.unsentStatusLabel.replace("{status}", w.sendUnconfirmed).replace("{next}", w.unsentStatusNext)} aria-expanded={explained}
        aria-controls={explained ? noteID : undefined} onClick={() => setExplained(open => !open)}><span aria-hidden="true">!</span></button>
    </span>}
    {!accepted && (retrying
      ? <button type="button" disabled aria-busy="true" aria-live="polite"><span className="pa-send-spinner" aria-hidden="true"/>{w.sending}</button>
      : restore
        ? canRestore && <button type="button" disabled={disabled} onClick={onRestore}>{w.restoreUnsentDraft}</button>
        : canRetry && <button type="button" disabled={disabled} onClick={onRetry}>{w.retryUnsentSend}</button>)}
    {offer && !confirming && <button type="button" className="pa-unsent-discard" disabled={disabled} onClick={() => setConfirming(true)}>{w.unsentClear}</button>}
    {unconfirmed && explained && <UnsentStatusNote id={noteID} w={w}/>}
    {offer && confirming && <UnsentDiscardConfirm kind={discard!} disabled={disabled} onConfirm={() => { setConfirming(false); onDiscard!() }} onCancel={() => setConfirming(false)} w={w}/>}
    {discardFailed && <p className="pa-unsent-failure" role="alert">{w.unsentDiscardFailed}</p>}
  </div>
}
