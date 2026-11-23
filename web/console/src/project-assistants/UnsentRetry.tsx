import type * as W from "@clawdline/contract"
import { errorCode } from "./client"
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

export function UnsentActions({ retrying, accepted, failure, restore, canRestore = true, canRetry, disabled, onRetry, onRestore, w, language }: {
  retrying: boolean, accepted: boolean, failure?: RetryFailure, restore: boolean, canRestore?: boolean, canRetry: boolean,
  disabled: boolean, onRetry: () => void, onRestore: () => void, w: Words, language: string
}) {
  const notice = !retrying && !accepted && failure ? retryFailureText(failure, w, language) : null
  return <div className="pa-unsent-actions">
    {notice
      ? <p className="pa-unsent-failure" role="alert">{notice.title} {notice.next} <small>({failure!.code})</small></p>
      : !retrying && <span role="status">{accepted ? w.sendAccepted : w.sendUnconfirmed}</span>}
    {!accepted && (retrying
      ? <button type="button" disabled aria-busy="true" aria-live="polite"><span className="pa-send-spinner" aria-hidden="true"/>{w.sending}</button>
      : restore
        ? canRestore && <button type="button" disabled={disabled} onClick={onRestore}>{w.restoreUnsentDraft}</button>
        : canRetry && <button type="button" disabled={disabled} onClick={onRetry}>{w.retryUnsentSend}</button>)}
  </div>
}
