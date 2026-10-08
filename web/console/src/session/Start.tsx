import { useEffect, useRef } from "react"
import * as L from "../legacy/bridge.js"
import { nextWord } from "../next-strings.js"
import { loadPersonas, personaById, personasNow, rememberPersona, rememberTeam, rememberedPersona, rememberedTeam, switchTeam } from "../personas.js"
import { drawRoleRow } from "./RoleRow.js"
import "./persona.css"
import "./machine-start.css"
import { hostedConsole, listTerminals, openTerminal, readTerminalMachine, TerminalRequestError } from "../pages/terminal/api.js"
import { firstSize } from "../pages/terminal/TerminalProjectList.js"
import { openNewTerminal, openTerminalPage } from "../pages/terminal/navigate.js"
import { TAB } from "../pages/terminal/tab.js"
import { terminalRefusalWords, unavailableWords } from "../pages/terminal/words.js"

/**
 * Starting a session, and picking one back up — `input/start.js`, function for
 * function, against the `div#start` sheet below and the `div#starting` band in
 * ./Starting.tsx. The copy it follows is `legacy/js/input/start.js`; see
 * `legacy/start-bridge.ts` for why it is followed rather than imported.
 *
 * It is imperative because the original is: every row, chip and sentence is
 * written into markup React draws once and never redraws, so there is nothing
 * for the two to fight over. React draws only the words that come with the
 * catalog, which `view/static.js` writes in the original.
 *
 * Where this page is not the original's, and why:
 *
 * - **No machine row.** The transport has no `machines` read (no Cloud), so the
 *   row stays hidden, as it does on the Mac's own page.
 * - **A separate machine action.** The machine dashboard opens a focused
 *   Clawdfather sheet; ordinary Project starts never offer it.
 * - **Write is assumed until refused.** This daemon's health carries no `write`
 *   flag; a `write_disabled` refusal turns it off, as the composer does.
 * - **The arriving row is found by its terminal id.** The original matches the
 *   exact selection key, which is minted before the row exists and so carries
 *   no conversation id. This daemon lists only assistant rows, and those carry
 *   the conversation id from the moment they appear, so the exact key would
 *   never match and every start would read as late. `byId` with the bare id is
 *   the original's own fallback for an unambiguous row.
 * - **A role row** (docs/personas.md), which the original never had: the
 *   machine's persona catalog as chips under the assistant chips, "No role"
 *   first and chosen until the person picks another. The last choice is
 *   remembered in this browser. The row is not drawn when the catalog is empty
 *   — an older daemon, or a machine on Cloud that does not offer `personas`.
 *   Resuming draws the same row over a place's past conversations, for the
 *   resume route's `/as/{persona}`. It starts at "No role" each time a place
 *   is entered and is not remembered: a past conversation's list does not say
 *   which role it had, and a new start's habit is not a claim about an old
 *   conversation. While the places are listed for a resume the row stands
 *   down, since nothing opens from that step.
 * - **Moving the highlight without opening** (a late arrival, or one after the
 *   person moved on) has no seam on this page, so that case leaves the list as
 *   it is.
 */

type Place = L.StartPlaceRow
type PastRow = L.PastRow
type Failure = L.StartFailure

interface StartHost {
  /** `openSession`: open that row. */
  open(id: string): void
  /** `SessionSelection.snapshot().open`: the row on screen, or null. */
  openId(): string | null
  /** Read the list now rather than on the stream's next beat. */
  refresh(): void
}

const HOLD = 15000 // how long the band waits before it admits it has stopped waiting
const MANY = 8 // places, past which a box to filter them earns its row
const MACHINE_PLACE = "@machine"

const api = L.startApi

let host: StartHost = { open: () => {}, openId: () => null, refresh: () => {} }
let write = true // `S.write`

let places: Place[] | null = null
let assistants: L.StartAssistantRow[] = []
let with_: string | null = null
/** The persona chosen for the next start; "" for none. */
let persona = ""
/** The team the role row shows when no persona is chosen. */
let team = ""
/** The persona chosen for the next resume from the place entered; "" for none. */
let resumeAs = ""
let loading = false
let pressing: string | null = null
let find = ""
let resume = false
let at: Place | null = null
let pasts: PastRow[] | null = null
/** The past list was asked for and not read: it is not empty, it is unknown, and the sheet must not say "no records". */
let pastsUnread = false
let capped = false
let reading = false
let wait: { id: string; from: string | null; late: boolean; place: Place } | null = null
let detached: string | null = null
let timer: ReturnType<typeof setTimeout> | null = null
let placesGeneration = 0
// Held at the placeholder's position through the first following list, so the
// arrival replaces the placeholder in place before ordinary ordering resumes.
let landed: string | null = null
let opener: HTMLElement | null = null
let machineOnly = false
type StartMode = "session" | "terminal"
let mode: StartMode = "session"
let terminalAccess: { state: "loading" | "ready" | "blocked"; reason: string } = { state: "loading", reason: "" }
let terminalGeneration = 0

