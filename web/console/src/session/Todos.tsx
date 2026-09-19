import { useEffect, useRef, useState } from "react"
import type { SessionRow } from "@clawdline/contract"
import { RefusalError } from "@clawdline/core"
import * as L from "../legacy/bridge.js"
import { readTodos, type TodoPage } from "../pages/work/api.js"
import { when } from "../pages/work/shared.js"
import { workWord, type WorkWord } from "../pages/work/words.js"
import "../pages/work/work.css"

/**
 * A session's to-dos (board-redesign §3.3, §5.2; design-decisions T6, D35):
 * what the machine is keeping for this session so that it does not forget.
 *
 * **Folded, and on the session's page only.** Their reader is the session;
 * a person looks when they want to see what the machine keeps for itself, and
 * never has to tend it — every row is made and closed by a broker fact (§3.4:
 * "人永遠不需要編輯待辦"). So there are no buttons here, the strip is folded
 * until opened, and nothing of it is on the board: #5 is "not on the same
 * page", not "collapsed on it".
 *
 * The count on the fold is read when the session is opened, and the list when
 * the fold is. A session whose conversation is not known yet, or a list that
 * could not be read, says that — neither is shown as "owes nothing" (DG-7).
 */
export function Todos({ row }: { row: SessionRow | null }) {
  const [open, setOpen] = useState(false)
  const [closed, setClosed] = useState(false)
  const [page, setPage] = useState<TodoPage | null>(null)
  const [failure, setFailure] = useState<"unknown" | "unreadable" | null>(null)
  const ticket = useRef(0)

  // A different session is a different list: fold it, and forget the last one.
  useEffect(() => {
    setOpen(false)
    setClosed(false)
    setPage(null)
  }, [row?.id])

  // Read on arrival and again each time the fold is opened; the last answer
  // stays on screen until the next one replaces it.
  useEffect(() => {
    const mine = ++ticket.current
    setFailure(null)
    if (!row) return
    readTodos(row.id, open && closed ? "all" : "outstanding").then(
      (p) => {
        if (mine === ticket.current) setPage(p)
      },
      (e: unknown) => {
        if (mine !== ticket.current) return
        setFailure(e instanceof RefusalError && e.code === "conversation_unknown" ? "unknown" : "unreadable")
      },
    )
  }, [row?.id, open, closed])

  if (!row) return null
  const owed = page ? (page.counts.open ?? 0) + (page.counts.handed_off ?? 0) : null
  return (
    <details
      className="session-todos"
      id="session-todos"
      open={open}
      onToggle={(ev) => setOpen((ev.currentTarget as HTMLDetailsElement).open)}
    >
      <summary>
        <b>{workWord("todosTitle")}</b>
        <span id="session-todos-count">
          {failure ? "?" : owed === null ? L.strings.webLoading : workWord("todosOpen", { n: owed })}
        </span>
      </summary>
      <div className="session-todos-body">
        <p>{workWord("todosLede")}</p>
        {failure === "unknown" ? (
          <p>{workWord("todosUnknown")}</p>
        ) : failure === "unreadable" ? (
          <p>{workWord("todosUnreadable")}</p>
        ) : page && page.todos.length === 0 ? (
          <p>{workWord("todosNone")}</p>
        ) : (
          page?.todos.map((t) => (
            <div
              key={t.id}
              className="session-todo"
              data-todo-id={t.id}
              data-state={t.state}
              data-escalated={t.escalation.length ? "" : undefined}
            >
              <b>{t.title || t.task_id}</b>
              <span className="session-todo-state">
                {t.escalation.length && t.state === "open" ? workWord("todoEscalated") : stateWords(t.state)}
              </span>
              <small>
                {t.origin === "dispatch" ? workWord("todoDispatch") : t.origin} · {t.task_id.slice(0, 8)} ·{" "}
                {when(t.closed_at ?? t.updated_at)}
                {t.state !== "open" ? ` · ${reasonWords(t.reason)}` : ""}
              </small>
            </div>
          ))
        )}
        {page?.next_cursor && (
          <p>{workWord("more", { n: Math.max(0, shownTotal(page, open && closed) - page.todos.length) })}</p>
        )}
        {page && (
          <button className="chip" type="button" style={{ marginTop: 8 }} onClick={() => setClosed((c) => !c)}>
            {workWord(closed ? "todosHideClosed" : "todosShowClosed")}
          </button>
        )}
      </div>
    </details>
  )
}

/** How many to-dos the page read would hold if it were all one page: the owed ones, or every one. */
function shownTotal(page: TodoPage, all: boolean): number {
  const c = page.counts
  const owed = (c.open ?? 0) + (c.handed_off ?? 0)
  return all ? owed + (c.done ?? 0) + (c.dropped ?? 0) : owed
}

const STATE_WORD: Record<string, WorkWord> = {
  open: "todoOpen",
  handed_off: "todoHandedOff",
  dropped: "todoDropped",
}

/** Why a to-do closed, in the catalog's words where it has them (the Projects page's landing words). */
function reasonWords(reason: string): string {
  const T = L.strings
  switch (reason) {
    case "landed":
      return T.webProjectLanded
    case "nothing_to_land":
      return T.webProjectNothingToLand
    case "abandoned":
      return T.webProjectAbandoned
  }
  return reason
}

/** A to-do's state in words: the catalog's 完成 for done, the wire's own word for one this page does not know. */
function stateWords(state: string): string {
  if (state === "done") return L.strings.webTaskDone
  return STATE_WORD[state] ? workWord(STATE_WORD[state]) : state
}
