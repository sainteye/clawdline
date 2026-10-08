import type { UpdateApplyState, UpdateStatus } from "@clawdline/contract"
import type { NextWord, nextWord } from "../next-strings.js"

/**
 * What the Settings page's update panel and the session list's update banner
 * say about `/v1/update` and `POST /v1/update/apply` (docs/updates.md), with
 * no DOM and no fetch, so every state can be checked without either.
 *
 * Nothing here imports at run time, so node's test runner can load it alone.
 */

/** No more often than this does an open console ask again while nothing is happening. */
export const UPDATE_READ_EVERY_MS = 10 * 60 * 1000

/** Browser lifecycle and time supplied to the shared update reading. */
export interface UpdateReadEnvironment {
  now(): number
  visible(): boolean
  onVisibilityChange(listener: () => void): () => void
  onFocus(listener: () => void): () => void
  /** Optional for adapters that cannot report a window leaving focus. */
  onBlur?(listener: () => void): () => void
  onOnline(listener: () => void): () => void
  setInterval(listener: () => void, ms: number): ReturnType<typeof setInterval>
  clearInterval(timer: ReturnType<typeof setInterval>): void
}

/** One reading for all mounted update views, including their resume checks. */
export function createUpdateReadStore(read: () => Promise<UpdateRead>, env: UpdateReadEnvironment) {
  let reading: UpdateRead | null = null
  let readAt: number | null = null
  let inFlight: Promise<UpdateRead> | null = null
  let timer: ReturnType<typeof setInterval> | null = null
  let wasVisible = env.visible()
  let lostFocus = false
  const listeners = new Set<() => void>()
  let unlisten: Array<() => void> = []

  const publish = (next: UpdateRead) => {
    reading = next
    readAt = env.now()
    for (const listener of listeners) listener()
  }

  const readNow = (): Promise<UpdateRead> => {
    if (inFlight) return inFlight
    inFlight = read().then((next) => {
      publish(next)
      return next
    }).finally(() => { inFlight = null })
    return inFlight
  }

  // The Settings panel also has a timer while an update moves. A browser
  // backgrounding that panel must not keep polling through this entry point.
  const refresh = (): Promise<UpdateRead | null> => env.visible() ? readNow() : Promise.resolve(reading)

  const stale = () => readAt === null || env.now() < readAt || env.now() - readAt >= UPDATE_READ_EVERY_MS
  const refreshIfStale = () => {
    if (env.visible() && stale()) void readNow()
  }
  const stopTimer = () => {
    if (timer !== null) env.clearInterval(timer)
    timer = null
  }
  const visibilityChanged = () => {
    if (!env.visible()) {
      wasVisible = false
      lostFocus = false
      stopTimer()
      return
    }
    const resumed = !wasVisible
    wasVisible = true
    lostFocus = false
    if (timer === null) timer = env.setInterval(refreshIfStale, UPDATE_READ_EVERY_MS)
    if (resumed) void readNow()
    else refreshIfStale()
  }
  const focused = () => {
    if (!env.visible()) return
    // Some browsers send focus before visibilitychange on the same return.
    // Recording the visible transition here makes its later event harmless.
    if (!wasVisible) { visibilityChanged(); return }
    if (lostFocus) {
      lostFocus = false
      void readNow()
    } else refreshIfStale()
  }
  const online = () => {
    if (!env.visible()) return
    // A failed read may be recent; the online transition is new evidence that
    // the next attempt can answer. Focus and visibility still obey freshness.
    if (reading?.kind === "unreachable" || reading?.kind === "refused") void readNow()
    else refreshIfStale()
  }

  const subscribe = (listener: () => void): (() => void) => {
    listeners.add(listener)
    if (listeners.size === 1) {
      wasVisible = env.visible()
      lostFocus = false
      unlisten = [
        env.onVisibilityChange(visibilityChanged),
        env.onFocus(focused),
        ...(env.onBlur ? [env.onBlur(() => { if (env.visible()) lostFocus = true })] : []),
        env.onOnline(online),
      ]
      visibilityChanged()
    }
    return () => {
      listeners.delete(listener)
      if (listeners.size === 0) {
        stopTimer()
        for (const remove of unlisten) remove()
        unlisten = []
      }
    }
  }

  return { refresh, publish, subscribe, current: () => reading }
}

/**
 * While an update is under way, or the daemon is restarting into one, the
 * panel asks this often: the steps take seconds to minutes, and the restart's
 * silence is a few seconds long.
 */
export const UPDATE_FOLLOW_EVERY_MS = 2_000

