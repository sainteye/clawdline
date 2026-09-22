import type { CloseReason } from "@clawdline/contract"
import { RefusalError } from "@clawdline/core"
import { client } from "../client.js"
import * as L from "../legacy/bridge.js"
import { getClosingId, setClosingId } from "./events.js"
import { byId, closeabilityLines, closeabilityOf, lostIfClosed, setConfirmSpin, type Closeable } from "../legacy/bridge.js"
import { nextWord } from "../next-strings.js"
import { toast, toastFailure } from "./toast.js"

/**
 * The second press before a session-changing action reaches the daemon —
 * `ActionConfirm` in `input/action-confirm.js`, writing into the
 * `div#action-confirm` that `Overlays` draws, with the close itself
 * (`SessionActions.end` and `finishEnd` in `input/detail-actions.js`) beside
 * it, because the sheet is what waits for that answer.
 *
 * Differences from the original, each forced by the daemon:
 *
 * - The close is `POST /v1/sessions/{id}/close`. The first confirmation never
 *   sends `force`: a stale page must still let the daemon disclose a newly
 *   pending landing. When the daemon answers `close_blocked`, the sheet opens
 *   again with its authoritative reasons; only that second, explicitly named
 *   "still close" decision sends `force`.
 * - A refusal that says what is still owed (`close_blocked`, carrying the
 *   same reasons the list shows) brings the sheet back with those reasons as
 *   the closeability lines — the original's `reopenEndWithLost` answer to a
 *   refusal that carries a list. Pressing "still close" overrides that known
 *   obligation and asks again under a fresh decision id.
 * - A command other than "end" is sent as one line with `/send` and
 *   acknowledged with a toast; the original also draws it into the transcript
 *   optimistically, which this console's transcript does not support.
 */

type Help = { explanation: string; detailsLabel: string; cancelLabel: string; confirmLabel: string }
interface Ask {
  title?: string
  say?: string
  go?: () => unknown
}
interface Pending {
  id: string
  kind: string
  action: string
  opener: HTMLElement | null
  ask: Ask | null
  lost: string[]
  why: string[]
  closeability: string | null
  help: Help | null
  /** True only after the daemon disclosed the obligations this decision overrides. */
  force: boolean
  /** The decision's own id, which is the close's `Idempotency-Key`. */
  request: string
}

/**
 * How the question differs when it is not asked from beside the conversation.
 *
 * `subject` names what would be acted on, and `focus` puts the opening focus
 * on the safe answer — both of them the Forget-a-machine question's rules
 * (`cloud/CloudGate.tsx`), because a question reached by swiping one of
 * thirteen rows has the same problem it does: nothing else on the screen says
 * which one this is about, and the press that got here was a gesture.
 */
export interface Asked {
  subject?: string
  focus?: "cancel" | "go"
}

export interface ConfirmHost {
  openId(): string | null
  writable(): boolean
  /** `closeDetail` (`session/open.js`). */
  closeDetail(): void
  /** Ask the daemon for the list again; the close changed it. */
  refresh(): void
}

let host: ConfirmHost = { openId: () => null, writable: () => false, closeDetail: () => {}, refresh: () => {} }
export function hostConfirm(next: ConfirmHost): void {
  host = next
}

const T = L.strings
const node = <E extends HTMLElement = HTMLElement>(id: string) => document.getElementById(id) as E | null

/**
 * `closeabilityHelpModel` (`view/closeability-help.js`, not among the copied
 * modules): the common "not yet attested" state is an explanation, not an
 * error dump.
 */
function closeabilityHelpModel(projected: Closeable | null): Help | null {
  if (
    !projected ||
    projected.state !== "needs_attestation" ||
    !Array.isArray(projected.reasons) ||
    !projected.reasons.some((reason) => reason.kind === "attestation")
  ) {
    return null
  }
  return {
    explanation: T.closeabilityAttestationExplanation,
    detailsLabel: T.closeabilityTechnicalDetails,
    cancelLabel: T.webReviewBeforeClosing,
    confirmLabel: T.webConfirmEndAnyway,
  }
}

/**
 * `Waiting` (`view/waits.js`): nothing for the first 150ms, and once up, up
 * for at least 320ms, so a fast close looks instant and a slow one never strobes.
 */