function checkTerminalAccess(): void {
  const generation = ++terminalGeneration
  if (hostedConsole()) {
    // The Cloud terminal page checks its own E2E grant before exposing input.
    terminalAccess = { state: "ready", reason: "" }
    draw()
    return
  }
  terminalAccess = { state: "loading", reason: "" }
  draw()
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 4000)
  Promise.allSettled([readTerminalMachine(controller.signal), listTerminals("", TAB, controller.signal)]).then(([machine, list]) => {
    clearTimeout(timeout)
    if (generation !== terminalGeneration || sheetHidden()) return
    const unavailable = machine.status === "fulfilled" ? unavailableWords(machine.value.capability) : nextWord("terminalUnavailableUnknown")
    const refusal = list.status === "rejected" ? list.reason : null
    const reason = refusal instanceof TerminalRequestError ? terminalRefusalWords(refusal.code) : refusal ? nextWord("terminalUnavailableUnknown") : unavailable
    terminalAccess = reason ? { state: "blocked", reason } : { state: "ready", reason: "" }
    draw()
  })
}

function setMode(next: StartMode): void {
  if (pressing || wait || machineOnly) return
  mode = next
  leave()
  if (next === "terminal") checkTerminalAccess()
  else {
    void loadPersonas().then(() => draw())
    draw()
  }
}

/* The list redraws when the wait changes: `renderList()` in the original. */
let version = 0
const listeners = new Set<() => void>()
function renderList(): void {
  version += 1
  listeners.forEach((fn) => fn())
}

/** `Waiting` (`view/waits.js`) for `Waits.startPress`: shown after 150ms, kept 320ms. */
const startPress = {
  visible: false,
  shown: 0,
  timer: null as ReturnType<typeof setTimeout> | null,
  start() {
    if (this.visible || this.timer) return
    this.timer = setTimeout(() => {
      this.timer = null
      this.visible = true
      this.shown = Date.now()
      draw()
    }, 150)
  },
  settle(then: () => void) {
    if (this.timer) clearTimeout(this.timer)
    this.timer = null
    const finish = () => {
      this.visible = false
      then()
    }
    if (!this.visible) {
      finish()
      return
    }
    const left = 320 - (Date.now() - this.shown)
    if (left <= 0) {
      finish()
      return
    }
    setTimeout(finish, left)
  },
}

const el = <E extends HTMLElement = HTMLElement>(id: string) => document.getElementById(id) as E
const T = () => L.strings

function sheetHidden(): boolean {
  return el("start")?.hidden !== false
}

function say(words: string): void {
  el("start-say").textContent = words || ""
}
function said(words: string, error?: unknown): void {
  const node = el("start-said")
  node.textContent = words || ""
  L.bindFailureLine(node, error || null)
}

function asked<A>(request: () => Promise<A>): Promise<A> {
  try {
    return Promise.resolve(request())
  } catch (e) {
    return Promise.reject(e)
  }
}

function why(e: Failure, reading?: "past"): string {
  return L.failureSentence(e, { sentence: ownWhy(e, reading), fallback: T().webStartFailed })
}

function ownWhy(e: Failure, reading?: "past"): string {
  const code = e && e.code
  // A Mac across Clawdline Cloud that does not list `past-sessions` among its
  // commands — the Go daemon today — is refused that read by the client before
  // it leaves. Said here, where it happens, with what can be done instead.
  if (reading === "past" && (code === "cloud_feature_unavailable" || code === "cloud_machine_unsupported")) {
    return nextWord("cloudPastUnavailable")
  }
  if (code === "cloud_not_carried") return nextWord("cloudNotCarried")
  if (code === "write_disabled") return T().webStartOff
  if (code === "coordinator_online") return nextWord("machineSessionOnline")
  if (code === "coordinator_liveness_unknown") return nextWord("machineSessionUnknown")
  if (code === "coordinator_session_exists") return nextWord("machineSessionExists")
  if (code === "machine_workspace_invalid") return nextWord("machineWorkspaceInvalid")
  if (code === "coordinator_store_invalid") return nextWord("machineRoleInvalid")
  if (code === "not_found") return T().webStartGone
  if (e && e.app && code === "terminal_closed") return L.fillString(T().webStartTerminalClosed, { app: e.app })
  if (code === "terminal_unsupported") return T().webStartTerminalUnsupported
  return ""
}

function matching(): Place[] {
  const q = find.trim().toLowerCase()
  return (places || []).filter((p) => {
    if (!q) return true
    return ((p.label || "") + " " + (p.path || "")).toLowerCase().indexOf(q) >= 0
  })
}

function matchingPast(): PastRow[] {
  const q = find.trim().toLowerCase()
  return (pasts || []).filter((r) => {
    if (!q) return true
    return (r.title || "").toLowerCase().indexOf(q) >= 0
  })
}

