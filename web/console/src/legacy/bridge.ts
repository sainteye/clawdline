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
import type { Icon, SessionRow, TaskRow } from "@clawdline/contract"

/* The imports below are the copied modules. They are plain JavaScript with no
   types, read with allowJs so the compiler infers what it can and checks none
   of it: they are byte-for-byte copies, and a type error found in them would
   have nowhere to be fixed. */

import { S } from "./js/core/state.js"
import { T, applyStrings, fill, words } from "./js/core/i18n.js"
import { atMac as atMacOriginal, hasKeyboard } from "./js/core/env.js"
import { generatedMark as generatedMarkOriginal, markForSession as markForSessionOriginal, projectLabel as projectLabelOriginal } from "./js/view/project-mark.js"
import { copyForUserMessages } from "./js/view/user-messages-data.js"
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
  setOptimisticSpinners,
  setSpinners,
  spinPhase,
} from "./js/core/pixels.js"
import { LOCAL_SESSION_MACHINE, machinePresentationForFleet, sessionSelectionKey } from "./js/session/selection.js"
import { bindSessionUI as bindSessionUIOriginal } from "./js/session/ui.js"
import {
  featureRootChip as featureRootChipOriginal,
  projectSessionCloseability,
  projectSessionWorkState,
  rowDepth as rowDepthOriginal,
  selfReportedPeerWaitCopy,
  sessionCloseabilityHTML,
  sessionCloseabilityShape,
  sessionStatusGlyphHTML,
  sessionWorkStateHTML,
  taskLive as taskLiveOriginal,
  taskOfChild as taskOfChildOriginal,
  taskShaping as taskShapingOriginal,
  taskWord as taskWordOriginal,
  tasksOfRoot as tasksOfRootOriginal,
} from "./js/view/derive.js"
import { coordinatorForSession as coordinatorForSessionOriginal, coordinatorRowModel as coordinatorRowModelOriginal } from "./js/input/coordinator-actions.js"

/**
 * Put the current fleet where the copied modules look for it.
 *
 * They read a module-level object rather than taking arguments, so this is the
 * seam, and it is the only thing that writes to it.
 */
export function publish(
  sessions: SessionRow[],
  openId: string | null,
  filter: string,
  selectedId: string | null = openId,
  tasks?: TaskRow[] | null,
): void {
  // The inferred types of the copied state object come from its initialisers —
  // `sessions: []` reads as never[] — so the seam is widened here rather than
  // by editing a file that is meant to stay identical to its source.
  const state = S as Record<string, unknown>
  // The wire carries no `machine`: a page served by this daemon is looking at
  // this machine, so the local page supplies the identity rather than the
  // daemon repeating it on every row. Checked against the original, whose
  // payload has no such field either and whose rows still read "Mac 電腦 · 這台
  // Mac" — the constant comes from the client there too. A row read across the
  // relay does carry one, the machine it came from (`cloud/relay-reader.ts`),
  // and keeps it: that row is not on this machine.
  state.sessions = sessions.map((s) => {
    const own = (s as { machine?: unknown }).machine
    return { ...s, machine: typeof own === "string" && own ? own : LOCAL_SESSION_MACHINE }
  })
  // The whole task list, replaced whole, as `handlers.tasks` does. A caller
  // with no answer yet passes nothing and the last list stands; the original
  // starts from an empty one, which leaves every task function answering as
  // though the feature did not exist.
  if (tasks) state.tasks = tasks
  else state.tasks = state.tasks ?? []
  state.arrived = true
  // Two ids, as in the original: the highlight and the conversation on screen
  // are separate since the keyboard can move one without the other.
  state.openId = openId
  state.selectedId = selectedId
  state.filter = filter
}

/* The list's order is not the copied `ordered()`: it adds the time a session
   last moved — the daemon's answer, on the row itself — inside each state, and
   so its hold is not the copied one either. `orderedRows`, `freezeOrder` and
   `thawOrder` come from `order-bridge.ts`. */
export { orderedRows, freezeOrder, thawOrder } from "./order-bridge.js"

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
/**
 * The transcript's pending cards' spinners, on the same clock. Their own list,
 * as the original keeps it (`optimisticSpinners`): the list replaces its
 * spinners on every draw, and a transcript card is not the list's to drop.
 */
