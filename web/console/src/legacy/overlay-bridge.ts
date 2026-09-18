// The overlays' part of the bridge: what the info sheet, the confirmation
// sheet and the toast need from the copied modules. Kept as its own file
// because the overlays arrived as a separate piece of work; bridge.ts
// re-exports all of it, and nothing outside legacy/ imports this file or the
// copied modules directly.
import { S } from "./js/core/state.js"
import { failureSentence as failureSentenceOriginal } from "./js/core/failure-text.js"
import { confirmSpin, drawSpinner, setConfirmSpin as setConfirmSpinOriginal, spinPhase } from "./js/core/pixels.js"
import {
  byId as byIdOriginal,
  closeabilityLines as closeabilityLinesOriginal,
  closeabilityPlainReasons as closeabilityPlainReasonsOriginal,
  lostIfClosed as lostIfClosedOriginal,
  owedBadgeHTML as owedBadgeHTMLOriginal,
  projectSessionCloseability,
  projectSessionWorkState,
  selfReportedPeerWaitCopy as selfReportedPeerWaitCopyOriginal,
  sessionCloseabilityHTML,
  sessionStatusGlyphHTML,
  sessionWorkStateHTML,
  suggestedReplyButtonHTML as suggestedReplyButtonHTMLOriginal,
  suggestedReplyKeydown as suggestedReplyKeydownOriginal,
} from "./js/view/derive.js"

/** A session as the copied modules hold it: the wire row, loosely. */
export type LegacySession = Record<string, unknown> & { id: string }

export interface Closeable {
  state: string
  failedClosed: boolean
  reasons: { kind: string; code: string }[]
  block: unknown
}

/** `byId` (`view/derive.js`): the published row for an id, or null when none or more than one. */
export const byId = byIdOriginal as (id: string | null | undefined) => LegacySession | null
export const closeabilityOf = projectSessionCloseability as (s: unknown) => Closeable
export const workStateOf = projectSessionWorkState as (s: unknown) => { state: string }
export const closeabilityLines = closeabilityLinesOriginal as (s: unknown) => string[]
export const closeabilityPlainReasons = closeabilityPlainReasonsOriginal as (s: unknown) => { text: string; count: number }[]
export const closeabilityBadgeHTML = sessionCloseabilityHTML as (s: unknown) => string
export const lostIfClosed = lostIfClosedOriginal as (id: string) => string[]
export const owedBadgeHTML = owedBadgeHTMLOriginal as (s: unknown) => string
export const selfReportedPeerWaitCopy = selfReportedPeerWaitCopyOriginal as (s: unknown) => string
export const statusGlyphHTML = sessionStatusGlyphHTML as (icon: string, copy: string) => string
export const workStateBadgeHTML = sessionWorkStateHTML as (s: unknown) => string
export const suggestedReplyButtonHTML = suggestedReplyButtonHTMLOriginal as (s: unknown, options: Record<string, unknown>) => string
export const suggestedReplyKeydown = suggestedReplyKeydownOriginal as (ev: KeyboardEvent) => void

/** `failureSentence` (`core/failure-text.js`): a failure's sentence and its `code · ref` tag. */
export const failureSentence = failureSentenceOriginal as (
  error: unknown,
  options?: string | { sentence?: string; fallback?: string },
) => string

/**
 * `setConfirmSpin` and the first draw `ActionConfirm.sync` gives it: the
 * confirmation's spinner joins the one clock every other spinner is on.
 */
export function setConfirmSpin(canvas: HTMLCanvasElement | null): void {
  setConfirmSpinOriginal(canvas)
  if (canvas) drawSpinner(confirmSpin, spinPhase)
}

/** The fields of the copied state object the overlays read. */
export const legacyState = S as unknown as {
  openId: string | null
  selectedId: string | null
  agent: unknown
  replyComposerIdentity?: unknown
}
