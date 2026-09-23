import type { ProjectLink, SessionInfo, SessionLimits, SessionModel } from "@clawdline/contract"
import { client } from "../client.js"
import * as L from "../legacy/bridge.js"
import { smartTitle as requestSmartTitle } from "../legacy/command-bridge.js"
import { nextWord } from "../next-strings.js"
import { repositoryNote } from "./links-note.js"
import { SessionFacts } from "./facts.js"
import { conversationBecameKnown, factsMissConversation } from "../session/info-freshness.js"
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
} from "../legacy/bridge.js"
import { toast } from "./toast.js"
import { ActionConfirm } from "./action-confirm.js"
import "./info-next.css"

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
 * - `links` is on this wire now, so the original's Links section is drawn from
 *   it, with the original's classes and its own words;
 * - `limits` is the account-level 5h/7d reading behind the status line; when
 *   the provider did not report a window, the section says it is unknown
 *   rather than silently disappearing;
 * - `files`, `permission` and `fastMode` are not on this wire, so their
 *   sections are absent;
 * - `session.seconds` is absent, so the running-for part of the meta line is;
 * - the title route keeps a manual name in this daemon's own config, so the
 *   original editor and its pencil are available on writable connections;
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
let editingTitle = false
let titleDraft = ""
let stateSeen = "" // work and closeability shape at the last draw
let conversationSeen = "" // the provider id the row knew at the last read
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
    sentence: code === "busy" ? T.webInfoBusy
      : (code === "unsupported" || code === "cloud_not_carried") && err?.layer === "browser" ? T.webInfoTitleCloud : "",
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

function smartTitleButton(disabled: boolean): string {
  return (
    '<button type="button" class="title-smart" data-title-smart="1" title="' + esc(nextWord("smartTitleButton")) +
    '" aria-label="' + esc(nextWord("smartTitleButton")) + '"' + (disabled ? " disabled" : "") + ">" +
    '<svg viewBox="0 0 18 18" aria-hidden="true" focusable="false">' +
    '<path d="M9 1.8c.35 2.75 1.7 4.1 4.45 4.45C10.7 6.6 9.35 7.95 9 10.7 8.65 7.95 7.3 6.6 4.55 6.25 7.3 5.9 8.65 4.55 9 1.8Z"></path>' +
    '<path d="M14.1 10.2c.18 1.45.9 2.17 2.35 2.35-1.45.18-2.17.9-2.35 2.35-.18-1.45-.9-2.17-2.35-2.35 1.45-.18 2.17-.9 2.35-2.35ZM4.15 11.7c.13 1 .62 1.5 1.62 1.63-1 .12-1.5.62-1.62 1.62-.13-1-.63-1.5-1.63-1.62 1-.13 1.5-.63 1.63-1.63Z"></path>' +
    "</svg></button>"
  )
}