function resumable(): boolean {
  return (with_ === "claude" || with_ === "codex") && typeof api.pastSessions === "function"
}

function when(unix: number | undefined): string {
  if (!unix) return ""
  const then = new Date(unix * 1000)
  const now = new Date()
  const sameDay =
    then.getFullYear() === now.getFullYear() && then.getMonth() === now.getMonth() && then.getDate() === now.getDate()
  if (sameDay) return L.clock(unix)
  try {
    return then.toLocaleDateString(undefined, { month: "short", day: "numeric" })
  } catch {
    return L.clock(unix)
  }
}

function drawWith(): void {
  const row = el("start-with")
  // No `machines` read on this transport, so the first half is always false.
  row.hidden = assistants.length < 2 || !!at
  if (row.hidden) {
    row.innerHTML = ""
    return
  }
  row.innerHTML = ""
  const label = document.createElement("span")
  label.className = "with-label"
  label.textContent = T().webStartWith
  row.appendChild(label)
  assistants.forEach((a) => {
    const chip = document.createElement("button")
    chip.type = "button"
    chip.className = "chip" + (a.id === with_ ? " on" : "")
    chip.textContent = a.label || a.id
    chip.disabled = !!pressing || !!wait
    chip.setAttribute("aria-pressed", a.id === with_ ? "true" : "false")
    chip.onclick = () => {
      with_ = a.id
      if (!resumable()) leave()
      draw()
    }
    row.appendChild(chip)
  })
}

/** The chosen persona, when the catalog still names it; "" for none. */
function chosenPersona(): string {
  const id = at ? resumeAs : persona
  return personaById(personasNow(), id) ? id : ""
}

/** The role chips; see the header. */
function drawPersona(): void {
  const row = el("start-persona")
  const catalog = personasNow() ?? []
  // Every draw rebuilds the chips; the sideways scroll is kept across it, so a
  // press on a chip the person scrolled to does not throw the row back.
  const scrolled = row.scrollLeft
  row.hidden = !catalog.length || (!at && resume && resumable())
  row.innerHTML = ""
  if (row.hidden) return
  const choose = (id: string) => {
    if (at) {
      resumeAs = id
    } else {
      persona = id
      rememberPersona(id)
    }
  }
  drawRoleRow(row, {
    personas: catalog,
    chosen: chosenPersona(),
    team,
    disabled: !!pressing || !!wait,
    press: "pressed",
    onPick: (id) => {
      choose(id)
      draw()
    },
    onTeam: (next) => {
      const switched = switchTeam(catalog, chosenPersona(), next)
      team = next
      rememberTeam(next)
      choose(switched.chosen)
      draw()
    },
  })
  const on = row.querySelector<HTMLElement>(".chip.on")
  // A remembered choice past the edge is brought into view when the sheet opens.
  row.scrollLeft = scrolled
  if (!scrolled && on) {
    const edge = row.getBoundingClientRect()
    const chip = on.getBoundingClientRect()
    if (chip.right > edge.right) row.scrollLeft = chip.left - edge.left - (edge.width - chip.width) / 2
  }
}

/** `drawMachines`: this transport has no machine read, so the row is hidden. */
function drawMachines(): void {
  const row = el("start-machine")
  row.hidden = true
  row.innerHTML = ""
}

function drawResume(): void {
  const row = el("start-resume")
  row.innerHTML = ""
  row.hidden = !write
  if (row.hidden) return

  if (at) {
    const back = document.createElement("button")
    back.type = "button"
    back.className = "chip back"
    back.innerHTML = '<span class="arrow">←</span><canvas></canvas>' + '<span class="name"></span>'
    const mark = back.querySelector("canvas") as HTMLCanvasElement
    if (!L.drawIcon(mark, at.icon, 4)) mark.classList.add("none")
    const name = back.querySelector(".name") as HTMLElement
    name.textContent = at.label || L.path(at.path)
    name.style.color = at.icon ? L.accentTint(at.icon.accent) : ""
    back.setAttribute("aria-label", T().webResumeBack)
    back.disabled = !!pressing || !!wait
    back.onclick = () => {
      leave()
      draw()
    }
    row.appendChild(back)
    return
  }

  if (typeof api.pastSessions === "function") {
    const chip = document.createElement("button")
    chip.type = "button"
    chip.className = "chip check" + (resume && resumable() ? " on" : "")
    chip.innerHTML =
      '<svg class="tick" viewBox="0 0 14 14" aria-hidden="true"' +
      ' focusable="false">' +
      '<rect class="box" x="0.5" y="0.5" width="13" height="13" rx="3.5"></rect>' +
      '<path class="mark" d="M3.6 7.1 5.9 9.4 10.4 4.6"' +
      ' stroke-linecap="round" stroke-linejoin="round"></path></svg>' +
      '<span class="label"></span>'
    ;(chip.querySelector(".label") as HTMLElement).textContent = T().webResumeWith
    chip.disabled = !!pressing || !!wait || !resumable()
    chip.setAttribute("aria-pressed", resume && resumable() ? "true" : "false")
    chip.onclick = () => {
      resume = !resume
      draw()
    }
    row.appendChild(chip)
  }

}

