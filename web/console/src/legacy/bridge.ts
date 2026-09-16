// The copied modules, given types and one way in.
//
// Everything under `js/` is the Swift app's console, copied byte for byte. It
// is copied rather than reimplemented because the instruction was to replicate
// the screen strictly, and the first attempt here did the other thing: it
// invented class names the stylesheet has never heard of, and Chinese the
// catalog does not contain, and got `tint` wrong in both its target colour and
// its amount. Reading the function beats inferring it from a screenshot.
//
// What comes through here is markup and words, not components. `derive.js`
// touches no DOM and returns strings, so React hands what it returns to
// dangerouslySetInnerHTML — the same input produces the same markup as the
// original, and the copied stylesheet then styles it identically.
import type { Icon, SessionRow } from "@clawdline/contract"

/* The imports below are the copied modules. They are plain JavaScript with no
   types, read with allowJs so the compiler infers what it can and checks none
   of it: they are byte-for-byte copies, and a type error found in them would
   have nowhere to be fixed. */

import { S } from "./js/core/state.js"
import { T, applyStrings, fill, words } from "./js/core/i18n.js"
import { hasKeyboard } from "./js/core/env.js"
import { copyCodeBlock, inlineMd, richText } from "./js/view/markdown.js"
import { boardWorkflowRecordHTML, parseBoardWorkflowRecord } from "./js/view/board-workflow-record.js"
import { clockOf, shortPath, tint } from "./js/core/util.js"
import { esc } from "./js/core/esc.js"
import {
  ASSISTANT_LOGOS,
  assistantLogo,
  assistantName,
  drawIconOnce,
  drawSpinner,
  setSpinners,
  spinPhase,
} from "./js/core/pixels.js"
import { LOCAL_SESSION_MACHINE, machinePresentationForFleet, sessionSelectionKey } from "./js/session/selection.js"
import {
  ordered,
  projectSessionCloseability,
  projectSessionWorkState,
  sessionCloseabilityHTML,
  sessionStatusGlyphHTML,
  sessionWorkStateHTML,
} from "./js/view/derive.js"

/**
 * Put the current fleet where the copied modules look for it.
 *
 * They read a module-level object rather than taking arguments, so this is the
 * seam, and it is the only thing that writes to it.
 */
export function publish(sessions: SessionRow[], openId: string | null, filter: string): void {
  // The inferred types of the copied state object come from its initialisers —
  // `sessions: []` reads as never[] — so the seam is widened here rather than
  // by editing a file that is meant to stay identical to its source.
  const state = S as Record<string, unknown>
  // The wire carries no `machine`: a page served by this daemon is looking at
  // this machine, so the local page supplies the identity rather than the
  // daemon repeating it on every row. Checked against the original, whose
  // payload has no such field either and whose rows still read "Mac 電腦 · 這台
  // Mac" — the constant comes from the client there too.
  state.sessions = sessions.map((s) => ({ ...s, machine: LOCAL_SESSION_MACHINE }))
  state.tasks = state.tasks ?? []
  state.arrived = true
  state.openId = openId
  state.selectedId = openId
  state.filter = filter
}

/** The list's own order and filter, so rows are arranged as the original arranges them. */
export function orderedRows(): SessionRow[] {
  return ordered() as SessionRow[]
}

