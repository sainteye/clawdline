// The start sheet's part of the bridge.
//
// `js/input/start.js` is the Swift app's sheet, byte for byte, and it is here
// as the text `session/Start.tsx` follows line by line — not imported. It
// cannot be: it imports the whole page (`view/list.js`, `view/composer.js`,
// `session/open.js`, `view/waits.js`), and it binds its listeners through
// `core/dom.js`, which looks every id up once at import, before React has
// drawn any of them. The copy is kept under the guard so a change to the
// original turns `tools/check-legacy-css.sh` red and Start.tsx is read again.
//
// `js/input/clawdfather.js` is imported: it touches no DOM and decides the
// Clawdfather row from the transport alone.
//
// The transport is `net/live.js`'s `places`, `pastSessions`, `startPlace` and
// `resumePlace`, spelled against this daemon. What it deliberately lacks:
//
// - `machines`: there is no Cloud here, so the machine row stays hidden as it
//   is on the Mac's own page.
// - `coordinatorBearings`: registering Clawdfather writes the Swift app's
//   coordinator record, which this app must never write. Without the read the
//   original does not draw the row at all (`clawdfatherChoiceSupported`), which
//   is its answer for a feature that is missing rather than refused.
import { T } from "./js/core/i18n.js"
import { makeJSONFetch } from "@clawdline/core/refusal"
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
import {
  clawdfatherChoiceSupported as clawdfatherChoiceSupportedOriginal,
  clawdfatherCreationChoice as clawdfatherCreationChoiceOriginal,
  clawdfatherCreationLabel as clawdfatherCreationLabelOriginal,
} from "./js/input/clawdfather.js"

export type StartIcon = { cells?: unknown[]; accent?: string } | null | undefined

export interface StartPlaceRow {
  id: string
  label?: string
  path?: string
  at?: number
  icon?: StartIcon
  machine?: string
}
export interface StartAssistantRow {
  id: string
  label?: string
}
export interface PastRow {
  id: string
  title: string
  at?: number
  live?: boolean
}
export interface StartAnswer {
  id?: string
  attach?: string
}

/** A refusal as `net/fetch.js` hands it on: the code, and `app` when there is one. */
export type StartFailure = Error & { code?: string; app?: string; reason?: string }

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
  places: () => jsonFetch<{ places?: StartPlaceRow[]; assistants?: StartAssistantRow[] }>("/v1/places"),
  pastSessions: (id: string, assistant?: string | null) => {
    let path = "/v1/places/" + encodeURIComponent(id) + "/sessions"
    if (assistant) path += "/" + encodeURIComponent(assistant)
    return jsonFetch<{ sessions?: PastRow[]; more?: boolean }>(path)
  },
  /** The key is minted once per press: a retry of this request is the same start. */
  startPlace: (id: string, assistant?: string | null, model?: string) => {
    let path = "/v1/places/" + encodeURIComponent(id) + "/start"
    if (assistant || model) path += "/" + encodeURIComponent(assistant || "claude")
    if (model) path += "/" + encodeURIComponent(model)
    return jsonFetch<StartAnswer>(path, post(uuid()))
  },
  resumePlace: (id: string, session: string, assistant?: string | null, requestId?: string) => {
    let path = "/v1/places/" + encodeURIComponent(id) + "/resume/"
    if (assistant) path += encodeURIComponent(assistant) + "/"
    path += encodeURIComponent(session)
    return jsonFetch<StartAnswer>(path, post(requestId || uuid()))
  },
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
export const bySessionId = bySessionIdOriginal as (id: string) => { id: string } | null
export const localSessionMachine = LOCAL_SESSION_MACHINE as string
export type SelectionIdentity = { key: string; rowId: string; machine: string; session: string }
export const sessionSelectionIdentity = sessionSelectionIdentityOriginal as (
  row: unknown,
  fallbackMachine?: string,
) => SelectionIdentity | null
export const sessionSelectionKeyOf = sessionSelectionKeyOriginal as (row: unknown) => string | null

export interface ClawdfatherChoice {
  state: string
  shown: boolean
  enabled: boolean
  checked: boolean
  coordinator: unknown
}
export const clawdfatherChoiceSupported = clawdfatherChoiceSupportedOriginal as (client: unknown) => boolean
export const clawdfatherCreationChoice = clawdfatherCreationChoiceOriginal as (
  payload: unknown,
  selected: boolean,
  fresh: boolean,
) => ClawdfatherChoice
export const clawdfatherCreationLabel = clawdfatherCreationLabelOriginal as () => string