function edge(): void {
  const list = el("start-list")
  const more = list.scrollHeight - list.scrollTop - list.clientHeight > 2
  list.dataset.more = more ? "1" : "0"
}

function draw(): void {
  L.setStartSpin(null)
  if (sheetHidden()) return
  const list = el("start-list")
  const box = el<HTMLInputElement>("start-filter")
  const machineAction = el<HTMLButtonElement>("start-machine-action")
  const tabs = el("start-mode-tabs")
  tabs.hidden = machineOnly
  const panel = el("start-mode-panel")
  panel.setAttribute("role", machineOnly ? "group" : "tabpanel")
  panel.setAttribute("aria-labelledby", machineOnly ? "start-title" : mode === "session" ? "start-session-tab" : "start-terminal-tab")
  panel.tabIndex = machineOnly ? -1 : 0
  el("start-session-tab").setAttribute("aria-selected", mode === "session" ? "true" : "false")
  el("start-terminal-tab").setAttribute("aria-selected", mode === "terminal" ? "true" : "false")
  el("start-session-tab").tabIndex = mode === "session" ? 0 : -1
  el("start-terminal-tab").tabIndex = mode === "terminal" ? 0 : -1
  el("start-title").textContent = machineOnly ? "Clawdfather" : mode === "terminal" ? nextWord("terminalStartTitle") : T().webStart
  machineAction.hidden = !machineOnly
  machineAction.disabled = !!pressing || !!wait || !with_ || !write
  machineAction.querySelector(".label")!.textContent =
    pressing === MACHINE_PLACE ? T().webStarting : nextWord("machineSessionStart")

  if (!write && mode === "session") {
    say(T().webStartOff)
    box.hidden = true
    el("start-with").hidden = true
    el("start-persona").hidden = true
    el("start-machine").hidden = true
    el("start-resume").hidden = true
    list.innerHTML = ""
    return
  }

  if (machineOnly) {
    drawMachines()
    drawWith()
    el("start-persona").hidden = true
    el("start-resume").hidden = true
    el("start-resume").innerHTML = ""
    box.hidden = true
    list.innerHTML = ""
    say(wait ? T().webStartWaiting : loading ? T().webLoading : nextWord("machineSessionScope"))
    return
  }

  if (mode === "terminal") {
    el("start-machine").hidden = true
    el("start-with").hidden = true
    el("start-persona").hidden = true
    el("start-resume").hidden = true
    el("start-resume").innerHTML = ""
    box.hidden = terminalAccess.state !== "ready" || !(places && places.length > MANY)
    box.placeholder = T().webStartFilter
    box.setAttribute("aria-label", T().webStartFilter)
    if (terminalAccess.state !== "ready") {
      list.innerHTML = ""
      say(terminalAccess.state === "loading" ? T().webLoading : terminalAccess.reason)
      return
    }
  } else {
    drawMachines()
    drawWith()
    drawPersona()
    drawResume()
  }

  if (at) {
    drawPast(list, box)
    return
  }

  box.placeholder = T().webStartFilter
  box.setAttribute("aria-label", T().webStartFilter)

  say(
    wait
      ? T().webStartWaiting
      : loading && !places
        ? T().webLoading
        : places && !places.length
          ? T().webStartEmpty
          : T().webStartPick,
  )

  box.hidden = terminalAccess.state !== "ready" && mode === "terminal" || !(places && places.length > MANY)
  if (box.hidden && box.value) {
    box.value = ""
    find = ""
  }

  list.innerHTML = ""
  matching().forEach((p) => {
    const li = document.createElement("li")
    const row = document.createElement("button")
    row.type = "button"
    row.className = "place"
    row.dataset.id = p.id
    row.disabled = !!pressing || !!wait
    if (pressing === p.id) row.dataset.busy = "1"
    row.innerHTML = '<canvas></canvas><span class="name"></span><span class="where"></span>'

    const mark = row.querySelector("canvas") as HTMLCanvasElement
    if (!L.drawIcon(mark, p.icon, 4)) mark.classList.add("none")

    const name = row.querySelector(".name") as HTMLElement
    name.textContent = p.label || p.path || ""
    name.style.color = p.icon ? L.accentTint(p.icon.accent) : ""
    const where = row.querySelector(".where") as HTMLElement
    if (pressing === p.id && startPress.visible) {
      where.innerHTML = '<canvas class="start-spin"></canvas><span></span>'
      ;(where.querySelector("span") as HTMLElement).textContent = T().webStarting
      L.setStartSpin(where.querySelector(".start-spin"))
      L.drawSpinner(L.spinClock.start(), L.spinClock.phase())
    } else {
      where.textContent = pressing === p.id ? T().webStarting : L.path(p.path)
    }

    li.appendChild(row)
    list.appendChild(li)
  })
  edge()
}

