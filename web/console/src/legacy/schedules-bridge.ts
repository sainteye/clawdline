// The schedules' part of the bridge.
//
// Six files under `js/` are the Swift app's, byte for byte: `view/schedules.js`
// (the list's rows and a schedule's retained runs), `net/schedules.js` (the
// list's one-minute lane and the project labels), `net/schedule-webhooks.js`
// (the Cloud webhook panel's words and helpers), `input/schedule-run.js`
// (Run now's words), `input/schedule.js` (the form) and
// `input/schedule-history.js` (the run-history sheet).
//
// The last two are here as the text `pages/schedules.tsx` follows line by
// line — not imported. They cannot be: they bind every listener at import
// through `core/dom.js`, which looks every id up once, before React has drawn
// any of them, and they read the page-wide `api` and `S.write` that this
// console does not set (`net/api.js` stays null here, and setting it for them
// would change what every other copied module that asks `typeof api.X` sees).
// `schedule-history.js` also imports `session/open.js` and `input/start.js`,
// which import the whole page. The copies are kept under the guard so a change
// to the original turns `tools/check-legacy-css.sh` red and the page is read
// again.
//
// What is imported touches no DOM when it is called: the run rows'
// renderer and the place lookup (`view/schedules.js` — its three
// `getElementById` calls run at import, find nothing yet, and are only read by
// `renderSchedules`, which is not used), the project labels and the places
// cache (`net/schedules.js`), and the words and helpers of the other two.
//
// The transport is `net/live.js`'s `schedules`, `schedule`, `createSchedule`,
// `updateSchedule`, `deleteSchedule`, `runSchedule` and `places`, spelled
// against this daemon, which answers them in the Swift app's shapes.
import { T, fill as fillOriginal } from "./js/core/i18n.js"
import { makeJSONFetch } from "@clawdline/core/refusal"
import { esc as escOriginal } from "./js/core/esc.js"
import { drawIcon as drawIconOriginal } from "./js/core/pixels.js"
import { shortPath as shortPathOriginal, tint as tintOriginal, uuid as uuidOriginal } from "./js/core/util.js"
import {
  failureSentence as failureSentenceOriginal,
  unansweredSentence as unansweredSentenceOriginal,
} from "./js/core/failure-text.js"
import {
  scheduleRunPlace as scheduleRunPlaceOriginal,
  scheduleRunsHTML as scheduleRunsHTMLOriginal,
} from "./js/view/schedules.js"
import {
  createPlacesCache as createPlacesCacheOriginal,
  loadScheduleProjects as loadScheduleProjectsOriginal,
} from "./js/net/schedules.js"
import {
  scheduleRunConfirmation as scheduleRunConfirmationOriginal,
  scheduleRunCopy as scheduleRunCopyOriginal,
  scheduleRunMessage as scheduleRunMessageOriginal,
} from "./js/input/schedule-run.js"
import {
  ScheduleWebhookClient as ScheduleWebhookClientOriginal,
  generateAndBindScheduleWebhook as generateAndBindScheduleWebhookOriginal,
  scheduleWebhookCanGenerate as scheduleWebhookCanGenerateOriginal,
  scheduleWebhookCopy as scheduleWebhookCopyOriginal,
  scheduleWebhookCurlExample as scheduleWebhookCurlExampleOriginal,
  scheduleWebhookHelpHTML as scheduleWebhookHelpHTMLOriginal,
  scheduleWebhookReceiptHeads as scheduleWebhookReceiptHeadsOriginal,
  scheduleWebhookManagementWarning as scheduleWebhookManagementWarningOriginal,
  scheduleWebhookTimelineHTML as scheduleWebhookTimelineHTMLOriginal,
  shouldObserveScheduleWebhook as shouldObserveScheduleWebhookOriginal,
} from "./js/net/schedule-webhooks.js"

/* ---- the wire: the Swift app's shapes, which this daemon answers in ---------
   Declared here rather than taken from the generated contract: these are the
   original's JSON objects, read the way its page reads them, and the page is
   the only reader. */

export type ScheduleIcon = { cells?: unknown[]; accent?: string } | null | undefined

/** `last_run` on a row and on a record. */
export interface ScheduleLastRun {
  task_id: string
  state: string
  at: number
}

/** One row of `GET /v1/orchestrator/schedules`: a valid schedule, or a file that is not one. */
export interface ScheduleListRow {
  id?: string
  title?: string
  enabled?: boolean
  next_fire?: number
  once?: boolean
  fired_at?: number
  project_dir?: string
  last_run?: ScheduleLastRun
  last_missed_at?: number
  // An invalid row.
  file?: string
  state?: string
  error?: string
  error_kind?: string
  // Added on this side from the Projects list, as `loadScheduleProjects` adds it.
  project?: { path: string; label: string; icon: ScheduleIcon }
}

export interface ScheduleList {
  schedules?: ScheduleListRow[]
  at?: number
}