/** The steps of an update that are still moving (`UpdateApplyState`). */
const MOVING: ReadonlySet<UpdateApplyState> = new Set<UpdateApplyState>(["downloading", "verifying", "staged", "restarting"])

/** Refusal codes that mean the machine predates the route or the Cloud word, not that anything failed. */
const OLDER_CODES = new Set(["not_implemented", "not_found", "unknown_command"])

/**
 * How one read of `/v1/update` came back.
 *
 * - `status`: an answer to speak about.
 * - `older`: the machine has no such route (404, 501 `not_implemented`) or
 *   Cloud word (`unknown_command`). Nothing failed; it is older.
 * - `unreachable`: nothing answered — the daemon is restarting, or the line to
 *   it is down. On its own it says nothing; during an update it is the restart.
 * - `refused`: something answered and it was not a status. Silent.
 */
export type UpdateRead =
  | { kind: "status"; status: UpdateStatus }
  | { kind: "older" }
  | { kind: "unreachable" }
  | { kind: "refused" }

/** What a request did, as the fetch saw it: an answer, or no answer at all. */
export type Answer = { transport: "answered"; status: number; parsed: unknown } | { transport: "failed" }

/** The refusal code in either spelling: flat `{"error":"code"}` or nested `{"error":{"code"}}`. */
export function refusalCode(parsed: unknown): string {
  if (!parsed || typeof parsed !== "object") return ""
  const error = (parsed as { error?: unknown }).error
  if (typeof error === "string") return error
  if (error && typeof error === "object" && typeof (error as { code?: unknown }).code === "string") {
    return (error as { code: string }).code
  }
  return ""
}

/** The refusal's sentence in either spelling, or "". */
export function refusalDetail(parsed: unknown): string {
  if (!parsed || typeof parsed !== "object") return ""
  const flat = (parsed as { detail?: unknown }).detail
  if (typeof flat === "string") return flat
  const error = (parsed as { error?: unknown }).error
  if (error && typeof error === "object") {
    const { message, detail } = error as { message?: unknown; detail?: unknown }
    if (typeof detail === "string") return detail
    if (typeof message === "string") return message
  }
  return ""
}

function older(status: number, code: string): boolean {
  return status === 404 || OLDER_CODES.has(code)
}

function unreachable(status: number): boolean {
  return status === 502 || status === 503 || status === 504
}

/** The status a read answered, or null when the answer is not one to speak about. */
export function settleUpdateRead(ok: boolean, parsed: unknown): UpdateStatus | null {
  if (!ok || !parsed || typeof parsed !== "object") return null
  const status = parsed as Partial<UpdateStatus>
  if (typeof status.state !== "string" || !status.running || !status.latest) return null
  return status as UpdateStatus
}

/** One read of `GET /v1/update`, sorted into what the panel can say about it. */
export function classifyUpdateRead(answer: Answer): UpdateRead {
  if (answer.transport === "failed") return { kind: "unreachable" }
  const ok = answer.status >= 200 && answer.status < 300
  const status = settleUpdateRead(ok, answer.parsed)
  if (status) return { kind: "status", status }
  if (ok) return { kind: "refused" }
  const code = refusalCode(answer.parsed)
  if (older(answer.status, code)) return { kind: "older" }
  if (unreachable(answer.status)) return { kind: "unreachable" }
  return { kind: "refused" }
}

/** How a press of 「立即更新」 came back. */
export type ApplyAnswer =
  | { kind: "started"; status: UpdateStatus | null }
  | { kind: "older" }
  | { kind: "refused"; code: string; detail: string }
  | { kind: "unreachable" }

/** One answer of `POST /v1/update/apply`. */
export function classifyApplyAnswer(answer: Answer): ApplyAnswer {
  if (answer.transport === "failed") return { kind: "unreachable" }
  if (answer.status >= 200 && answer.status < 300) {
    return { kind: "started", status: settleUpdateRead(true, answer.parsed) }
  }
  const code = refusalCode(answer.parsed)
  if (older(answer.status, code)) return { kind: "older" }
  if (unreachable(answer.status) && !code) return { kind: "unreachable" }
  return { kind: "refused", code: code || "http_" + answer.status, detail: refusalDetail(answer.parsed) }
}

/** A stamp as the panel prints it: its first eight characters. */
export function shortStamp(stamp: string | undefined): string {
  return (stamp ?? "").slice(0, 8)
}

/** A build as a person reads it: its release version, or its short commit for a source build. */
export function buildName(build: { stamp?: string; version?: string } | undefined): string {
  return build?.version || shortStamp(build?.stamp)
}