function drawPast(list: HTMLElement, box: HTMLInputElement): void {
  box.placeholder = T().webResumeFilter
  box.setAttribute("aria-label", T().webResumeFilter)

  say(
    wait
      ? T().webStartWaiting
      : reading && !pasts
        ? T().webLoading
        : pastsUnread
          ? ""
          : pasts && !pasts.length
            ? T().webResumeEmpty
            : T().webResumePick,
  )

  box.hidden = !(pasts && pasts.length > 1)
  if (box.hidden && box.value) {
    box.value = ""
    find = ""
  }

  list.innerHTML = ""
  const all = matchingPast()
  all.forEach((r) => {
    const li = document.createElement("li")
    const row = document.createElement("button")
    row.type = "button"
    row.className = "place past"
    row.dataset.session = r.id
    const open = r.live ? L.bySessionId(r.id) : null
    row.disabled = !!pressing || !!wait || (!!r.live && !open)
    if (pressing === r.id) row.dataset.busy = "1"
    row.innerHTML = '<span class="name"></span><span class="where"></span>'

    ;(row.querySelector(".name") as HTMLElement).textContent = r.title
    const where = row.querySelector(".where") as HTMLElement
    if (pressing === r.id && startPress.visible) {
      where.innerHTML = '<canvas class="start-spin"></canvas><span></span>'
      ;(where.querySelector("span") as HTMLElement).textContent = T().webResuming
      L.setStartSpin(where.querySelector(".start-spin"))
      L.drawSpinner(L.spinClock.start(), L.spinClock.phase())
    } else if (pressing === r.id) {
      where.textContent = T().webResuming
    } else if (r.live) {
      row.dataset.live = "1"
      where.textContent = T().webResumeLive
    } else {
      where.textContent = when(r.at)
    }

    li.appendChild(row)
    list.appendChild(li)
  })

  if (capped && all.length) {
    const note = document.createElement("li")
    note.className = "note"
    note.setAttribute("role", "status")
    note.textContent = T().webResumeCapped
    list.appendChild(note)
  }
  edge()
}

function load(): void {
  if (loading || typeof api.places !== "function") return
  const generation = ++placesGeneration
  loading = true
  draw()
  asked(() => api.places())
    .then((d) => {
      if (generation !== placesGeneration) return
      places = (d && d.places) || []
      assistants = (d && d.assistants) || []
      if (!with_ || !assistants.some((a) => a.id === with_)) {
        with_ = assistants.length ? assistants[0].id : null
      }
    })
    .catch((e: Failure) => {
      if (generation !== placesGeneration) return
      places = places || []
      said(why(e), e)
    })
    .then(() => {
      if (generation !== placesGeneration) return
      loading = false
      draw()
    })
}

function enter(place: Place): void {
  at = place
  pasts = null
  pastsUnread = false
  capped = false
  find = ""
  el<HTMLInputElement>("start-filter").value = ""
  said("")
  reading = true
  draw()
  asked(() => api.pastSessions(place.id, with_))
    .then((d) => {
      if (!at || at.id !== place.id) return
      pasts = (d && d.sessions) || []
      pastsUnread = false
      capped = !!(d && d.more)
    })
    .catch((e: Failure) => {
      if (!at || at.id !== place.id) return
      pastsUnread = !pasts
      pasts = pasts || []
      if (e && e.code === "not_found") {
        leave()
        places = null
        load()
      }
      said(why(e, "past"), e)
    })
    .then(() => {
      reading = false
      draw()
    })
}

function leave(): void {
  at = null
  resumeAs = ""
  pasts = null
  pastsUnread = false
  capped = false
  reading = false
  find = ""
  const box = el<HTMLInputElement>("start-filter")
  if (box) box.value = ""
  said("")
}

function press(id: string): void {
  if (mode === "terminal") {
    if (pressing || terminalAccess.state !== "ready") return
    const place = (places || []).find((p) => p.id === id)
    if (!place) return
    if (hostedConsole()) { close(); openNewTerminal(id); return }
    pressing = id
    draw()
    const size = firstSize()
    void openTerminal(id, size.cols, size.rows).then((made) => {
      pressing = null
      close()
      openTerminalPage(id, made.id)
    }, (e: unknown) => {
      pressing = null
      said(nextWord("terminalOpenFailed", { why: e instanceof TerminalRequestError ? terminalRefusalWords(e.code) : L.failureSentence(e) }), e)
      draw()
    })
    return
  }
  if (pressing || wait || !write || typeof api.startPlace !== "function") return
  const place = (places || []).find((p) => p.id === id) ?? null
  if (resume && resumable() && place) {
    enter(place)
    return
  }
  pressing = id
  said("")
  draw()
  startPress.start()
  const as = id === MACHINE_PLACE ? "" : chosenPersona()
  asked(() => api.startPlace(id, with_, undefined, as || undefined))
    .then((d) => {
      startPress.settle(() => {
        pressing = null
        began(d && d.id, place || (id === MACHINE_PLACE
          ? { id, label: "Clawdfather", path: nextWord("machineSessionScope") } : null), d && d.attach)
      })
    })
    .catch((e: Failure) => {
      startPress.settle(() => {
        pressing = null
        if (e && e.code === "write_disabled") {
          write = false
        } else if (e && e.code === "not_found") {
          places = null
          load()
        }
        said(why(e), e)
        draw()
      })
    })
}

