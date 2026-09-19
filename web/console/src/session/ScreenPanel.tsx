import type { Screen, SessionRow } from "@clawdline/contract"
import { useEffect, useRef, useState } from "react"
import { client } from "../client.js"
import * as L from "../legacy/bridge.js"
import {
  paintRows,
  readScreen,
  screenBadgeHTML,
  visibleInterval,
} from "../legacy/screen-bridge.js"

/**
 * The terminal itself, as it is right now, in the transcript's space
 * (`view/terminal.js`, `#screen-panel` in the original's `index.html`).
 *
 * **It is a mirror and it decides nothing.** What tmux drew is what is drawn
 * here, and a line it cannot explain is a line it does not have to.
 *
 * **There is no scrollback and this must not imply there is.** An assistant
 * runs on the alternate screen — measured `alternate_on=1, history_size=0` on
 * every live pane — so what arrives is the visible screen and nothing above it.
 * The panel says how many lines it got and offers no way to ask for more,
 * because there is no more to ask for.
 *
 * **The header says which backend it is looking at, always.** On tmux a
 * `pipe-pane` signal makes this live within about four milliseconds of the pane
 * moving; where no such signal exists the same panel is a sample taken when
 * somebody asks, no faster than the floor the daemon itself named. Those are
 * very different things and drawing them identically is a defect this
 * repository already had. There is no Refresh button: the panel keeps its own
 * lease, and on the backend that has to be asked it says how often it may.
 *
 * **Its own EventSource, and that is the one deliberate difference from the
 * original.** There the `screen` frame arrives on the page's single stream and
 * is handed to `Terminal.observe`. Here the console's stream belongs to the
 * fleet store in `@clawdline/core`, which listens for `sessions` alone, so this
 * opens a second connection for as long as the panel is open and closes it with
 * the panel. It costs one of the browser's six connections to this host while
 * somebody is watching a terminal, which is the cheapest way to keep the `即時`
 * badge true without changing a package this task does not own.
 */
