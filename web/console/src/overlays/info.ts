import type { SessionInfo, SessionModel } from "@clawdline/contract"
import { client } from "../client.js"
import * as L from "../legacy/bridge.js"
import { SessionFacts } from "./facts.js"
import {
  byId,
  closeabilityBadgeHTML,
  closeabilityLines,
  closeabilityOf,
  closeabilityPlainReasons,
  closeabilityShape,
  failureSentence,
  legacyState,
  owedBadgeHTML,
  selfReportedPeerWaitCopy,
  statusGlyphHTML,
  suggestedReplyButtonHTML,
  workStateBadgeHTML,
  workStateOf,
} from "./legacy.js"
import { toast } from "./toast.js"

/**
 * The Session info card — `Info` in `input/info.js`, function for function,
 * writing into the `div#info` that `Overlays` draws. It is imperative because
 * the original is: the body is one `innerHTML`, redrawn only when what it says
 * changed, keeping whatever somebody was typing.
 *
 * What this daemon's answer carries decides what is drawn, and nothing is
 * drawn for a field the answer does not have:
 *
 * - hero, status, model switch and token use are the original's sections;
 * - `limits`, `files`, `links`/`deploy`, `permission` and `fastMode` are not on
 *   this wire, so their sections are absent — not drawn with the original's
 *   empty-state sentences, each of which ("this is not a git repo", "nothing
 *   to open", "Claude only records a window once spent") states a fact about
 *   the session that this daemon never read;
 * - `session.seconds` is absent, so the running-for part of the meta line is;
 * - there is no title route, so the title is the original's read-only button
 *   (disabled, no pencil), which is how it looks where editing is off;
 * - no suggested reply is on the wire, so its button never appears and its
 *   press is not handled here.
 */

/** Fields of the original's answer this daemon may add later; read as the original reads them. */
type Facts = SessionInfo & { session: SessionInfo["session"] & { seconds?: number } }

export interface InfoHost {
  /** The session on screen, `SessionSelection.snapshot().open`. */
  openId(): string | null
  /** `S.write`. */
  writable(): boolean
}

let host: InfoHost = { openId: () => null, writable: () => false }
export function hostInfo(next: InfoHost): void {
  host = next
}

const node = (id: string) => document.getElementById(id)
const T = L.strings
const esc = L.escapeHTML
const fill = L.fillString

let forId: string | null = null // whose card this is
let data: Facts | null = null // as the daemon sent it; null until an answer has arrived
let loading = false
let ticket = 0 // the answer that is still wanted, so a stale one can be dropped
let drawn = false // the sections have risen once; a redraw should not make them rise again
let pending: string | null = null // the word sent after `/model`, until the record names that model
let busy = false // a model command on its way
let stateSeen = "" // work and closeability shape at the last draw
let confirming: number | undefined // the timer reading back after a sent `/model`

function hidden(): boolean {
  return node("info")?.hidden !== false
}

function say(w: string): void {
  const el = node("info-say")
  if (!el) return
  el.textContent = w || ""
  el.hidden = !w
}

function said(w: string, calm = false): void {
  const el = node("info-said")
  if (!el) return
  el.textContent = w || ""
  el.className = "said" + (calm ? " calm" : "")
}

/** What the daemon refused with, in the card's own words where it has some. */
function why(e: unknown): string {
  const err = e as { code?: string; layer?: string } | null
  const code = err && err.code
  return failureSentence(e, {
    sentence: code === "busy" ? T.webInfoBusy : code === "unsupported" && err?.layer === "browser" ? T.webInfoTitleCloud : "",
    fallback: T.webInfoFailed,
  })
}

/** `1d 2h`, `2h 14m`, `14m`. */
function span(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds))
  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  const m = Math.floor((s % 3600) / 60)
  if (d) return d + "d " + h + "h"
  if (h) return h + "h " + m + "m"
  return m + "m"
}

function count(x: unknown): string {
  return typeof x === "number" ? esc(x.toLocaleString()) : esc(T.webInfoUnknown)
}
/** `6.59M`, `80.1K`, `318`; the exact number is in the title. */
function compact(n: unknown): string {
  if (typeof n !== "number") return T.webInfoUnknown
  if (n >= 1e6) return (n / 1e6).toFixed(n >= 1e7 ? 1 : 2) + "M"
  if (n >= 1e3) return (n / 1e3).toFixed(n >= 1e4 ? 0 : 1) + "K"
  return String(n)
}
function dollars(x: number): string {
  return x < 0.01 ? "<$0.01" : "$" + x.toFixed(2)
}

