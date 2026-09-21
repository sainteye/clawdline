import { useEffect, useLayoutEffect, useRef } from "react"
import * as L from "../legacy/bridge.js"
import { nextWord } from "../next-strings.js"
import { toast } from "../overlays/index.js"
import { readAnswer, readFailure, readReady, type ReadState } from "../read-state.js"
import { invalidScheduleErrorHTML } from "../schedule-errors.js"
import "../schedule-errors.css"
import {
  createPlacesCache,
  drawIcon,
  esc,
  failureSentence,
  fill,
  loadScheduleProjects,
  scheduleApi,
  scheduleRunConfirmation,
  scheduleRunCopy,
  scheduleRunMessage,
  scheduleRunPlace,
  scheduleRunsHTML,
  scheduleWebhookCanGenerate,
  scheduleWebhookCopy,
  scheduleWebhookCurlExample,
  scheduleWebhookHelpHTML,
  scheduleWebhookManagementWarning,
  scheduleWebhookTimelineHTML,
  shortPath,
  strings,
  tint,
  unansweredSentence,
  type ScheduleAssistant,
  type ScheduleBody,
  type ScheduleFailure,
  type ScheduleListRow,
  type SchedulePlace,
  type SchedulePlaces,
  type ScheduleRecord,
  type ScheduleRun,
} from "../legacy/schedules-bridge.js"
import overlaysMarkup from "./schedules/overlays.html?raw"

/**
 * Schedules: `details#schedules` under the session list, and the three sheets
 * it opens — `#schedule-history` (a schedule's retained runs), `#schedule-form`
 * (making, changing and removing one) and `#schedule-delete-confirm`.
 *
 * Not a drawer page, so this module exports no `page`; `Sessions.tsx` draws the
 * section where the original's `index.html` has it, inside `#list-scroll`.
 *
 * The section's element and its summary are React's, drawn once with the
 * markup's own props; everything that changes — `hidden`, the count, the rows —
 * is written by `renderSchedules` below, as `view/schedules.js` writes it. The
 * three sheets are the original's markup between their own tags
 * (`index.html` 1183–1336, comments included), put into the body right after
 * `#root` the first time the section is drawn, which is where the original
 * has them: at body level, after the app. React owns none of them.
 *
 * What fills them is `input/schedule.js` and `input/schedule-history.js`,
 * followed line by line; `legacy/schedules-bridge.ts` says why they cannot be
 * imported. `net/schedules.js`'s lane is here too, for the same reason: it
 * reads `api` and `S.arrived` and draws through `renderSchedules`, which looks
 * its elements up at import. The pure parts of those files — the rows of a
 * schedule's runs, the project labels, the places cache, Run now's words and
 * the webhook panel's words — are imported from the copies.
 *
 * Differences from the original, each a fact about this console rather than a
 * choice about the screen:
 *
 * - **`write` is assumed until refused** (`S.write`), as `session/Start.tsx`
 *   assumes it: this daemon's health carries no `write` flag.
 * - **The webhook panel stays hidden.** Its client exists in the original only
 *   on the Cloud path (`main.js`'s `scheduleWebhookManagement`); on the local
 *   page it is null and the panel is hidden, with its toggle and help still
 *   inserted by the first draw — which is what happens here. The Cloud actions
 *   behind its buttons are unreachable while that client is null and are not
 *   carried.
 * - **A resumed run opens its session when it arrives** rather than through
 *   `Start.began`'s "starting" band, which `session/Start.tsx` does not export.
 * - **Escape and the list keys.** `input/keys.js` closes the form from its own
 *   chain and ignores the list keys while it is open; `App`'s chain does not
 *   know this sheet, so the form answers them itself before they reach it.
 */

/* ---- shared ------------------------------------------------------------- */

const T = () => strings
function el<E extends HTMLElement = HTMLElement>(id: string): E {
  return document.getElementById(id) as E
}
function lang(): string {
  return document.documentElement.lang
}

// `S.write`: true until a write is refused with `write_disabled`.
let write = true
// `S.arrived`: set from the page's `arrived`.
let arrivedFlag = false

/** How this page opens a session by row id, handed in by `Sessions.tsx`. */
const host: { open: (id: string) => void } = { open: () => {} }

/* ---- view/schedules.js: the rows ------------------------------------------ */

function relativeTime(unix: number | undefined, at?: number): string {
  if (!unix) return ""
  const seconds = unix - (at || Date.now() / 1000)
  const absolute = Math.abs(seconds)
  const unit: Intl.RelativeTimeFormatUnit = absolute < 90 * 60 ? "minute" : absolute < 36 * 3600 ? "hour" : "day"
  const size = unit === "minute" ? 60 : unit === "hour" ? 3600 : 86400
  let value = Math.round(seconds / size)
  if (!value) value = seconds < 0 ? -1 : 1
  try {
    return new Intl.RelativeTimeFormat(document.documentElement.lang || undefined, { numeric: "auto" }).format(value, unit)
  } catch {
    // refusal-ok: Intl.RelativeTimeFormat refusing a locale is not a machine refusal and carries no code
    const amount = Math.abs(value) + unit.charAt(0)
    return value < 0 ? amount + " ago" : "in " + amount
  }
}

function result(value: unknown): { state: string; label: string } {
  const state = String(value || "").toLowerCase()
  if (state === "success") return { state, label: T().webTaskDone }
  if (state === "failure" || state === "timeout" || state === "cancelled" || state === "spawn_failed")
    return { state, label: T().webTaskFailed }
  if (state === "queued" || state === "spawning" || state === "briefed") {
    return { state, label: T().webTaskRunning }
  }
  return { state: "none", label: "—" }
}

function validRow(schedule: ScheduleListRow, at?: number): string {
  const outcome = result(schedule.last_run && schedule.last_run.state)
  const next = schedule.next_fire ? relativeTime(schedule.next_fire, at) : ""
  const nextTitle = schedule.next_fire ? new Date(schedule.next_fire * 1000).toLocaleString() : ""
  const enabled = schedule.enabled ? T().webScheduleEnabled : T().webScheduleDisabled
  const nextLine = schedule.next_fire
    ? (schedule.enabled ? T().webScheduleNext + " " : T().webScheduleDisabled + " · next ") + next
    : T().webScheduleNoNext
  const missed = schedule.last_missed_at
    ? '<time class="schedule-missed" title="' +
      esc(new Date(schedule.last_missed_at * 1000).toLocaleString()) +
      '">' +
      esc(T().webScheduleMissed + " " + relativeTime(schedule.last_missed_at, at)) +
      "</time>"
    : ""
  const projectData = schedule.project && typeof schedule.project === "object" ? schedule.project : null
  const project = projectData
    ? '<span class="schedule-project"><canvas class="schedule-project-mark" aria-hidden="true"></canvas>' +
      '<span class="schedule-project-name" title="' +
      esc(projectData.path || "") +
      '">' +
      esc(projectData.label || "") +
      "</span></span>"
    : ""
  const projectSeparator = project && nextLine ? '<span class="schedule-meta-sep" aria-hidden="true"> · </span>' : ""
  return (
    '<li class="schedule-row" data-id="' +
    esc(schedule.id) +
    '" role="button" tabindex="0" ' +
    'style="cursor:pointer">' +
    '<div class="schedule-name"><span class="enabled-dot" data-enabled="' +
    (schedule.enabled ? "1" : "0") +
    '" role="img" aria-label="' +
    enabled +
    '"></span>' +
    '<span class="schedule-title">' +
    esc(schedule.title || nextWord("scheduleUntitled")) +
    "</span></div>" +
    '<span class="schedule-result" data-state="' +
    esc(outcome.state) +
    '">' +
    esc(outcome.label) +
    "</span>" +
    '<div class="schedule-meta">' +
    project +
    projectSeparator +
    '<time class="schedule-next"' +
    (nextTitle ? ' title="' + esc(nextTitle) + '"' : "") +
    ">" +
    esc(nextLine) +
    "</time></div>" +
    missed +
    "</li>"
  )
}

