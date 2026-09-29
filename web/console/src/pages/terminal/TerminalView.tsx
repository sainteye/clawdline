import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react"
import type { Terminal as TerminalRow, TerminalControl, TerminalFrame, TerminalHolder, TerminalRefusal, TerminalState, TerminalStatus } from "@clawdline/contract"
import type { Terminal as XTerm } from "@xterm/xterm"
import type { FitAddon } from "@xterm/addon-fit"
import { quietFleetStream } from "../../client.js"
import { byteWords, nextWord } from "../../next-strings.js"
import {
  TerminalRequestError,
  closeTerminal,
  fetchTransport,
  readHistory,
  readTerminal,
  resizeTerminal,
  streamURL,
} from "./api.js"
import { frameBytes } from "./frame-writer.js"
import { TerminalInputClient, type InputState } from "./input-client.js"
import { TAB } from "./tab.js"
import { holderWords, terminalRefusalWords } from "./words.js"

/**
 * One terminal: its screen, drawn by xterm.js from the daemon's frames, and
 * its header, which is always on screen and always says the truth about the
 * three things that decide whether a keystroke lands — which machine, how
 * fresh the screen is, and who holds input.
 *
 * xterm.js is loaded here and nowhere else (a dynamic import, so the console's
 * main bundle does not carry it). It never runs the program's output: every
 * frame is the whole screen, written in place (frame-writer.ts), and the
 * terminal is kept at the frame's size. What it does do is encode keys the way
 * the program asked (DECCKM, the keypad, mouse modes), compose IME input, and
 * hand the bytes to the input client, which owns every rule about sending
 * them (input-client.ts).
 *
 * The stream is this page's own EventSource on /v1/terminals/{id}/stream; the
 * page never opens /v1/events, and while it is on screen the console's session
 * list reads instead of holding that stream open (a browser keeps about six
 * connections to one host; client.ts `quietFleetStream`).
 */

const FONT = 'ui-monospace, "SF Mono", "Cascadia Mono", "Noto Sans Mono CJK TC", Menlo, Consolas, monospace'
const MIN_FONT = 9

function baseFont(): number {
  return typeof window !== "undefined" && window.matchMedia?.("(max-width: 899px)").matches ? 13 : 14
}

