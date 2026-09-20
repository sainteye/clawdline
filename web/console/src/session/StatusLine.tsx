import { useCallback, useEffect, useRef, useState } from "react"
import type { SessionInfo, SessionInfoContext, SessionLimitWindow, SessionLimits, SessionRow } from "@clawdline/contract"
import { contextCell } from "./context.js"
import * as L from "../legacy/bridge.js"
import { SessionFacts, requestInfo } from "../overlays/index.js"

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
 * - `.files` stays hidden: the working tree is not part of this daemon's read.
 * - `.deploy` stays hidden: a running deploy comes from the project-link walk,
 *   which this daemon does not have.
 * - `.limits` carries the plan windows, from the same `/info` answer: the
 *   account-level reading every session of that assistant shares. The newest
 *   reading per assistant is held and drawn over an older answer, and while an
 *   answer is still loading the held one is drawn, as the original's
 *   `SessionFacts` does (`machineLimits`).
 */
export function StatusLine({ row, listPending = false }: { row: SessionRow | null; listPending?: boolean }) {
  const T = L.strings
  // The original draws only when the open session changes, so a page that has
  // never had one open shows the markup as written: a bare button, no title.
  const drawn = useRef(false)
  if (row) drawn.current = true
  const info = useSessionInfo(row)
  const windows = row ? (info ? overlayMachineLimits(info) : machineLimits(row.assistant))?.windows ?? [] : []

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
      <button className="files" id="status-line-files" type="button" hidden></button>
      <a
        className="deploy"
        id="status-line-deploy"
        hidden
        target="_blank"
        rel="noopener noreferrer"
        {...(drawn.current ? { "data-kind": "" } : {})}
      ></a>
      <div
        className="limits"
        id="status-line-limits"
        dangerouslySetInnerHTML={{ __html: windows.length ? limitsHTML(windows) : "" }}
      ></div>
    </footer>
  )
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
 * `limitsHTML` in `status-line.js`: one `.limit` per window, its percentage
 * rounded and clamped, coloured by the same two thresholds as the context
 * reading; a window with no percentage says "unknown".
 */
function limitsHTML(windows: SessionLimitWindow[]): string {
  return windows
    .map((w) => {
      const pct = typeof w.usedPercent === "number" ? Math.max(0, Math.min(100, Math.round(w.usedPercent))) : null
      const level = pct === null ? "" : pct >= 85 ? "bad" : pct >= 60 ? "warn" : "ok"
      return (
        `<span class="limit" data-level="${level}">${L.escapeHTML(w.name)} ` +
        `<b>${pct === null ? L.escapeHTML(L.strings.webInfoUnknown) : pct + "%"}</b></span>`
      )
    })
    .join("")
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
  // Held with the id it answers for, so the first paint after a switch cannot
  // show the previous session's model under the new one's name.
  const [answer, setAnswer] = useState<{ id: string; info: SessionInfo } | null>(null)
  const current = useRef<string | null>(null)
  const ticket = useRef(0)
  const nextAt = useRef(0)
  const stateSeen = useRef("")
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