function invalidRow(schedule: ScheduleListRow): string {
  return (
    '<li class="schedule-row invalid">' +
    '<div class="schedule-name"><span class="enabled-dot" data-enabled="invalid" role="img" aria-label="' +
    esc(nextWord("scheduleInvalidStatus")) +
    '"></span>' +
    '<span class="schedule-title">' +
    esc(schedule.file || nextWord("scheduleInvalid")) +
    "</span></div>" +
    '<span class="schedule-result" data-state="invalid">' +
    esc(nextWord("scheduleInvalidStatus")) +
    "</span>" +
    invalidScheduleErrorHTML(schedule, nextWord) +
    "</li>"
  )
}

/** `renderSchedules`, with its three elements looked up when it is called rather than at import. */
function renderSchedules(schedules: ScheduleListRow[] | undefined, at?: number): void {
  const section = document.getElementById("schedules")
  const count = document.getElementById("schedules-count")
  const rows = document.getElementById("schedule-rows")
  const list = schedules || []
  if (!section || !rows || !count) return
  section.hidden = list.length === 0 && !write
  if (!list.length) {
    rows.innerHTML = ""
    count.textContent = ""
    return
  }
  count.textContent = String(list.length)
  rows.innerHTML = list
    .map((schedule) => (schedule && schedule.state === "invalid" ? invalidRow(schedule) : validRow(schedule || {}, at)))
    .join("")
  const projects: Record<string, NonNullable<ScheduleListRow["project"]>> = {}
  list.forEach((schedule) => {
    if (schedule && schedule.id && schedule.project) projects[schedule.id] = schedule.project
  })
  const rendered = rows.querySelectorAll<HTMLElement>(".schedule-row[data-id]")
  for (let i = 0; i < rendered.length; i++) {
    const projectData = projects[rendered[i].dataset.id || ""]
    if (!projectData) continue
    const mark = rendered[i].querySelector<HTMLCanvasElement>(".schedule-project-mark")
    if (mark && !drawIcon(mark, projectData.icon, 3)) mark.classList.add("none")
    const name = rendered[i].querySelector<HTMLElement>(".schedule-project-name")
    if (name) name.style.color = projectData.icon ? tint(projectData.icon.accent) : ""
  }
}

/* ---- net/schedules.js: the one-minute lane -------------------------------- */

const Schedules = (() => {
  let started = false
  let inFlight = false
  const projectBySchedule: Record<string, NonNullable<ScheduleListRow["project"]>> = {}
  // The Projects list, read at most once every few minutes (`createPlacesCache`).
  const placesCache = createPlacesCache({ places: () => scheduleApi.places() })
  let lastRefreshAt: number | null = null
  let lane: ReturnType<typeof setInterval> | null = null
  const LANE_MS = 60000

  const pageHidden = () => document.hidden === true

  function refresh(): void {
    if (inFlight || !arrivedFlag) return
    inFlight = true
    lastRefreshAt = Date.now()
    scheduleApi
      .schedules()
      .then((data) => {
        const schedules = (data && data.schedules) || []
        const at = data && data.at
        renderSchedules(
          schedules.map((schedule) => {
            const project = schedule && schedule.id ? projectBySchedule[schedule.id] : undefined
            return project ? { ...schedule, project } : schedule
          }),
          at,
        )
        return loadScheduleProjects(schedules, scheduleApi.schedule, placesCache.read).then((withProjects) => {
          withProjects.forEach((schedule) => {
            if (schedule && schedule.id && schedule.project) projectBySchedule[schedule.id] = schedule.project
          })
          renderSchedules(
            withProjects.map((schedule) => {
              const project = schedule && schedule.id ? projectBySchedule[schedule.id] : undefined
              return project && !schedule.project ? { ...schedule, project } : schedule
            }),
            at,
          )
        })
      })
      .catch(() => {
        // **Nothing is drawn from here, and that is the point.** A refusal —
        // the relay seam's `cloud_read_unavailable` and
        // `cloud_schedules_unpublished`, a machine that did not answer the
        // fan-out, or a dropped request on the direct path — means the
        // inventory is unknown, and `renderSchedules([])` would be this page
        // saying "there are none" on the Mac's behalf. Not drawing keeps the
        // section as it was: absent before any answer, and holding the last
        // truthful list after one. An inventory that really is empty still
        // arrives as an answer and still draws, which is the difference a
        // person can see (`net/schedules.js`, the same rule).
      })
      .then(() => {
        inFlight = false
      })
  }

  function beginWhenAuthed(attempt: number): void {
    if (!arrivedFlag) {
      if (attempt >= 40) return
      setTimeout(() => beginWhenAuthed(attempt + 1), 250)
      return
    }
    refresh()
  }

  function watchVisibility(): void {
    document.addEventListener("visibilitychange", () => {
      if (pageHidden()) {
        if (lane !== null) {
          clearInterval(lane)
          lane = null
        }
        return
      }
      if (lane !== null) return
      lane = setInterval(refresh, LANE_MS)
      if (lastRefreshAt === null || Date.now() - lastRefreshAt >= LANE_MS) refresh()
    })
  }

  return {
    /** Read again now: a read already in the air cannot contain the row just made, so it lands first. */
    refresh(tries?: number): void {
      const n = tries || 0
      if (inFlight && n < 10) {
        setTimeout(() => Schedules.refresh(n + 1), 300)
        return
      }
      refresh()
    },
    start(): void {
      if (started) return
      started = true
      beginWhenAuthed(0)
      if (!pageHidden()) lane = setInterval(refresh, LANE_MS)
      watchVisibility()
    },
  }
})()

/* ---- input/schedule.js: making, changing and removing a schedule ------------ */

type ScheduleFailureLike = ScheduleFailure | null | undefined