/**
 * The older daemon's one line, or null: only `update_available` and
 * `differs` speak, and a machine that is current, ahead, or could not tell
 * says nothing. This is what a daemon from before release installs gets,
 * worded as it always was.
 */
export function updateNotice(status: UpdateStatus | null, say: typeof nextWord): string | null {
  if (!status) return null
  let key: NextWord
  if (status.state === "update_available") key = "machineUpdateAvailable"
  else if (status.state === "differs") key = "machineUpdateDiffers"
  else return null
  const running = shortStamp(status.running.stamp)
  const latest = shortStamp(status.latest.stamp)
  if (!running || !latest) return null
  return say(key, { running, latest })
}

/** Whether a status names an update that is still moving. */
export function applyMoving(status: UpdateStatus | null | undefined): boolean {
  const state = status?.apply?.state
  return !!state && MOVING.has(state)
}

/** Everything the panel is drawn from: the reads, and what this page has done. */
export interface UpdatePanelInput {
  /** The most recent read, or null before the first one has come back. */
  read: UpdateRead | null
  /** The most recent read that was a status, kept across the restart's silence. */
  last: UpdateStatus | null
  /** This page pressed 「立即更新」, or watched an update move, and is following it through. */
  following: boolean
  /** The press is on the wire. */
  sending: boolean
  /** The press was answered with a refusal. */
  pressRefused: { code: string; detail: string } | null
  /** The press was answered by a machine that has no apply route. */
  pressOlder: boolean
  /** The page reads this machine through Clawdline Cloud. */
  overCloud: boolean
  /** The auto-update switch is being saved. */
  autoSaving: boolean
  /** The sentence for the last auto-update save that failed, or "". */
  autoFailed: string
}

export interface UpdateFact {
  key: "running" | "latest" | "checked" | "channel"
  label: string
  value: string
  /** The release notes, beside the latest version. */
  href?: string
  hrefLabel?: string
}

/** The code and sentence behind a plain explanation, for 「技術細節」 and its copy button. */
export interface UpdateDetails {
  code: string
  detail: string
}

/** A rolled-back or failed update, a refused press, or a machine too old to update from here. */
export interface UpdateProblem {
  kind: "rolled_back" | "failed" | "refused" | "older"
  /** What happened and what it means, in words a newcomer can act on. */
  sentence: string
  /** The code and the daemon's own sentence, folded away; null when there are none. */
  details: UpdateDetails | null
  /** A command to run on the machine, shown as code beside the sentence. */
  command: string | null
}

/** The one line that installs or reinstalls Clawdline on a machine (README.md, install.sh). */
export const INSTALL_COMMAND = "curl -fsSL https://raw.githubusercontent.com/sainteye/clawdline/main/install.sh | sh"

/**
 * The address that opens the Settings page at its update panel: the needs-update
 * line and the banner send a person here. `#page=settings` alone still opens the
 * page at its top.
 */
export const UPDATE_PANEL_FOCUS = "update"