/** One retained run in a record's `runs`. */
export interface ScheduleRun {
  task_id: string
  state: string
  assistant?: string
  project_dir?: string
  created?: number
  finished_at?: number
  terminal_id?: string
  session_id?: string
  summary?: string
  attached?: boolean
  attach_session?: string
}

/** `GET /v1/orchestrator/schedules/:id`'s `schedule`. */
export interface ScheduleRecord {
  id: string
  title?: string
  enabled?: boolean
  file?: string
  when?: { at?: string; days?: string | string[]; on?: string }
  task?: {
    assistant?: string
    model?: string
    project_dir?: string
    title?: string
    instructions?: string
    timeout_minutes?: number
    [key: string]: unknown
  }
  close_tab?: string
  catch_up_hours?: number
  notify_on_failure?: boolean
  once?: boolean
  fired_at?: number
  next_fire?: number
  last_run?: ScheduleLastRun
  runs?: ScheduleRun[]
  runs_may_be_truncated?: boolean
  last_missed_at?: number
  webhook_binding_availability?: string
  webhook_hook_id?: string
}

/** What create and save answer. */
export interface ScheduleWriteAnswer {
  ok?: boolean
  schedule?: { id: string; title?: string; enabled?: boolean; next_fire?: number }
  dispatch_enabled?: boolean
}

/** The body create and save send: `input/schedule.js`'s `payload`, field for field. */
export interface ScheduleBody {
  title: string
  at: string
  days: string | string[]
  place_id: string | null
  assistant: string | null
  model: string
  instructions: string
  enabled: boolean
  close_tab: string
  catch_up_hours: number
  notify_on_failure: boolean
  timeout_minutes: number
}

export interface SchedulePlace {
  id: string
  label?: string
  path: string
  at?: number
  icon?: ScheduleIcon
  machine?: string
}
export interface ScheduleAssistant {
  id: string
  label?: string
}
export interface SchedulePlaces {
  places?: SchedulePlace[]
  assistants?: ScheduleAssistant[]
  unanswered?: unknown[]
}

/** A refusal as `net/fetch.js` hands it on: the code, and `app` or `reason` when there is one. */
export type ScheduleFailure = Error & { code?: string; app?: string; reason?: string }

/** `net/fetch.js`'s `jsonFetch`, with credentials and refusal metadata retained. */
const jsonFetch = makeJSONFetch({
  words: {
    offline: (T as Record<string, string>).webOffline,
    requestFailed: (T as Record<string, string>).webRequestFailed,
    notJSON: (T as Record<string, string>).webNotJSON,
  },
  defaults: { credentials: "same-origin" },
  refusalFields: [
    { source: "app", target: "app", type: "string" },
    { source: "reason", target: "reason", type: "string" },
  ],
})

/** `net/fetch.js`'s `post`. */
function post(body: unknown, extra: Record<string, string>): RequestInit {
  return {
    method: "POST",
    headers: { "Content-Type": "application/json", ...extra },
    body: JSON.stringify(body || {}),
  }
}

/** The transport, as `net/live.js` spells it. Each write mints its own key, once per press. */
export const scheduleApi = {
  schedules: () => jsonFetch<ScheduleList>("/v1/orchestrator/schedules"),
  schedule: (id: string) =>
    jsonFetch<{ schedule?: ScheduleRecord }>("/v1/orchestrator/schedules/" + encodeURIComponent(id)),
  createSchedule: (schedule: ScheduleBody) =>
    jsonFetch<ScheduleWriteAnswer>("/v1/orchestrator/schedules", post(schedule, { "Idempotency-Key": uuid() })),
  updateSchedule: (id: string, schedule: ScheduleBody) => {
    const opts = post(schedule, { "Idempotency-Key": uuid() })
    opts.method = "PATCH"
    return jsonFetch<ScheduleWriteAnswer>("/v1/orchestrator/schedules/" + encodeURIComponent(id), opts)
  },
  deleteSchedule: (id: string) =>
    jsonFetch<{ ok?: boolean; deleted?: string }>("/v1/orchestrator/schedules/" + encodeURIComponent(id), {
      method: "DELETE",
      headers: { "Idempotency-Key": uuid() },
    }),
  runSchedule: (id: string) =>
    jsonFetch<Record<string, unknown>>(
      "/v1/orchestrator/schedules/" + encodeURIComponent(id) + "/run",
      post({}, { "Idempotency-Key": uuid() }),
    ),
  places: () => jsonFetch<SchedulePlaces>("/v1/places"),
  /** `net/live.js`'s `resumePlace`, as `start-bridge.ts` spells it. */
  resumePlace: (id: string, session: string, assistant?: string | null) => {
    let path = "/v1/places/" + encodeURIComponent(id) + "/resume/"
    if (assistant) path += encodeURIComponent(assistant) + "/"
    path += encodeURIComponent(session)
    return jsonFetch<{ id?: string; attach?: string }>(path, post({}, { "Idempotency-Key": uuid() }))
  },
}

/* ---- the copied functions, typed ------------------------------------------- */