function pick(sessionID: string): void {
  if (pressing || wait || !write || !at || typeof api.resumePlace !== "function") return
  const row = (pasts || []).find((r) => r.id === sessionID)
  if (!row) return

  if (row.live) {
    const open = L.bySessionId(sessionID)
    if (!open) return
    close()
    host.open(open.id)
    return
  }

  const place = at
  pressing = sessionID
  said("")
  draw()
  startPress.start()
  const as = chosenPersona()
  asked(() => api.resumePlace(place.id, sessionID, with_, undefined, as || undefined))
    .then((d) => {
      startPress.settle(() => {
        pressing = null
        began(d && d.id, { id: place.id, label: row.title, path: place.path, icon: place.icon }, d && d.attach)
      })
    })
    .catch((e: Failure) => {
      startPress.settle(() => {
        pressing = null
        if (e && e.code === "write_disabled") {
          write = false
        } else if (e && e.code === "not_found") {
          said(T().webResumeGone)
          enter(place)
          return
        }
        said(why(e), e)
        draw()
      })
    })
}

function began(id: string | undefined, place: Place | null, attach: string | undefined): void {
  detached = attach || null
  close()
  if (!id) {
    band(detached ? detachedWords() : T().webStartSlow, true)
    return
  }
  wait = { id, from: host.openId(), late: false, place: place || { id: "" } }
  band(detached ? detachedWords() : T().webStartWaiting, false)
  renderList()
  // The Swift server nudges its watcher here, so the new row arrives the way
  // every other one does; this daemon's stream has no nudge, so the page asks.
  host.refresh()
  if (timer) clearTimeout(timer)
  timer = setTimeout(() => {
    if (!wait) return
    wait.late = true
    band(detached ? detachedWords() : T().webStartSlow, true)
    renderList()
  }, HOLD)
}

function detachedWords(): string {
  return L.fillString(T().webStartDetached, { command: detached })
}

function band(words: string, slow: boolean): void {
  const starting = el("starting")
  starting.dataset.state = slow ? "slow" : "waiting"
  starting.hidden = false
  el("starting-say").textContent = words
  L.setBandSpin(slow ? null : el<HTMLCanvasElement>("starting-spin"))
  const spin = L.spinClock.band()
  if (spin) L.drawSpinner(spin, L.spinClock.phase())
}

function hideBand(): void {
  el("starting").hidden = true
  L.setBandSpin(null)
}

function openMode(machine: boolean, requested: StartMode = "session"): void {
  const active = document.activeElement
  opener = machine ? el("counts") : active instanceof HTMLElement && active !== document.body ? active : el("start-go")
  machineOnly = machine
  mode = machine ? "session" : requested
  el("start-title").textContent = machine ? "Clawdfather" : T().webStart
  el("start").hidden = false
  said("")
  leave()
  persona = rememberedPersona()
  team = rememberedTeam()
  if (mode === "session") void loadPersonas().then(() => draw())
  if (!wait) load()
  if (mode === "terminal") checkTerminalAccess()
  draw()
  el("start-title").focus({ preventScroll: true })
}

function open(): void { openMode(false) }

function close(): void {
  if (pressing) return
  placesGeneration += 1
  loading = false
  el("start").hidden = true
  L.setStartSpin(null)
  const visible = (node: HTMLElement | null) =>
    node?.isConnected && !node.closest("[hidden]") && node.getClientRects().length > 0 && getComputedStyle(node).visibility !== "hidden"
  const target = [opener, el("start-go"), el("rows"), el("brand")].find(visible)
  opener = null
  target?.focus({ preventScroll: true })
}

/** `byId(wait.identity)`, by the bare terminal id: see the header. */
function arrived(id: string) {
  return L.byId(id)
}

