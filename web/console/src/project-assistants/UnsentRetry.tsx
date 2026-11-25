import type * as W from "@clawdline/contract"
import { useEffect, useId, useRef, useState } from "react"
import { errorCode, sendBlockedByInterruptedTurn } from "./client"
import type { HeldOutcome } from "./view-state"
import type { Words } from "./words"

export type RetryFailure = { code: string, at: number, interrupted?: true, initial?: true }
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
    if (sendBlockedByInterruptedTurn(error)) return { kind: "failed", failure: { code, at: now(), interrupted: true }, editable: false }
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

/**
 * Whether pressing Retry again can change the answer. A turn awaiting review,
 * a Bot that refuses messages, or a message the source would not accept
 * fails the same way every time; those rows offer Clear (and Edit when the
 * text can go back to the composer), never Retry.
 */
export function retryAllowed(failure: RetryFailure | undefined): boolean {
  if (!failure) return true
  if (failure.interrupted) return false
  const kind = retryFailureKind(failure.code)
  return kind === "again" || kind === "later" || kind === "reload"
}

export function retryFailureText(failure: RetryFailure, w: Words, language: string) {
  const time = new Intl.DateTimeFormat(language, { hour: "2-digit", minute: "2-digit", hourCycle: "h23" }).format(failure.at)
  const title = (reason: string) => failure.initial ? reason : w.retryFailedAt.replace("{time}", time).replace("{reason}", reason)
  if (failure.interrupted) {
    return { title: title(w.sendInterrupted), next: "" }
  }
  const kind = retryFailureKind(failure.code)
  const reason = { again: w.retryReasonUnknown, later: failure.initial ? w.retryReasonRefused : w.retryReasonUnreachable, reload: w.retryReasonChanged, settings: w.retryReasonRefused, edit: w.retryReasonNotAccepted }[kind]
  const next = { again: w.retryNextAgain, later: w.retryNextLater, reload: w.retryNextReload, settings: w.retryNextSettings, edit: w.retryNextEdit }[kind]
  return { title: title(reason), next }
}

/**
 * How a held send may leave this device. The device's journal is its only
 * record; the source keeps a message only after admitting it. `delete`: the
 * source refused it or answered for another attempt (`needsEdit`), so this
 * text was never delivered. `remove`: the outcome is unknown; removing it stops
 * this device from tracking or retrying it, and a message the source did admit
 * still appears in the transcript from the source's own history. Either way
 * one press of Clear does it. An admitted message may be outside the loaded
 * history, so Dismiss hides its local status. An open transport cannot be cleared.
 */
export type UnsentDiscard = "delete" | "remove" | "hide"
export function unsentDiscardKind(row: Pick<HeldOutcome, "needsEdit">, state: { accepted: boolean, transporting: boolean }): UnsentDiscard | null {
  if (state.transporting) return null
  if (state.accepted) return "hide"
  return row.needsEdit ? "delete" : "remove"
}

/** What the status icon opens: the delivery fact, then the plain next step. */
export function UnsentStatusNote({ id, w }: { id: string, w: Words }) {
  return <p id={id} className="pa-unsent-note" role="note"><strong>{w.sendUnconfirmed}</strong>{w.unsentStatusNext}</p>
}

export function UnsentActions({ retrying, accepted, failure, restore, canRestore = true, canRetry, disabled, onRetry, onRestore, discard = null, discardFailed = false, onDiscard, w, language }: {
  retrying?: boolean, accepted: boolean, failure?: RetryFailure, restore?: boolean, canRestore?: boolean, canRetry?: boolean,
  disabled?: boolean, onRetry?: () => void, onRestore?: () => void, discard?: UnsentDiscard | null, discardFailed?: boolean, onDiscard?: () => void, w: Words, language: string
}) {
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
        : canRetry && retryAllowed(failure) && <button type="button" disabled={disabled} onClick={onRetry}>{w.retryUnsentSend}</button>)}
    {offer && <button type="button" className="pa-unsent-discard" disabled={disabled} onClick={onDiscard}>{discard === "hide" ? w.dismissHeldNote : w.unsentClear}</button>}
    {unconfirmed && explained && <UnsentStatusNote id={noteID} w={w}/>}
    {discardFailed && <p className="pa-unsent-failure" role="alert">{w.unsentDiscardFailed}</p>}
  </div>
}