const Schedule = (() => {
  const DAY_CODES = ["sun", "mon", "tue", "wed", "thu", "fri", "sat"]
  const DAY_KEYS = [
    "webScheduleSun",
    "webScheduleMon",
    "webScheduleTue",
    "webScheduleWed",
    "webScheduleThu",
    "webScheduleFri",
    "webScheduleSat",
  ]
  const CLOSE_VALUES = ["on_success", "always", "never"]
  const CLOSE_KEYS = ["webScheduleCloseSuccess", "webScheduleCloseAlways", "webScheduleCloseNever"]
  const CATCH_DEFAULT = 6
  const TIMEOUT_DEFAULT = 30
  const MODELS = ["haiku", "sonnet", "opus"]

  let places: SchedulePlace[] | null = null
  let placesReading: ReadState<SchedulePlaces> = { phase: "loading" }
  let placesNote = ""
  let assistants: ScheduleAssistant[] = []
  let chosenPlace: string | null = null
  let chosenPlacePath: string | null = null
  let chosenAssistant: string | null = null
  let chosenModel = ""
  let days: string | string[] = "daily"
  let daysGuessed = false
  let closeTab = "on_success"
  let enabled = true
  let notify = true
  let creating = false
  let editingId: string | null = null
  let loadingEdit = false
  let deleteBusy = false

  const busy = () => creating || loadingEdit

  function said(words: string): void {
    el("schedule-said").textContent = words || ""
  }

  function hint(): string {
    const reading = placeReadProblem()
    if (reading) return reading
    if (!el<HTMLInputElement>("schedule-at").value) return T().webScheduleNeedsTime
    if (!chosenPlace) return T().webScheduleNeedsPlace
    return ""
  }

  function paint(): void {
    const b = busy()
    el("schedule-sheet").setAttribute("aria-busy", b ? "true" : "false")
    ;[
      "schedule-title",
      "schedule-at",
      "schedule-instructions",
      "schedule-catch",
      "schedule-timeout",
      "schedule-cancel",
      "schedule-go",
      "schedule-delete",
    ].forEach((id) => {
      const placesReady = readReady(placesReading) && (places?.length ?? 0) > 0
      el<HTMLInputElement>(id).disabled = b || (id === "schedule-go" && !placesReady)
    })
    ;["schedule-with", "schedule-model", "schedule-days", "schedule-close", "schedule-flags"].forEach((id) => {
      const chips = el(id).querySelectorAll<HTMLButtonElement>(".chip")
      for (let i = 0; i < chips.length; i++) chips[i].disabled = b
    })
    const rows = el("schedule-places").querySelectorAll<HTMLButtonElement>(".place")
    for (let j = 0; j < rows.length; j++) rows[j].disabled = b
  }

  function drawWith(): void {
    const row = el("schedule-with")
    row.innerHTML = ""
    const choices = assistants.slice()
    if (chosenAssistant && !choices.some((a) => a.id === chosenAssistant)) {
      choices.push({ id: chosenAssistant, label: chosenAssistant })
    }
    row.hidden = choices.length < 2
    el("schedule-with-label").hidden = row.hidden
    if (row.hidden) return
    choices.forEach((a) => {
      const chip = document.createElement("button")
      chip.type = "button"
      chip.className = "chip" + (a.id === chosenAssistant ? " on" : "")
      chip.textContent = a.label || a.id
      chip.setAttribute("aria-pressed", a.id === chosenAssistant ? "true" : "false")
      chip.onclick = () => {
        chosenAssistant = a.id
        if (chosenAssistant !== "claude") chosenModel = ""
        drawWith()
        drawModel()
      }
      row.appendChild(chip)
    })
    paint()
  }

  function drawModel(): void {
    const row = el("schedule-model")
    row.innerHTML = ""
    const show = chosenAssistant === "claude"
    el("schedule-model-label").hidden = !show
    row.hidden = !show
    if (!show) return
    MODELS.forEach((m) => {
      const chip = document.createElement("button")
      chip.type = "button"
      chip.className = "chip" + (m === chosenModel ? " on" : "")
      chip.textContent = m
      chip.setAttribute("aria-pressed", m === chosenModel ? "true" : "false")
      chip.onclick = () => {
        chosenModel = chosenModel === m ? "" : m
        drawModel()
      }
      row.appendChild(chip)
    })
    paint()
  }

  function drawDays(): void {
    const row = el("schedule-days")
    row.innerHTML = ""
    const daily = document.createElement("button")
    daily.type = "button"
    const dailyOn = days === "daily" && !daysGuessed
    daily.className = "chip" + (dailyOn ? " on" : "")
    daily.textContent = T().webScheduleDaily
    daily.setAttribute("aria-pressed", dailyOn ? "true" : "false")
    daily.onclick = () => {
      days = "daily"
      daysGuessed = false
      drawDays()
    }
    row.appendChild(daily)
    DAY_CODES.forEach((code, i) => {
      const on = Array.isArray(days) && days.indexOf(code) >= 0
      const chip = document.createElement("button")
      chip.type = "button"
      chip.className = "chip" + (on ? " on" : "")
      chip.textContent = T()[DAY_KEYS[i]]
      chip.setAttribute("aria-pressed", on ? "true" : "false")
      chip.onclick = () => toggleDay(code)
      row.appendChild(chip)
    })
    paint()
  }

  function toggleDay(code: string): void {
    const picked = Array.isArray(days) ? days.slice() : []
    const at = picked.indexOf(code)
    if (at >= 0) picked.splice(at, 1)
    else picked.push(code)
    days = picked.length ? DAY_CODES.filter((c) => picked.indexOf(c) >= 0) : "daily"
    daysGuessed = false
    drawDays()
  }

  function drawClose(): void {
    const row = el("schedule-close")
    row.innerHTML = ""
    CLOSE_VALUES.forEach((value, i) => {
      const chip = document.createElement("button")
      chip.type = "button"
      chip.className = "chip" + (value === closeTab ? " on" : "")
      chip.textContent = T()[CLOSE_KEYS[i]]
      chip.setAttribute("aria-pressed", value === closeTab ? "true" : "false")
      chip.onclick = () => {
        closeTab = value
        drawClose()
      }
      row.appendChild(chip)
    })
    paint()
  }

  function checkChip(on: boolean, label: string): HTMLButtonElement {
    const chip = document.createElement("button")
    chip.type = "button"
    chip.className = "chip check" + (on ? " on" : "")
    chip.innerHTML =
      '<svg class="tick" viewBox="0 0 14 14" aria-hidden="true"' +
      ' focusable="false">' +
      '<rect class="box" x="0.5" y="0.5" width="13" height="13" rx="3.5"></rect>' +
      '<path class="mark" d="M3.6 7.1 5.9 9.4 10.4 4.6"' +
      ' stroke-linecap="round" stroke-linejoin="round"></path></svg>' +
      '<span class="label"></span>'
    chip.querySelector(".label")!.textContent = label
    chip.setAttribute("aria-pressed", on ? "true" : "false")
    return chip
  }

  function drawFlags(): void {
    const row = el("schedule-flags")
    row.innerHTML = ""
    const enabledChip = checkChip(enabled, T().webScheduleEnabled)
    enabledChip.onclick = () => {
      enabled = !enabled
      drawFlags()
    }
    row.appendChild(enabledChip)
    const notifyChip = checkChip(notify, T().webScheduleNotify)
    notifyChip.onclick = () => {
      notify = !notify
      drawFlags()
    }
    row.appendChild(notifyChip)
    paint()
  }

  function markPicked(row: HTMLElement, on: boolean): void {
    row.setAttribute("aria-pressed", on ? "true" : "false")
    row.style.borderColor = on ? "var(--accent-ed)" : ""
    row.style.opacity = on ? "1" : ""
    row.querySelector<HTMLElement>(".where")!.style.color = on ? "var(--accent)" : ""
  }

  function drawPlaces(): void {
    const list = el("schedule-places")
    list.innerHTML = ""
    list.dataset.readState = placesReading.phase
    const why = placeReadProblem()
    if (why) {
      const none = document.createElement("li")
      none.className = "note"
      none.textContent = why
      list.appendChild(none)
    }
    if (placesNote && placesReading.phase === "ready") {
      const partial = document.createElement("li")
      partial.className = "note"
      partial.setAttribute("role", "status")
      partial.textContent = placesNote
      list.appendChild(partial)
    }
    ;(places || []).forEach((p) => {
      const li = document.createElement("li")
      const row = document.createElement("button")
      row.type = "button"
      row.className = "place"
      row.dataset.id = p.id
      row.innerHTML = '<canvas></canvas><span class="name"></span><span class="where"></span>'
      const mark = row.querySelector("canvas")!
      if (!drawIcon(mark, p.icon, 4)) mark.classList.add("none")
      const name = row.querySelector<HTMLElement>(".name")!
      name.textContent = p.label || p.path
      name.style.color = p.icon ? tint(p.icon.accent) : ""
      row.querySelector(".where")!.textContent = shortPath(p.path)
      markPicked(row, p.id === chosenPlace)
      li.appendChild(row)
      list.appendChild(li)
    })
    drawPicked()
    paint()
  }

  function drawPicked(): void {
    const box = el("schedule-picked")
    const open = box.getAttribute("aria-expanded") === "true"
    const p = (places || []).filter((x) => x.id === chosenPlace)[0]
    box.innerHTML = '<canvas></canvas><span class="name"></span>' + '<span class="where"></span><span class="chev"></span>'
    const mark = box.querySelector("canvas")!
    if (!drawIcon(mark, p && p.icon, 4)) mark.classList.add("none")
    box.classList.toggle("none", !p && !chosenPlacePath)
    const name = box.querySelector<HTMLElement>(".name")!
    if (p) {
      name.textContent = p.label || p.path
    } else if (chosenPlacePath) {
      name.textContent = shortPath(chosenPlacePath)
    } else {
      name.textContent = T().webStartPick
    }
    name.style.color = p && p.icon ? tint(p.icon.accent) : ""
    box.querySelector(".where")!.textContent = p ? shortPath(p.path) : ""
    box.querySelector(".chev")!.textContent = open ? "⌄" : "›"
  }

  function togglePlaces(): void {
    if (busy()) return
    const box = el("schedule-picked")
    const open = box.getAttribute("aria-expanded") === "true"
    box.setAttribute("aria-expanded", open ? "false" : "true")
    el("schedule-places").hidden = open
    drawPicked()
  }

  function pickPlace(id: string): void {
    if (busy()) return
    chosenPlace = id
    chosenPlacePath = null
    const rows = el("schedule-places").querySelectorAll<HTMLElement>(".place")
    for (let i = 0; i < rows.length; i++) markPicked(rows[i], rows[i].dataset.id === id)
    el("schedule-picked").setAttribute("aria-expanded", "false")
    el("schedule-places").hidden = true
    drawPicked()
    said("")
  }

  function ensurePlaces(): Promise<void> {
    if (places && places.length && !placesNote) return Promise.resolve()
    placesReading = { phase: "loading" }
    return scheduleApi
      .places()
      .then((d: SchedulePlaces) => {
        places = (d && d.places) || []
        assistants = (d && d.assistants) || []
        placesNote = unansweredSentence(d)
        placesReading = readAnswer(d, places.length === 0 && placesNote === "")
      })
      .catch((error) => {
        placesReading = readFailure(error)
      })
  }

  function placeReadProblem(): string {
    if (placesReading.phase === "loading") return T().webLoading
    if (placesReading.phase === "empty_authoritative") return T().webStartEmpty
    if (placesReading.phase === "unanswered") {
      return failureSentence(placesReading.error, {
        sentence: nextWord("schedulePlacesUnanswered"),
        fallback: T().webRequestFailed,
      })
    }
    if (placesReading.phase === "refused") {
      return failureSentence(placesReading.error, { fallback: T().webRequestFailed })
    }
    return ""
  }

  function defaultAssistant(preferred: string | null | undefined): void {
    chosenAssistant = preferred || (assistants.length ? assistants[0].id : null)
  }

  function reset(): void {
    creating = false
    editingId = null
    loadingEdit = false
    places = null
    placesReading = { phase: "loading" }
    placesNote = ""
    assistants = []
    chosenPlace = null
    chosenPlacePath = null
    chosenAssistant = null
    chosenModel = ""
    days = "daily"
    daysGuessed = false
    closeTab = "on_success"
    enabled = true
    notify = true
    el<HTMLInputElement>("schedule-title").value = ""
    el<HTMLInputElement>("schedule-at").value = ""
    el<HTMLTextAreaElement>("schedule-instructions").value = ""
    el<HTMLInputElement>("schedule-catch").value = String(CATCH_DEFAULT)
    el<HTMLInputElement>("schedule-timeout").value = String(TIMEOUT_DEFAULT)
    el<HTMLDetailsElement>("schedule-more").open = false
    el("schedule-picked").setAttribute("aria-expanded", "false")
    el("schedule-places").hidden = true
    el("schedule-form-title").textContent = T().webScheduleNew
    el("schedule-go").textContent = T().webScheduleCreate
    el("schedule-delete").hidden = true
    el("schedule-form-say").textContent = T().webScheduleNewSay
    said("")
    drawWith()
    drawModel()
    drawDays()
    drawClose()
    drawFlags()
    drawPlaces()
  }

  /** The `+` beside the Schedules list. Every field blank, "Daily" the one thing already on. */
  function open(): void {
    if (!el("schedule-form").hidden) return
    reset()
    el("schedule-form").hidden = false
    el("schedule-title").focus({ preventScroll: true })
    ensurePlaces().then(() => {
      defaultAssistant(null)
      drawWith()
      drawModel()
      drawPlaces()
    })
  }

  function placeIdForPath(path: string | undefined): string | null {
    const match = (places || []).filter((p) => p.path === path)[0]
    return match ? match.id : null
  }

  /** Edit in a schedule's run-history sheet. */
  function openEdit(id: string): void {
    if (!el("schedule-form").hidden || !el("schedule-delete-confirm").hidden) return
    reset()
    editingId = id
    loadingEdit = true
    el("schedule-form-title").textContent = T().webScheduleEdit
    el("schedule-go").textContent = T().webScheduleSave
    el("schedule-form-say").textContent = ""
    el("schedule-delete").hidden = false
    el("schedule-form").hidden = false
    paint()
    scheduleApi
      .schedule(id)
      .then((d) => {
        fillFromRecord((d && d.schedule) || ({} as ScheduleRecord))
      })
      .catch((e: ScheduleFailureLike) => {
        loadingEdit = false
        said(why(e, T().webRequestFailed))
        paint()
      })
  }

  function fillFromRecord(record: ScheduleRecord): void {
    const when = record.when || {}
    const task = record.task || {}
    el<HTMLInputElement>("schedule-title").value = record.title || ""
    el<HTMLInputElement>("schedule-at").value = when.at || ""
    const whenDays = when.days
    const heard = Array.isArray(whenDays) ? DAY_CODES.filter((c) => whenDays.indexOf(c) >= 0) : null
    days = heard && heard.length ? heard : "daily"
    daysGuessed = false
    el<HTMLTextAreaElement>("schedule-instructions").value = task.instructions || ""
    enabled = record.enabled !== false
    closeTab = record.close_tab && CLOSE_VALUES.indexOf(record.close_tab) >= 0 ? record.close_tab : "on_success"
    notify = record.notify_on_failure !== false
    el<HTMLInputElement>("schedule-catch").value = String(record.catch_up_hours != null ? record.catch_up_hours : CATCH_DEFAULT)
    el<HTMLInputElement>("schedule-timeout").value = String(
      task.timeout_minutes != null ? task.timeout_minutes : TIMEOUT_DEFAULT,
    )
    drawDays()
    drawClose()
    drawFlags()
    said("")
    ensurePlaces().then(() => {
      chosenPlace = placeIdForPath(task.project_dir)
      chosenPlacePath = chosenPlace ? null : task.project_dir || null
      defaultAssistant(task.assistant)
      chosenModel = chosenAssistant === "claude" && task.model && MODELS.indexOf(task.model) >= 0 ? task.model : ""
      loadingEdit = false
      drawWith()
      drawModel()
      drawPlaces()
      paint()
    })
  }

  function close(): void {
    if (busy()) return
    el("schedule-form").hidden = true
  }

  function askDelete(): void {
    if (busy() || !editingId) return
    if (!write) {
      said(T().webStartOff)
      return
    }
    el("schedule-form").hidden = true
    deleteBusy = false
    el("schedule-delete-confirm-title").textContent = T().webScheduleDelete
    el("schedule-delete-confirm-say").textContent = fill(T().webScheduleDeleteAsk, {
      title: el<HTMLInputElement>("schedule-title").value,
    })
    el("schedule-delete-confirm-go").textContent = T().webScheduleDelete
    el("schedule-delete-confirm").hidden = false
    paintDeleteConfirm()
    el("schedule-delete-confirm-go").focus({ preventScroll: true })
  }

  function closeDeleteConfirm(restore: boolean): void {
    if (deleteBusy) return
    el("schedule-delete-confirm").hidden = true
    if (restore) {
      el("schedule-form").hidden = false
      el("schedule-delete").focus({ preventScroll: true })
    }
  }

  function paintDeleteConfirm(): void {
    el("schedule-delete-confirm-sheet").setAttribute("aria-busy", deleteBusy ? "true" : "false")
    el<HTMLButtonElement>("schedule-delete-confirm-cancel").disabled = deleteBusy
    el<HTMLButtonElement>("schedule-delete-confirm-go").disabled = deleteBusy
  }

  function confirmDelete(): void {
    if (deleteBusy || !editingId) return
    const id = editingId
    deleteBusy = true
    paintDeleteConfirm()
    scheduleApi
      .deleteSchedule(id)
      .then(() => {
        deleteBusy = false
        el("schedule-delete-confirm").hidden = true
        editingId = null
        Schedules.refresh()
        toast(T().webScheduleDeleted)
      })
      .catch((e: ScheduleFailureLike) => {
        deleteBusy = false
        closeDeleteConfirm(true)
        said(why(e, T().webRequestFailed))
      })
  }

  function why(e: ScheduleFailureLike, fallback: string): string {
    const code = e && e.code
    return failureSentence(e, {
      sentence:
        // Reading one schedule in full has no route on the Mac's Cloud bridge
        // (`carry.ts`, `NO_MAC_ROUTE.schedule`), so this is what Edit meets
        // on a phone. Said the way `Start.tsx` and `Snippets.tsx` say it —
        // the copied catalog has no sentence for a code the Swift app never
        // sent — rather than as "The request failed", which is what a person
        // can only report.
        code === "cloud_not_carried"
          ? nextWord("cloudNotCarried")
          : code === "write_disabled"
            ? T().webStartOff
            : code === "rate_limited" || code === "busy"
              ? T().webFailRateLimited
              : "",
      fallback,
    })
  }

  /** Create and Save both land here; `editingId` says which of the two this press is. */
  function create(): void {
    if (busy()) return
    if (!write) {
      said(T().webStartOff)
      return
    }
    const problem = hint()
    if (problem) {
      said(problem)
      if (!el<HTMLInputElement>("schedule-at").value) el("schedule-at").focus({ preventScroll: true })
      return
    }
    const editing = !!editingId

    let catchUp = parseInt(el<HTMLInputElement>("schedule-catch").value, 10)
    if (!(catchUp >= 0)) catchUp = CATCH_DEFAULT
    let timeout = parseInt(el<HTMLInputElement>("schedule-timeout").value, 10)
    if (!(timeout >= 1)) timeout = TIMEOUT_DEFAULT

    const payload: ScheduleBody = {
      title: el<HTMLInputElement>("schedule-title").value.trim(),
      at: el<HTMLInputElement>("schedule-at").value,
      days,
      place_id: chosenPlace,
      assistant: chosenAssistant,
      model: chosenModel,
      instructions: el<HTMLTextAreaElement>("schedule-instructions").value,
      enabled,
      close_tab: closeTab,
      catch_up_hours: catchUp,
      notify_on_failure: notify,
      timeout_minutes: timeout,
    }

    creating = true
    said("")
    paint()
    const request = editing ? scheduleApi.updateSchedule(editingId!, payload) : scheduleApi.createSchedule(payload)
    request
      .then((d) => {
        creating = false
        close()
        Schedules.refresh()
        if (!editing && d && d.dispatch_enabled === false) {
          toast(T().webScheduleDispatchOff)
        } else {
          toast(editing ? T().webScheduleSaved : T().webScheduleCreated)
        }
      })
      .catch((e: ScheduleFailureLike) => {
        creating = false
        said(why(e, editing ? T().webRequestFailed : T().webScheduleFailed))
        paint()
      })
  }

  return {
    open,
    openEdit,
    close,
    create,
    pick: pickPlace,
    toggle: togglePlaces,
    askDelete,
    closeDeleteConfirm,
    confirmDelete,
  }
})()

