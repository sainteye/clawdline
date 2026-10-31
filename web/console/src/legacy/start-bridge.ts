// The start sheet's part of the bridge.
//
// React owns the Start sheet in `session/Start.tsx`. The retired Swift sheet
// was a text reference during the port and has since been removed.
//
// `js/input/clawdfather.js` remains imported for compatibility with legacy
// helpers. The React sheet has its own machine-workspace action.
//
// The transport is `net/live.js`'s `places`, `pastSessions`, `startPlace` and
// `resumePlace`, spelled against this daemon. What it deliberately lacks:
//
// - `machines`: there is no Cloud here, so the machine row stays hidden as it
//   is on the Mac's own page.
// - `coordinatorBearings`: the React start sheet does not offer coordinator
//   registration. Existing role records and machine Bearings remain available
//   through their daemon APIs independently of this transport.
import { T } from "./js/core/i18n.js"
import { makeJSONFetch } from "@clawdline/core/refusal"
import { resumePath, startPath } from "../session/place-routes.js"
import type { Assistant, SessionRow, StartAssistant } from "@clawdline/contract"
import { S } from "./js/core/state.js"
import { bindFailureLine as bindFailureLineOriginal } from "./js/core/failure-text.js"
import {
  bandSpin as bandSpinOriginal,
  drawIcon as drawIconOriginal,
  drawSpinner as drawSpinnerOriginal,
  setBandSpin as setBandSpinOriginal,
  setStartSpin as setStartSpinOriginal,
  spinPhase as spinPhaseOriginal,
  startSpin as startSpinOriginal,
} from "./js/core/pixels.js"
import { uuid as uuidOriginal } from "./js/core/util.js"
import { bySessionId as bySessionIdOriginal } from "./js/view/derive.js"
import {
  LOCAL_SESSION_MACHINE,
  sessionSelectionIdentity as sessionSelectionIdentityOriginal,
  sessionSelectionKey as sessionSelectionKeyOriginal,
} from "./js/session/selection.js"

export type StartIcon = { cells?: unknown[]; accent?: string; svg?: string } | null | undefined

export interface StartPlaceRow {
  id: string
  label?: string
  path?: string
  at?: number
  icon?: StartIcon
  machine?: string
}
export type StartAssistantRow = Pick<StartAssistant, "id"> & Partial<StartAssistant>
export interface PastRow {
  id: string
  title: string
  at?: number
  live?: boolean
}
export interface StartAnswer {
  id?: string
  attach?: string
  backend?: "iterm" | "tmux" | "owned"
  assistant?: Assistant
  cwd?: string
}

/** A refusal as `net/fetch.js` hands it on: the code, and `app` when there is one. */
export type StartFailure = Error & { code?: string; app?: string; reason?: string; detail?: string; detailKey?: string; detail_key?: string }

/** `net/fetch.js`'s `jsonFetch`, with this page's refusal metadata retained. */
const jsonFetch = makeJSONFetch({
  words: {
    offline: (T as Record<string, string>).webOffline,
    requestFailed: (T as Record<string, string>).webRequestFailed,
    notJSON: (T as Record<string, string>).webNotJSON,
  },
  refusalFields: [
    { source: "app", target: "app", type: "string" },
    { source: "reason", target: "reason", type: "string" },
  ],
})

function post(key: string): RequestInit {
  return {
    method: "POST",
    headers: { "Content-Type": "application/json", "Idempotency-Key": key },
    body: JSON.stringify({}),
  }
}

/** The transport, as `net/live.js` spells it. */
export const startApi = {
  /**
   * This page's machine's Projects, or — from a start sheet opened for another
   * machine on the account — that machine's, which is the naming the schedule
   * form already uses (`relay-writer.ts` `namedMachine`). Each place id the
   * hosted client answers carries its own machine, so the start, the resume
   * and the past list that follow need no machine of their own.
   */
  places: (machine?: string | null) => jsonFetch<{ places?: StartPlaceRow[]; assistants?: StartAssistantRow[] }>(
    "/v1/places" + (machine ? "?machine=" + encodeURIComponent(machine) : "")),
  pastSessions: (id: string, assistant?: string | null, cursor?: string) => {
    let path = "/v1/places/" + encodeURIComponent(id) + "/sessions"
    if (assistant) path += "/" + encodeURIComponent(assistant)
    return jsonFetch<{ sessions?: PastRow[]; more?: boolean; next_cursor?: string }>(path + (cursor ? "?cursor=" + encodeURIComponent(cursor) : ""))
  },
  /**
   * The key is minted once per press: a retry of this request is the same start.
   * The paths, persona included, are `session/place-routes.ts`'s.
   */
  startPlace: (id: string, assistant?: string | null, model?: string, persona?: string) =>
    jsonFetch<StartAnswer>(startPath(id, assistant, model, persona), post(uuid())),
  resumePlace: (id: string, session: string, assistant?: string | null, requestId?: string, persona?: string) =>
    jsonFetch<StartAnswer>(resumePath(id, session, assistant, persona), post(requestId || uuid())),
}

export const uuid = uuidOriginal as () => string
export const bindFailureLine = bindFailureLineOriginal as (element: Element | null, error: unknown) => void
export const drawIcon = drawIconOriginal as (canvas: HTMLCanvasElement, icon: StartIcon, cellPx: number) => boolean
export const drawSpinner = drawSpinnerOriginal as (canvas: HTMLCanvasElement | null, phase: number) => void
export const setBandSpin = setBandSpinOriginal as (canvas: HTMLCanvasElement | null) => void
export const setStartSpin = setStartSpinOriginal as (canvas: HTMLCanvasElement | null) => void
/** The clock's live values, read when they are needed rather than copied at import. */
export const spinClock = {
  phase: () => spinPhaseOriginal as number,
  band: () => bandSpinOriginal as HTMLCanvasElement | null,
  start: () => startSpinOriginal as HTMLCanvasElement | null,
}
export function bySessionId(id: string, assistant?: string | null, machine?: string): { id: string } | null {
  if (!assistant && !machine) return bySessionIdOriginal(id)
  const rows = (S as unknown as { sessions: SessionRow[] }).sessions
  return rows.find(row => row.sessionId === id && (!assistant || row.assistant === assistant) &&
    (!machine || sessionSelectionIdentityOriginal(row)?.machine === machine)) ?? null
}
export const localSessionMachine = LOCAL_SESSION_MACHINE as string
export type SelectionIdentity = { key: string; rowId: string; machine: string; session: string }
export const sessionSelectionIdentity = sessionSelectionIdentityOriginal as (
  row: unknown,
  fallbackMachine?: string,
) => SelectionIdentity | null
export const sessionSelectionKeyOf = sessionSelectionKeyOriginal as (row: unknown) => string | null