function sec(i: number, title: string, aside: string, body: string): string {
  return (
    '<section class="sec" style="--i:' + i + '"><h3><span>' + esc(title) + "</span>" +
    (aside ? '<span class="aside">' + aside + "</span>" : "") + "</h3>" + body + "</section>"
  )
}
function note(text: string): string {
  return '<p class="note">' + esc(text) + "</p>"
}

type Row = Record<string, any> & { id: string }
function session(): Row | null {
  return forId ? (byId(forId) as Row | null) : null
}

function statusShape(s: Row | null): string {
  if (!s) return ""
  const disposition = s.disposition || {}
  return [
    s.state || "", s.line || "", s.work_state || "", s.work_note || "",
    s.work_provenance || "", JSON.stringify(s.owed || {}),
    JSON.stringify(s.coordination || {}),
    disposition.scope || "", disposition.taskId || "", disposition.title || "",
    closeabilityShape(s),
  ].join("")
}

/** Only an idle session takes a `/model`: typed into a working one it interrupts the turn. */
function canSwitch(): boolean {
  const s = session()
  return !!(s && host.writable() && s.state === "idle" && !busy)
}
/** By prefix, so `claude-haiku-4-5-20251001` finds `claude-haiku-4-5`. */
function onModel(current: string, m: SessionModel): boolean {
  return !!current && (current === m.id || current.indexOf(m.id) === 0)
}

/** The name on its own, for pasting elsewhere; drawn rather than typed. */
function copyTitle(title: string): string {
  return (
    '<button type="button" class="title-copy" data-copy="' + esc(title) +
    '" data-copy-said="' + esc(T.webLinksCopied) + '" title="' + esc(T.webInfoCopyTitle) +
    '" aria-label="' + esc(T.webInfoCopyTitle) + '">' +
    '<svg viewBox="0 0 16 16" aria-hidden="true" focusable="false">' +
    '<rect x="5.25" y="1.75" width="9" height="9" rx="2"></rect>' +
    '<rect x="1.75" y="5.25" width="9" height="9" rx="2"></rect></svg></button>'
  )
}

function hero(s: Facts["session"], u: Facts["usage"]): string {
  const model = s.model || (u && u.model) || ""
  const title = s.title || model || T.webInfoUnknown
  const modelMeta = s.title && model ? '<span class="dot">·</span><span class="model-name">' + esc(model) + "</span>" : ""
  const meta: string[] = []
  if (s.cwd) meta.push('<span title="' + esc(T.webInfoDirectory) + '">' + esc(L.path(s.cwd)) + "</span>")
  if (s.sessionId) {
    meta.push(
      '<button type="button" class="sid" data-copy="' + esc(s.sessionId) + '" title="' +
        esc(T.webInfoSessionId + ": " + s.sessionId) + '">' + esc(String(s.sessionId).slice(0, 8)) + "</button>",
    )
  }
  if (typeof s.seconds === "number") {
    meta.push('<span title="' + esc(T.webInfoRunningFor) + '">' + esc(span(s.seconds)) + "</span>")
  }
  // The original's read-only title: `disabled` whenever editing is not
  // possible, and this daemon has no route to write a title with.
  const headline =
    '<div class="title-row"><button type="button" class="session-title" data-title-edit="1" title="' +
    esc(T.webInfoEditTitle) + '" disabled><span>' + esc(title) + '</span><i aria-hidden="true">✎</i></button>' +
    (s.title ? copyTitle(s.title) : "") + "</div>"
  return (
    '<div class="hero">' +
    '<div class="who">' + L.assistantLogoHTML(s.assistant) + '<span class="assistant-name">' +
    esc(s.assistant || T.webInfoUnknown) + "</span>" + modelMeta + "</div>" +
    headline +
    '<div class="meta">' + meta.join('<span class="dot">·</span>') + "</div>" +
    "</div>"
  )
}