/* ---- input/schedule-history.js: one schedule as work that happened ----------- */

type WebhookHook = { hook_id?: string; state?: string; revision?: number } | null

/** `main.js`'s `scheduleWebhookManagement`: made only on the Cloud path, so null on this page. */
function managementClient(): { copy?: (text: string) => unknown } | null {
  return null
}

const ScheduleHistory = (() => {
  let scheduleId: string | null = null
  let record: ScheduleRecord | null = null
  let places: SchedulePlace[] = []
  let placesNote = ""
  let loading = false
  let pressing: string | null = null
  let ticket = 0
  // The Cloud management client. Null on this transport, as on the original's
  // local page, and so is everything it would have read; they are `let`, as
  // there, so each reads as the value it holds rather than as a constant null.
  let webhook = managementClient()
  let hook: WebhookHook = null
  const deliveries: unknown[] = []
  let ephemeralURL: string | null = null
  let effectiveTier: string | null = null
  const observedReceipts: Record<string, unknown> = {}
  let helpOpen = false
  let webhookOpen = false
  let runningNow = false

  const scheduleRunWords = () => scheduleRunCopy(lang())

  function ensureRunNowButton(): void {
    const edit = el("schedule-history-edit")
    if (!edit || document.getElementById("schedule-history-run-now")) return
    const button = document.createElement("button")
    button.id = "schedule-history-run-now"
    button.type = "button"
    button.className = "chip confirm-go"
    button.addEventListener("click", runNow)
    edit.parentNode!.insertBefore(button, edit)
  }

  const webhookWords = () => scheduleWebhookCopy(lang())
  const webhookNode = (id: string) => document.getElementById(id)

  function ensureWebhookToggle(): void {
    const panel = webhookNode("schedule-webhook")
    const title = webhookNode("schedule-webhook-title")
    if (!panel || !title || webhookNode("schedule-webhook-panel-toggle")) return
    const toggle = document.createElement("button")
    toggle.id = "schedule-webhook-panel-toggle"
    toggle.type = "button"
    toggle.className = "chip schedule-webhook-panel-toggle"
    toggle.setAttribute(
      "aria-controls",
      "schedule-webhook-note schedule-webhook-warning " +
        "schedule-webhook-help schedule-webhook-status schedule-webhook-timeline",
    )
    toggle.addEventListener("click", () => {
      webhookOpen = !webhookOpen
      drawWebhook()
    })
    title.insertAdjacentElement("afterend", toggle)
    panel.dataset.collapsed = "true"
  }

  function ensureWebhookHelp(): void {
    const panel = webhookNode("schedule-webhook")
    if (!panel || webhookNode("schedule-webhook-help-toggle")) return
    const actions = panel.querySelector(".buttons")
    const status = webhookNode("schedule-webhook-status")
    if (!actions || !status) return

    const toggle = document.createElement("button")
    toggle.id = "schedule-webhook-help-toggle"
    toggle.type = "button"
    toggle.className = "chip schedule-webhook-help-toggle"
    toggle.setAttribute("aria-controls", "schedule-webhook-help")
    toggle.setAttribute("aria-expanded", "false")
    toggle.addEventListener("click", () => {
      helpOpen = !helpOpen
      drawWebhook()
    })
    actions.appendChild(toggle)

    const region = document.createElement("section")
    region.id = "schedule-webhook-help"
    region.className = "schedule-webhook-help"
    region.hidden = true
    region.setAttribute("role", "region")
    region.setAttribute("aria-labelledby", toggle.id)

    const content = document.createElement("div")
    content.id = "schedule-webhook-help-content"
    region.appendChild(content)

    const copy = document.createElement("button")
    copy.id = "schedule-webhook-help-copy-example"
    copy.type = "button"
    copy.className = "chip"
    copy.addEventListener("click", () => {
      if (!webhook || typeof webhook.copy !== "function") return
      Promise.resolve(webhook.copy(scheduleWebhookCurlExample())).then(() => {
        webhookNode("schedule-webhook-status")!.textContent = webhookWords().exampleCopied
      }, webhookFailed)
    })
    region.appendChild(copy)
    panel.insertBefore(region, status)
  }

  function drawWebhook(): void {
    const panel = webhookNode("schedule-webhook")
    if (!panel) return
    ensureWebhookToggle()
    ensureWebhookHelp()
    const words = webhookWords()
    panel.hidden = !webhook || !record
    panel.dataset.collapsed = webhookOpen ? "false" : "true"
    const panelToggle = webhookNode("schedule-webhook-panel-toggle")
    if (panelToggle) {
      panelToggle.textContent = webhookOpen ? words.hideDetails : words.showDetails
      panelToggle.setAttribute("aria-expanded", webhookOpen ? "true" : "false")
    }
    webhookNode("schedule-webhook-title")!.textContent = words.title
    webhookNode("schedule-webhook-note")!.textContent = ephemeralURL ? words.once : words.idempotency
    const managementContext = {
      bindingAvailability: record && record.webhook_binding_availability,
      effectiveTier,
      language: lang(),
    }
    webhookNode("schedule-webhook-warning")!.textContent = scheduleWebhookManagementWarning(hook, managementContext)
    webhookNode("schedule-webhook-generate")!.textContent = words.generate
    webhookNode("schedule-webhook-copy")!.textContent = words.copy
    webhookNode("schedule-webhook-rotate")!.textContent = words.rotate
    webhookNode("schedule-webhook-disable")!.textContent = words.disable
    const helpToggle = webhookNode("schedule-webhook-help-toggle")
    const help = webhookNode("schedule-webhook-help")
    if (helpToggle && help) {
      helpToggle.textContent = helpOpen ? words.hideHelp : words.howToUse
      helpToggle.setAttribute("aria-expanded", helpOpen ? "true" : "false")
      help.hidden = !helpOpen
      webhookNode("schedule-webhook-help-content")!.innerHTML = scheduleWebhookHelpHTML(lang())
      webhookNode("schedule-webhook-help-copy-example")!.textContent = words.copyExample
    }
    const current = hook
    webhookNode("schedule-webhook-generate")!.hidden = !(!current || current.state === "disabled")
    ;(webhookNode("schedule-webhook-generate") as HTMLButtonElement).disabled = !scheduleWebhookCanGenerate(
      hook,
      managementContext,
    )
    webhookNode("schedule-webhook-copy")!.hidden = !ephemeralURL
    webhookNode("schedule-webhook-rotate")!.hidden = !current || current.state !== "active"
    ;(webhookNode("schedule-webhook-rotate") as HTMLButtonElement).disabled =
      effectiveTier === null || effectiveTier === "free"
    webhookNode("schedule-webhook-disable")!.hidden = !current || current.state === "disabled"
    webhookNode("schedule-webhook-timeline")!.innerHTML = scheduleWebhookTimelineHTML(deliveries, observedReceipts, lang())
    // `observeVisibleTimeline` returns at once without a management client.
  }

  /** `loadWebhook`: with no management client it draws the panel, hidden, and asks nothing. */
  function loadWebhook(): Promise<void> {
    drawWebhook()
    return Promise.resolve()
  }

  /** `webhookAction`: every action needs the management client, which this transport does not have. */
  function webhookAction(_action: string): Promise<void> {
    if (!webhook || !record) return Promise.resolve()
    return Promise.resolve()
  }

  function webhookFailed(): void {
    webhookNode("schedule-webhook-status")!.textContent = webhookWords().unavailable
    drawWebhook()
  }

  const terminalIsOpen = (id: string) => !!L.byId(id)

  function run(taskId: string | undefined): ScheduleRun | null {
    return ((record && record.runs) || []).filter((candidate) => candidate && candidate.task_id === taskId)[0] || null
  }

  const projectPlace = (selected: ScheduleRun | null) => scheduleRunPlace(selected, places)

  /**
   * What the history sheet says about a refusal.
   *
   * It used to end `return T().webRequestFailed`, so the five codes below were
   * named and every other one — `machine_offline`, `rate_limited`, `busy`,
   * `forbidden`, `store_unavailable`, `cloud_read_timeout` — arrived as "請求
   * 失敗" with no code after it. A person reading that could neither act on it
   * nor report it. The five stay, because this sheet has better words for them
   * than the general catalog; everything else now goes where every other
   * refusal in this console goes (`core/failure-text.js`), which has a sentence
   * per code and puts `code · ref` after it.
   */
  function why(e: ScheduleFailureLike): string {
    return failureSentence(e, { sentence: ownWhy(e), fallback: T().webRequestFailed })
  }

  /** The five codes `scheduleRunMessage` names; everything else is the catalog's. */
  const RUN_NAMED = ["schedule_active", "schedule_spent", "orchestrator_disabled", "write_disabled", "not_found"]

  /** "Run now" refused: the copied words where they exist, the catalog's otherwise. */
  function runSentence(error: ScheduleFailureLike): string {
    const code = (error && error.code) || ""
    const own = RUN_NAMED.includes(code) ? scheduleRunMessage(error, lang()) : ""
    return failureSentence(error, { sentence: own, fallback: scheduleRunMessage(error, lang()) })
  }

  /** This sheet's own words, where it has better ones; "" to let the catalog answer. */
  function ownWhy(e: ScheduleFailureLike): string {
    // As in the form above: the sheet is opened by reading one schedule in
    // full, which this Mac has no Cloud route for.
    if (e && e.code === "cloud_not_carried") return nextWord("cloudNotCarried")
    if (e && e.code === "write_disabled") return T().webStartOff
    if (e && e.code === "not_found") return T().webResumeGone
    if (e && e.app && e.code === "terminal_closed") {
      return fill(T().webStartTerminalClosed, { app: e.app })
    }
    if (e && e.code === "terminal_unsupported") {
      return T().webStartTerminalUnsupported
    }
    return ""
  }

  function draw(): void {
    ensureRunNowButton()
    const runs = (record && record.runs) || []
    el("schedule-history-sheet").setAttribute("aria-busy", loading || pressing || runningNow ? "true" : "false")
    el("schedule-history-title").textContent = (record && record.title) || T().webScheduleEdit
    const path = record && record.task && record.task.project_dir
    const next = record && record.next_fire ? new Date(record.next_fire * 1000).toLocaleString() : ""
    el("schedule-history-meta").textContent = [path, next].filter(Boolean).join(" · ")
    el("schedule-history-runs-label").textContent = T().webResumePick
    el("schedule-run-rows").innerHTML = scheduleRunsHTML(runs, Date.now() / 1000, terminalIsOpen)
    el("schedule-history-empty").textContent = T().webResumeEmpty
    el("schedule-history-empty").hidden = loading || runs.length > 0 || !!el("schedule-history-said").textContent
    el("schedule-history-capped").textContent = T().webResumeCapped
    el("schedule-history-capped").hidden = !(record && record.runs_may_be_truncated)
    el("schedule-history-edit").textContent = T().webScheduleEdit
    el<HTMLButtonElement>("schedule-history-edit").disabled = loading || !!pressing || runningNow || !record
    const runNowButton = document.getElementById("schedule-history-run-now") as HTMLButtonElement | null
    if (runNowButton) {
      runNowButton.textContent = runningNow ? scheduleRunWords().running : scheduleRunWords().button
      runNowButton.disabled = loading || !!pressing || runningNow || !record
    }
    el("schedule-history-close").textContent = T().webClose
    el<HTMLButtonElement>("schedule-history-close").disabled = !!pressing || runningNow

    const buttons = el("schedule-run-rows").querySelectorAll<HTMLButtonElement>(".schedule-run-button")
    for (let i = 0; i < buttons.length; i++) {
      buttons[i].disabled =
        buttons[i].disabled ||
        !!pressing ||
        runningNow ||
        (buttons[i].dataset.action === "resume" && !projectPlace(run(buttons[i].dataset.taskId)))
      if (pressing && buttons[i].dataset.taskId === pressing) {
        buttons[i].querySelector(".schedule-run-action")!.textContent = T().webResuming
      }
    }
  }

  function open(id: string | undefined): void {
    if (!id || !el("schedule-history").hidden) return
    scheduleId = id
    record = null
    places = []
    placesNote = ""
    pressing = null
    runningNow = false
    webhookOpen = false
    loading = true
    el("schedule-history-said").textContent = ""
    el("schedule-history").hidden = false
    draw()
    el("schedule-history-close").focus({ preventScroll: true })
    const mine = ++ticket
    const detail = scheduleApi.schedule(id)
    const availablePlaces = scheduleApi.places().catch(() => ({ places: [] }) as SchedulePlaces)
    Promise.all([detail, availablePlaces])
      .then((answers) => {
        if (mine !== ticket || scheduleId !== id) return
        record = (answers[0] && answers[0].schedule) || null
        places = (answers[1] && answers[1].places) || []
        placesNote = unansweredSentence(answers[1])
        if (placesNote && ((record && record.runs) || []).some((candidate) => candidate && candidate.session_id)) {
          el("schedule-history-said").textContent = placesNote
        }
        loading = false
        draw()
        loadWebhook()
      })
      .catch((e: ScheduleFailureLike) => {
        if (mine !== ticket || scheduleId !== id) return
        loading = false
        el("schedule-history-said").textContent = why(e)
        draw()
      })
  }

  function close(force?: boolean): void {
    if ((pressing || runningNow) && !force) return
    ticket += 1
    scheduleId = null
    record = null
    places = []
    placesNote = ""
    loading = false
    pressing = null
    runningNow = false
    hook = null
    deliveries.length = 0
    ephemeralURL = null
    effectiveTier = null
    for (const key of Object.keys(observedReceipts)) delete observedReceipts[key]
    helpOpen = false
    webhookOpen = false
    el("schedule-history").hidden = true
  }

  function edit(): void {
    if (loading || pressing || runningNow || !record) return
    const id = scheduleId!
    close(true)
    Schedule.openEdit(id)
  }

  function runNow(): void {
    if (loading || pressing || runningNow || !record) return
    if (!write) {
      el("schedule-history-said").textContent = scheduleRunWords().writeOff
      return
    }
    if (!window.confirm(scheduleRunConfirmation(record.title || scheduleId || "", lang()))) return

    const id = scheduleId!
    runningNow = true
    el("schedule-history-said").textContent = ""
    draw()
    scheduleApi
      .runSchedule(id)
      .then(() => {
        if (scheduleId !== id) return
        runningNow = false
        el("schedule-history-said").textContent = scheduleRunWords().accepted
        draw()
        Schedules.refresh()
        return scheduleApi.schedule(id).then(
          (answer) => {
            if (scheduleId !== id) return
            record = (answer && answer.schedule) || record
            draw()
          },
          () => {},
        )
      })
      .catch((error: ScheduleFailureLike) => {
        if (scheduleId !== id) return
        runningNow = false
        if (error && error.code === "write_disabled") write = false
        // `scheduleRunMessage` is the Swift app's (`js/input/schedule-run.js`,
        // copied byte for byte and so not correctable there): it names five
        // codes and answers every other one with "無法啟動這個排程。" That was
        // the sentence a `machine_offline` or a `rate_limited` press got. The
        // five keep their words — they are better than the catalog's — and the
        // rest now reach the catalog, which names them and tags them.
        el("schedule-history-said").textContent = runSentence(error)
        draw()
      })
  }

  /** `Start.began`'s last step: open the resumed session once its row has arrived. */
  function openWhenArrived(id: string, tries = 0): void {
    if (L.byId(id)) {
      host.open(id)
      return
    }
    if (tries >= 60) return
    setTimeout(() => openWhenArrived(id, tries + 1), 500)
  }

  function pick(taskId: string | undefined, action: string | undefined): void {
    if (loading || pressing || runningNow) return
    const selected = run(taskId)
    if (!selected) return

    if (action === "open") {
      const live = selected.terminal_id && L.byId(selected.terminal_id)
      if (!live) {
        draw()
        return
      }
      close(true)
      host.open(live.id)
      return
    }

    if (action !== "resume" || !selected.session_id) return
    if (!write) {
      el("schedule-history-said").textContent = T().webStartOff
      return
    }
    const place = projectPlace(selected)
    if (!place) {
      el("schedule-history-said").textContent = !place && placesNote ? placesNote : T().webRequestFailed
      return
    }
    pressing = taskId!
    el("schedule-history-said").textContent = ""
    draw()
    scheduleApi
      .resumePlace(place.id, selected.session_id, selected.assistant)
      .then((answer) => {
        pressing = null
        close(true)
        if (answer && answer.id) openWhenArrived(answer.id)
      })
      .catch((e: ScheduleFailureLike) => {
        pressing = null
        if (e && e.code === "write_disabled") write = false
        el("schedule-history-said").textContent = why(e)
        draw()
      })
  }

  return { open, close, edit, pick, runNow, webhookAction }
})()