export const Start = {
  open,
  openTerminal: () => openMode(false, "terminal"),
  openMachine: () => openMode(true),
  close,
  press,
  pick,
  typed(value: string) {
    find = value
    draw()
  },
  scrolled: edge,
  isOpen: () => !sheetHidden(),

  /** Bind the page: how to open a row, and which row is open. */
  host(next: StartHost) {
    host = next
  },
  subscribe(fn: () => void) {
    listeners.add(fn)
    return () => {
      listeners.delete(fn)
    }
  },
  version: () => version,

  /** `placeholder`: the place the arriving row stands in for, while it is worth drawing. */
  placeholder(): Place | null {
    if (!wait || wait.late || arrived(wait.id)) return null
    return wait.place
  },

  /** `arrange`: the arriving row, or the one that just landed, first. */
  arrange<R extends { id: string }>(list: R[]): R[] {
    const id = wait ? wait.id : landed
    if (!id) return list
    const i = list.findIndex((r) => r.id === id)
    if (i < 0) return list
    return [list[i]].concat(list.slice(0, i), list.slice(i + 1))
  },

  dismiss() {
    wait = null
    detached = null
    if (timer) clearTimeout(timer)
    timer = null
    hideBand()
    renderList()
  },

  /** `check`: every list that arrives, until the one with this session in it. */
  check(): boolean {
    if (!wait && landed) {
      landed = null
      renderList()
      return false
    }
    if (!wait || !arrived(wait.id)) return false
    const { id, from, late } = wait
    landed = late ? null : id
    wait = null
    if (timer) clearTimeout(timer)
    timer = null
    if (detached) {
      // The attach command is useful immediately after a detached start, but
      // the arrival is no longer a pending start. Let the hint go after the
      // existing start wait interval instead of pinning it above every row.
      const hint = detached
      band(detachedWords(), true)
      timer = setTimeout(() => {
        if (wait || detached !== hint) return
        timer = null
        detached = null
        hideBand()
      }, HOLD)
    } else hideBand()
    draw()
    renderList()
    if (late || host.openId() !== from) return false
    host.open(id)
    return true
  },
}

