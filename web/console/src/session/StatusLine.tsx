import { useCallback, useEffect, useRef, useState } from "react"
import type { GitSnapshot, ProjectLink, SessionInfo, SessionInfoContext, SessionLimits, SessionRow } from "@clawdline/contract"
import { contextCell } from "./context.js"
import { conversationBecameKnown } from "./info-freshness.js"
import { statusLimitCells } from "./status-limits.js"
import * as L from "../legacy/bridge.js"
import { SessionFacts, requestInfo } from "../overlays/index.js"
import { nextWord } from "../next-strings.js"
import { conversationNotStarted } from "./readiness.js"
import { readGit } from "../legacy/git-bridge.js"

/**
 * The status line under the open conversation — the original's `footer#status-line`.
 *
 * How to use it: render it in `section.pane-detail`, **directly after
 * `<Composer />`**, as the last child of the pane, and give it the same row:
 *
 *     <Composer row={row} onDid={onDid} />
 *     <StatusLine row={row} />
 *
 * It is inside the column and not across the foot of the window — see the
 * original's markup for why — and `legacy/status-line.css` styles it by
 * `.status-line`, so it needs no wrapper. `listPending` is optional: pass `true`
 * while the session list has not answered yet, and the empty row stays blank
 * instead of asking the reader to pick a session from a list that is not there
 * (the original's `listUnknown`).
 *
 * The four parts are all here, in the original's order: `.open`, `.files`,
 * `.deploy`, `.limits`. What this daemon can fill is filled; the rest is the
 * original's empty element:
 *
 * - `.open` carries the model, the context reading and the cost, from
 *   `/v1/sessions/{id}/info` — the same read the original's `status-line.js`
 *   makes, held for a minute, asked again when a turn ends. Until the first
 *   answer it says the assistant and "Loading…", as the original does. The
 *   answer is `SessionFacts` (`overlays/facts.ts`), shared with the Session
 *   info card, which a press on the button opens; it is disabled while no
 *   session is open.
 * - `.files` carries the branch and changed-file marks from the lock-free Git
 *   read. It opens the same panel as the Tools menu, including on a phone.
 * - `.deploy` carries whatever is running, from the same `/info` answer's
 *   `deploy` rows — the project-link walk, served from a projection per
 *   working directory (`links.go`), so the chip costs the page nothing beyond
 *   the read it was already making.
 * - `.limits` carries the plan windows, from the same `/info` answer: the
 *   account-level reading every session of that assistant shares. The newest
 *   reading per assistant is held and drawn over an older answer, and while an
 *   answer is still loading the held one is drawn, as the original's
 *   `SessionFacts` does (`machineLimits`).
 */
export function StatusLine({
  row,
  listPending = false,
  onOpenGit,
}: {
  row: SessionRow | null
  listPending?: boolean
  onOpenGit?: () => void
}) {
  const T = L.strings
  // The original draws only when the open session changes, so a page that has
  // never had one open shows the markup as written: a bare button, no title.
  const drawn = useRef(false)
  if (row) drawn.current = true
  const info = useSessionInfo(row)
  const windows = row ? (info ? overlayMachineLimits(info) : machineLimits(row.assistant))?.windows ?? [] : []
  const limitCells = statusLimitCells(windows, T.webInfoUnknown)
  const deploy = runningDeploy(info)
  const progress = useDeployProgress(deploy)
  const git = useGitStatus(row)

  let open
  if (!drawn.current) {
    open = <button className="open" id="status-line-open" type="button" disabled></button>
  } else {
    open = (
      <button
        className="open"
        id="status-line-open"
        type="button"
        title={`${T.webSessionInfo} (⌘I)`}
        aria-label={T.webSessionInfo}
        disabled={!row}
        onClick={requestInfo}
      >
        {row ? (
          info ? (
            <>
              <span
                className="item model"
                dangerouslySetInnerHTML={{
                  __html: modelHTML(info.session.assistant, modelName(info) || L.assistantDisplayName(info.session.assistant)),
                }}
              />
              {contextItem(info.context)}
              {typeof info.usage?.costUsd === "number" ? (
                <span className="item cost">{dollars(info.usage.costUsd)}</span>
              ) : null}
            </>
          ) : conversationNotStarted(row) ? (
            <>
              <span
                className="item model"
                dangerouslySetInnerHTML={{ __html: modelHTML(row.assistant, L.assistantDisplayName(row.assistant)) }}
              />
              <span className="item empty">{nextWord("sessionNotStartedShort")}</span>
            </>
          ) : (
            <>
              <span
                className="item model"
                dangerouslySetInnerHTML={{ __html: modelHTML(row.assistant, L.assistantDisplayName(row.assistant)) }}
              />
              <span className="item empty">{T.webLoading}</span>
            </>
          )
        ) : listPending ? null : (
          <span className="empty">{T.webPickSession}</span>
        )}
      </button>
    )
  }

  return (
    <footer className="status-line" id="status-line">
      {open}
      <button
        className="files"
        id="status-line-files"
        type="button"
        hidden={!git}
        title={T.webGitTitle}
        aria-label={T.webGitTitle}
        onClick={onOpenGit}
      >
        {git ? gitStatus(git) : null}
      </button>
      {deployChip(deploy, progress, drawn.current)}
      <div className="limits" id="status-line-limits">
        {limitCells.map((window, index) => (
          <span className="limit" data-level={window.level} key={`${window.name}:${index}`}>
            {window.name} <b>{window.value}</b>
          </span>
        ))}
      </div>
    </footer>
  )
}