/* ---- view/static.js: the form's words that never change while it is open ---- */

let paintedCatalog = ""

function paintStatic(): void {
  const t = T()
  const signature = [t.webScheduleNew, t.webScheduleTitle, t.webScheduleModel, t.webScheduleDelete, t.webCancel].join("")
  if (signature === paintedCatalog) return
  paintedCatalog = signature
  const text = (node: Element | null, value: string | undefined) => {
    if (node && typeof value === "string") node.textContent = value
  }
  const attr = (node: Element | null, name: string, value: string | undefined) => {
    if (node && typeof value === "string") node.setAttribute(name, value)
  }
  const node = (id: string) => document.getElementById(id)
  attr(node("schedule-new"), "title", t.webScheduleNew)
  attr(node("schedule-new"), "aria-label", t.webScheduleNew)
  // What `reset` and `openEdit` rewrite on every open is only painted under a closed sheet.
  if (node("schedule-form")?.hidden !== false) {
    text(node("schedule-form-title"), t.webScheduleNew)
    text(node("schedule-form-say"), t.webScheduleNewSay)
    text(node("schedule-go"), t.webScheduleCreate)
  }
  text(node("schedule-title-label"), t.webScheduleTitle)
  attr(node("schedule-title"), "aria-label", t.webScheduleTitle)
  text(node("schedule-at-label"), t.webScheduleAt)
  attr(node("schedule-at"), "aria-label", t.webScheduleAt)
  text(node("schedule-days-label"), t.webScheduleOn)
  attr(node("schedule-days"), "aria-label", t.webScheduleOn)
  text(node("schedule-where-label"), t.webScheduleWhere)
  text(node("schedule-with-label"), t.webScheduleWith)
  attr(node("schedule-places"), "aria-label", t.webScheduleWhere)
  text(node("schedule-first-label"), t.webScheduleFirst)
  attr(node("schedule-instructions"), "aria-label", t.webScheduleFirst)
  text(node("schedule-more-label"), t.webScheduleMore)
  text(node("schedule-close-label"), t.webScheduleWhenDone)
  attr(node("schedule-close"), "aria-label", t.webScheduleWhenDone)
  text(node("schedule-catch-label") && node("schedule-catch-label")!.querySelector(".field-label"), t.webScheduleCatchUp)
  text(
    node("schedule-timeout-label") && node("schedule-timeout-label")!.querySelector(".field-label"),
    t.webScheduleTimeout,
  )
  text(node("schedule-model-label"), t.webScheduleModel)
  text(node("schedule-delete"), t.webScheduleDelete)
  text(node("schedule-cancel"), t.webCancel)
  text(node("schedule-delete-confirm-cancel"), t.webCancel)
}