function namingAssistantName(assistant: string | undefined): string {
  if (assistant === "claude") return "Claude Code"
  if (assistant === "codex") return "Codex"
  return L.assistantDisplayName(assistant)
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
  const headline = editingTitle
    ? '<form class="title-editor" data-title-form="1"><input name="title" type="text" maxlength="200"' +
      ' value="' + esc(titleDraft) + '" aria-label="' + esc(T.webInfoEditTitle) + '"' +
      (busy || !host.writable() ? " disabled" : "") + '><span class="title-actions">' +
      '<button type="submit" class="chip"' + (busy || !host.writable() ? " disabled" : "") + ">" +
      esc(T.webScheduleSave) + '</button><button type="button" class="chip quiet" data-title-cancel="1"' +
      (busy ? " disabled" : "") + ">" + esc(T.webCancel) + "</button></span></form>"
    : '<div class="title-row"><button type="button" class="session-title" data-title-edit="1" title="' +
      esc(T.webInfoEditTitle) + '"' + (!host.writable() || busy ? " disabled" : "") + '><span>' +
      esc(title) + '</span><i aria-hidden="true">✎</i></button>' +
      smartTitleButton(!host.writable() || busy) +
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

function limitsHTML(limits: SessionLimits): string {
  if (!limits.windows.length) return note(T.webInfoUnknown)
  return limits.windows
    .map((window) => {
      const pct = Math.max(0, Math.min(100, Math.round(window.usedPercent)))
      const level = pct >= 85 ? "bad" : pct >= 60 ? "warn" : "ok"
      const reset = window.resetsAt
        ? '<span class="when">' + esc(fill(T.webInfoResets, { when: L.clock(window.resetsAt) })) + "</span>"
        : ""
      const value = window.hit ? T.webInfoLimitHit : pct + "%"
      return (
        '<div class="win" data-level="' + level + '">' +
        '<span class="wn">' + esc(window.name) + "</span>" +
        '<span class="bar" aria-hidden="true"><i style="--w:' + pct + '%"></i></span>' +
        '<span class="pct">' + esc(value) + "</span>" + reset + "</div>"
      )
    })
    .join("")
}

/**
 * `STATES` in `input/info.js`: the four dots that have a word. A state outside
 * it — including the empty one a server row carries when nothing measured it —
 * has no word here, and `status` says what there is to say instead.
 */
const LINK_STATES: Record<string, string> = {
  ok: "webLinkOk", fail: "webLinkFail", down: "webLinkDown", running: "webLinkRunning",
}

/** Only an explicit web address becomes an anchor: a `javascript:` in an `href` is script running on this page with this page's cookie. */
function openable(url: string): boolean {
  return /^https?:\/\//i.test(url || "")
}

/**
 * `linksHTML` in `input/info.js`, row for row: a `.dep-row` holding a `.dep`
 * with its dot, label, state word and host, and a `.dep-note` under it when
 * there is one line worth saying about why.
 *
 * The daemon's own additions are drawn in the same shapes: a `server` row that
 * nothing measured has no `data-state` — so the stylesheet leaves its dot the
 * faint default rather than colouring it a verdict nobody took — and says
 * which kind of nothing it was where the receipt's word would go.
 */
function linksHTML(links: ProjectLink[], d: Facts): string {
  const note = repositoryNote(d.repository, d.repositoryUnreadable, d.deployQuiet,
    { word: nextWord, clock: L.clock })
  const noteHTML = note ? '<p class="note">' + esc(note) + "</p>" : ""
  if (!links.length) return '<p class="note">' + esc(T.webLinksEmpty) + "</p>" + noteHTML
  const rows = links
    .map((link) => {
      const word = link.status || (LINK_STATES[link.state] ? T[LINK_STATES[link.state]] : "") ||
        (link.unknownReason === "status_not_run"
          ? nextWord("stackStatusNotRun")
          : link.unknownReason === "nothing_declared"
            ? nextWord("stackNothingDeclared")
            : "")
      const url = String(link.url || "")
      const far = link.local && !L.atMac()
      const detail = link.why ? String(link.why) : far ? T.webLinksLocal : ""
      const where = url.replace(/^https?:\/\//i, "")
      const inner =
        '<span class="dot"></span><span class="lbl">' + esc(link.label || link.kind) + "</span>" +
        (word ? '<span class="st">' + esc(word) + "</span>" : "") +
        '<span class="host" title="' + esc(url) + '">' + esc(where) + "</span>"
      const row = openable(url)
        ? '<a class="dep" data-state="' + esc(link.state || "") + '" href="' + esc(url) +
          '" target="_blank" rel="noopener noreferrer">' + inner + "</a>"
        : '<div class="dep" data-state="' + esc(link.state || "") + '">' + inner + "</div>"
      return '<div class="dep-row">' + row + (detail ? '<p class="dep-note">' + esc(detail) + "</p>" : "") + "</div>"
    })
    .join("")
  return rows + noteHTML
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
  if (d.limits) {
    const when = d.limits.at ? esc(fill(T.webInfoAsOf, { when: L.clock(d.limits.at) })) : ""
    out += sec(++i, T.webInfoLimits, when, limitsHTML(d.limits))
  }
  // Where this project can be opened. The section is drawn only when the
  // daemon read the directory at all: a session with no working directory has
  // not got an empty project, it has not been placed.
  if (d.links || d.repository) {
    // A held reading is served however old it is, so the card says its age
    // where the original says a plan reading's — the same words in the same
    // place.
    const when = d.linksObservedAt ? esc(fill(T.webInfoAsOf, { when: L.clock(d.linksObservedAt) })) : ""
    out += sec(++i, T.webLinks, when, linksHTML(d.links || [], d))
  }
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
  let titleBox = box.querySelector<HTMLInputElement>(".title-editor input")
  const keptTitle = titleBox ? titleBox.value : null
  const titleFocused = !!titleBox && document.activeElement === titleBox
  const titleCaret = titleFocused ? titleBox?.selectionStart ?? null : null
  const again = drawn
  box.classList.toggle("again", again)
  box.innerHTML = data ? html(data) : ""
  if (data) drawn = true
  const refresh = node("info-refresh") as HTMLButtonElement | null
  if (refresh) refresh.disabled = loading
  typed = box.querySelector<HTMLInputElement>(".other input")
  if (typed && kept) typed.value = kept
  if (typed && focused && !typed.disabled) typed.focus({ preventScroll: true })
  titleBox = box.querySelector<HTMLInputElement>(".title-editor input")
  if (titleBox && keptTitle !== null) {
    titleBox.value = keptTitle
    titleDraft = keptTitle
  }
  if (titleBox && titleFocused && !titleBox.disabled) {
    titleBox.focus({ preventScroll: true })
    if (titleCaret !== null) titleBox.setSelectionRange(titleCaret, titleCaret)
  }
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
    const row = session()
    conversationSeen = String(row?.sessionId || "")
    const force = factsMissConversation(conversationSeen, data)
    drawn = false
    pending = null
    busy = false
    editingTitle = false
    titleDraft = ""
    window.clearTimeout(confirming)
    said("")
    overlay.hidden = false
    load(forId, force)
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
    editingTitle = false
    titleDraft = ""
    conversationSeen = ""
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
    const s = session()
    const conversation = String(s?.sessionId || "")
    const refresh = conversationBecameKnown(conversationSeen, conversation)
    conversationSeen = conversation
    if (refresh) {
      load(forId, true)
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

  editTitle(): void {
    if (!host.writable() || busy || !data?.session) return
    editingTitle = true
    titleDraft = data.session.title || ""
    said("")
    draw()
    const input = node("info-body")?.querySelector<HTMLInputElement>(".title-editor input")
    if (input) {
      input.focus({ preventScroll: true })
      input.select()
    }
  },

  cancelTitle(): void {
    if (busy) return
    editingTitle = false
    titleDraft = ""
    draw()
  },

  saveTitle(value: string): void {
    const id = forId
    if (!id || !host.writable() || busy) return
    titleDraft = String(value || "")
    busy = true
    said("")
    draw()
    client.title(id, titleDraft).then(
      (answer) => {
        if (forId !== id) return
        busy = false
        editingTitle = false
        titleDraft = ""
        if (data?.session && typeof answer.display_title === "string") {
          data.session.title = answer.display_title
        }
        SessionFacts.drop(id)
        if (answer.local_applied === false) said(T.webInfoTitleNotDurable, true)
        else if (answer.downstream === "queued") said(T.webInfoTitleQueued, true)
        else if (["busy", "unavailable", "failed"].includes(answer.downstream)) said(T.webInfoTitleLocal, true)
        else said(T.webInfoTitleSaved, true)
        draw()
      },
      (e) => {
        if (forId !== id) return
        busy = false
        said(why(e), false)
        draw()
      },
    )
  },

  confirmSmartTitle(opener: HTMLElement): void {
    const id = forId
    if (!id || !host.writable() || busy || !data?.session) return
    const assistant = namingAssistantName(data.session.namingAssistant || "codex")
    ActionConfirm.open(nextWord("smartTitleAction"), id, opener, {
      title: nextWord("smartTitleConfirmTitle"),
      say: nextWord("smartTitleConfirmSay", { assistant }),
      waiting: nextWord("smartTitleWorking"),
      go: (request) => Info.generateSmartTitle(request),
    }, { focus: "cancel" })
  },

  generateSmartTitle(request: string): Promise<void> {
    const id = forId
    if (!id || !host.writable() || busy) return Promise.resolve()
    busy = true
    said("")
    draw()
    return requestSmartTitle(id, request).then(
      (answer) => {
        if (forId !== id) return
        busy = false
        if (data?.session && typeof answer.display_title === "string") {
          data.session.title = answer.display_title
        }
        SessionFacts.drop(id)
        said(nextWord("smartTitleSaved"), true)
        draw()
      },
      (error) => {
        if (forId !== id) return
        busy = false
        said(why(error), false)
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
    const editTitle = t.closest<HTMLButtonElement>("button[data-title-edit]")
    if (editTitle) {
      if (!editTitle.disabled) Info.editTitle()
      return
    }
    const smartTitle = t.closest<HTMLButtonElement>("button[data-title-smart]")
    if (smartTitle) {
      if (!smartTitle.disabled) Info.confirmSmartTitle(smartTitle)
      return
    }
    if (t.closest("button[data-status-review]")) Info.close()
    const cancelTitle = t.closest<HTMLButtonElement>("button[data-title-cancel]")
    if (cancelTitle) Info.cancelTitle()
  }
  const onSubmit = (ev: SubmitEvent) => {
    const form = ev.target as HTMLFormElement | null
    if (form?.dataset?.titleForm) {
      ev.preventDefault()
      const title = form.querySelector<HTMLInputElement>('input[name="title"]')
      Info.saveTitle(title ? title.value : "")
      return
    }
    if (!form || !form.dataset || !form.dataset.other) return
    ev.preventDefault()
    const input = form.querySelector("input")
    Info.switchTo(input ? input.value : "", "")
  }
  const onKeyDown = (ev: KeyboardEvent) => {
    const target = ev.target as Element | null
    if (ev.key === "Escape" && target?.closest(".title-editor")) {
      ev.preventDefault()
      ev.stopPropagation()
      Info.cancelTitle()
    }
  }
  overlay.addEventListener("click", onOverlay)
  sheet.addEventListener("click", onSheet)
  close.addEventListener("click", onClose)
  refresh.addEventListener("click", onRefresh)
  body.addEventListener("click", onBody)
  body.addEventListener("submit", onSubmit)
  body.addEventListener("keydown", onKeyDown)
  return () => {
    overlay.removeEventListener("click", onOverlay)
    sheet.removeEventListener("click", onSheet)
    close.removeEventListener("click", onClose)
    refresh.removeEventListener("click", onRefresh)
    body.removeEventListener("click", onBody)
    body.removeEventListener("submit", onSubmit)
    body.removeEventListener("keydown", onKeyDown)
  }
}