/** Whether an address asks for the update panel itself, not only the Settings page. */
export function asksForUpdatePanel(hash: string): boolean {
  const parts = String(hash || "").replace(/^#/, "").split("&")
  return parts.includes("page=settings") && parts.includes("focus=" + UPDATE_PANEL_FOCUS)
}

/** What a copy button puts on the clipboard: the code and the sentence on one line. */
export function detailsText(details: UpdateDetails): string {
  return reason(details.code, details.detail)
}

export interface UpdatePanelView {
  /**
   * `nothing`: draw nothing. `legacy`: the older daemon's one line.
   * `older`: the needs-update line. `panel`: the panel.
   */
  kind: "nothing" | "legacy" | "older" | "panel"
  legacyLine: string | null
  facts: UpdateFact[]
  /** The one-line state under the facts: current, available, checking failed, … */
  stateLine: string | null
  /** What a failed check or an unknown comparison said, folded under the state line. */
  stateDetails: UpdateDetails | null
  /** The last update that finished, with when: 「{時間} 已從 {from} 更新到 {to}。」 */
  done: string | null
  /** `below`: the button follows the problem it retries, not the facts. */
  press: { shown: boolean; enabled: boolean; label: string; below: boolean }
  /** Where a moving update is, the restart included. */
  progress: string | null
  /** A rolled-back or failed update, a refused press, or a too-old machine, said plainly. */
  problem: UpdateProblem | null
  /** A macOS app bundle waiting for the app to quit. */
  staged: string | null
  /** A source build's one sentence on how it is updated. */
  sourceNote: string | null
  /**
   * The switch: its name never changes; `state` is the 開／關 beside it, and
   * `beta` the sentence a beta-channel reader needs, since auto-update installs
   * only stable releases.
   */
  auto: { shown: boolean; on: boolean; enabled: boolean; state: string; beta: string | null; note: string | null }
  /** How soon the panel should read again. */
  everyMs: number
  /** The daemon is between the old release and the new one. */
  restarting: boolean
}

type Say = (key: NextWord, holes?: Record<string, string | number>) => string

function details(code: string | undefined, detail: string | undefined): UpdateDetails | null {
  const c = (code ?? "").trim()
  const d = (detail ?? "").trim()
  return c || d ? { code: c, detail: d } : null
}

function reason(code: string | undefined, detail: string | undefined): string {
  const c = (code ?? "").trim()
  const d = (detail ?? "").trim()
  if (c && d) return c + " — " + d
  return c || d
}

/**
 * The panel, from its input. `when` turns an RFC3339 time into the words a
 * person reads; it is the component's, so this file stays free of the clock
 * and the locale.
 */
export function updatePanel(input: UpdatePanelInput, say: Say, when: (iso: string) => string): UpdatePanelView {
  const view: UpdatePanelView = {
    kind: "nothing",
    legacyLine: null,
    facts: [],
    stateLine: null,
    stateDetails: null,
    done: null,
    press: { shown: false, enabled: false, label: say("updateNow"), below: false },
    progress: null,
    problem: null,
    staged: null,
    sourceNote: null,
    auto: { shown: false, on: false, enabled: false, state: "", beta: null, note: null },
    everyMs: UPDATE_READ_EVERY_MS,
    restarting: false,
  }
  const read = input.read
  const status = read?.kind === "status" ? read.status : input.last
  const silent = read?.kind === "unreachable"
  // The restart: the page was following an update, or the last status said one
  // was moving, and now nothing answers. That is the update, not a failure.
  const restarting = silent && (input.following || applyMoving(input.last))
  view.restarting = restarting
  if (input.following || input.sending || restarting || (read?.kind === "status" && applyMoving(read.status))) {
    view.everyMs = UPDATE_FOLLOW_EVERY_MS
  }

  if (input.pressOlder && !status) {
    view.kind = "older"
    return view
  }
  if (!status) {
    if (restarting) {
      view.kind = "panel"
      view.progress = say("updateRestarting")
    }
    return view
  }
  // A daemon from before release installs answers without `install_kind`:
  // today's notice, worded as it was.
  if (!status.install_kind) {
    view.legacyLine = updateNotice(status, say as typeof nextWord)
    view.kind = view.legacyLine ? "legacy" : "nothing"
    return view
  }

  view.kind = "panel"
  const release = status.install_kind === "release"
  const latestName = buildName(status.latest)
  view.facts.push({ key: "running", label: say("updateRunning"), value: buildName(status.running) || say("updateUnknownVersion") })
  if (latestName) {
    const fact: UpdateFact = { key: "latest", label: say("updateLatest"), value: latestName }
    if (release && status.latest.notes_url) {
      fact.href = status.latest.notes_url
      fact.hrefLabel = say("updateNotes")
    }
    view.facts.push(fact)
  }
  if (status.checked_at) view.facts.push({ key: "checked", label: say("updateChecked"), value: when(status.checked_at) })
  if (release && status.channel) {
    const channel = status.channel === "beta" ? say("updateChannelBeta") : status.channel === "stable" ? say("updateChannelStable") : status.channel
    view.facts.push({ key: "channel", label: say("updateChannel"), value: channel })
  }

  if (status.state === "current") view.stateLine = say("updateIsCurrent")
  else if (status.state === "ahead") view.stateLine = say("updateIsAhead")
  else if (status.state === "update_available" && latestName) view.stateLine = say("updateIsAvailable", { latest: latestName })
  else if (status.state === "unknown") {
    view.stateLine = say("updateIsUnknown")
    view.stateDetails = details("", status.reason || status.error)
  }
  if (status.error && status.state !== "unknown") {
    view.stateLine = [view.stateLine, say("updateCheckFailed")].filter(Boolean).join(" ")
    view.stateDetails = details("", status.error)
  }

  if (!release) {
    view.sourceNote =
      status.install_kind === "source_deploy" ? say("updateSourceDeploy")
        : status.install_kind === "source_checkout" ? say("updateSourceCheckout")
          : say("updateKindUnknown")
    return view
  }

  const apply = status.apply
  const moving = !restarting && applyMoving(status)
  const to = apply?.to || latestName
  const from = apply?.from || buildName(status.running)
  if (restarting) view.progress = say("updateRestarting")
  else if (input.sending) view.progress = say("updateSending")
  else if (moving && apply) {
    const key: NextWord =
      apply.state === "downloading" ? "updateDownloading"
        : apply.state === "verifying" ? "updateVerifying"
          : apply.state === "staged" ? "updateStaged"
            : "updateRestarting"
    view.progress = say(key, { to })
  }
  // A finished update stays said — after the page reloads into the new
  // console, and after one auto-update installed overnight — for as long as
  // it is the machine's last.
  if (!moving && !restarting && apply?.state === "healthy" && apply.from) {
    const installed = apply.to || buildName(status.running)
    view.done = apply.at
      ? say("updateDoneAt", { when: when(apply.at), from: apply.from, to: installed })
      : say("updateDone", { from: apply.from, to: installed })
  } else if (!view.progress && apply?.state === "healthy" && input.following) {
    view.progress = say("updateHealthy", { to: buildName(status.running) || to })
  }

  if (!moving && !restarting && apply?.state === "rolled_back") {
    view.problem = { kind: "rolled_back", sentence: say("updateRolledBack", { to, from }), details: details(apply.error?.code, apply.error?.detail), command: null }
  } else if (!moving && !restarting && apply?.state === "failed") {
    view.problem = { kind: "failed", sentence: say("updateFailed", { to, from }), details: details(apply.error?.code, apply.error?.detail), command: null }
  }
  if (input.pressRefused) {
    view.problem = { kind: "refused", sentence: say("updateRefused"), details: details(input.pressRefused.code, input.pressRefused.detail), command: null }
  }
  if (input.pressOlder) {
    view.problem = { kind: "older", sentence: say("updateApplyOlder"), details: null, command: INSTALL_COMMAND }
    // "Can be installed" is not true from this page; the problem says how.
    if (!status.error) view.stateLine = null
  }
  if (apply?.staged_app && apply.state === "healthy") view.staged = say("updateStagedApp")

  // Right after the update this page followed, a read may still call what it
  // installed available: no button for that. A newer release than it is one
  // more press, on the same page — it is not reloaded when the new daemon
  // serves the console it already runs.
  const justInstalled = apply?.state === "healthy" && input.following && !moving && (!status.latest.version || status.latest.version === apply.to)
  view.press.shown = status.state === "update_available" && !justInstalled && !input.pressOlder
  view.press.enabled = view.press.shown && !input.sending && !moving && !restarting
  // After a rollback or a failure the button installs the same release again:
  // it says so, and it sits under the sentence that explains what went wrong.
  const again = (apply?.state === "rolled_back" || apply?.state === "failed") && !moving && !restarting &&
    (!status.latest.version || status.latest.version === apply.to)
  if (again) {
    view.press.label = say("updateRetry")
    // "A new version can be installed" beside "it did not start" reads as two
    // stories about one release: the problem says it alone.
    if (status.state === "update_available" && !status.error) view.stateLine = null
  }
  view.press.below = view.problem !== null

  view.auto.shown = true
  view.auto.on = status.auto_apply === true
  view.auto.state = view.auto.on ? say("updateAutoOn") : say("updateAutoOff")
  view.auto.beta = status.channel === "beta" ? say("updateAutoBeta") : null
  view.auto.enabled = !input.overCloud && !input.autoSaving
  view.auto.note = input.autoFailed || (input.overCloud ? say("updateAutoCloud") : null)
  return view
}

/**
 * Whether the page should look at the console its machine now serves: it was
 * following an update and the new daemon answered healthy. A page reading the
 * machine through Clawdline Cloud is served by Cloud, not by the machine, so
 * the machine's update does not change it.
 */
export function shouldLookForNewConsole(input: { following: boolean; overCloud: boolean; read: UpdateRead | null }): boolean {
  if (!input.following || input.overCloud || input.read?.kind !== "status") return false
  return input.read.status.apply?.state === "healthy"
}

/** Where the banner remembers a dismissal: per version, in this browser. */
export const UPDATE_BANNER_DISMISSED_KEY = "clawdline.update-banner.dismissed"

/**
 * The version the session list's banner announces, or null. Only a release
 * install with a newer release and no update already moving, and only until
 * that version has been dismissed in this browser.
 */
export function updateBannerVersion(status: UpdateStatus | null, dismissed: string | null): string | null {
  if (!status || status.install_kind !== "release" || status.state !== "update_available") return null
  if (applyMoving(status)) return null
  const version = buildName(status.latest)
  if (!version || version === dismissed) return null
  return version
}