/* ---- the three sheets, put in once, and their wiring ---------------------- */

let overlaysBound = false

/** Tab from the last of `items` wraps to the first, and back (`input/action-confirm.js`'s trap). */
function trap(ev: KeyboardEvent, items: (HTMLElement | null)[]): void {
  const at = items.indexOf(document.activeElement as HTMLElement)
  if (at < 0) return
  if ((!ev.shiftKey && at === items.length - 1) || (ev.shiftKey && at <= 0)) {
    ev.preventDefault()
    items[ev.shiftKey ? items.length - 1 : 0]!.focus()
  }
}

function ensureOverlays(): void {
  if (overlaysBound) return
  if (!document.getElementById("schedule-form")) {
    const root = document.getElementById("root")
    if (root) root.insertAdjacentHTML("afterend", overlaysMarkup)
    else document.body.insertAdjacentHTML("beforeend", overlaysMarkup)
  }
  if (!document.getElementById("schedule-form")) return
  overlaysBound = true

  // input/schedule.js's wiring.
  el("schedule-form").addEventListener("click", () => Schedule.close())
  el("schedule-sheet").addEventListener("click", (ev) => ev.stopPropagation())
  el("schedule-cancel").addEventListener("click", () => Schedule.close())
  el("schedule-go").addEventListener("click", () => Schedule.create())
  el("schedule-picked").addEventListener("click", () => Schedule.toggle())
  el("schedule-places").addEventListener("click", (ev) => {
    const row = (ev.target as Element).closest ? ((ev.target as Element).closest(".place") as HTMLButtonElement | null) : null
    if (!row || row.disabled) return
    Schedule.pick(row.dataset.id || "")
  })
  el("schedule-title").addEventListener("input", () => {
    el("schedule-said").textContent = ""
  })
  el("schedule-at").addEventListener("input", () => {
    el("schedule-said").textContent = ""
  })
  el("schedule-form").addEventListener("keydown", (ev) => {
    if (ev.key !== "Tab") return
    trap(ev, [el("schedule-cancel"), el("schedule-go")])
  })
  // `input/keys.js`'s answer while this sheet is open, given here because `App`'s
  // chain does not know it: Escape closes it (refused while a request is in
  // flight, by `close` itself), and no list key reaches the page behind it. ⌘
  // shortcuts are answered before the sheet check there and still go on.
  el("schedule-form").addEventListener("keydown", (ev) => {
    if (ev.metaKey || ev.ctrlKey || ev.altKey) return
    if (ev.key === "Escape") Schedule.close()
    ev.stopPropagation()
  })
  el("schedule-delete").addEventListener("click", () => Schedule.askDelete())
  el("schedule-delete-confirm").addEventListener("click", () => Schedule.closeDeleteConfirm(true))
  el("schedule-delete-confirm-sheet").addEventListener("click", (ev) => ev.stopPropagation())
  el("schedule-delete-confirm-cancel").addEventListener("click", () => Schedule.closeDeleteConfirm(true))
  el("schedule-delete-confirm-go").addEventListener("click", () => Schedule.confirmDelete())
  el("schedule-delete-confirm").addEventListener("keydown", (ev) => {
    if (ev.key !== "Tab") return
    trap(ev, [el("schedule-delete-confirm-cancel"), el("schedule-delete-confirm-go")])
  })
  document.addEventListener(
    "keydown",
    (ev) => {
      if (ev.key !== "Escape" || el("schedule-delete-confirm").hidden) return
      ev.preventDefault()
      ev.stopPropagation()
      Schedule.closeDeleteConfirm(true)
    },
    true,
  )

  // input/schedule-history.js's wiring, the sheet half.
  el("schedule-history").addEventListener("click", () => ScheduleHistory.close())
  el("schedule-history-sheet").addEventListener("click", (ev) => ev.stopPropagation())
  el("schedule-history-close").addEventListener("click", () => ScheduleHistory.close())
  el("schedule-history-edit").addEventListener("click", () => ScheduleHistory.edit())
  ;["generate", "copy", "rotate", "disable"].forEach((action) => {
    const button = document.getElementById("schedule-webhook-" + action)
    if (button) button.addEventListener("click", () => ScheduleHistory.webhookAction(action))
  })
  el("schedule-run-rows").addEventListener("click", (ev) => {
    const button = (ev.target as Element).closest
      ? ((ev.target as Element).closest(".schedule-run-button") as HTMLButtonElement | null)
      : null
    if (!button || button.disabled) return
    ScheduleHistory.pick(button.dataset.taskId, button.dataset.action)
  })
  el("schedule-history").addEventListener("keydown", (ev) => {
    if (ev.key !== "Tab") return
    const items = [document.getElementById("schedule-history-run-now"), el("schedule-history-edit")]
      .concat(
        Array.from(
          el("schedule-history-sheet").querySelectorAll<HTMLElement>("#schedule-webhook button:not([hidden]):not([disabled])"),
        ),
      )
      .concat(Array.from(el("schedule-run-rows").querySelectorAll<HTMLElement>(".schedule-run-button:not([disabled])")))
      .concat([el("schedule-history-close")])
      .filter((item): item is HTMLElement => !!item && !(item as HTMLButtonElement).disabled)
    const at = items.indexOf(document.activeElement as HTMLElement)
    if (at < 0 || !items.length) return
    if ((!ev.shiftKey && at === items.length - 1) || (ev.shiftKey && at === 0)) {
      ev.preventDefault()
      items[ev.shiftKey ? items.length - 1 : 0].focus()
    }
  })
  document.addEventListener(
    "keydown",
    (ev) => {
      if (ev.key !== "Escape" || el("schedule-history").hidden) return
      ev.preventDefault()
      ev.stopPropagation()
      ScheduleHistory.close()
    },
    true,
  )
}

