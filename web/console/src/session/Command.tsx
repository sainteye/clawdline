import { useEffect, useRef } from "react"
import type { IntentDraft } from "@clawdline/contract"
import { client } from "../client.js"
import * as L from "../legacy/bridge.js"
import { planIntent, setCommandSpinner } from "../legacy/command-bridge.js"
import * as V from "../legacy/voice-bridge.js"
import { unansweredSentence } from "../legacy/schedules-bridge.js"
import { openScheduleFrom } from "../pages/schedules.js"
import { openNewWorkItem, type NewWorkItemDraft } from "../pages/work/new-item.js"
import { toast } from "../overlays/toast.js"
import { nextWord } from "../next-strings.js"
import "./command.css"

type Place = L.StartPlaceRow
type Assistant = L.StartAssistantRow
type Failure = L.StartFailure

interface CommandHost {
  open(id: string): void
  refresh(): void
}

const HOLD = 15_000
const POLL = 400
const SEND_TRIES = 4
const SEND_WAIT = 2_000
const MODELS = ["haiku", "sonnet", "opus"]
const SURE = 0.5

let host: CommandHost = { open: () => {}, refresh: () => {} }
let phase: "idle" | "thinking" | "draft" | "opening" = "idle"
let places: Place[] | null = null
let placesNote = ""
let assistants: Assistant[] = []
let chosenPlace: string | null = null
let chosenAssistant: string | null = null
let chosenModel = ""
let openedID: string | null = null
let run = 0

const el = <E extends HTMLElement = HTMLElement>(id: string) => document.getElementById(id) as E
const T = () => L.strings
const busy = () => phase === "thinking" || phase === "opening"

function workKind(value: string): NonNullable<NewWorkItemDraft["kind"]> {
  switch (value) {
    case "issue":
    case "epic":
    case "refactor":
    case "plan":
      return value
    default:
      return "feature"
  }
}

function sayTop(words: string): void {
  el("command-say").textContent = words || ""
}

function sayStatus(words: string, waiting = false): void {
  const box = el("command-said")
  if (!waiting) {
    setCommandSpinner(null)
    box.textContent = words || ""
    return
  }
  box.innerHTML = '<canvas class="wait-spin"></canvas><span></span>'
  box.lastElementChild!.textContent = words || ""
  setCommandSpinner(box.firstElementChild as HTMLCanvasElement)
}

function paint(): void {
  const b = busy()
  el("command-sheet").setAttribute("aria-busy", b ? "true" : "false")
  el<HTMLButtonElement>("voice-go").disabled = b
  el<HTMLButtonElement>("command-mic").disabled = b
  el<HTMLTextAreaElement>("command-text").disabled = phase !== "idle"
  el<HTMLTextAreaElement>("command-instructions").disabled = b
  el("command-list").querySelectorAll<HTMLButtonElement>(".place").forEach((row) => (row.disabled = b))
  el("command-with").querySelectorAll<HTMLButtonElement>(".chip").forEach((chip) => (chip.disabled = b))
  el("command-model").querySelectorAll<HTMLButtonElement>(".chip").forEach((chip) => (chip.disabled = b))
  el<HTMLButtonElement>("command-go").disabled =
    b || (phase === "idle" && !el<HTMLTextAreaElement>("command-text").value.trim()) || (phase === "draft" && !chosenPlace)
}

function drawWith(): void {
  const row = el("command-with")
  row.innerHTML = ""
  row.hidden = assistants.length < 2
  if (row.hidden) return
  const label = document.createElement("span")
  label.className = "with-label"
  label.textContent = T().webCommandWith
  row.appendChild(label)
  assistants.forEach((assistant) => {
    const chip = document.createElement("button")
    chip.type = "button"
    chip.className = "chip" + (assistant.id === chosenAssistant ? " on" : "")
    chip.textContent = assistant.label || assistant.id
    chip.setAttribute("aria-pressed", assistant.id === chosenAssistant ? "true" : "false")
    chip.disabled = busy()
    chip.onclick = () => {
      chosenAssistant = assistant.id
      if (chosenAssistant !== "claude") chosenModel = ""
      drawWith()
      drawModel()
    }
    row.appendChild(chip)
  })
}

function drawModel(): void {
  const row = el("command-model")
  row.innerHTML = ""
  row.hidden = chosenAssistant !== "claude"
  if (row.hidden) return
  const label = document.createElement("span")
  label.className = "with-label"
  label.textContent = T().webCommandModel
  row.appendChild(label)
  MODELS.forEach((model) => {
    const chip = document.createElement("button")
    chip.type = "button"
    chip.className = "chip" + (model === chosenModel ? " on" : "")
    chip.textContent = model
    chip.setAttribute("aria-pressed", model === chosenModel ? "true" : "false")
    chip.disabled = busy()
    chip.onclick = () => {
      chosenModel = chosenModel === model ? "" : model
      drawModel()
    }
    row.appendChild(chip)
  })
}