function modelsHTML(models: SessionModel[], current: string): string {
  const can = canSwitch()
  let chips = models
    .map((m) => {
      const on = onModel(current, m)
      const wait = !on && pending === m.command
      return (
        '<button type="button" class="m" data-model="' + esc(m.command) + '" data-name="' + esc(m.name) + '"' +
        (on ? ' data-on="1" aria-current="true"' : "") + (wait ? ' data-pending="1"' : "") +
        (can && !on ? "" : " disabled") + ">" + esc(m.name) + "</button>"
      )
    })
    .join("")
  // A word typed rather than picked has no row of its own, so it is drawn as one while on its way.
  if (pending && !models.some((m) => m.command === pending)) {
    chips += '<button type="button" class="m" data-pending="1" disabled>' + esc(pending) + "</button>"
  }
  const other =
    '<form class="other" data-other="1">' +
    '<input type="text" name="model" autocomplete="off" autocapitalize="off" autocorrect="off" spellcheck="false" ' +
    'placeholder="' + esc(T.webInfoModelOther) + '" aria-label="' + esc(T.webInfoModelOther) + '"' + (can ? "" : " disabled") + ">" +
    '<button type="submit" class="chip"' + (can ? "" : " disabled") + ">" + esc(T.webSend) + "</button></form>"
  return '<div class="models">' + chips + other + "</div>" + (can ? "" : note(T.webInfoModelBusy))
}

/**
 * The list's one-line statuses, unabridged: each is a native `details` whose
 * summary is the status and whose body says what it means.
 */
function statusHTML(s: Row | null): string {
  if (!s) return note(T.webInfoUnknown)
  const work = workStateOf(s)
  let workSaid: string
  if (work.state === "working") {
    workSaid =
      '<span class="session-work-copy" data-work-state="working">' + esc(s.line || T.webStateWorking) + "</span>" + owedBadgeHTML(s)
  } else if (work.state === "waiting_you") {
    workSaid =
      '<span class="session-work-copy" data-work-state="waiting_you">' + statusGlyphHTML("🙋", T.sessionWaiting) + "</span>" + owedBadgeHTML(s)
  } else if (work.state === "waiting_session") {
    const waits = ((s.coordination || {}).waitingOn || []).map((wait: Record<string, string>) =>
      [wait.ownerLabel || wait.ownerSessionId, wait.releaseCondition].filter(Boolean).join(" · "),
    )
    const waitingCopy = waits.join(" · ") || selfReportedPeerWaitCopy(s) || T.closeabilityMoverSession
    workSaid =
      '<span class="session-work-copy" data-work-state="waiting_session">' + statusGlyphHTML("⏳", waitingCopy) + "</span>" + owedBadgeHTML(s)
  } else {
    workSaid = workStateBadgeHTML(s)
  }
  const context = s.disposition && s.disposition.title ? '<p class="status-context">' + esc(s.disposition.title) + "</p>" : ""
  const tap = '<span class="status-tap">' + esc(T.webInfoTapForDetails) + "</span>"
  let out =
    '<details class="session-status-detail" data-status-kind="work"><summary>' + workSaid + tap +
    '</summary><div class="status-explanation"><p>' + esc(T.webInfoWorkStatusMeaning) + "</p>" + context + "</div></details>"

  out += suggestedReplyButtonHTML(s, {
    openId: host.openId(),
    composerIdentity: legacyState.replyComposerIdentity,
    writable: host.writable() && !legacyState.agent,
    zh: (document.documentElement.lang || "").toLowerCase().startsWith("zh"),
  })
  const closeable = closeabilityOf(s)
  if (closeable.block) {
    const reasons = closeabilityLines(s)
    const plain = closeabilityPlainReasons(s)
    const reasonHTML = plain.length
      ? '<ul class="status-reasons">' +
        plain
          .map((reason) => "<li>" + esc(reason.text) + (reason.count > 1 ? ' <span class="reason-count">×' + reason.count + "</span>" : "") + "</li>")
          .join("") +
        "</ul>"
      : ""
    const technical = reasons.length
      ? '<details class="closeability-technical"><summary><span class="help-mark" aria-hidden="true">?</span>' +
        esc(T.closeabilityTechnicalDetails) + '</summary><div class="technical-copy"><ul>' +
        reasons.map((line) => "<li>" + esc(line) + "</li>").join("") + "</ul></div></details>"
      : ""
    out +=
      '<details class="session-status-detail" data-status-kind="closeability"><summary>' + closeabilityBadgeHTML(s) + tap +
      '</summary><div class="status-explanation"><p>' + esc(T.webInfoCloseabilityMeaning) + "</p>" + reasonHTML +
      '<button type="button" class="status-review" data-status-review>' + esc(T.webReviewBeforeClosing) + "</button>" +
      technical + "</div></details>"
  }
  return '<div class="session-statuses">' + out + "</div>"
}

