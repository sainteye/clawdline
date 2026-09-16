import { useCallback, useEffect, useRef, useState } from "react"
import type { SessionInfo, SessionInfoReply, SessionRow } from "@clawdline/contract"
import { client } from "../client.js"
import * as L from "../legacy/bridge.js"

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
 * - `.open` carries the model and the cost, from `/v1/sessions/{id}/info` —
 *   the same read the original's `status-line.js` makes, held for a minute,
 *   asked again when a turn ends. Until the first answer it says the assistant
 *   and "Loading…", as the original does. Context use is not drawn: this
 *   daemon does not read it. The button is disabled because the Session info
 *   card it opens does not exist here.
 * - `.files` stays hidden: the working tree is not part of this daemon's read.
 * - `.deploy` stays hidden: a running deploy comes from the project-link walk,
 *   which this daemon does not have.
 * - `.limits` stays empty: plan windows are not part of this daemon's read.
 */
export function StatusLine({ row, listPending = false }: { row: SessionRow | null; listPending?: boolean }) {
  const T = L.strings
  // The original draws only when the open session changes, so a page that has
  // never had one open shows the markup as written: a bare button, no title.
  const drawn = useRef(false)
  if (row) drawn.current = true
  const info = useSessionInfo(row)

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
        disabled
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
      <div className="limits" id="status-line-limits"></div>
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

function dollars(x: number): string {
  return x < 0.01 ? "<$0.01" : "$" + x.toFixed(2)
}

/**
 * One small cache in front of the info read, as `SessionFacts` is: an answer
 * is fresh for a minute, a second asker while one read is out gets the same
 * promise, and a session that was open before shows what was last read while
 * a newer answer is on its way.
 */
const FRESH_MS = 60_000
const held = new Map<string, { at: number; info: SessionInfo }>()
const pending = new Map<string, Promise<SessionInfo>>()

function readInfo(id: string, force: boolean): Promise<SessionInfo> {
  const hit = held.get(id)
  if (!force && hit && Date.now() - hit.at < FRESH_MS) return Promise.resolve(hit.info)
  const out = pending.get(id)
  if (!force && out) return out
  const read = fetch(client.url(`/v1/sessions/${encodeURIComponent(id)}/info?parts=summary`))
    .then((r) => (r.ok ? (r.json() as Promise<SessionInfoReply>) : Promise.reject(new Error(`info ${r.status}`))))
    .then((reply) => {
      held.set(id, { at: Date.now(), info: reply.info })
      return reply.info
    })
    .finally(() => {
      if (pending.get(id) === read) pending.delete(id)
    })
  pending.set(id, read)
  return read
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
    readInfo(want, force).then(
      (info) => {
        if (mine === ticket.current && current.current === want) setAnswer({ id: want, info })
      },
      () => {},
    )
  }, [])

  useEffect(() => {
    current.current = id
    ticket.current += 1
    stateSeen.current = state
    nextAt.current = 0
    owed.current = ""
    const last = id ? held.get(id) : undefined
    setAnswer(id && last ? { id, info: last.info } : null)
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
  return held.get(id)?.info ?? null
}
