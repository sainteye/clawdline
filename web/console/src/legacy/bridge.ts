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
import { T, applyStrings, fill } from "./js/core/i18n.js"
import { shortPath, tint } from "./js/core/util.js"
import { ASSISTANT_LOGOS, assistantLogo, assistantName, drawIconOnce, setSpinners } from "./js/core/pixels.js"
import { LOCAL_SESSION_MACHINE, machinePresentationForFleet } from "./js/session/selection.js"
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
 * Hand the list's spinner canvases to the clock that already exists.
 *
 * There is one clock for every spinner on the page, deliberately: eight
 * separate timers would drift apart and the list would look like eight machines
 * rather than one. It also stops while the page is hidden and does not replay
 * what it missed, because a spinner has nothing to catch up on.
 *
 * The first version here ran its own animation frame and passed a float where
 * the phase is an index into eight positions, so every draw threw and the
 * canvas kept the 300×150 an unsized canvas defaults to — a rectangle of
 * nothing three rows tall, which is the exact failure the original's comment
 * warns about.
 */
export function registerSpinners(canvases: HTMLCanvasElement[]): void {
  setSpinners(canvases)
}
export const path = shortPath as (cwd: string | undefined) => string
export const accentTint = tint as (hex: string | undefined) => string
export const strings = T as Record<string, string>
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