/** The section's own two listeners: `#schedule-new`, and a row opening its runs. */
function bindSection(): () => void {
  const add = document.getElementById("schedule-new")
  const rows = document.getElementById("schedule-rows")
  if (!add || !rows) return () => {}
  const onAdd = (ev: Event) => {
    // Inside the <summary>: its click must not reach the disclosure.
    ev.stopPropagation()
    Schedule.open()
  }
  const onRowClick = (ev: Event) => {
    const target = ev.target as Element
    const row = target.closest ? (target.closest(".schedule-row[data-id]") as HTMLElement | null) : null
    if (row) ScheduleHistory.open(row.dataset.id)
  }
  const onRowKey = (ev: KeyboardEvent) => {
    if (ev.key !== "Enter" && ev.key !== " ") return
    const target = ev.target as Element
    const row = target.closest ? (target.closest(".schedule-row[data-id]") as HTMLElement | null) : null
    if (!row) return
    ev.preventDefault()
    ScheduleHistory.open(row.dataset.id)
  }
  add.addEventListener("click", onAdd)
  rows.addEventListener("click", onRowClick)
  rows.addEventListener("keydown", onRowKey)
  return () => {
    add.removeEventListener("click", onAdd)
    rows.removeEventListener("click", onRowClick)
    rows.removeEventListener("keydown", onRowKey)
  }
}