function waiting(onShow: () => void, showAfter = 150, minShown = 320) {
  return {
    visible: false,
    shown: 0,
    timer: undefined as number | undefined,
    start() {
      if (this.visible || this.timer !== undefined) return
      this.timer = window.setTimeout(() => {
        this.timer = undefined
        this.visible = true
        this.shown = Date.now()
        onShow()
      }, showAfter)
    },
    settle(then: () => void) {
      window.clearTimeout(this.timer)
      this.timer = undefined
      const finish = () => {
        this.visible = false
        then()
      }
      if (!this.visible) {
        finish()
        return
      }
      const left = minShown - (Date.now() - this.shown)
      if (left <= 0) finish()
      else window.setTimeout(finish, left)
    },
  }
}

const endWait = waiting(() => ActionConfirm.sync())

export const ActionConfirm = {
  pending: null as Pending | null,
  busy: false,

  isOpen(): boolean {
    return node("action-confirm")?.hidden === false
  },

  /**
   * `ask` is for a caller that owns its own destructive action: its two
   * sentences and a function whose promise the sheet waits for.
   */
  open(kind: string, sessionID?: string | null, opener?: HTMLElement | null, ask?: Ask, asked?: Asked): void {
    const id = sessionID || host.openId()
    const overlay = node("action-confirm")
    const sheet = node("action-confirm-sheet")
    const title = node("action-confirm-title")
    const go = node("action-confirm-go")
    if (!id || !host.writable() || !overlay || !sheet || !title || !go) return
    const action = kind === "end" ? T.webEndSession : kind
    const returnFocus = opener || node("detail-actions-trigger")
    const row = byId(id)
    // What the close would take, at the only moment it can still change the outcome.
    const lost = kind === "end" ? lostIfClosed(id) : []
    // Why the broker cannot yet say it is safe, and which one thing moves each of those.
    const why = kind === "end" ? closeabilityLines(row) : []
    const closeable = kind === "end" ? closeabilityOf(row) : null
    const help = closeabilityHelpModel(closeable)
    this.pending = {
      id, kind, action, opener: returnFocus, ask: ask || null, lost, why,
      closeability: closeable && closeable.state, help, force: false, request: mintRequest(),
    }
    this.busy = false
    sheet.dataset.kind = kind
    title.textContent = ask && ask.title
      ? ask.title
      : kind === "end"
        ? asked && asked.subject
          ? nextWord("endNamedTitle", { session: asked.subject })
          : T.webConfirmEndTitle
        : L.fillString(T.webConfirmActionTitle, { action })
    if (ask && ask.say) this.renderSay(ask.say, null, [])
    else if (kind === "end") this.renderSay(this.endSay(lost, why, this.pending.closeability, help), help, why)
    else this.renderSay(L.fillString(T.webConfirmActionSay, { action }), null, [])
    overlay.hidden = false
    this.sync()
    // Cancel, not the destructive button, when the question was reached from
    // the list: the same rule, and for the same reason, as the question that
    // forgets a machine (`cloud/CloudGate.tsx`).
    const cancel = node<HTMLButtonElement>("action-confirm-cancel")
    const first = asked && asked.focus === "cancel" && cancel ? cancel : go
    first.focus({ preventScroll: true })
  },

  endSay(lost: string[], why: string[], closeability: string | null, help: Help | null): string {
    let said = T.webConfirmEndSay
    if (lost && lost.length) {
      said += "\n" + T.webConfirmEndLoses + "\n" + lost.map((item) => "· " + item).join("\n")
    }
    if (help) return said + "\n" + help.explanation
    // Whenever the projection is not `safe`, including `unknown`: an absence of
    // proof is what the reader most needs to see before ending somebody's work.
    if (closeability && closeability !== "safe") {
      said += "\n" + T.closeabilityNotProven
      if (why && why.length) {
        said += "\n" + T.closeabilityWhy + ":\n" + why.map((item) => "· " + item).join("\n")
      }
    }
    return said
  },

  /** Plain meaning first; the broker vocabulary one deliberate disclosure away. */
  renderSay(said: string, help: Help | null, why: string[]): void {
    const sheet = node("action-confirm-sheet")
    const say = node("action-confirm-say")
    if (!sheet || !say) return
    document.getElementById("action-confirm-technical")?.remove()
    sheet.setAttribute("aria-describedby", "action-confirm-say")
    say.textContent = said
    if (!help) return

    const technical = document.createElement("details")
    technical.id = "action-confirm-technical"
    technical.className = "closeability-technical"
    const summary = document.createElement("summary")
    const mark = document.createElement("span")
    mark.className = "help-mark"
    mark.setAttribute("aria-hidden", "true")
    mark.textContent = "?"
    summary.append(mark, document.createTextNode(help.detailsLabel))
    technical.appendChild(summary)
    const body = document.createElement("div")
    body.className = "technical-copy"
    const list = document.createElement("ul")
    for (const line of why || []) {
      const item = document.createElement("li")
      item.textContent = line
      list.appendChild(item)
    }
    body.appendChild(list)
    technical.appendChild(body)
    say.insertAdjacentElement("afterend", technical)
    sheet.setAttribute("aria-describedby", "action-confirm-say action-confirm-technical")
  },

  /**
   * The daemon refused the close and said what is still owed. Its reasons are
   * the authoritative list, from a fresher reading than the page's, so the
   * sheet comes back up carrying them.
   */
  reopenEndBlocked(id: string, reasons: readonly CloseReason[]): void {
    this.open("end", id)
    const pending = this.pending
    if (!pending) return
    // A fresh request id: the refusal is the daemon's answer to the first
    // ask, and pressing again is a second decision about what it said, not a
    // retry of a request whose answer was lost.
    pending.request = mintRequest()
    const row = byId(id) as Record<string, unknown> | null
    const base = (row && (row.closeability as Record<string, unknown>)) || {}
    const refused = { ...(row || { id }), closeability: { ...base, state: "blocked", reasons: [...reasons] } }
    const projected = closeabilityOf(refused)
    pending.why = closeabilityLines(refused)
    pending.closeability = projected.state
    pending.help = closeabilityHelpModel(projected)
    pending.force = true
    this.renderSay(this.endSay(pending.lost, pending.why, pending.closeability, pending.help), pending.help, pending.why)
    this.sync()
  },

  close(restore: boolean): void {
    const overlay = node("action-confirm")
    if (!overlay || overlay.hidden || this.busy) return
    const opener = this.pending && this.pending.opener
    this.pending = null
    this.renderSay("", null, [])
    this.sync()
    overlay.hidden = true
    if (restore && opener && document.contains(opener)) opener.focus({ preventScroll: true })
  },

  run(): void {
    const pending = this.pending
    if (!pending || this.busy) return
    if (pending.ask && pending.ask.go) {
      this.busy = true
      this.sync()
      Promise.resolve(pending.ask.go()).then(
        () => this.finish(),
        () => this.finish(),
      )
      return
    }
    if (pending.kind === "end") {
      // The decision stays on screen until the daemon has answered; both ways
      // out are disabled so the one request is the only thing in flight. A
      // refusal to start leaves nothing to release them, so the sheet lets go.
      this.busy = true
      this.sync()
      if (!end(pending.id, pending.request, pending.force)) {
        this.busy = false
        this.sync()
        this.close(false)
      }
      return
    }
    this.close(false)
    prompt(pending.action, pending.id)
  },

  sync(): void {
    const sheet = node("action-confirm-sheet")
    const cancel = node<HTMLButtonElement>("action-confirm-cancel")
    const go = node<HTMLButtonElement>("action-confirm-go")
    if (!sheet || !cancel || !go) return
    setConfirmSpin(null)
    sheet.setAttribute("aria-busy", this.busy ? "true" : "false")
    cancel.disabled = this.busy
    go.disabled = this.busy
    const help = this.pending && this.pending.help
    cancel.textContent = help ? help.cancelLabel : T.webCancel
    if (this.busy && endWait.visible) {
      go.innerHTML = '<span class="busy"><canvas></canvas><span></span></span>'
      const word = go.querySelector(".busy span")
      if (word) word.textContent = T.webClosing
      setConfirmSpin(go.querySelector("canvas"))
    } else {
      go.textContent = help
        ? help.confirmLabel
        : this.pending?.force
          ? T.webConfirmEndAnyway
          : T.webConfirm
    }
  },

  finish(): void {
    this.busy = false
    this.sync()
    this.close(false)
  },

  /**
   * `paintStatic` for this sheet: the two buttons in the catalog's words. Run
   * when the words arrive, and only while the sheet is closed — an open one is
   * already saying its own.
   */
  paint(): void {
    if (this.isOpen()) return
    const cancel = node("action-confirm-cancel")
    const go = node("action-confirm-go")
    if (cancel) cancel.textContent = T.webCancel
    if (go) go.textContent = T.webConfirm
  },
}