function usageHTML(u: Facts["usage"]): string {
  if (!u) return note(T.webInfoNoUsage)
  const cells: [unknown, string][] = [
    [u.input, T.webInfoInput], [u.output, T.webInfoOutput],
    [u.cacheRead, T.webInfoCacheRead], [u.cacheWrite, T.webInfoCacheWrite],
  ]
  return (
    '<div class="big"><span class="n" title="' + count(u.total) + '">' + esc(compact(u.total)) + "</span>" +
    '<span class="unit">' + esc(T.webInfoTokens) + "</span>" +
    // Only where a price is known; Codex bills against a plan and says nothing.
    (typeof u.costUsd === "number" ? '<span class="cost">' + esc(dollars(u.costUsd)) + "</span>" : "") +
    "</div>" +
    '<div class="grid">' +
    cells.map((c) => '<div class="cell"><b title="' + count(c[0]) + '">' + esc(compact(c[0])) + "</b><i>" + esc(c[1]) + "</i></div>").join("") +
    "</div>"
  )
}

function html(d: Facts): string {
  const s = d.session || ({} as Facts["session"])
  const u = d.usage
  const models = d.models || []
  // The model is whatever the record last named, so a `/model` this card sent
  // stops being pending the moment the record agrees with it.
  const current = s.model || (u && u.model) || ""
  if (pending && models.some((m) => m.command === pending && onModel(current, m))) pending = null
  let out = hero(s, u)
  let i = 0
  out += sec(++i, T.webInfoStatus, "", statusHTML(session() || (s as unknown as Row)))
  // Read-only pairings get no buttons rather than dead ones.
  if (models.length && host.writable()) out += sec(++i, T.webInfoSwitchModel, "", modelsHTML(models, current))
  out += sec(++i, T.webInfoUsage, "", usageHTML(u))
  return out
}

function draw(): void {
  if (hidden()) return
  const box = node("info-body")
  if (!box) return
  stateSeen = statusShape(session())
  say(loading && !data ? T.webLoading : "")
  // A redraw under somebody's fingers keeps what they had typed and where the caret was.
  let typed = box.querySelector<HTMLInputElement>(".other input")
  const kept = typed ? typed.value : ""
  const focused = !!typed && document.activeElement === typed
  const again = drawn
  box.classList.toggle("again", again)
  box.innerHTML = data ? html(data) : ""
  if (data) drawn = true
  const refresh = node("info-refresh") as HTMLButtonElement | null
  if (refresh) refresh.disabled = loading
  typed = box.querySelector<HTMLInputElement>(".other input")
  if (typed && kept) typed.value = kept
  if (typed && focused && !typed.disabled) typed.focus({ preventScroll: true })
}

function load(id: string, force: boolean): void {
  const mine = ++ticket
  loading = true
  said("")
  draw()
  SessionFacts.get(id, force).then(
    (facts) => {
      if (mine !== ticket) return // closed, or opened again on another session
      data = facts as Facts | null
      loading = false
      SessionFacts.receiveFull(id, facts)
      draw()
    },
    (e) => {
      if (mine !== ticket) return
      loading = false
      said(why(e), false)
      draw()
    },
  )
}

/**
 * Read back until a sent `/model` turns up, then stop — quietly: nobody asked
 * for a refresh, so the card is not blanked and the refresh button is not
 * greyed. A `/model` that never lands leaves the chip pending.
 */
function readBack(id: string, tries: number): void {
  window.clearTimeout(confirming)
  if (tries <= 0) return
  confirming = window.setTimeout(() => {
    if (forId !== id || hidden() || !pending) return
    SessionFacts.drop(id)
    SessionFacts.get(id, true).then(
      (facts) => {
        if (forId !== id || hidden() || !pending || !facts) return
        data = facts as Facts
        SessionFacts.receiveFull(id, facts)
        draw()
        if (pending) readBack(id, tries - 1)
        else said("")
      },
      () => {},
    )
  }, 800)
}