/**
 * `details#schedules`, `index.html` 292–312. Hidden until the first answer;
 * then `renderSchedules` decides. "Schedules" and the list's name are English
 * in the original's markup and nothing paints them; `#schedule-new`'s title
 * and label are `static.js`'s.
 */
export function ScheduleSection({ arrived, onOpen }: { arrived: boolean; onOpen?: (id: string) => void }) {
  arrivedFlag = arrived
  const openRef = useRef(onOpen)
  openRef.current = onOpen

  useLayoutEffect(() => {
    ensureOverlays()
    host.open = (id) => openRef.current?.(id)
    paintStatic()
    return bindSection()
  }, [])

  // `static.js` again whenever the catalog has changed.
  useEffect(() => {
    paintStatic()
  })

  useEffect(() => {
    if (arrived) Schedules.start()
  }, [arrived])

  return (
    <details className="schedules" id="schedules" open hidden>
      <summary>
        <span>{nextWord("schedulesHeading")}</span>
        <span className="count" id="schedules-count"></span>
        <button className="add" id="schedule-new" type="button" title={T().webScheduleNew} aria-label={T().webScheduleNew}>
          <svg viewBox="0 0 14 14" aria-hidden="true" focusable="false">
            <path d="M7 2.6v8.8M2.6 7h8.8" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round"></path>
          </svg>
        </button>
      </summary>
      <ul className="schedule-rows" id="schedule-rows" aria-label={nextWord("schedulesListLabel")}></ul>
    </details>
  )
}
