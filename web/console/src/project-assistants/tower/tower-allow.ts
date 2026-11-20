import type * as W from "@clawdline/contract"
import type { SourceClient } from "../client"
// @ts-expect-error -- Node's strip-types runner uses source extensions.
import { authorized, command, errorCode, failure, provablyNotSent } from "../client.ts"
// @ts-expect-error -- Node's strip-types runner uses source extensions.
import { readView, type ReadScope } from "../read-views.ts"
// @ts-expect-error -- Node's strip-types runner uses source extensions.
import { canWrite, permissionState, sameFence, samePermission } from "../view-state.ts"

/** Why the tower cannot allow, or did not finish allowing. Each one is shown as
 * it is; none of them sends again. */
export type AllowReason =
  | "none"      // Nothing is waiting any more.
  | "expired"   // The offer's deadline passed.
  | "answered"  // Someone answered it, here or on another device.
  | "changed"   // A different turn or offer is current: read it again in the chat.
  | "revoked"   // This page lost its authorization for the source.
  | "read_only" // The source is readable but this page may not answer.
  | "offline"   // The source could not be reached; nothing was sent.
  | "unknown"   // The answer was sent and its outcome is unknown.
  | "failed"    // The source refused the answer.
  | "invalid"   // The source sent something this page cannot read.

export class AllowError extends Error {
  constructor(readonly reason: AllowReason, readonly code = "") { super(reason); this.name = "AllowError" }
}

/** One waiting offer, as read for the tower's card. */
export interface Waiting {
  scope: ReadScope
  epoch: string
  summary: W.ProjectAssistantReadPermissionSummary
  offer: W.ProjectAssistantPermissionOffer
  /** The option "Allow once" sends, when the offer has one. A question, or an
   * offer with only persistent choices, has none: it is answered in the chat. */
  once?: W.ProjectAssistantPermissionOption
  /** Other offers waiting on the same Bot, answered in the chat. */
  more: number
  writable: boolean
}

type Read = (q: W.ProjectAssistantReadViewRequest, signal: AbortSignal) => Promise<W.ProjectAssistantReadViewResponse>

function scopeOf(item: W.ProjectAssistantBotSummary): ReadScope {
  return { key: item.bot.key, project: item.bot.project, conversation_id: item.bot.conversation_id }
}

function reader(source: SourceClient, current: () => readonly SourceClient[], scope: ReadScope): Read {
  return async (q, signal) => {
    const answer = await authorized(source, current, s => typeof source.view === "function" ? source.view(q, s) : Promise.reject(failure("pa_unsupported")), signal)
    return readView(answer, q, scope)
  }
}

/** A read or preflight failure: nothing was sent. */
function readReason(error: unknown): AllowError {
  if (error instanceof AllowError) return error
  if ((error as Error)?.name === "AbortError") throw error
  const code = errorCode(error)
  switch (code) {
    case "pa_expired": return new AllowError("expired", code)
    case "pa_revoked": case "pa_unpaired": case "pa_scope_denied": return new AllowError("revoked", code)
    case "pa_read_only": return new AllowError("read_only", code)
    case "pa_stale_generation": case "pa_cursor_expired": case "pa_not_found": return new AllowError("changed", code)
    case "pa_invalid": return new AllowError("invalid", code)
    default: return new AllowError("offline", code)
  }
}

const request = (scope: ReadScope, mode: W.ProjectAssistantReadViewMode, epoch = "", cursor = "", page_size = 100): W.ProjectAssistantReadViewRequest =>
  ({ key: scope.key, conversation_id: scope.conversation_id, mode, source_epoch: epoch, cursor, after_seq: 0, page_size })

const current = (p: Pick<W.ProjectAssistantReadPermissionSummary, "turn_id" | "fence">, head: W.ProjectAssistantReadViewHead) =>
  !!head.conversation.active_turn_id && p.turn_id === head.conversation.active_turn_id && sameFence(p.fence, head.current_fence)

/** Read the first offer waiting on a Bot, in full: snapshot, the permission
 * list, then that offer's detail, as the chat reads it. */