/** `div#start`, as `index.html` writes it; `static.js`'s words drawn by React. */
export function StartSheet() {
  const T = L.strings
  const bound = useRef(false)
  useEffect(() => {
    if (bound.current) return
    bound.current = true
    const start = el("start")
    const sheet = el("start-sheet")
    const filter = el<HTMLInputElement>("start-filter")
    const list = el("start-list")
    const onOverlay = () => close()
    const onSheet = (ev: Event) => ev.stopPropagation()
    const onClose = () => close()
    const onMachine = () => press(MACHINE_PLACE)
    const onSessionTab = () => setMode("session")
    const onTerminalTab = () => setMode("terminal")
    const onInput = () => Start.typed(filter.value)
    const onScroll = () => Start.scrolled()
    const onList = (ev: Event) => {
      const row = (ev.target as Element).closest ? (ev.target as Element).closest<HTMLButtonElement>(".place") : null
      if (!row || row.disabled) return
      if (row.dataset.session) pick(row.dataset.session)
      else if (row.dataset.id) press(row.dataset.id)
    }
    // `input/keys.js`, the part of it about this sheet: Escape closes it, and
    // while it is open the list's own keys and ⌘I stand down. Registered in the
    // capture phase so the page's listener, which does not know this sheet, is
    // not reached for a press this sheet has answered.
    const onKey = (ev: KeyboardEvent) => {
      if (sheetHidden()) return
      if (document.getElementById("action-confirm")?.hidden === false) return
      if (document.getElementById("info")?.hidden === false) return
      const meta = ev.metaKey || ev.ctrlKey
      if (ev.key === "Escape") {
        close()
        ev.stopImmediatePropagation()
        return
      }
      if (["ArrowLeft", "ArrowRight", "Home", "End"].includes(ev.key) &&
          (document.activeElement === el("start-session-tab") || document.activeElement === el("start-terminal-tab"))) {
        ev.preventDefault()
        ev.stopImmediatePropagation()
        const next = ev.key === "Home" ? "session" : ev.key === "End" ? "terminal" : mode === "session" ? "terminal" : "session"
        setMode(next)
        el(next === "session" ? "start-session-tab" : "start-terminal-tab").focus()
        return
      }
      if (ev.key === "Tab") {
        const focusable = [...sheet.querySelectorAll<HTMLElement>('button:not([disabled]):not([hidden]):not([tabindex="-1"]), input:not([disabled]):not([hidden]), [tabindex]:not([tabindex="-1"])')]
          .filter((node) => !node.closest("[hidden]") && node.getClientRects().length > 0)
        if (!focusable.length) {
          ev.preventDefault()
          return
        }
        const index = focusable.indexOf(document.activeElement as HTMLElement)
        const next = index < 0
          ? (ev.shiftKey ? focusable.length - 1 : 0)
          : (index + (ev.shiftKey ? -1 : 1) + focusable.length) % focusable.length
        ev.preventDefault()
        focusable[next].focus()
        ev.stopImmediatePropagation()
        return
      }
      if (meta && (ev.key === "i" || ev.key === "I")) {
        ev.preventDefault()
        ev.stopImmediatePropagation()
        return
      }
      if (meta || ev.altKey) return
      const active = document.activeElement as HTMLElement | null
      const typing = !!active && (active.tagName === "INPUT" || active.tagName === "TEXTAREA" || active.isContentEditable)
      if (!typing) ev.stopImmediatePropagation()
    }
    start.addEventListener("click", onOverlay)
    sheet.addEventListener("click", onSheet)
    el("start-close").addEventListener("click", onClose)
    el("start-machine-action").addEventListener("click", onMachine)
    el("start-session-tab").addEventListener("click", onSessionTab)
    el("start-terminal-tab").addEventListener("click", onTerminalTab)
    filter.addEventListener("input", onInput)
    list.addEventListener("scroll", onScroll, { passive: true })
    list.addEventListener("click", onList)
    document.addEventListener("keydown", onKey, true)
    return () => {
      bound.current = false
      start.removeEventListener("click", onOverlay)
      sheet.removeEventListener("click", onSheet)
      el("start-close")?.removeEventListener("click", onClose)
      el("start-machine-action")?.removeEventListener("click", onMachine)
      el("start-session-tab")?.removeEventListener("click", onSessionTab)
      el("start-terminal-tab")?.removeEventListener("click", onTerminalTab)
      filter.removeEventListener("input", onInput)
      list.removeEventListener("scroll", onScroll)
      list.removeEventListener("click", onList)
      document.removeEventListener("keydown", onKey, true)
    }
  }, [])

  return (
    <div className="overlay" id="start" hidden>
      <div className="sheet" role="dialog" aria-modal="true" id="start-sheet" aria-labelledby="start-title">
        <h2 id="start-title" tabIndex={-1}>{T.webStart}</h2>
        <div className="start-mode-tabs" id="start-mode-tabs" role="tablist" aria-label={nextWord("terminalStartModeLabel")}>
          <button type="button" role="tab" id="start-session-tab" aria-selected="true" aria-controls="start-mode-panel">{nextWord("terminalStartSessionTab")}</button>
          <button type="button" role="tab" id="start-terminal-tab" aria-selected="false" aria-controls="start-mode-panel">{nextWord("terminalStartTerminalTab")}</button>
        </div>

        <div className="block" id="start-mode-panel" role="tabpanel" aria-labelledby="start-session-tab" tabIndex={0}>
          <p className="say" id="start-say" role="status" aria-live="polite"></p>
          <div className="row" id="start-machine" hidden></div>
          <div className="row" id="start-with" hidden></div>
          <div className="row persona-row" id="start-persona" role="group" aria-label={nextWord("personaPicker")} hidden></div>
          <div className="row" id="start-resume" hidden></div>
          <button className="chip machine-start" id="start-machine-action" type="button" hidden>
            <span className="clawdfather-crown" aria-hidden="true" />
            <span className="label">{nextWord("machineSessionStart")}</span>
          </button>
          <input
            className="find"
            id="start-filter"
            type="search"
            name="p2k"
            placeholder={T.webStartFilter}
            hidden
            autoComplete="off"
            autoCapitalize="off"
            autoCorrect="off"
            spellCheck={false}
            data-1p-ignore=""
            data-lpignore="true"
            data-bwignore=""
            data-form-type="other"
            aria-label={T.webStartFilter}
          />
          <ul className="places" id="start-list"></ul>
          <p className="said" id="start-said" role="status" aria-live="polite"></p>
        </div>

        <button className="chip wide" id="start-close" type="button">
          {T.webClose}
        </button>
      </div>
    </div>
  )
}

/**
 * `placeholder()`'s node, `li.row.starting-row`: the list-shaped promise of the
 * place just pressed. Not a session, so it has no selection key and nothing
 * can select, filter or count it.
 */
export function StartingRow({ place }: { place: Place }) {
  const T = L.strings
  const markRef = useRef<HTMLCanvasElement>(null)
  const spinRef = useRef<HTMLCanvasElement>(null)
  useEffect(() => {
    const mark = markRef.current
    if (!mark) return
    if (!L.drawIcon(mark, place.icon, 4)) mark.classList.add("none")
    else mark.classList.remove("none")
  }, [place])
  useEffect(() => {
    L.paintSpinner(spinRef.current)
  })
  return (
    <li className="row starting-row" role="status" aria-label={T.webStartWaiting}>
      <canvas className="mark" ref={markRef}></canvas>
      <div className="title" style={{ color: place.icon ? L.accentTint(place.icon.accent) : "" }}>
        <span className="label">{place.label || place.path || T.webStarting}</span>
      </div>
      <div className="meta">
        <span className="path">{L.path(place.path)}</span>
      </div>
      <div className="state">
        <canvas className="spin" ref={spinRef}></canvas>
        <span className="line">{T.webStartWaiting}</span>
      </div>
    </li>
  )
}