function gitStatus(git: GitSnapshot) {
  const branch = git.branch || String(git.head || "").slice(0, 8)
  const staged = git.files.filter((file) => file.staged).length
  const unstaged = git.files.filter((file) => file.unstaged && file.kind !== "untracked" && file.kind !== "conflict").length
  const untracked = git.files.filter((file) => file.kind === "untracked").length
  const conflicts = git.files.filter((file) => file.kind === "conflict").length
  return (
    <>
      <span className="branch">⎇ {branch}{git.ahead ? ` ↑${git.ahead}` : ""}{git.behind ? ` ↓${git.behind}` : ""}</span>
      {git.clean ? <span className="mark" data-k="clean">✓</span> : null}
      {staged ? <span className="mark" data-k="staged">+{staged}</span> : null}
      {unstaged ? <span className="mark" data-k="unstaged">*{unstaged}</span> : null}
      {untracked ? <span className="mark" data-k="untracked">?{untracked}</span> : null}
      {conflicts ? <span className="mark" data-k="conflict">!{conflicts}</span> : null}
    </>
  )
}

/** The working tree follows the same visible-minute and turn-end cadence as info. */
function useGitStatus(row: SessionRow | null): GitSnapshot | null {
  const id = row?.id ?? null
  const state = row?.state ?? ""
  const [answer, setAnswer] = useState<{ id: string; git: GitSnapshot } | null>(null)
  const ticket = useRef(0)
  const stateSeen = useRef(state)

  const load = useCallback(() => {
    if (!id || document.hidden) return
    const mine = ++ticket.current
    readGit(id).then(
      (data) => {
        if (mine === ticket.current && data.git) setAnswer({ id, git: data.git })
      },
      () => {
        if (mine === ticket.current) setAnswer(null)
      },
    )
  }, [id])

  useEffect(() => {
    ticket.current += 1
    stateSeen.current = state
    setAnswer(null)
    load()
  }, [id, load])

  useEffect(() => {
    const ended = stateSeen.current === "working" && state !== "working"
    stateSeen.current = state
    if (ended) load()
  }, [state, load])

  useEffect(() => {
    if (!id) return
    const timer = window.setInterval(load, FRESH_MS)
    const visible = () => {
      if (!document.hidden) load()
    }
    document.addEventListener("visibilitychange", visible)
    return () => {
      window.clearInterval(timer)
      document.removeEventListener("visibilitychange", visible)
    }
  }, [id, load])

  return answer?.id === id ? answer.git : null
}

/**
 * `runningDeploy` in `input/status-line.js`: the one row the chip at the foot
 * of the page is drawn from, out of everything the project has an address for.
 *
 * **A local run wins over a deploy.** Both are "something is happening", but
 * only one of them is happening on the machine in front of the person, started
 * by the person, and holding up the next thing they were going to do; a deploy
 * running in somebody's cloud can wait for the Links sheet. There is one chip
 * and this is how it is spent.
 */
export function runningDeploy(info: SessionInfo | null): ProjectLink | null {
  const rows = (info && (info.links || info.deploy)) || []
  const running = rows.filter((row) => row && row.state === "running" &&
    (row.kind === "run" || row.kind === "deploy" || row.kind === "ci"))
  return running.filter((row) => row.kind === "run")[0] || running[0] || null
}

/**
 * `deployProgress`: how far along, by elapsed time against how long this
 * usually takes. Null when either number is missing or nonsense — a bar drawn
 * from a number nobody wrote is a bar that lies, and the stylesheet has a
 * `data-known="false"` animation for exactly that.
 */