/* ---- the close (`SessionActions.end` / `finishEnd`) ------------------------ */

let endTicket = 0
let settlingEnd = false
let endWasOpen = false

/**
 * A fresh request id for one decision.
 *
 * `crypto.randomUUID` only exists in a secure context, and this console is
 * served over plain http on a LAN, so the fallback is the one `press-holds.ts`
 * uses: it has to be unrepeated, not unguessable.
 */
let minted = 0
function mintRequest(): string {
  const c = globalThis.crypto
  if (typeof c?.randomUUID === "function") {
    try {
      return c.randomUUID()
    } catch {
      /* below */
    }
  }
  minted += 1
  const bytes = new Uint8Array(8)
  c.getRandomValues(bytes)
  return "close-" + minted + "-" + Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("")
}

/** `false` is the only answer that tells the sheet nothing is coming back. */
function end(id: string, request: string, force: boolean): boolean {
  if (!id || !host.writable() || getClosingId()) return false
  const ticket = ++endTicket
  setClosingId(id)
  endWasOpen = host.openId() === id
  settlingEnd = false
  endWait.start()
  ActionConfirm.sync()
  client.close(id, force, request).then(
    () => finishEnd(id, ticket, true),
    (e) => finishEnd(id, ticket, false, e),
  )
  return true
}