function markPicked(row: HTMLButtonElement, on: boolean): void {
  row.setAttribute("aria-pressed", on ? "true" : "false")
  row.style.borderColor = on ? "var(--accent-ed)" : ""
  row.style.opacity = on ? "1" : ""
  const where = row.querySelector<HTMLElement>(".where")
  if (where) where.style.color = on ? "var(--accent)" : ""
}

function drawList(): void {
  let where = document.getElementById("command-where-label")
  if (!where) {
    where = document.createElement("div")
    where.id = "command-where-label"
    where.className = "with-label"
    where.textContent = T().webCommandWhere
    el("command-list").parentNode!.insertBefore(where, el("command-list"))
  }
  const list = el("command-list")
  list.innerHTML = ""
  if (placesNote) {
    const note = document.createElement("li")
    note.className = "note"
    note.setAttribute("role", "status")
    note.textContent = placesNote
    list.appendChild(note)
  }
  for (const place of places || []) {
    const li = document.createElement("li")
    const row = document.createElement("button")
    row.type = "button"
    row.className = "place"
    row.dataset.id = place.id
    row.disabled = busy()
    row.innerHTML = '<canvas></canvas><span class="name"></span><span class="where"></span>'
    const mark = row.querySelector("canvas")!
    if (!L.paintIcon(mark, place.icon as never, 4)) mark.classList.add("none")
    const name = row.querySelector<HTMLElement>(".name")!
    name.textContent = place.label || place.path || ""
    name.style.color = place.icon ? L.accentTint(place.icon.accent) : ""
    row.querySelector<HTMLElement>(".where")!.textContent = L.path(place.path)
    markPicked(row, place.id === chosenPlace)
    row.onclick = () => pick(place.id)
    li.appendChild(row)
    list.appendChild(li)
  }
}

function pick(id: string): void {
  if (busy()) return
  chosenPlace = id
  el("command-list").querySelectorAll<HTMLButtonElement>(".place").forEach((row) => markPicked(row, row.dataset.id === id))
  paint()
}

async function ensurePlaces(): Promise<void> {
  if (places && !placesNote) return
  const answer = await L.startApi.places()
  places = answer.places || []
  assistants = answer.assistants || []
  placesNote = unansweredSentence(answer)
}

function reveal(draft: IntentDraft, instructions: string): void {
  if (draft.kind === "schedule") {
    close()
    openScheduleFrom(draft, instructions)
    return
  }
  if (draft.kind === "work") {
    const matchedProject = draft.place_id ? places?.find((place) => place.id === draft.place_id) : undefined
    const project = matchedProject ? {
      id: matchedProject.id,
      label: matchedProject.label || matchedProject.path || matchedProject.id,
      path: matchedProject.path || "",
      icon: matchedProject.icon,
    } : undefined
    close()
    openNewWorkItem({
      projectID: draft.place_id ?? "",
      project,
      kind: workKind(draft.work_kind),
      title: draft.title,
      description: draft.description,
    })
    return
  }
  chosenAssistant = draft.assistant && assistants.some((a) => a.id === draft.assistant)
    ? draft.assistant
    : assistants[0]?.id || null
  const confident = draft.confidence >= SURE && !!draft.place_id
  chosenPlace = confident ? draft.place_id || null : null
  chosenModel = chosenAssistant === "claude" && MODELS.includes(draft.model) ? draft.model : ""
  el<HTMLTextAreaElement>("command-instructions").value = instructions
  drawWith()
  drawModel()
  drawList()
  el("command-draft").hidden = false
  phase = "draft"
  sayTop(T().webCommandDraft)
  if (confident && chosenPlace) {
    paint()
    void openIt(chosenPlace, chosenAssistant, chosenModel, instructions)
    return
  }
  sayStatus(draft.question || T().webCommandUnsure)
  paint()
}