export function deployProgress(row: ProjectLink | null, now = Date.now()): number | null {
  const started = Number(row && row.startedAt)
  const typical = Number(row && row.typicalSeconds)
  if (!Number.isFinite(started) || !Number.isFinite(typical) || started <= 0 || typical <= 0) return null
  return Math.max(0, Math.min(1, (now / 1000 - started) / typical))
}

/**
 * `drawDeploy`: the chip, or the original's empty hidden element.
 *
 * `phase` is producer text and is drawn verbatim, in every language, in place
 * of the percentage — the bar is already saying how far along this is, and
 * "compiling" answers the question a percentage cannot. `data-kind` is what
 * the stylesheet reads to keep a local run from being mistaken for a deploy at
 * a glance.
 */
function deployChip(row: ProjectLink | null, progress: number | null, drawn: boolean) {
  if (!row) {
    return (
      <a className="deploy" id="status-line-deploy" hidden target="_blank" rel="noopener noreferrer"
        {...(drawn ? { "data-kind": "" } : {})}></a>
    )
  }
  const known = progress !== null
  const pct = known ? Math.round((progress as number) * 100) : null
  const label = row.label || "deploy"
  const phase = String(row.phase == null ? "" : row.phase).trim()
  const said = label + " " + (phase || (known ? pct + "%" : L.strings.webLinkRunning))
  const href = /^https?:\/\//i.test(String(row.url || "")) ? { href: row.url } : {}
  return (
    <a
      className="deploy"
      id="status-line-deploy"
      target="_blank"
      rel="noopener noreferrer"
      data-known={known ? "true" : "false"}
      data-kind={row.kind || "deploy"}
      title={said}
      aria-label={said}
      {...href}
    >
      <span className="label">{label}</span>
      <span className="track" aria-hidden="true">
        <i style={{ "--w": (known ? pct : 0) + "%" } as React.CSSProperties}></i>
      </span>
      <span className="pct">{phase ? phase : known ? pct + "%" : "…"}</span>
    </a>
  )
}

/**
 * The chip's own clock, as the original's `deployTicker` is: a bar drawn from
 * elapsed time has to be redrawn for the time to elapse. It runs only while a
 * running row with two usable numbers is on screen, and a hidden page moves
 * nothing — there is nobody to move it for.
 */
function useDeployProgress(row: ProjectLink | null): number | null {
  const [, tick] = useState(0)
  const known = deployProgress(row) !== null
  useEffect(() => {
    if (!row || row.state !== "running" || !known) return
    const id = window.setInterval(() => {
      if (!document.hidden) tick((n) => n + 1)
    }, 1000)
    return () => window.clearInterval(id)
  }, [row, row?.label, row?.startedAt, known])
  return deployProgress(row)
}

/**
 * The logo and the name, as `identityHTML` writes them: `pixels.js`'s
 * `assistantLogo` followed by `<span class="word">`.
 */
function modelHTML(assistant: string | undefined, word: string): string {
  return `${L.assistantLogoHTML(assistant)}<span class="word">${L.escapeHTML(word)}</span>`
}

/**
 * `modelName` in `status-line.js`: the session's current model, shown by the
 * name its picker row gives it. The row is matched by id or by id prefix, so a
 * dated id still finds its name; a model with no row is shown as written.
 */
function modelName(info: SessionInfo): string {
  const current = info.session.model || info.usage?.model || ""
  const row = info.models.find((m) => current && (current === m.id || current.indexOf(m.id) === 0))
  return row ? row.name : current
}

/**
 * The `.item.context` cell of `identityHTML` in `status-line.js`. What it says
 * is decided in `context.ts`, which is where the tests are; this is the markup
 * the original writes for it, `legacy/status-line.css` colouring `data-level`.
 */
function contextItem(at: SessionInfoContext | undefined) {
  const cell = contextCell(at, L.strings.webInfoTokens)
  if (!cell) return null
  return (
    <span className="item context" data-level={cell.level} title={cell.title}>
      ctx <b>{cell.percent}%</b>
    </span>
  )
}

/**
 * `machineLimitsByAssistant` in `createTieredSessionFacts`: the newest plan
 * reading per assistant. Plan windows belong to the account, so an answer for
 * one session updates what every session of that assistant shows, and two
 * answers that complete out of order are ordered by when the daemon read the
 * windows (`readAtMs`, or the provider's own `at` when an answer has no stamp).
 */
const heldLimits = new Map<string, { readAtMs: number; limits: SessionLimits }>()

function machineLimits(assistant: string | undefined): SessionLimits | null {
  return (assistant && heldLimits.get(assistant)?.limits) || null
}

