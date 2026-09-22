import { useEffect, useLayoutEffect, useRef, useState } from "react"
import type { GitFileDiff, GitSnapshot, SessionRow } from "@clawdline/contract"
import * as L from "../legacy/bridge.js"
import { bodyHTML, gitSentence, readGit, readGitDiff } from "../legacy/git-bridge.js"

/**
 * `section#git-panel`: a read-only view of the open session's repository,
 * taking the transcript's space (`input/git-panel.js`).
 *
 * **It owns no cache beyond the time it is visible.** Opening and refreshing
 * both ask Git at that moment, and a ticket makes an answer for the previous
 * session harmless if the reader moves on while it is in flight — the panel
 * closes and follows the selection, so the only thing that could arrive late
 * is a reading of a repository nobody is looking at any more.
 *
 * The body is written as markup rather than as elements, by the original's own
 * builders (`legacy/git-bridge.ts`), so the copied stylesheet has the DOM it
 * was written for. React renders the section once and never its children.
 */
export function GitPanel({ row, open, onClose }: { row: SessionRow | null; open: boolean; onClose: (restore: boolean) => void }) {
  const T = L.strings
  const body = useRef<HTMLDivElement>(null)
  const [state, setState] = useState<Panel>(emptyPanel())
  // `ticket`: a reading is the current one only while nobody has asked again
  // and the panel is still open on the same session.
  const ticket = useRef(0)
  const drawn = useRef<string | null>(null)

  const load = (id: string) => {
    const mine = ++ticket.current
    setState({ ...emptyPanel(), loading: true })
    readGit(id).then(
      (data) => {
        if (mine !== ticket.current) return
        setState({ ...emptyPanel(), snapshot: data.git || { branch: "", head: "", ahead: 0, behind: 0, clean: true, files: [] } })
      },
      (e: unknown) => {
        if (mine !== ticket.current) return
        // Said by its code, never by the machine's English sentence, and never
        // flattened into one (`gitSentence`): "無法讀取 Git 變更" was every
        // refusal this panel had, including the one that was this console
        // refusing its own request.
        setState({ ...emptyPanel(), error: gitSentence(e, T.webGitFailed) })
      },
    )
  }

  // Opening is what asks. Closing raises the ticket, so a reading still in
  // flight cannot draw into a panel that has gone.
  useEffect(() => {
    if (open && row) load(row.id)
    else {
      ticket.current += 1
      setState(emptyPanel())
    }
    // The id is the subject; `load` reads everything else through refs.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, row?.id])

  // `follow`: the reader moved to another session, or left the detail. The
  // panel goes with them, and without taking focus back to a menu they are no
  // longer in.
  useEffect(() => {
    if (open) onClose(false)
    // Only when the session changes; `open` is deliberately not a dependency.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [row?.id])

  useLayoutEffect(() => {
    const el = body.current
    if (!el) return
    const want = open ? bodyHTML(state) : ""
    if (want === drawn.current) return
    drawn.current = want
    el.innerHTML = want
  })

  // Focus lands on Close, which is the way out of a panel that took the whole
  // pane; `preventScroll` because the pane under it has just been hidden.
  useLayoutEffect(() => {
    if (!open) return
    document.getElementById("git-close")?.focus({ preventScroll: true })
  }, [open])

  return (
    <section
      className="panel-view"
      id="git-panel"
      aria-labelledby="git-title"
      hidden={!open}
      onKeyDown={(ev) => {
        if (ev.key !== "Escape") return
        ev.preventDefault()
        ev.stopPropagation()
        onClose(true)
      }}
    >
      <div className="panel-view-head">
        <h2 id="git-title">{T.webGitTitle}</h2>
        <button className="chip" id="git-refresh" type="button" onClick={() => row && load(row.id)}>
          {T.webGitRefresh}
        </button>
        <button className="chip" id="git-close" type="button" onClick={() => onClose(true)}>
          {T.webGitClose}
        </button>
      </div>
      <div
        className="scroller panel-view-body"
        id="git-body"
        aria-live="polite"
        ref={body}
        onClick={(event) => {
          const target = (event.target as HTMLElement).closest<HTMLButtonElement>("button[data-git-path]")
          const path = target?.dataset.gitPath
          if (!path || !row) return
          if (state.openPath === path) {
            setState((at) => ({ ...at, openPath: null, diffLoading: false, diffError: null, diff: null }))
            return
          }
          const mine = ++ticket.current
          setState((at) => ({ ...at, openPath: path, diffLoading: true, diffError: null, diff: null }))
          readGitDiff(row.id, path).then(
            (data) => {
              if (mine !== ticket.current) return
              setState((at) => ({ ...at, diffLoading: false, diff: data.diff }))
            },
            (error: unknown) => {
              if (mine !== ticket.current) return
              setState((at) => ({ ...at, diffLoading: false, diffError: gitSentence(error, T.webGitFailed) }))
            },
          )
        }}
      ></div>
    </section>
  )
}

interface Panel {
  loading: boolean
  error: string | null
  snapshot: GitSnapshot | null
  openPath: string | null
  diffLoading: boolean
  diffError: string | null
  diff: GitFileDiff | null
}

function emptyPanel(): Panel {
  return { loading: false, error: null, snapshot: null, openPath: null, diffLoading: false, diffError: null, diff: null }
}