async function requestDraft(): Promise<void> {
  const text = el<HTMLTextAreaElement>("command-text").value.trim()
  if (!text) {
    sayStatus(T().webCommandEmpty)
    return
  }
  phase = "thinking"
  sayStatus(T().webCommandThinking, true)
  paint()
  const mine = run
  try {
    const answer = await planIntent(text)
    if (phase !== "thinking" || mine !== run) return
    const first = typeof answer.draft.instructions === "string" ? answer.draft.instructions : text
    await ensurePlaces()
    if (phase === "thinking" && mine === run) reveal(answer.draft, first)
  } catch (error) {
    if (phase !== "thinking" || mine !== run) return
    phase = "idle"
    const code = (error as Failure | null)?.code
    const own = code === "write_disabled" ? T().webStartOff : code === "no_planner" ? nextWord("commandNoPlanner") : code === "busy" ? T().webCommandBusy : ""
    sayStatus(L.failureSentence(error, { sentence: own, fallback: T().webCommandFailed }))
    paint()
  }
}

async function openIt(place: string, assistant: string | null, model: string, instructions: string): Promise<void> {
  const mine = ++run
  phase = "opening"
  sayStatus(T().webStarting, true)
  paint()
  try {
    const answer = await L.startApi.startPlace(place, assistant, model)
    if (mine !== run) return
    if (!answer.id) {
      finish(T().webStartSlow)
      return
    }
    openedID = answer.id
    host.refresh()
    waitForSession(answer.id, instructions, mine)
  } catch (error) {
    if (mine !== run) return
    finish(whyStart(error))
  }
}

function waitForSession(id: string, instructions: string, mine: number): void {
  const began = Date.now()
  const poll = () => {
    if (mine !== run || phase !== "opening") return
    if (L.byId(id)) {
      if (!instructions.trim()) arrive(id, mine)
      else void sendInstructions(id, instructions, SEND_TRIES, mine)
      return
    }
    if (Date.now() - began >= HOLD) {
      finish(T().webStartSlow)
      return
    }
    window.setTimeout(poll, POLL)
  }
  poll()
}

async function sendInstructions(id: string, instructions: string, tries: number, mine: number): Promise<void> {
  if (mine !== run) return
  sayStatus(T().webSending, true)
  try {
    await client.send(id, instructions)
    if (mine === run) arrive(id, mine)
  } catch (error) {
    if (mine !== run) return
    if ((error as Failure | null)?.code === "showing_a_menu" && tries > 0) {
      window.setTimeout(() => void sendInstructions(id, instructions, tries - 1, mine), SEND_WAIT)
      return
    }
    // refusal-ok: showing_a_menu is the only actionable refusal here; every other send failure has the same retry path.
    finish((error as Failure | null)?.code === "showing_a_menu" ? T().webWaitingSay : T().sendFailed)
  }
}

function arrive(id: string, mine: number): void {
  if (mine !== run) return
  phase = "idle"
  close()
  host.open(id)
}

function finish(words: string): void {
  phase = "draft"
  sayStatus(words)
  paint()
}

function whyStart(error: unknown): string {
  const failure = error as Failure | null
  const code = failure?.code
  let own = ""
  if (code === "write_disabled") own = T().webStartOff
  else if (code === "not_found") own = T().webStartGone
  else if (failure?.app && code === "terminal_closed") own = L.fillString(T().webStartTerminalClosed, { app: failure.app })
  else if (code === "terminal_unsupported") own = T().webStartTerminalUnsupported
  return L.failureSentence(error, { sentence: own, fallback: T().webStartFailed })
}

function reset(): void {
  phase = "idle"
  places = null
  placesNote = ""
  assistants = []
  chosenPlace = null
  chosenAssistant = null
  chosenModel = ""
  openedID = null
  el<HTMLTextAreaElement>("command-text").value = ""
  el<HTMLTextAreaElement>("command-instructions").value = ""
  el("command-draft").hidden = true
  el("command-with").innerHTML = ""
  el("command-model").innerHTML = ""
  el("command-list").innerHTML = ""
  document.getElementById("command-where-label")?.remove()
  sayTop(T().webCommandSay)
  sayStatus("")
  paint()
}

function open(): void {
  if (!el("command").hidden) return
  el("command").hidden = false
  sayTop(T().webCommandSay)
  paint()
}

function close(): void {
  run += 1
  V.cancel()
  el("command").hidden = true
  reset()
}

function heard(text: string): void {
  if (!text) return
  open()
  if (phase !== "idle") reset()
  el<HTMLTextAreaElement>("command-text").value = text
  paint()
  el<HTMLTextAreaElement>("command-text").focus({ preventScroll: true })
}

function voiceChanged(state: V.VoiceState): void {
  const recording = state === "recording"
  for (const id of ["voice-go", "command-mic"]) el(id)?.setAttribute("aria-pressed", recording ? "true" : "false")
}

function listen(): void {
  if (busy()) return
  open()
  V.press({
    host: el("command-voice"),
    composer: null,
    sink: heard,
    guard: () => el("command").hidden,
    changed: voiceChanged,
    say: toast,
  })
}