/** `captureMachineLimits` and `overlayMachineLimits` together: remember this answer's windows if they are the newest, then answer with the newest. */
function overlayMachineLimits(info: SessionInfo): SessionLimits | undefined {
  const assistant = info.session.assistant || ""
  const limits = info.limits
  if (assistant && limits) {
    let readAtMs = Number(limits.readAtMs)
    if (!Number.isFinite(readAtMs) && Number.isFinite(Number(limits.at))) readAtMs = Number(limits.at) * 1000
    if (Number.isFinite(readAtMs)) {
      const held = heldLimits.get(assistant)
      if (!held || readAtMs >= held.readAtMs) heldLimits.set(assistant, { readAtMs, limits })
    }
  }
  return machineLimits(assistant) ?? limits
}

function dollars(x: number): string {
  return x < 0.01 ? "<$0.01" : "$" + x.toFixed(2)
}

/**
 * When the original reads: when the open session changes, when a turn ends
 * (the moment totals most likely moved, and then regardless of age), and on
 * a visible minute clock. A hidden page reads nothing and asks once when it is
 * visible again. A failed read keeps the last good answer.
 */
function useSessionInfo(row: SessionRow | null): SessionInfo | null {
  const id = row?.id ?? null
  const state = row?.state ?? ""
  const conversation = row?.sessionId ?? ""
  // Held with the id it answers for, so the first paint after a switch cannot
  // show the previous session's model under the new one's name.
  const [answer, setAnswer] = useState<{ id: string; info: SessionInfo } | null>(null)
  const current = useRef<string | null>(null)
  const ticket = useRef(0)
  const nextAt = useRef(0)
  const stateSeen = useRef("")
  const conversationSeen = useRef("")
  const owed = useRef<"" | "due" | "force">("")

  const load = useCallback((force: boolean) => {
    const want = current.current
    if (!want) return
    owed.current = ""
    const mine = ++ticket.current
    nextAt.current = Date.now() + FRESH_MS
    SessionFacts.getSummary(want, force).then(
      (info) => {
        if (info && mine === ticket.current && current.current === want) setAnswer({ id: want, info })
      },
      () => {},
    )
  }, [])

  // `StatusLine.receive`: the card read the whole answer, and the line shows it too.
  useEffect(
    () =>
      SessionFacts.subscribe((from, info) => {
        if (from === current.current) setAnswer({ id: from, info })
      }),
    [],
  )

  useEffect(() => {
    current.current = id
    conversationSeen.current = conversation
    ticket.current += 1
    stateSeen.current = state
    nextAt.current = 0
    owed.current = ""
    const last = SessionFacts.peek(id)
    setAnswer(id && last ? { id, info: last } : null)
    if (!id) return
    if (document.hidden) owed.current = "due"
    else load(false)
    // Only a change of session starts this; `state` is read here as the
    // starting point and followed by the effect below.
  }, [id, load])

  // Codex writes its rollout only after the first message. The terminal row
  // therefore commonly learns `sessionId` after the first `/info` answer has
  // already been cached. That answer honestly had no usage then, but it must
  // not remain on screen for the rest of the minute after the record appears.
  useEffect(() => {
    const refresh = conversationBecameKnown(conversationSeen.current, conversation)
    conversationSeen.current = conversation
    if (!id || !refresh) return
    if (document.hidden) owed.current = "force"
    else load(true)
  }, [id, conversation, load])

  useEffect(() => {
    if (!id) return
    const endedTurn = stateSeen.current === "working" && state !== "working"
    stateSeen.current = state
    if (!endedTurn) return
    if (document.hidden) owed.current = "force"
    else load(true)
  }, [id, state, load])

  useEffect(() => {
    if (!id) return
    const tick = window.setInterval(() => {
      if (document.hidden) {
        if (Date.now() >= nextAt.current && !owed.current) owed.current = "due"
        return
      }
      if (Date.now() >= nextAt.current) load(false)
    }, FRESH_MS)
    const visible = () => {
      if (document.hidden || !owed.current) return
      if (owed.current === "force") load(true)
      else if (Date.now() >= nextAt.current) load(false)
      else owed.current = ""
    }
    document.addEventListener("visibilitychange", visible)
    return () => {
      window.clearInterval(tick)
      document.removeEventListener("visibilitychange", visible)
    }
  }, [id, load])

  if (!id) return null
  if (answer?.id === id) return answer.info
  return SessionFacts.peek(id)
}

/** How long an answer stays fresh, as `SessionFacts` holds it. */
const FRESH_MS = 60_000