export function registerPendingSpinners(canvases: HTMLCanvasElement[]): void {
  setOptimisticSpinners(canvases)
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

/* The detail head's helpers, from the copied modules. */
/** Whether this page is being read on the Mac that serves it. */
export const atMac = atMacOriginal as () => boolean
/** A session's project mark, falling back to a generated one as the original does. */
export const markForSession = markForSessionOriginal as (session: unknown, projectKey?: string) => (Icon & { generated?: boolean }) | null
export const projectLabel = projectLabelOriginal as (key: string | undefined) => string
export const generatedMark = generatedMarkOriginal as (key: string | undefined) => (Icon & { generated?: boolean }) | null
/** The "my messages" copy in the page's language, from that module's own table. */
export const userMessagesCopy = copyForUserMessages as (language: string) => { title: string } & Record<string, string>

/* The render seam the copied modules call back through (and `thawOrder` too). */
/** Tell the copied modules how to redraw this page's list. */
export const bindSessionUI = bindSessionUIOriginal as (ui: Record<string, () => void>) => void

/* Dispatched work and the coordinator, from the copied modules (`view/derive.js`,
   `input/coordinator-actions.js`). They read `S.tasks` and `S.sessions`, which
   `publish` fills. */

/** A task as the copied modules read it: the Swift list's row. */
export type LegacyTask = TaskRow

/** Still going: queued, spawning or briefed. */
export const taskLive = taskLiveOriginal as (t: LegacyTask | null | undefined) => boolean
/** Whether a task still decides where its child's row sits. */
export const taskShaping = taskShapingOriginal as (t: LegacyTask | null | undefined) => boolean
/** The task a session is the child of, live or long over; the freshest wins. */
export const taskOfChild = taskOfChildOriginal as (id: string) => LegacyTask | null
/** The tasks a session is the root of, of those still shaping the list. */
export const tasksOfRoot = tasksOfRootOriginal as (id: string) => LegacyTask[]
/** One word for where a task got to. */
export const taskWord = taskWordOriginal as (t: LegacyTask | null | undefined) => string
/** How far a row is indented under the sessions that asked for it: 0, 1 or 2. */
export const rowDepth = rowDepthOriginal as (id: string) => number
/** The Feature Root chip, or null. */
export const featureRootChip = featureRootChipOriginal as (
  s: SessionRow,
) => { text: string; title: string; live: boolean; childCount: number } | null
/** A session's own account of the peer it waits on; "" when not complete enough to trust. */
export const selfReportedPeerWait = selfReportedPeerWaitCopy as (s: SessionRow) => string
/** Everything in the closeability badge whose identity can change its words. */
export const closeabilityShape = sessionCloseabilityShape as (s: unknown) => string
/** The Clawdfather record on a row, normalised, or null. */
export const coordinatorForSession = coordinatorForSessionOriginal as (
  s: SessionRow | null | undefined,
) => { label: string; status: string; commands: unknown[] } | null
/** The coordinator-only mark and badge, or null for an ordinary row. */
export const coordinatorRowModel = coordinatorRowModelOriginal as (
  s: SessionRow,
) => { badge: string; label: string; mark: { role: string; ariaHaspopup: "dialog"; ariaLabel: string } } | null

/**
 * Tokens the way the assistant counts them out loud: `840`, `9.3k`, `84.4k`,
 * `120k`.
 *
 * Not a copy: this is `agentTokens` from `session/agent.js` (line 129), restated.
 * That module imports the list, the transcript and the composer renderers, so
 * copying it would bring the whole page with it; the function itself is four
 * lines and has no state. If `session/agent.js` is ever copied, this goes and
 * its export is used instead.
 */
export function agentTokens(n: number): string {
  if (n < 1000) return String(n)
  const k = n / 1000
  return (k < 100 ? k.toFixed(1) : String(Math.round(k))) + "k"
}

/* The overlays' part of the bridge. */
export * from "./overlay-bridge.js"

/* The settings page's part of the bridge. */
export * from "./settings-bridge.js"

/* The usage page's part of the bridge. */
export * from "./usage-bridge.js"

/* The projects page's part of the bridge. */
export * from "./projects-bridge.js"

/* The devices and plan pages' parts of the bridge. */
export * from "./devices-bridge.js"
export * from "./plan-bridge.js"

/* The start sheet's part of the bridge. */
export * from "./start-bridge.js"