export const strings = T as Record<string, string>
export const fill = fillOriginal as (s: string, holes: Record<string, unknown>) => string
export const esc = escOriginal as (s: unknown) => string
export const drawIcon = drawIconOriginal as (canvas: HTMLCanvasElement, icon: ScheduleIcon, cellPx: number) => boolean
export const tint = tintOriginal as (hex: string | undefined) => string
export const shortPath = shortPathOriginal as (path: string | undefined) => string
export const uuid = uuidOriginal as () => string
export const failureSentence = failureSentenceOriginal as (
  error: unknown,
  options: { sentence?: string; fallback: string } | string,
) => string
export const unansweredSentence = unansweredSentenceOriginal as (answer: unknown) => string

export const scheduleRunsHTML = scheduleRunsHTMLOriginal as (
  runs: ScheduleRun[] | undefined,
  at: number,
  terminalIsOpen: (id: string) => boolean,
) => string
export const scheduleRunPlace = scheduleRunPlaceOriginal as (
  run: ScheduleRun | null,
  places: SchedulePlace[],
) => SchedulePlace | null

export const loadScheduleProjects = loadScheduleProjectsOriginal as (
  schedules: ScheduleListRow[],
  readSchedule: ((id: string) => Promise<{ schedule?: ScheduleRecord }>) | null,
  readPlaces: (() => Promise<SchedulePlaces>) | null,
) => Promise<ScheduleListRow[]>
export const createPlacesCache = createPlacesCacheOriginal as (options: {
  places: (machine?: string) => Promise<SchedulePlaces>
}) => { read: () => Promise<SchedulePlaces>; forget: () => void }

export type ScheduleRunWords = Record<
  "button" | "running" | "confirm" | "accepted" | "active" | "spent" | "dispatchOff" | "writeOff" | "gone" | "failed",
  string
>
export const scheduleRunCopy = scheduleRunCopyOriginal as (language: string) => ScheduleRunWords
export const scheduleRunMessage = scheduleRunMessageOriginal as (error: unknown, language: string) => string
export const scheduleRunConfirmation = scheduleRunConfirmationOriginal as (title: string, language: string) => string

// Every word is a string except `states`, a table of the timeline's words, which this page reads
// only through `scheduleWebhookTimelineHTML`.
export const scheduleWebhookCopy = scheduleWebhookCopyOriginal as unknown as (
  language: string,
) => Record<string, string>
export const scheduleWebhookHelpHTML = scheduleWebhookHelpHTMLOriginal as (language: string) => string
export const scheduleWebhookCurlExample = scheduleWebhookCurlExampleOriginal as () => string
export const scheduleWebhookManagementWarning = scheduleWebhookManagementWarningOriginal as (
  hook: unknown,
  context: Record<string, unknown>,
) => string
export const scheduleWebhookCanGenerate = scheduleWebhookCanGenerateOriginal as (
  hook: unknown,
  context: Record<string, unknown>,
) => boolean
export const scheduleWebhookTimelineHTML = scheduleWebhookTimelineHTMLOriginal as (
  deliveries: unknown[],
  observed: Record<string, unknown>,
  language: string,
) => string

export interface ScheduleWebhookHook {
  hook_id: string
  state: string
  revision: number
  availability?: string
}
export interface ScheduleWebhookSecretResult {
  hook: ScheduleWebhookHook | null
  publicURL: string | null
  secretAvailable: boolean
  duplicate: boolean
}
export interface ScheduleWebhookClientAPI {
  read(hookID: string): Promise<{ hook?: ScheduleWebhookHook } | ScheduleWebhookHook>
  deliveries(hookID: string): Promise<{ deliveries?: unknown[] }>
  entitlements(): Promise<string | null>
  create(machineID: string): Promise<ScheduleWebhookSecretResult>
  rotate(hookID: string, revision: number): Promise<ScheduleWebhookSecretResult>
  disable(hookID: string, revision: number): Promise<{ hook?: ScheduleWebhookHook } | ScheduleWebhookHook>
  observe(hookID: string, deliveryID: string, receiptVersion: number): Promise<Record<string, unknown>>
}
export const ScheduleWebhookClient = ScheduleWebhookClientOriginal as unknown as new (options: {
  origin: string
  fetch?: typeof globalThis.fetch
  idempotencyKey?: () => string
}) => ScheduleWebhookClientAPI
export const scheduleWebhookReceiptHeads = scheduleWebhookReceiptHeadsOriginal as (
  deliveries: unknown[],
) => { deliveryID: string; receiptVersion: number }[]
export const shouldObserveScheduleWebhook = shouldObserveScheduleWebhookOriginal as (context: {
  open: boolean
  visibilityState: string
  renderedVersion: number
}) => boolean
export const generateAndBindScheduleWebhook = generateAndBindScheduleWebhookOriginal as (
  client: ScheduleWebhookClientAPI,
  bind: (scheduleID: string, hookID: string, replaceHookID: string | null) => Promise<ScheduleWebhookHook>,
  machineID: string,
  scheduleID: string,
  currentHook: ScheduleWebhookHook | null,
) => Promise<ScheduleWebhookSecretResult>
