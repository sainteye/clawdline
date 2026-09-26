import { useEffect, useLayoutEffect, useRef, useState } from "react"
import type { SessionRow, SessionShell, ShellOutputReply } from "@clawdline/contract"
import * as L from "../legacy/bridge.js"
import { openShell, SHELL_BEAT_MS, SHELL_WINDOW_BYTES, stepShell, sticksToBottom, type ShellRead, type ShellView } from "./background.js"

/**
 * `section#shell-panel`: what one background command has printed, in the
 * transcript's space — the Swift app's `input/shell-panel.js`, on the same
 * furniture as `GitPanel`.
 *
 * A background command has no conversation, only the file it is appending to,
 * so that file's tail is what a reader is handed. **It reads itself again
 * while it is open**, every `SHELL_BEAT_MS` while the page is visible, because
 * a command printing into a file moves nothing on the event stream. It stops
 * when the command has ended, when the id is no longer served (the ordinary
 * end of watching one), when a read fails, and on close.
 *
 * `shell` is the row as the strip showed it when the panel was opened. It is
 * kept, not read fresh from the session row, because a command that ends
 * drops off that row at exactly the moment somebody is watching it land.
 *
 * There is no Stop button: killing a command is not this panel's job yet.
 */
export function ShellPanel({ row, shell, onClose }: {
  row: SessionRow | null
  shell: SessionShell | null
  onClose: (restore: boolean) => void
}) {
  const T = L.strings
  const open = !!row && !!shell
  const [view, setView] = useState<ShellView | null>(null)
  const body = useRef<HTMLDivElement>(null)
  const first = useRef(true)
  const stick = useRef(true)
  // A read is the current one only while the panel is open on the same
  // session and command it was asked for.
  const ticket = useRef(0)
  const key = open ? row.id + "\n" + shell.id : ""

  useEffect(() => {
    if (!open) {
      ticket.current += 1
      setView(null)
      return
    }
    const mine = ++ticket.current
    const sessionID = row.id
    let current = openShell(shell)
    first.current = true
    stick.current = true
    setView(current)
    let stopped = false
    let inFlight = false
    let timer: number | undefined
    const stop = () => {
      stopped = true
      if (timer !== undefined) window.clearInterval(timer)
      document.removeEventListener("visibilitychange", onVisible)
    }
    const read = async () => {
      if (stopped || inFlight || document.visibilityState === "hidden") return
      inFlight = true
      let said: ShellRead
      try {
        said = { kind: "answer", reply: await readShell(sessionID, shell.id) }
      } catch (e) {
        said = (e as { code?: string } | null)?.code === "not_found"
          ? { kind: "missing" }
          : { kind: "failed", sentence: L.failureSentence(e, T.webShellFailed) }
      } finally {
        inFlight = false
      }
      if (stopped || mine !== ticket.current) return
      const step = stepShell(current, said)
      current = step.view
      if (step.repaint) {
        const el = body.current
        stick.current = !el || sticksToBottom(first.current, el.scrollTop, el.clientHeight, el.scrollHeight)
        setView(current)
      }
      if (!step.poll) stop()
    }
    // One read on return to a page that was hidden, then the beat again.
    const onVisible = () => {
      if (document.visibilityState === "visible") void read()
    }
    void read()
    timer = window.setInterval(() => void read(), SHELL_BEAT_MS)
    document.addEventListener("visibilitychange", onVisible)
    return () => {
      ticket.current += 1
      stop()
    }
    // The key is the subject; the row and shell are read through it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key])

  // Left where a reader watching a log expects to be left: at the bottom,
  // unless they had scrolled up to read something further back.
  useLayoutEffect(() => {
    const el = body.current
    if (!el || !view) return
    if (stick.current) el.scrollTop = el.scrollHeight
    if (!view.loading) first.current = false
  }, [view])

  useLayoutEffect(() => {
    if (!open) return
    document.getElementById("shell-close")?.focus({ preventScroll: true })
  }, [open, key])

  const meta = view?.meta ?? shell
  const ended = !!view?.ended
  const text = view?.text ?? ""
  return (
    <section
      className="panel-view"
      id="shell-panel"
      aria-labelledby="shell-title"
      hidden={!open}
      onKeyDown={(ev) => {
        if (ev.key !== "Escape") return
        ev.preventDefault()
        ev.stopPropagation()
        onClose(true)
      }}
    >
      <div className="panel-view-head">
        <h2 id="shell-title">{T.webShellTitle}</h2>
        <button className="chip" id="shell-close" type="button" onClick={() => onClose(true)}>
          {T.webShellClose}
        </button>
      </div>
      <div className="scroller panel-view-body" id="shell-body" ref={body}>
        {open && meta ? (
          <>
            {meta.command ? <p className="shell-cmd">{meta.command}</p> : null}
            <p className="shell-said" data-ended={ended ? "1" : "0"}>
              <span className="dot" />
              {meta.what ? <span>{meta.what}</span> : null}
              <span className="id">{meta.id}</span>
              <span>{ended ? T.webShellEnded : T.webShellRunning}</span>
            </p>
            {view?.error ? <div className="git-note err" role="alert">{view.error}</div> : null}
            {text.trim()
              ? <pre className="shell-out">{text}</pre>
              : view?.loading
                ? <div className="git-note" role="status">{T.webLoading}</div>
                : <div className="git-note">{T.webShellQuiet}</div>}
          </>
        ) : null}
      </div>
    </section>
  )
}

/**
 * `GET /v1/sessions/{id}/shells/{shell}`: the tail of one command's output. A
 * refusal is thrown with its code, so the panel can tell "the command is over"
 * (`not_found`) from a read that failed.
 */
export async function readShell(sessionID: string, shellID: string): Promise<ShellOutputReply> {
  let res: Response
  try {
    res = await fetch("/v1/sessions/" + encodeURIComponent(sessionID) + "/shells/" + encodeURIComponent(shellID) +
      "?bytes=" + SHELL_WINDOW_BYTES)
  } catch {
    const dead = new Error(L.strings.webOffline) as Error & { code?: string }
    dead.code = "offline"
    throw dead
  }
  const text = await res.text()
  let data: Record<string, unknown> | null = null
  try {
    data = text ? JSON.parse(text) : null
  } catch {
    /* below */
  }
  if (!res.ok) {
    const raw = data?.error
    const err: { code?: string; message?: string } =
      typeof raw === "string"
        ? { code: raw, message: typeof data?.detail === "string" ? data.detail : raw }
        : raw && typeof raw === "object"
          ? (raw as { code?: string; message?: string })
          : { code: "http_" + res.status, message: res.statusText || L.strings.webRequestFailed }
    const failure = new Error(err.message || err.code) as Error & { code?: string }
    failure.code = err.code
    throw failure
  }
  if (!data) throw new Error(L.strings.webNotJSON)
  return data as unknown as ShellOutputReply
}