export async function readWaiting(source: SourceClient, live: () => readonly SourceClient[], item: W.ProjectAssistantBotSummary, signal: AbortSignal, now = Date.now): Promise<Waiting> {
  const scope = scopeOf(item), read = reader(source, live, scope)
  try {
    const snap = await read(request(scope, "snapshot", "", "", 1), signal)
    const epoch = snap.source_epoch
    if (!snap.head.pending_permission_count) throw new AllowError("none")
    const list = await read(request(scope, "permissions", epoch), signal)
    const pending = list.items.map(r => r.permission_summary!).filter(p => current(p, list.head) && permissionState(p, now()) === "pending")
    if (!pending.length) throw new AllowError("none")
    const summary = pending[0]
    const page = await read(request(scope, "detail", epoch, summary.detail_cursor, 1), signal)
    const offer = page.items[0]?.permission
    if (!offer || !samePermission(offer, summary)) throw new AllowError("changed")
    const state = permissionState(offer, now())
    if (state === "expired") throw new AllowError("expired")
    if (state !== "pending") throw new AllowError("answered")
    return { scope, epoch, summary, offer, once: offer.options.find(o => o.kind === "allow_once"), more: pending.length - 1, writable: canWrite(page.source) }
  } catch (error) { throw readReason(error) }
}

/** Allow once: read the offer again, check the source may write, the fence,
 * the turn, the deadline and the option, and only then send. One call sends at
 * most once; a failure after sending is "unknown", never retried here. */
export async function allowOnce(source: SourceClient, live: () => readonly SourceClient[], waiting: Waiting, signal: AbortSignal, now = Date.now): Promise<W.ProjectAssistantReceipt> {
  const { scope, offer, once } = waiting
  if (!once) throw new AllowError("changed")
  let intent: W.ProjectAssistantPermissionAnswerRequest
  try {
    if (permissionState(offer, now()) === "expired") throw new AllowError("expired")
    const page = await reader(source, live, scope)(request(scope, "detail", waiting.epoch, waiting.summary.detail_cursor, 1), signal)
    const fresh = page.items[0]?.permission
    if (!canWrite(page.source)) throw new AllowError(page.source.state === "revoked" ? "revoked" : page.source.fresh && page.source.state === "online" ? "read_only" : "offline")
    if (!fresh || !samePermission(fresh, offer) || !sameFence(page.head.current_fence, offer.fence)) throw new AllowError("changed")
    const state = permissionState(fresh, now())
    if (state === "expired") throw new AllowError("expired")
    if (state !== "pending") throw new AllowError("answered")
    if (!current(fresh, page.head)) throw new AllowError("changed")
    if (!fresh.options.some(o => o.id === once.id && o.kind === "allow_once")) throw new AllowError("changed")
    intent = { command: command(scope.key, "answer"), fence: fresh.fence, offer_id: fresh.id, turn_id: fresh.turn_id, option_id: once.id }
  } catch (error) { throw readReason(error) }
  let answer: W.ProjectAssistantReceiptResponse
  try {
    answer = await authorized(source, live, s => source.answer(intent, s), signal)
  } catch (error) {
    const code = errorCode(error)
    if (provablyNotSent(error)) throw readReason(error)
    // The source's own refusals before admission say what happened.
    if (code === "pa_expired") throw new AllowError("expired", code)
    if (code === "pa_stale_generation" || code === "pa_not_found") throw new AllowError("changed", code)
    if (code === "pa_read_only") throw new AllowError("read_only", code)
    if (code === "pa_scope_denied" || code === "pa_invalid") throw new AllowError("failed", code)
    throw new AllowError("unknown", (error as Error)?.name === "AbortError" ? "aborted" : code)
  }
  const receipt = answer?.receipt
  if (!receipt || receipt.command?.idempotency_key !== intent.command.idempotency_key) throw new AllowError("unknown", "pa_outcome_unknown")
  if (receipt.state === "failed") throw new AllowError("failed")
  if (receipt.state === "unknown") throw new AllowError("unknown", "pa_outcome_unknown")
  return receipt
}