/**
 * One ending, whichever answer arrives first: the list can show the row gone
 * before the POST returns, and once either has answered the other is only the
 * tail of the same trip.
 */
function finishEnd(id: string, ticket: number, ok: boolean, error?: unknown): void {
  if (getClosingId() !== id || ticket !== endTicket || settlingEnd) return
  settlingEnd = true
  endWait.settle(() => {
    if (getClosingId() !== id || ticket !== endTicket) return
    setClosingId(null)
    settlingEnd = false
    endTicket += 1
    ActionConfirm.finish()
    if (!ok && error instanceof RefusalError && error.code === "close_blocked") {
      endWasOpen = false
      ActionConfirm.reopenEndBlocked(id, error.reasons)
      return
    }
    const open = host.openId()
    if (ok && endWasOpen && (!open || open === id)) host.closeDetail()
    endWasOpen = false
    host.refresh()
    if (ok) toast(T.webEndSession + " ✓", false)
    else toastFailure(error, T.webRequestFailed)
  })
}

/** The list no longer has the session being closed: that is the answer. */
export function endedIfGone(ids: ReadonlySet<string>): void {
  const id = getClosingId()
  if (id && !ids.has(id)) finishEnd(id, endTicket, true)
}

/** `SessionActions.prompt`: one command typed into the session, acknowledged with a toast. */
function prompt(action: string, id: string): void {
  if (!id || !host.writable()) return
  client.send(id, action).then(
    () => toast(action + " ✓"),
    (e) => toastFailure(e, T.webRequestFailed),
  )
}

/** The sheet's own listeners, as `input/action-confirm.js` binds them. Returns the unbinding. */
export function bindActionConfirm(): () => void {
  const overlay = node("action-confirm")
  const sheet = node("action-confirm-sheet")
  const cancel = node("action-confirm-cancel")
  const go = node("action-confirm-go")
  if (!overlay || !sheet || !cancel || !go) return () => {}
  const onCancel = () => ActionConfirm.close(true)
  const onGo = () => ActionConfirm.run()
  const onOverlay = () => ActionConfirm.close(true)
  const onSheet = (ev: Event) => ev.stopPropagation()
  // Focus stays on the sheet: Tab walks its controls and wraps.
  const onKey = (ev: KeyboardEvent) => {
    if (ev.key !== "Tab") return
    if (ActionConfirm.busy) {
      ev.preventDefault()
      return
    }
    const technical = document.querySelector<HTMLElement>("#action-confirm-technical > summary")
    const items = [technical, cancel, go].filter((x): x is HTMLElement => !!x)
    const at = items.indexOf(document.activeElement as HTMLElement)
    if ((!ev.shiftKey && at === items.length - 1) || (ev.shiftKey && at <= 0)) {
      ev.preventDefault()
      items[ev.shiftKey ? items.length - 1 : 0].focus()
    }
  }
  cancel.addEventListener("click", onCancel)
  go.addEventListener("click", onGo)
  overlay.addEventListener("click", onOverlay)
  sheet.addEventListener("click", onSheet)
  overlay.addEventListener("keydown", onKey)
  return () => {
    cancel.removeEventListener("click", onCancel)
    go.removeEventListener("click", onGo)
    overlay.removeEventListener("click", onOverlay)
    sheet.removeEventListener("click", onSheet)
    overlay.removeEventListener("keydown", onKey)
  }
}