function press(): void {
  if (busy()) return
  if (phase === "idle") {
    void requestDraft()
    return
  }
  if (phase !== "draft") return
  if (openedID) {
    const mine = ++run
    phase = "opening"
    paint()
    void sendInstructions(openedID, el<HTMLTextAreaElement>("command-instructions").value, SEND_TRIES, mine)
  } else if (chosenPlace) {
    void openIt(chosenPlace, chosenAssistant, chosenModel, el<HTMLTextAreaElement>("command-instructions").value)
  }
}

export const Command = {
  host(next: CommandHost): void {
    host = next
  },
  openAndListen(): void {
    listen()
  },
  close,
  press,
  repaint: paint,
}

export function CommandSheet() {
  const bound = useRef(false)
  useEffect(() => {
    if (bound.current) return
    bound.current = true
    const overlay = el("command")
    const sheet = el("command-sheet")
    const onOverlay = () => close()
    const onSheet = (event: Event) => event.stopPropagation()
    const onClose = () => close()
    const onGo = () => press()
    const onMic = () => listen()
    const onKey = (event: KeyboardEvent) => {
      if (overlay.hidden) return
      if (event.key === "Escape") {
        close()
        event.stopImmediatePropagation()
        return
      }
      if (event.key !== "Tab") return
      const items = [el<HTMLButtonElement>("command-cancel"), el<HTMLButtonElement>("command-go")]
      const at = items.indexOf(document.activeElement as HTMLButtonElement)
      if (at >= 0 && ((!event.shiftKey && at === items.length - 1) || (event.shiftKey && at === 0))) {
        event.preventDefault()
        items[event.shiftKey ? items.length - 1 : 0].focus()
      }
    }
    overlay.addEventListener("click", onOverlay)
    sheet.addEventListener("click", onSheet)
    el("command-cancel").addEventListener("click", onClose)
    el("command-go").addEventListener("click", onGo)
    el("command-mic").addEventListener("click", onMic)
    document.addEventListener("keydown", onKey, true)
    return () => {
      bound.current = false
      overlay.removeEventListener("click", onOverlay)
      sheet.removeEventListener("click", onSheet)
      el("command-cancel")?.removeEventListener("click", onClose)
      el("command-go")?.removeEventListener("click", onGo)
      el("command-mic")?.removeEventListener("click", onMic)
      document.removeEventListener("keydown", onKey, true)
    }
  }, [])

  const words = L.strings
  return (
    <div className="overlay" id="command" hidden>
      <div className="sheet command-sheet" id="command-sheet" role="dialog" aria-modal="true" aria-labelledby="command-title">
        <h2 id="command-title">{words.webCommand}</h2>
        <p className="say" id="command-say" role="status" aria-live="polite">{words.webCommandSay}</p>
        <div className="voice" id="command-voice" role="status" hidden></div>
        <div className="block command-input">
          <textarea className="find heard" id="command-text" rows={5} placeholder={words.webCommandHeard} aria-label={words.webCommandHeard} autoComplete="off" autoCapitalize="sentences" spellCheck={false} data-1p-ignore="" data-lpignore="true" data-bwignore="" onInput={() => paint()}></textarea>
        </div>
        <div className="block draft" id="command-draft" hidden>
          <div className="row" id="command-with"></div>
          <div className="row" id="command-model"></div>
          <ul className="places" id="command-list"></ul>
          <textarea className="find" id="command-instructions" rows={4} aria-label={words.webCommandFirst} spellCheck={false} data-1p-ignore="" data-lpignore="true" data-bwignore=""></textarea>
        </div>
        <p className="said" id="command-said" role="status" aria-live="polite"></p>
        <div className="buttons">
          <button className="chip redo command-mic" id="command-mic" type="button" title={words.webCommand} aria-label={words.webCommandLabel} aria-pressed="false">
            <svg className="ico ico-mic" viewBox="0 0 24 24" aria-hidden="true"><rect x="9" y="3" width="6" height="11" rx="3" fill="currentColor"></rect><path d="M5.75 11.75v0.5a6.25 6.25 0 0 0 12.5 0v-0.5M12 18.5V21" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round"></path></svg>
            <svg className="ico ico-stop" viewBox="0 0 24 24" aria-hidden="true"><rect x="7" y="7" width="10" height="10" rx="2.5" fill="currentColor"></rect></svg>
          </button>
          <button className="chip" id="command-cancel" type="button">{words.webCancel}</button>
          <button className="chip confirm-go" id="command-go" type="button" disabled>{words.webCommandGo}</button>
        </div>
      </div>
    </div>
  )
}