export function ScreenPanel({
  row,
  open,
  onClose,
}: {
  row: SessionRow | null
  open: boolean
  onClose: (restore: boolean) => void
}) {
  const T = L.strings
  const [screen, setScreen] = useState<Screen | null>(null)
  const [failed, setFailed] = useState(false)
  const closeRef = useRef<HTMLButtonElement>(null)
  // Two counters, because they answer two different questions and collapsing
  // them into one is what this panel was wrong about first.
  //
  // `generation` is cancellation: it changes when the panel opens, closes or
  // changes session, and an answer from a previous generation belongs to a
  // session nobody is looking at any more.
  //
  // `asked`/`answered` is ordering: an older answer must not paint over a newer
  // one. **A newer request is not a reason to throw an older answer away.** The
  // first build refused any answer that was not the newest request, and on a
  // signalled backend the pane moves several times a second — so every answer
  // was stale before it arrived, the screen never landed, and the panel sat on
  // "載入中⋯" while issuing thirty requests in five seconds. Measured on 7763 on
  // 2026-09-18 before this was split in two.
  const generation = useRef(0)
  const asked = useRef(0)
  const answered = useRef(0)
  // What this panel believes it is showing, for the two callbacks that must not
  // re-arm themselves every time it changes: the stream's revision comparison
  // and the keepalive's "is the poll already asking" question.
  const held = useRef<Screen | null>(null)
  held.current = screen
  const polling = useRef(false)
  const id = row?.id ?? ""
  const channel = screen?.channel ?? ""
  const askAgain = screen?.askAgainAfterMs ?? 0

  // Opening is a fresh panel: nothing from the session before it survives, and
  // the close button takes the keyboard, as the original's `open()` does.
  useEffect(() => {
    if (!open) return
    generation.current += 1
    answered.current = 0
    asked.current = 0
    held.current = null
    setScreen(null)
    setFailed(false)
    closeRef.current?.focus({ preventScroll: true })
  }, [open, id])

  /**
   * Ask for the screen, which is also how this page says it is still watching.
   *
   * **Reading is the subscription.** The daemon attached its pipe because
   * somebody read and takes it off when nobody has read for thirty seconds — so
   * the keepalive here is not a poll for content, it is the lease. It runs at
   * half the lease so one lost request does not drop the pipe, and on a
   * signalled backend it costs the machine nothing at all when the pane has not
   * moved, because the answer comes out of a reading the signal invalidates.
   *
   * **Neither clock runs while the page is hidden.** A phone with the console
   * put away was asking the Mac for this screen every second. Coming back asks
   * once at once and then resumes; the lease lapsing meanwhile costs one
   * `pipe-pane` on that ask, which is what a lease is for.
   */
  useEffect(() => {
    if (!open || !id) return
    const mineGeneration = generation.current
    let alive = true
    const load = () => {
      const order = ++asked.current
      readScreen(id).then(
        (data) => {
          if (!alive || mineGeneration !== generation.current || order <= answered.current) return
          answered.current = order
          held.current = data
          setFailed(false)
          setScreen(data)
        },
        () => {
          if (!alive || mineGeneration !== generation.current || order <= answered.current) return
          answered.current = order
          setFailed(true)
        },
      )
    }
    load()

    const keepalive = visibleInterval(() => {
      // The poll already asks more often than the lease needs renewing.
      if (!polling.current) load()
    }, 15000)
    keepalive.start()

    // **Only the revision travels on the stream** — the screen itself comes
    // through the authenticated GET, exactly as a transcript append does — so
    // this is a comparison and a fetch, and a revision this panel already holds
    // is dropped without asking the daemon anything. That is where the
    // byte-identical fifth of captures would otherwise have gone.
    let stream: EventSource | null = null
    if (typeof EventSource === "function") {
      stream = new EventSource(client.url("/v1/events"))
      stream.addEventListener("screen", (ev) => {
        let moved: { id?: string; revision?: string }
        try {
          moved = JSON.parse((ev as MessageEvent).data)
        } catch {
          return
        }
        if (!moved.id || !moved.revision || moved.id !== id) return
        if (held.current && held.current.revision === moved.revision) return
        load()
      })
    }

    return () => {
      alive = false
      generation.current += 1
      keepalive.stop()
      stream?.close()
    }
  }, [open, id])

  /**
   * The other clock, and it exists only on a backend that has no change signal.
   *
   * It runs no faster than the interval the daemon itself named, because that
   * number is the machine's and not this page's.
   */
  useEffect(() => {
    if (!open || !id || channel !== "on-demand") {
      polling.current = false
      return
    }
    polling.current = true
    const mineGeneration = generation.current
    const after = Math.max(1000, Number(askAgain) || 1000)
    const poll = visibleInterval(() => {
      const order = ++asked.current
      readScreen(id).then(
        (data) => {
          if (mineGeneration !== generation.current || order <= answered.current) return
          answered.current = order
          held.current = data
          setFailed(false)
          setScreen(data)
        },
        () => {
          if (mineGeneration !== generation.current || order <= answered.current) return
          answered.current = order
          setFailed(true)
        },
      )
    }, after)
    poll.start()
    return () => {
      polling.current = false
      poll.stop()
    }
  }, [open, id, channel, askAgain])

  const body = () => {
    if (failed) {
      return (
        <div className="screen-note err" role="alert">
          {T.webScreenGone}
        </div>
      )
    }
    if (!screen || screen.text == null) {
      return (
        <div className="screen-note" role="status">
          {screen && screen.readable === false && screen.pending === false
            ? T.webScreenGone
            : T.webLoading}
        </div>
      )
    }
    return (
      <div className="screen-text" dangerouslySetInnerHTML={{ __html: paintRows(screen.text) }} />
    )
  }

  return (
    <section className="panel-view" id="screen-panel" aria-labelledby="screen-title" hidden={!open}>
      <div className="panel-view-head">
        <h2 id="screen-title">{T.webScreenTitle}</h2>
        <span
          className="screen-channel"
          id="screen-badge"
          aria-live="polite"
          dangerouslySetInnerHTML={{ __html: open ? screenBadgeHTML(screen) : "" }}
        />
        {/* 243 columns against a phone's fifty, and one layout: the capture is
            soft-wrapped with each row hanging under its own indent. There is no
            control to turn that off — see the stylesheet's note beside
            `.screen-text` for what the wrapping costs. */}
        <button
          className="chip"
          id="screen-close"
          type="button"
          ref={closeRef}
          onClick={() => onClose(true)}
        >
          {T.webClose}
        </button>
      </div>
      <div className="scroller panel-view-body" id="screen-body" aria-live="polite">
        {open ? body() : null}
      </div>
    </section>
  )
}