export function workState(row: SessionRow): { state: string } {
  return projectSessionWorkState(row) as { state: string }
}
export function closeability(row: SessionRow): { block?: boolean } {
  return projectSessionCloseability(row) as { block?: boolean }
}
export function workStateHTML(row: SessionRow): string {
  return sessionWorkStateHTML(row) as string
}
export function closeabilityHTML(row: SessionRow): string {
  return sessionCloseabilityHTML(row) as string
}
export function glyphHTML(icon: string, copy: string): string {
  return sessionStatusGlyphHTML(icon, copy) as string
}
export function machineFor(row: SessionRow): { label: string; id: string; kind: string } {
  return machinePresentationForFleet(row, S.sessions, T) as { label: string; id: string; kind: string }
}
/** The row's `data-selection-key`, as `list.js` writes it: machine and session together. */
export function selectionKey(row: SessionRow): string | undefined {
  return (sessionSelectionKey(row) as string | null) ?? undefined
}
export function whoHTML(assistant: string | undefined): string {
  if (!assistant || !(ASSISTANT_LOGOS as Record<string, unknown>)[assistant]) return ""
  return `${assistantLogo(assistant)}<span>${assistantName(assistant)}</span>`
}
export function hasLogo(assistant: string | undefined): boolean {
  return !!assistant && !!(ASSISTANT_LOGOS as Record<string, unknown>)[assistant]
}
export function paintIcon(canvas: HTMLCanvasElement | null, icon: Icon | undefined, cellPx: number): boolean {
  if (!canvas) return false
  return drawIconOnce(canvas, icon, cellPx) as boolean
}
/**
 * Draw a spinner canvas once, now, at the phase the shared clock is on.
 *
 * This is what gives the canvas its size, and it cannot be left to the clock.
 * The clock is `createVisibleInterval`, which arms no timer at all while the
 * page is hidden, and `turnSpinners` returns before drawing anything under
 * reduced motion. In either case a canvas that only the clock would draw keeps
 * the 300×150 an undrawn canvas defaults to, and its row grows from 86 pixels
 * to 219. A background tab is hidden, so that was the page as measured.
 * `list.js` draws once when it fills the row, before it hands the canvas to the
 * clock, and this is the same call.
 *
 * `spinPhase` is the clock's own index into the eight ring positions, so a row
 * drawn between two ticks shows the position the others are in.
 */
export function paintSpinner(canvas: HTMLCanvasElement | null): void {
  if (canvas) drawSpinner(canvas, spinPhase)
}
/**
 * Hand the list's spinner canvases to the clock that already exists.
 *
 * There is one clock for every spinner on the page, deliberately: eight
 * separate timers would drift apart and the list would look like eight machines
 * rather than one. It also stops while the page is hidden and does not replay
 * what it missed, because a spinner has nothing to catch up on. That is why
 * registering a canvas does not size it: see `paintSpinner`.
 */
export function registerSpinners(canvases: HTMLCanvasElement[]): void {
  setSpinners(canvases)
}
export const path = shortPath as (cwd: string | undefined) => string
export const accentTint = tint as (hex: string | undefined) => string
export const strings = T as Record<string, string>
/** Interface copy as HTML: `*emphasis*` and `` `typed` `` only, escaped first. */
export const wordsHTML = words as (s: string) => string
/** Asked of the pointer each time, because a keyboard can be attached while the page is open. */
export const keyboard = hasKeyboard as () => boolean
export const fillString = fill as (s: string, holes: Record<string, unknown>) => string

/**
 * Load the catalog the daemon serves.
 *
 * The same file the Swift app's console reads, so a state has one name across
 * both apps. Failing to load leaves the built-in English in place: a console
 * that refused to start over a missing translation would be worse than one that
 * starts in English.
 */
export async function loadStrings(get: () => Promise<Record<string, string>>): Promise<void> {
  try {
    applyStrings(await get())
  } catch {
    /* built-in English stays */
  }
}

/* The transcript's renderers, copied rather than ported (the child replicating
   the transcript asked for exactly this before its connection dropped). */

/** A message body as the original renders it: markdown into safe HTML. */
export const richTextHTML = richText as (text: string) => string
/** One line of inline markdown as HTML. */
export const inlineMdHTML = inlineMd as (text: string) => string
/** The code-block copy button's action. */
export const copyCode = copyCodeBlock as (text: string) => void
/** A board-workflow record inside a message, if there is one; null otherwise. */
export const parseWorkflowRecord = parseBoardWorkflowRecord as (text: string, role: string) => unknown
export const workflowRecordHTML = boardWorkflowRecordHTML as (record: unknown, options?: Record<string, unknown>) => string

/* Small helpers the transcript uses, from the copied modules. */
export const escapeHTML = esc as (s: unknown) => string
export const clock = clockOf as (unix: number) => string
export const assistantDisplayName = assistantName as (kind: string | undefined) => string
export const assistantLogoHTML = (kind: string | undefined): string =>
  kind && (ASSISTANT_LOGOS as Record<string, unknown>)[kind] ? (assistantLogo(kind) as string) : ""
/** The reader's own preference for drawing assistant marks, as the original stores it. */
export const assistantIconsOn = (): boolean => !!(S as Record<string, unknown>).assistantIcons