/** Strip SGR and other CSI sequences for the read-only history. */
function plain(line: string): string {
  return line.replace(/\x1b\[[0-9;:?]*[ -/]*[@-~]/g, "").replace(/\x1b[=>]/g, "")
}

function clock(at: number): string {
  return new Date(at * 1000).toLocaleTimeString()
}

type History = { kind: "loading" } | { kind: "failed"; why: string } | { kind: "lines"; lines: string[] }
type Stream = "connecting" | "open" | "lost"

function errorWords(e: unknown): string {
  if (e instanceof TerminalRequestError) return terminalRefusalWords(e.code)
  return e instanceof Error ? e.message : String(e)
}

export function TerminalView({ id, shown, onBack }: { id: string; shown: boolean; onBack: () => void }) {
  const host = useRef<HTMLDivElement>(null)
  const scroller = useRef<HTMLDivElement>(null)
  const term = useRef<XTerm | null>(null)
  const fit = useRef<FitAddon | null>(null)
  const input = useRef<TerminalInputClient | null>(null)
  const lastFrame = useRef<TerminalFrame | null>(null)
  const ctrlArmed = useRef(false)
  const autoAcquired = useRef(false)
  const resizeTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  const [meta, setMeta] = useState<TerminalRow | null>(null)
  const [metaFailed, setMetaFailed] = useState("")
  const [inputState, setInputState] = useState<InputState | null>(null)
  const [control, setControl] = useState<TerminalControl | null>(null)
  const [status, setStatus] = useState<TerminalStatus>("running")
  const [closedBy, setClosedBy] = useState<TerminalHolder | null>(null)
  const [lastAt, setLastAt] = useState<number | null>(null)
  const [stream, setStream] = useState<Stream>("connecting")
  const [revoked, setRevoked] = useState("")
  const [loaded, setLoaded] = useState<"loading" | "ready" | "failed">("loading")
  const [asking, setAsking] = useState(false)
  const [busy, setBusy] = useState<string | null>(null)
  const [said, setSaid] = useState("")
  const [history, setHistory] = useState<History | null>(null)
  const [ctrl, setCtrl] = useState(false)

  // ---- the input client, one per terminal and tab
  useEffect(() => {
    const c = new TerminalInputClient(id, TAB, fetchTransport)
    input.current = c
    autoAcquired.current = false
    const stop = c.subscribe(setInputState)
    const leave = () => c.releaseOnLeave()
    window.addEventListener("pagehide", leave)
    return () => {
      window.removeEventListener("pagehide", leave)
      stop()
      void c.release()
      c.dispose()
      input.current = null
    }
  }, [id])

  // ---- what the terminal is
  useEffect(() => {
    let live = true
    setMeta(null)
    setMetaFailed("")
    setStatus("running")
    setClosedBy(null)
    setRevoked("")
    setHistory(null)
    setAsking(false)
    setSaid("")
    readTerminal(id, TAB).then(
      (t) => {
        if (!live) return
        setMeta(t)
        setStatus(t.status)
        setControl((was) => was ?? t.control)
      },
      (e) => live && setMetaFailed(errorWords(e)),
    )
    return () => {
      live = false
    }
  }, [id])

  // ---- drawing
  const fitViewerFont = useCallback(() => {
    const t = term.current
    const f = lastFrame.current
    const box = scroller.current
    const screen = host.current?.querySelector(".xterm-screen")
    if (!t || !f || !box || !screen || t.cols <= 0) return
    const base = baseFont()
    const current = t.options.fontSize ?? base
    let want = base
    if (!(input.current?.state.holding ?? false)) {
      // A viewer's window may be narrower than the controller's: the font
      // shrinks until the controller's width fits, down to 9px, and past that
      // the screen scrolls sideways inside its box.
      const cell = screen.getBoundingClientRect().width / t.cols
      const room = box.clientWidth - 14
      if (cell > 0 && room > 0) want = Math.max(MIN_FONT, Math.min(base, Math.floor(((current * room) / (cell * f.cols)) * 2) / 2))
    }
    if (current !== want) t.options.fontSize = want
  }, [])

  const draw = useCallback((f: TerminalFrame) => {
    const t = term.current
    if (!t) return
    if (t.cols !== f.cols || t.rows !== f.rows) t.resize(Math.max(1, f.cols), Math.max(1, f.rows))
    t.write(frameBytes(f))
    fitViewerFont()
  }, [fitViewerFont])

  // The controller's window decides the size (plan v3 D3): the fit at the
  // base font, sent when it differs from what the daemon draws.
  const proposeSize = useCallback(() => {
    if (resizeTimer.current) clearTimeout(resizeTimer.current)
    resizeTimer.current = setTimeout(() => {
      const c = input.current
      const t = term.current
      const addon = fit.current
      const f = lastFrame.current
      if (!c || !t || !addon || !c.state.holding || c.state.epoch === null) return
      if (t.options.fontSize !== baseFont()) t.options.fontSize = baseFont()
      const d = addon.proposeDimensions()
      if (!d || !Number.isFinite(d.cols) || !Number.isFinite(d.rows) || d.cols < 2 || d.rows < 2) return
      if (f && f.cols === d.cols && f.rows === d.rows) return
      resizeTerminal(id, c.state.epoch, TAB, d.cols, d.rows).catch((e) => {
        setSaid(nextWord("terminalActionFailed", { action: "resize", why: errorWords(e) }))
      })
    }, 120)
  }, [id])

  // ---- xterm.js, loaded on first use
  useEffect(() => {
    let disposed = false
    const el = host.current
    if (!el) return
    setLoaded("loading")
    const load = async () => {
      const [{ Terminal }, { FitAddon }] = await Promise.all([
        import("@xterm/xterm"),
        import("@xterm/addon-fit"),
        import("@xterm/xterm/css/xterm.css"),
      ])
      if (disposed) return
      const t = new Terminal({
        fontFamily: FONT, fontSize: baseFont(), cursorBlink: true, scrollback: 0,
        cols: lastFrame.current?.cols ?? 80, rows: lastFrame.current?.rows ?? 24,
        theme: { background: "#101114", foreground: "#e8e6e3", cursor: "#e8e6e3" },
        screenReaderMode: false,
      })
      const addon = new FitAddon()
      t.loadAddon(addon)
      t.open(el)
      t.textarea?.setAttribute("aria-label", nextWord("terminalEntry"))
      const encoder = new TextEncoder()
      t.onData((data) => {
        let out = data
        if (ctrlArmed.current && data.length === 1) {
          const code = data.toUpperCase().charCodeAt(0)
          if (code >= 0x40 && code <= 0x5f) out = String.fromCharCode(code & 0x1f)
          ctrlArmed.current = false
          setCtrl(false)
        }
        input.current?.type(encoder.encode(out))
      })
      t.onBinary((data) => {
        const bytes = new Uint8Array(data.length)
        for (let i = 0; i < data.length; i++) bytes[i] = data.charCodeAt(i) & 0xff
        input.current?.type(bytes)
      })
      term.current = t
      fit.current = addon
      setLoaded("ready")
      if (lastFrame.current) draw(lastFrame.current)
    }
    load().catch(() => !disposed && setLoaded("failed"))
    // Every paste goes to /paste as one request, never through the keystroke
    // path, whatever xterm.js would have made of it.
    const onPaste = (ev: ClipboardEvent) => {
      const text = ev.clipboardData?.getData("text/plain") ?? ""
      ev.preventDefault()
      ev.stopImmediatePropagation()
      if (text) input.current?.paste(text)
    }
    // Keys typed into the terminal are the program's: the console's own
    // shortcuts (Escape leaves a page, Ctrl+K moves to the list) must not see
    // them.
    const onKey = (ev: KeyboardEvent) => ev.stopPropagation()
    el.addEventListener("paste", onPaste, true)
    el.addEventListener("keydown", onKey)
    return () => {
      disposed = true
      el.removeEventListener("paste", onPaste, true)
      el.removeEventListener("keydown", onKey)
      term.current?.dispose()
      term.current = null
      fit.current = null
    }
  }, [id, draw])

  // ---- the stream
  useEffect(() => {
    if (!shown) return
    // This page's stream is the connection it holds; the session list reads
    // instead of holding a second one while it is on screen (client.ts).
    const loud = quietFleetStream()
    let es: EventSource | null = null
    let retry: ReturnType<typeof setTimeout> | null = null
    let ended = false
    const connect = () => {
      setStream("connecting")
      es = new EventSource(streamURL(id, TAB))
      es.onopen = () => setStream("open")
      es.onerror = () => {
        if (ended || !es) return
        if (es.readyState === EventSource.CLOSED) {
          // Refused before it began (403, 429, 404…): ask the terminal
          // itself why, then try again unless it is gone.
          setStream("lost")
          es = null
          readTerminal(id, TAB).then(
            (t) => {
              setStatus(t.status)
              if (!ended && t.status === "running") retry = setTimeout(connect, 3_000)
            },
            (e) => {
              const code = e instanceof TerminalRequestError ? e.code : ""
              setSaid(errorWords(e))
              if (code === "terminal_closed") setStatus("closed")
              else if (!ended && code !== "terminal_forbidden") retry = setTimeout(connect, 3_000)
            },
          )
        } else {
          setStream("connecting")
        }
      }
      es.addEventListener("frame", (ev) => {
        const f = JSON.parse((ev as MessageEvent).data) as TerminalFrame
        lastFrame.current = f
        input.current?.heardStream()
        setLastAt(f.at)
        setStream("open")
        draw(f)
        if (f.dead) setStatus("exited")
      })
      es.addEventListener("beat", () => input.current?.heardStream())
      es.addEventListener("control", (ev) => {
        const c = JSON.parse((ev as MessageEvent).data) as TerminalControl
        setControl(c)
        input.current?.controlEvent(c)
      })
      es.addEventListener("state", (ev) => {
        const s = JSON.parse((ev as MessageEvent).data) as TerminalState
        setStatus(s.status)
        if (s.closed_by) setClosedBy(s.closed_by)
        if (s.status === "closed" || s.status === "exited") {
          input.current?.terminalGone("terminal_closed")
          if (s.status === "closed") {
            ended = true
            es?.close()
          }
        }
      })
      es.addEventListener("refusal", (ev) => {
        const r = JSON.parse((ev as MessageEvent).data) as TerminalRefusal
        setRevoked(r.error)
        input.current?.terminalGone("terminal_access_revoked")
        ended = true
        es?.close()
      })
    }
    connect()
    return () => {
      ended = true
      if (retry) clearTimeout(retry)
      es?.close()
      loud()
    }
  }, [id, shown, draw])

  // ---- the lease follows what the terminal says
  useEffect(() => {
    const c = input.current
    if (!c || !control || status !== "running" || autoAcquired.current) return
    autoAcquired.current = true
    // Nobody holds it: this tab takes it, so opening a terminal means typing
    // in it. Somebody does: this tab watches until the person takes over.
    if (!control.held) void c.control("acquire").then((code) => code && setSaid(terminalRefusalWords(code)))
  }, [control, status])

  // Typing is on only while it can land.
  const holding = !!inputState?.holding
  const stale = !!inputState?.stale
  const running = status === "running" && !revoked
  useEffect(() => {
    const t = term.current
    if (!t) return
    t.options.disableStdin = !(holding && !stale && running)
  }, [holding, stale, running, loaded])

  useEffect(() => {
    if (holding) proposeSize()
    else fitViewerFont()
  }, [holding, proposeSize, fitViewerFont])

  useLayoutEffect(() => {
    const box = scroller.current
    if (!box || typeof ResizeObserver === "undefined") return
    const watch = new ResizeObserver(() => {
      if (input.current?.state.holding) proposeSize()
      else fitViewerFont()
    })
    watch.observe(box)
    return () => watch.disconnect()
  }, [proposeSize, fitViewerFont])

  // ---- actions
  const act = async (name: string, fn: () => Promise<string>) => {
    setBusy(name)
    setSaid("")
    try {
      const code = await fn()
      if (code) setSaid(terminalRefusalWords(code))
    } finally {
      setBusy(null)
    }
  }
  const acquire = (action: "acquire" | "takeover") => act(action, async () => {
    const code = (await input.current?.control(action)) ?? ""
    if (!code) term.current?.focus()
    return code
  })
  const release = () => act("release", async () => {
    await input.current?.release()
    return ""
  })
  const close = () => act("close", async () => {
    const epoch = input.current?.state.epoch
    if (!input.current?.state.holding || epoch === null || epoch === undefined) return "not_controller"
    try {
      await closeTerminal(id, epoch, TAB)
      setAsking(false)
      setStatus("closed")
      setClosedBy({ name: "", local: false, same_device: true, same_client: true })
      input.current?.terminalGone("terminal_closed")
      return ""
    } catch (e) {
      return e instanceof TerminalRequestError ? e.code : "network"
    }
  })
  const showHistory = async () => {
    setHistory({ kind: "loading" })
    try {
      const h = await readHistory(id)
      setHistory({ kind: "lines", lines: h.lines.map(plain) })
    } catch (e) {
      setHistory({ kind: "failed", why: errorWords(e) })
    }
  }
  const key = (bytes: string) => {
    if (bytes === "ctrl") {
      ctrlArmed.current = !ctrlArmed.current
      setCtrl(ctrlArmed.current)
      term.current?.focus()
      return
    }
    input.current?.type(new TextEncoder().encode(bytes))
    term.current?.focus()
  }
  const arrow = (final: string) => (lastFrame.current?.modes.app_cursor ? "\x1bO" : "\x1b[") + final

  // ---- words
  const holder = control?.held ? control.holder : null
  const someoneElse = !!control?.held && !holder?.same_client
  const fresh = stream === "connecting" && lastAt === null
    ? nextWord("terminalConnecting")
    : stale || stream === "lost"
      ? nextWord("terminalStale")
      : lastAt !== null ? nextWord("terminalFresh", { time: clock(lastAt) }) : nextWord("terminalConnecting")
  const notice = inputState?.notice
  // A terminal that ended says so below the header (`endWords`); there is no
  // screen to look at before typing again.
  const noticeWords = notice && notice !== "released" && notice !== "terminal_closed" && notice !== "terminal_access_revoked"
    ? [
      notice === "reacquired" ? nextWord("terminalReacquired") : terminalRefusalWords(notice),
      inputState && inputState.dropped > 0 ? nextWord("terminalDropped", { bytes: byteWords(inputState.dropped) }) : "",
      nextWord("terminalLookFirst"),
    ].filter(Boolean).join(" ")
    : ""
  const ended = status === "closed" || status === "exited"
  const endWords = revoked
    ? terminalRefusalWords(revoked)
    : status === "closed"
      ? closedBy
        ? closedBy.same_client ? nextWord("terminalClosedByYou") : nextWord("terminalClosedBy", { name: holderWords(closedBy) })
        : nextWord("terminalRefusalClosed")
      : status === "exited" ? nextWord("terminalExited")
        : status === "unreachable" ? nextWord("terminalRefusalUnreachable") : ""

  return (
    <div className="terminal-view" data-holding={holding ? "" : undefined} data-stale={stale ? "" : undefined}>
      <header className="terminal-head">
        <div className="terminal-head-row">
          <button className="board-button" type="button" onClick={onBack}>{nextWord("terminalBack")}</button>
          <dl className="terminal-facts">
            <div><dt>{nextWord("terminalMachine")}</dt><dd>{nextWord("devicesThisMachine")}</dd></div>
            <div data-tone={stale || stream === "lost" ? "warn" : undefined}>
              <dt className="terminal-sr">{nextWord("terminalFresh", { time: "" }).trim()}</dt>
              <dd className="terminal-fresh">{fresh}</dd>
            </div>
            <div><dt>{nextWord("terminalControlLabel")}</dt><dd className="terminal-holder">{holderWords(holder)}</dd></div>
          </dl>
        </div>
        <div className="terminal-actions" role="group" aria-label={nextWord("terminalControlLabel")}>
          {!holding && !someoneElse && (
            <button className="board-button" type="button" disabled={!!busy || ended || !!revoked}
              aria-busy={busy === "acquire" ? "true" : undefined} onClick={() => void acquire("acquire")}>
              {nextWord("terminalAcquire")}
            </button>
          )}
          {!holding && someoneElse && (
            <button className="board-button" type="button" disabled={!!busy || ended || !!revoked}
              aria-busy={busy === "takeover" ? "true" : undefined} onClick={() => void acquire("takeover")}>
              {nextWord("terminalTakeover")}
            </button>
          )}
          {holding && (
            <button className="board-button" type="button" disabled={!!busy}
              aria-busy={busy === "release" ? "true" : undefined} onClick={() => void release()}>
              {nextWord("terminalRelease")}
            </button>
          )}
          <button className="board-button" type="button" aria-pressed={history !== null}
            onClick={() => (history ? setHistory(null) : void showHistory())}>
            {history ? nextWord("terminalHistoryBack") : nextWord("terminalHistory")}
          </button>
          <button className="board-button terminal-close" type="button" disabled={ended || !!busy}
            aria-expanded={asking}
            onClick={() => (holding ? setAsking(true) : setSaid(nextWord("terminalCloseNeedsControl")))}>
            {nextWord("terminalClose")}
          </button>
          {inputState?.full && <span className="terminal-badge" role="alert">{nextWord("terminalInputFull")}</span>}
        </div>
        {asking && (
          <div className="terminal-ask" role="group" aria-label={nextWord("terminalClose")}>
            <p>{nextWord("terminalCloseAsk")}</p>
            <button className="board-button terminal-danger" type="button" disabled={!!busy}
              aria-busy={busy === "close" ? "true" : undefined} onClick={() => void close()}>
              {nextWord("terminalCloseConfirm")}
            </button>
            <button className="board-button" type="button" disabled={!!busy} onClick={() => setAsking(false)}>
              {nextWord("terminalCancel")}
            </button>
          </div>
        )}
        <p className="terminal-note terminal-session-note">{nextWord("terminalSessionNote")}</p>
        <p className="terminal-status-line" role="status" aria-live="polite">
          {[
            metaFailed,
            said,
            noticeWords,
            inputState?.retrying ? nextWord("terminalRetrying") : "",
            !holding && running && !ended ? nextWord("terminalViewOnly") : "",
          ].filter(Boolean).join(" ")}
        </p>
        {endWords && <p className="terminal-ended" role="alert">{endWords}</p>}
      </header>
      {history && (
        <section className="terminal-history" aria-label={nextWord("terminalHistoryTitle")}>
          <h2>{nextWord("terminalHistoryTitle")}</h2>
          {history.kind === "loading" && <p className="terminal-note">{nextWord("terminalHistoryLoading")}</p>}
          {history.kind === "failed" && (
            <p className="terminal-note" role="alert">{nextWord("terminalHistoryFailed", { why: history.why })}</p>
          )}
          {history.kind === "lines" && <pre tabIndex={0}>{history.lines.join("\n")}</pre>}
        </section>
      )}
      <div className="terminal-scroll" ref={scroller} hidden={history !== null}>
        {loaded === "loading" && <p className="terminal-note terminal-loading">{nextWord("terminalConnecting")}</p>}
        {loaded === "failed" && <p className="terminal-note" role="alert">{nextWord("terminalRefusalUnreachable")}</p>}
        <div className="terminal-host" ref={host} data-meta-cols={meta?.cols} />
      </div>
      <div className="terminal-keys" role="group" aria-label={nextWord("terminalKeys")} hidden={history !== null}>
        {[
          ["Esc", "\x1b"], ["Ctrl", "ctrl"], ["Tab", "\t"],
          ["←", "D"], ["↑", "A"], ["↓", "B"], ["→", "C"],
        ].map(([label, value]) => (
          <button key={label} type="button" className="terminal-key"
            disabled={!(holding && !stale && running)}
            aria-pressed={value === "ctrl" ? ctrl : undefined}
            aria-label={value === "ctrl" ? nextWord("terminalKeyCtrl") : undefined}
            onPointerDown={(ev) => ev.preventDefault()}
            onClick={() => key(value.length === 1 && value >= "A" && value <= "D" ? arrow(value) : value)}>
            {label}
          </button>
        ))}
      </div>
    </div>
  )
}