export const Info = {
  open(): void {
    const open = host.openId()
    const overlay = node("info")
    if (!open || !overlay) return
    forId = open
    data = SessionFacts.peek(forId) as Facts | null
    drawn = false
    pending = null
    busy = false
    window.clearTimeout(confirming)
    said("")
    overlay.hidden = false
    load(forId, false)
    ;(node("info-close") as HTMLButtonElement | null)?.focus({ preventScroll: true })
  },

  isOpen(): boolean {
    return !hidden()
  },

  refresh(): void {
    if (hidden() || !forId || loading) return
    load(forId, true)
  },

  close(): void {
    const overlay = node("info")
    if (overlay) overlay.hidden = true
    // An answer still on its way is no longer wanted by the card. The shared
    // cache stays: the status line is still showing this session.
    ticket += 1
    loading = false
    data = null
    forId = null
    pending = null
    busy = false
    window.clearTimeout(confirming)
  },

  /**
   * The session under the card changed, so the card is about something else;
   * or its state did, and the buttons depend on that. Redrawn only then: a
   * redraw wipes a half-typed model name.
   */
  follow(): void {
    if (hidden()) return
    const open = host.openId()
    if (!open || open !== forId) {
      Info.close()
      return
    }
    // The status line reads the same answer on its own clock; a card open
    // beside it should not be the last to know.
    const shared = SessionFacts.peek(forId) as Facts | null
    if (shared && shared !== data) {
      data = shared
      draw()
      return
    }
    const s = session()
    if (data && s && statusShape(s) !== stateSeen) draw()
  },

  /** `/model <word>`, typed into the session as one line. One word: a second would be typed as part of it. */
  switchTo(raw: string, name: string): void {
    const id = forId
    const word = String(raw || "").trim().split(/\s+/)[0] || ""
    if (!id || !word || !canSwitch()) return
    busy = true
    said("")
    draw()
    client.send(id, "/model " + word).then(
      () => {
        if (forId !== id) return
        busy = false
        pending = word
        SessionFacts.drop(id)
        said(fill(T.webInfoModelSent, { model: name || word }), true)
        draw()
        readBack(id, 5)
      },
      (e) => {
        if (forId !== id) return
        busy = false
        window.clearTimeout(confirming)
        said(why(e), false)
        draw()
      },
    )
  },

  /** One clipboard path for the session id and the session's name, told apart by what the toast says. */
  copy(text: string | undefined, saidText: string | undefined): void {
    if (!text || !navigator.clipboard) return
    navigator.clipboard.writeText(text).then(
      () => toast(saidText || T.webInfoCopied),
      () => {},
    )
  },
}

/** The card's own listeners, as `input/info.js` binds them. Returns the unbinding. */
export function bindInfo(): () => void {
  const overlay = node("info")
  const sheet = node("info-sheet")
  const close = node("info-close")
  const refresh = node("info-refresh")
  const body = node("info-body")
  if (!overlay || !sheet || !close || !refresh || !body) return () => {}
  const onOverlay = () => Info.close()
  const onSheet = (ev: Event) => ev.stopPropagation()
  const onClose = () => Info.close()
  const onRefresh = () => Info.refresh()
  const onBody = (ev: MouseEvent) => {
    const t = ev.target as Element | null
    if (!t || !t.closest) return
    const chip = t.closest<HTMLButtonElement>("button[data-model]")
    if (chip) {
      if (!chip.disabled) Info.switchTo(chip.dataset.model || "", chip.dataset.name || "")
      return
    }
    const copy = t.closest<HTMLButtonElement>("button[data-copy]")
    if (copy) {
      Info.copy(copy.dataset.copy, copy.dataset.copySaid)
      return
    }
    if (t.closest("button[data-status-review]")) Info.close()
  }
  const onSubmit = (ev: SubmitEvent) => {
    const form = ev.target as HTMLFormElement | null
    if (!form || !form.dataset || !form.dataset.other) return
    ev.preventDefault()
    const input = form.querySelector("input")
    Info.switchTo(input ? input.value : "", "")
  }
  overlay.addEventListener("click", onOverlay)
  sheet.addEventListener("click", onSheet)
  close.addEventListener("click", onClose)
  refresh.addEventListener("click", onRefresh)
  body.addEventListener("click", onBody)
  body.addEventListener("submit", onSubmit)
  return () => {
    overlay.removeEventListener("click", onOverlay)
    sheet.removeEventListener("click", onSheet)
    close.removeEventListener("click", onClose)
    refresh.removeEventListener("click", onRefresh)
    body.removeEventListener("click", onBody)
    body.removeEventListener("submit", onSubmit)
  }
}
