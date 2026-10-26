import { useEffect, useRef, useState, useSyncExternalStore } from "react"
import { flushSync } from "react-dom"
import type { Terminal as TerminalRow } from "@clawdline/contract"
import type { Terminal as XTerm } from "@xterm/xterm"
import type { FitAddon } from "@xterm/addon-fit"
import { nextWord } from "../../next-strings.js"
import { showModalWithoutChoice } from "../../modal-focus.js"
import { watchTerminalHost, type TerminalHost } from "../../cloud/terminal-host.js"
import { acquireTerminalConnection } from "../../cloud/terminal-connection-owner.js"
import { TerminalObservation } from "../../cloud/terminal-observation.js"
import { CloudTerminalSession, type CloudTerminalSnapshot, type CloudTerminalHistory } from "../../cloud/terminal-session.js"
import { frameBytes, frameDeltaBytes } from "./frame-writer.js"
import { TAB } from "./tab.js"
import { closeTerminalPane, openTerminalPage } from "./navigate.js"
import { firstSize } from "./TerminalProjectList.js"
import { TerminalBackButton } from "./TerminalBackButton.js"
import { CloudTerminalSkeleton, TerminalSkeletonKeys, TerminalSkeletonLines } from "./TerminalSkeleton.js"
import { COMBO_KEYS, PANEL_KEYS, QUICK_KEYS, bindTerminalKeyboard, isRegionKey, readClipboardPaste, withCtrl } from "./keys.js"
import { PasteField } from "./PasteField.js"
import { holderWords, terminalRefusalWords, terminalShortID } from "./words.js"
import { acquireVisibleTerminal, beginCloudTerminal, cloudTerminalBody, cloudTerminalPhase, keyRefusalControl, keyRefusalNotice,
  reconnectCloudTerminal, shownKeyRefusal, type CloudTerminalPhase, type KeyRefusalNotice } from "./cloud-view.js"
import { beginTerminalClose, observeTerminalEnded, observeTerminalRunning, settleTerminalClose, terminalCloseState, watchTerminalClose } from "../../cloud/terminal-close-state.js"
import { centerCursorLine, followCursorLine } from "./cursor-line.js"
import { CLOUD_TERMINAL_LIST_RETRY_DELAYS_MS, terminalListErrorKind } from "../../session/cloud-terminal-all.js"

const empty: CloudTerminalSnapshot = { state: "opening", frame: null, control: null, canType: false, hasLease: false, reason: "", carrier: "relay" }
const FIRST_FRAME_MS = 12_000
/** The page's content-free receipt timeline, readable and copyable where a terminal request failed. */
function TerminalDiagnostics({ observation }: { observation: { current: TerminalObservation | null } }) {
  const [text, setText] = useState("")
  const [copied, setCopied] = useState("")
  return <details className="terminal-diagnostics" onToggle={(event) => { if (event.currentTarget.open) { setText(observation.current?.text() ?? ""); setCopied("") } }}>
    <summary>{nextWord("terminalDiagnostics")}</summary>
    <p className="terminal-note">{nextWord("terminalDiagnosticsNote")}</p>
    <button className="board-button" type="button" onClick={() => {
      const next = observation.current?.text() ?? ""
      setText(next)
      void navigator.clipboard.writeText(next).then(() => setCopied(nextWord("terminalDiagnosticsCopied")),
        // refusal-ok: a clipboard write is refused by the browser's permission, which carries no machine code to name.
        () => setCopied(nextWord("terminalDiagnosticsCopyFailed")))
    }}>{nextWord("terminalDiagnosticsCopy")}</button>
    {copied && <span className="terminal-note" role="status"> {copied}</span>}
    <pre className="terminal-diagnostics-text">{text}</pre>
  </details>
}

function reason(error: unknown): string {
  const code = (error as { code?: string })?.code
  if (code === "terminal_history_line_too_large") return nextWord("terminalCloudHistoryLineTooLarge")
  if (code === "terminal_history_too_large") return nextWord("terminalCloudHistoryTooLarge")
  if (code === "terminal_input_paused") return nextWord("terminalCloudTypeAheadDropped")
  return code ?? (error instanceof Error ? error.message : "cloud_failed")
}
/**
 * Hosted-only terminal page. The local TerminalView and its fetch/SSE route remain untouched.
 * `project` is the Cloud Project id the page address carries; `channelProject` is the same
 * Project's machine-local id, the only one the machine's terminal channel knows (cloud-project.ts).
 */
export function CloudTerminalPage({ project, channelProject, machine, label, id, shown, create = false }: {
  project: string; channelProject: string; machine: string; label: string; id: string; shown: boolean
  /** With no `id`: open a new terminal in the project as soon as the machine's channel is up, once. */
  create?: boolean
}) {
  const [host, setHost] = useState<TerminalHost | null>(null)
  const [session, setSession] = useState<CloudTerminalSession | null>(null)
  const [snapshot, setSnapshot] = useState<CloudTerminalSnapshot>(empty)
  const [meta, setMeta] = useState<TerminalRow | null>(null)
  const [loading, setLoading] = useState(true)
  const [firstFramePending, setFirstFramePending] = useState(true)
  const [frameTimedOut, setFrameTimedOut] = useState(false)
  const [error, setError] = useState("")
  /**
   * The last refused key, said beside the terminal itself with the way back to typing. A key is never
   * dropped with nothing said: the status line alone can be scrolled off while the person types.
   */
  const [keyRefused, setKeyRefused] = useState<(KeyRefusalNotice & { why: string; control: string }) | null>(null)
  const keyFailed = (e: unknown) => {
    const code = (e as { code?: string })?.code ?? "terminal_send_failed"
    // A key the machine refused because the terminal is gone ends the page, not just the key.
    if (code === "terminal_closed") { setError(code); return }
    const why = reason(e)
    const control = session?.snapshot.control ?? null
    setKeyRefused({ ...keyRefusalNotice(code, control, session?.snapshot.state ?? ""), why: why === code ? terminalRefusalWords(code) : why,
      control: keyRefusalControl(control) })
  }
  const keySent = () => setKeyRefused(null)
  // Attaching again by itself after a refusal that means "not right now"
  // (terminal_busy while the machine cannot verify this browser, a timeout):
  // which attempt is next for which terminal, and the wait being shown.
  const attachKey = `${machine}/${channelProject}/${id}`
  const [attachRetry, setAttachRetry] = useState({ key: attachKey, attempt: 0 })
  const [retryWait, setRetryWait] = useState<{ attempt: number; code: string } | null>(null)
  const [needsReview, setNeedsReview] = useState(false)
  const [busy, setBusy] = useState("")
  const [history, setHistory] = useState<CloudTerminalHistory | null>(null)
  const [reader, setReader] = useState(false)
  const [ctrl, setCtrl] = useState(false)
  const [pasteMessage, setPasteMessage] = useState("")
  const [pasting, setPasting] = useState(false)
  const [nativePaste, setNativePaste] = useState(false)
  const pasteBusy = useRef(false)
  const ctrlArmed = useRef(false)
  const [keysOpen, setKeysOpen] = useState(false)
  const [endedAt, setEndedAt] = useState<Date | null>(null)
  const [diagnosticsText, setDiagnosticsText] = useState<string | null>(null)
  const [dockNote, setDockNote] = useState("")
  const [closeConfirm, setCloseConfirm] = useState(false)
  const closeDialog = useRef<HTMLDialogElement>(null)
  const closeOpener = useRef<HTMLButtonElement>(null)
  const closeCancel = useRef<HTMLButtonElement>(null)
  const closeMessage = useRef<HTMLParagraphElement>(null)
  const closeRecord = useSyncExternalStore(watchTerminalClose, () => terminalCloseState(machine, id), () => null)
  const screen = useRef<HTMLDivElement>(null)
  const scroller = useRef<HTMLDivElement>(null)
  const terminal = useRef<XTerm | null>(null)
  const fit = useRef<FitAddon | null>(null)
  const lastRev = useRef("")
  const drawnFrame = useRef<CloudTerminalSnapshot["frame"]>(null)
  const back = useRef<HTMLButtonElement>(null)
  const historyFocus = useRef<HTMLDivElement>(null)
  const actionMenu = useRef<HTMLDetailsElement>(null)
  const observed = useRef<TerminalObservation | null>(null)
  const showInputLine = () => requestAnimationFrame(() => centerCursorLine(scroller.current, terminal.current))
  const unfollow = useRef(() => {})
  useEffect(() => watchTerminalHost(setHost), [])
  useEffect(() => {
    const dialog = closeDialog.current
    if (!dialog) return
    if (closeConfirm) showModalWithoutChoice(dialog)
    if (!closeConfirm && dialog.open) dialog.close()
  }, [closeConfirm])
  useEffect(() => {
    if (closeRecord)
      closeMessage.current?.focus()
  }, [closeRecord])

  useEffect(() => {
    if (!shown || !host || !channelProject) return
    let live = true
    let retryTimer: ReturnType<typeof setTimeout> | null = null
    const attempt = attachRetry.key === attachKey ? attachRetry.attempt : 0
    const observation = new TerminalObservation()
    let release: (() => void) | null = null
    let stop: (() => void) | null = null
    setSnapshot(empty); setLoading(true); setFirstFramePending(!!id); setFrameTimedOut(false); setError(""); setNeedsReview(false); setMeta(null); setKeyRefused(null); setEndedAt(null)
    if (attempt === 0) setRetryWait(null)
    void (async () => {
      try {
        const lease = await acquireTerminalConnection(host, machine, TAB, observation)
        if (!live) { lease.release(); return }
        release = lease.release
        observed.current = lease.observation
        const next = lease.session
        stop = next.subscribe((value) => live && setSnapshot(value))
        setSession(next)
        // A terminal nobody else holds is taken on entry; keys typed before that answer wait for it
        // instead of being refused for a lease that is on its way.
        const entered = async () => {
          const begun = await beginCloudTerminal(next, channelProject, id, TAB, true)
          if (live && "meta" in begun) setMeta(begun.meta)
          // Attaching forgets this page's epoch; a same-tab holder can safely
          // reacquire after the host confirms no input outcome is uncertain.
          if (live && await acquireVisibleTerminal(next, id, TAB, next.snapshot.control, next.snapshot.state) === "needs_review")
            setNeedsReview(true)
        }
        await (id ? next.expectControl(entered) : entered())
        if (live) setRetryWait(null)
      } catch (e) {
        if (!live) return
        const code = reason(e)
        if (id && code === "terminal_closed") observeTerminalEnded(machine, id)
        const delay = CLOUD_TERMINAL_LIST_RETRY_DELAYS_MS[attempt]
        if (terminalListErrorKind(code) === "retryable" && delay !== undefined) {
          setRetryWait({ attempt: attempt + 1, code })
          retryTimer = setTimeout(() => { if (live) setAttachRetry({ key: attachKey, attempt: attempt + 1 }) }, delay)
        } else {
          setRetryWait(null)
          setError(code)
        }
      }
      finally { if (live) setLoading(false) }
    })()
    return () => { live = false; if (retryTimer) clearTimeout(retryTimer); stop?.(); release?.(); setSession(null) }
  }, [host, machine, channelProject, id, shown, attachRetry, attachKey])

  useEffect(() => {
    if (!id || loading || !firstFramePending || snapshot.frame || error || !shown) return
    const timer = setTimeout(() => { setFirstFramePending(false); setFrameTimedOut(true) }, FIRST_FRAME_MS)
    return () => clearTimeout(timer)
  }, [id, loading, firstFramePending, snapshot.frame, error, shown, session])
  useEffect(() => {
    if (snapshot.frame) { setFirstFramePending(false); setFrameTimedOut(false) }
  }, [snapshot.frame])

  useEffect(() => {
    if (!id || !screen.current || !shown) return
    let cancelled = false
    void (async () => {
      try {
        const [{ Terminal }, { FitAddon }] = await Promise.all([import("@xterm/xterm"), import("@xterm/addon-fit")])
        await import("@xterm/xterm/css/xterm.css")
        if (cancelled || !screen.current) return
        const term = new Terminal({ cols: 80, rows: 24, scrollback: 0, fontSize: 13,
          fontFamily: 'ui-monospace, "SF Mono", "Noto Sans Mono CJK TC", Menlo, Consolas, monospace',
          theme: { background: "#101114", foreground: "#e8e6e3", cursor: "#e8e6e3" }, screenReaderMode: reader })
        const addon = new FitAddon()
        term.loadAddon(addon)
        term.open(screen.current)
        term.textarea?.setAttribute("aria-label", nextWord("terminalScreenFor", { project: label }))
        term.onData((data) => {
          const { out } = withCtrl(ctrlArmed.current, data)
          if (ctrlArmed.current) { ctrlArmed.current = false; setCtrl(false) }
          void session?.input(new TextEncoder().encode(out)).then(keySent, keyFailed)
          showInputLine()
        })
        term.onBinary((data) => { const bytes = Uint8Array.from(data, (char) => char.charCodeAt(0) & 0xff)
          void session?.input(bytes).then(keySent, keyFailed); showInputLine() })
        terminal.current = term
        fit.current = addon
        unfollow.current = followCursorLine(() => scroller.current, term)
        const current = session?.snapshot.frame
        if (current) { term.resize(current.cols, current.rows); term.write(frameBytes(current), showInputLine); lastRev.current = current.rev; drawnFrame.current = current }
      } catch (e) { if (!cancelled) setError(reason(e)) }
    })()
    const element = screen.current
    const paste = (event: ClipboardEvent) => {
      event.preventDefault(); event.stopImmediatePropagation()
      const text = event.clipboardData?.getData("text/plain") ?? ""
      if (text) void session?.paste(text).then(keySent, keyFailed)
    }
    const unbindKeys = bindTerminalKeyboard(element, () => back.current?.focus())
    element.addEventListener("paste", paste, true)
    return () => { cancelled = true; element.removeEventListener("paste", paste, true); unbindKeys()
      unfollow.current(); unfollow.current = () => {}
      terminal.current?.dispose(); terminal.current = null; fit.current = null; lastRev.current = ""; drawnFrame.current = null }
  }, [id, shown, session, label])

  useEffect(() => {
    const term = terminal.current
    const frame = snapshot.frame
    if (!term || !frame || frame.rev === lastRev.current) return
    const box = scroller.current
    const follow = !box || box.contains(document.activeElement) || box.scrollHeight - box.clientHeight - box.scrollTop < 32
    lastRev.current = frame.rev
    if (term.cols !== frame.cols || term.rows !== frame.rows) term.resize(frame.cols, frame.rows)
    term.write(frameDeltaBytes(drawnFrame.current, frame), () => { if (follow) showInputLine() })
    drawnFrame.current = frame
  }, [snapshot.frame])
  // The screen always takes keys: one typed during a brief pause waits in the session, and one the
  // session refuses raises the notice beside the screen. A disabled screen swallowed keys unsaid.
  useEffect(() => { if (snapshot.canType) setKeyRefused(null) }, [snapshot.canType])
  useEffect(() => { if (terminal.current) terminal.current.options.screenReaderMode = reader }, [reader])
  useEffect(() => {
    const box = screen.current?.parentElement
    if (!box || !session || !snapshot.canType || typeof ResizeObserver === "undefined") return
    let timer: ReturnType<typeof setTimeout> | null = null
    const suggest = () => {
      if (timer) clearTimeout(timer)
      timer = setTimeout(() => {
        showInputLine()
        const size = fit.current?.proposeDimensions()
        if (!size || size.cols < 2 || size.rows < 2 ||
          (size.cols === snapshot.frame?.cols && size.rows === snapshot.frame?.rows)) return
        void session.resize(size.cols, size.rows).catch((error) => setError(reason(error)))
      }, 120)
    }
    const observer = new ResizeObserver(suggest)
    observer.observe(box)
    suggest()
    return () => { observer.disconnect(); if (timer) clearTimeout(timer) }
  }, [session, snapshot.canType, snapshot.frame?.cols, snapshot.frame?.rows])

  const run = async (name: string, action: () => Promise<void>) => {
    setBusy(name); setError("")
    try { await action() } catch (e) { setError(reason(e)) } finally { setBusy("") }
  }
  const openNew = () => void run("open", async () => {
    if (!session) return
    const size = firstSize()
    let answer
    try { answer = await session.create(channelProject, size.cols, size.rows) }
    catch (error) {
      if (reason(error) === "terminal_receipt_timeout") throw Object.assign(new Error("terminal_open_state_unknown"), { code: "terminal_open_state_unknown" })
      throw error
    }
    const next = answer.result?.id
    if (typeof next !== "string") throw new Error("terminal_bad_receipt")
    openTerminalPage(project, next)
  })
  const goBack = closeTerminalPane
  // A new terminal asked for from the start sheet: once this column has the
  // machine's channel, and only once, so a reconnect never opens a second.
  const asked = useRef(false)
  useEffect(() => {
    if (!create || id || !session || loading || asked.current) return
    asked.current = true
    openNew()
  }, [create, id, session, loading])
  const submitClose = () => void run("close", async () => {
    if (!session) return
    setCloseConfirm(false)
    let requestID = ""
    try {
      await session.close((request) => { requestID = request; beginTerminalClose(machine, id, request) })
      if (requestID) settleTerminalClose(machine, id, requestID, "ok")
      goBack()
    } catch (failure) {
      const code = reason(failure)
      if (requestID) settleTerminalClose(machine, id, requestID,
        (failure as { receiptStatus?: unknown })?.receiptStatus === "refused" && code !== "terminal_closed" ? "refused" : "unknown", code)
      else setError(code)
    }
  })
  // Settles an unknown close from the machine's own read: gone is ended, still there offers the
  // close again, and an unreachable answer leaves it to TERMINAL_CLOSE_UNKNOWN_MS.
  const readCloseOutcome = async (): Promise<void> => {
    if (!session) return
    try {
      const answer = await session.request("read", { terminal_id: id, client: TAB })
      const status = (answer.result as { status?: unknown } | undefined)?.status
      if (status === "closed") observeTerminalEnded(machine, id)
      else if (status === "running" || status === "exited") observeTerminalRunning(machine, id)
    } catch (failure) {
      if (reason(failure) === "terminal_closed") observeTerminalEnded(machine, id)
      else throw failure
    }
  }
  const queryClose = () => void run("close-read", readCloseOutcome)
  // An unknown close is read back once by itself, so the page does not wait for a press.
  const closeUnknown = closeRecord?.status === "unknown" ? closeRecord.requestID : ""
  useEffect(() => {
    if (closeUnknown && session) queryClose()
  }, [closeUnknown, session])
  const reconnect = () => void run("reconnect", async () => {
    if (!session) return
    setFirstFramePending(!snapshot.frame); setFrameTimedOut(false)
    await reconnectCloudTerminal(session, id)
  })
  const sendKey = (value: string) => {
    if (value === "ctrl") {
      ctrlArmed.current = !ctrlArmed.current
      setCtrl(ctrlArmed.current)
      terminal.current?.focus()
      return
    }
    const bytes = /^[ABCD]$/.test(value) ? (snapshot.frame?.modes.app_cursor ? "\x1bO" : "\x1b[") + value : value
    const { out } = withCtrl(ctrlArmed.current, bytes)
    ctrlArmed.current = false; setCtrl(false)
    void session?.input(new TextEncoder().encode(out)).then(keySent, keyFailed)
    terminal.current?.focus()
    showInputLine()
  }
  const recordPaste = (stage: string) => {
    try {
      const log = (host?.client as { viewerEvents?: { record(event: string, data: Record<string, unknown>, key?: string): unknown } } | undefined)?.viewerEvents
      log?.record("cloud.terminal.paste", { machine, stage }, `cloud.terminal.paste|${stage}`)
    } catch { /* Diagnostics cannot affect pasting. */ }
  }
  const pasteFromClipboard = async () => {
    if (pasteBusy.current) return
    pasteBusy.current = true; setPasting(true); setPasteMessage("")
    try {
      const outcome = await readClipboardPaste(navigator.clipboard,
        () => session?.snapshot.canType && !history ? null : "terminalPasteUnavailable",
        text => session!.paste(text),
        { preferred: window.matchMedia("(any-pointer: coarse)").matches,
          open: () => flushSync(() => setNativePaste(true)) }, recordPaste)
      if (outcome && outcome !== "terminalPasteReadFailed" && outcome !== "terminalPasteEmpty") setPasteMessage(nextWord(outcome))
    } catch (failure) { setError(reason(failure)) }
    finally { pasteBusy.current = false; setPasting(false) }
  }
  const accessError = error === "forbidden" || error === "terminal_forbidden" || error === "terminal_access_revoked"
  const holder = !snapshot.control?.held ? nextWord("terminalControlNobody") :
    (snapshot.control.holder?.name || snapshot.control.holder?.same_device)
      ? holderWords(snapshot.control.holder)
      : nextWord("terminalControlOtherDevice")
  const phase: CloudTerminalPhase = cloudTerminalPhase({ state: snapshot.state, error, status: meta?.status ?? "",
    closeStatus: closeRecord?.status ?? "", retrying: !!retryWait && !error, frameTimedOut, needsReview,
    canType: snapshot.canType, hasFrame: !!snapshot.frame, control: snapshot.control })
  useEffect(() => { if (phase === "ended" && !endedAt) setEndedAt(new Date()) }, [phase, endedAt])
  // Shown only while control is as it was when the key was refused.
  const refusal = phase === "ended" ? null : shownKeyRefusal(keyRefused, snapshot.control)
  const focusInput = () => terminal.current?.focus()
  // One press takes input, from nobody, from this tab's forgotten lease, or from another device:
  // every device here is the same person's, so taking over asks nothing more (2026-10-18).
  const takeInput = () => void run("acquire", async () => {
    await session!.acquire(snapshot.control?.held && !snapshot.control.holder?.same_client ? "takeover" : "acquire")
    setNeedsReview(false); setKeyRefused(null); focusInput()
  })
  const retryNow = () => { if (retryWait) setAttachRetry({ key: attachKey, attempt: retryWait.attempt }); else reconnect() }
  const copyDiagnostics = () => {
    const text = `carrier=${snapshot.carrier}\n${observed.current?.text() ?? ""}`
    void navigator.clipboard.writeText(text).then(() => { setDiagnosticsText(null); setDockNote(nextWord("terminalDiagnosticsCopied")) },
      // refusal-ok: a clipboard write is refused by the browser's permission, which carries no machine code to name.
      () => { setDiagnosticsText(text); setDockNote(nextWord("terminalDiagnosticsCopyFailed")) })
  }
  const body = cloudTerminalBody(host, channelProject, id)
  if (body === "offline" || !host) return <CloudTerminalSkeleton label={label} status={nextWord("terminalPhaseReconnecting")} onBack={goBack} />
  if (body === "no_project") return <p className="terminal-note" role="alert">{nextWord("terminalNoProject")}</p>
  if (body === "list") return <section className="terminal-wrap" aria-label={nextWord("terminalEntryFor", { project: label })}>
    <header className="board-head terminal-page-head"><TerminalBackButton onClick={goBack} /></header>
    {!error && <p className="terminal-note" role="status">{retryWait ? nextWord("terminalCloudRetrying", { code: retryWait.code,
      attempt: retryWait.attempt, max: CLOUD_TERMINAL_LIST_RETRY_DELAYS_MS.length }) : create ? nextWord("terminalOpening") : nextWord("terminalListLoading")}</p>}
    {error && <div><p className="terminal-note" role="alert">{accessError ? nextWord("terminalCloudNotAuthorized", { code: error }) :
      error === "terminal_open_state_unknown" ? nextWord("terminalCloudOpenUnknown") : nextWord("terminalOpenFailed", { why: error })}</p>
      <TerminalDiagnostics observation={observed} /></div>}
  </section>

  const time = endedAt ? endedAt.toLocaleTimeString(document.documentElement.lang || undefined, { hour: "2-digit", minute: "2-digit" }) : ""
  const phaseWords = phase === "typing" ? nextWord("terminalPhaseTyping") : phase === "watching" ? nextWord("terminalPhaseWatching") :
    phase === "other_holder" ? nextWord("terminalPhaseOtherHolder", { holder }) : phase === "needs_review" ? nextWord("terminalPhaseNeedsReview") :
    phase === "reconnecting" ? (retryWait ? nextWord("terminalPhaseReconnectingCount", { attempt: retryWait.attempt, max: CLOUD_TERMINAL_LIST_RETRY_DELAYS_MS.length }) : nextWord("terminalPhaseReconnecting")) :
    phase === "offline" ? nextWord("terminalPhaseOffline") : phase === "ended" ? nextWord("terminalPhaseEnded", { time }) :
    phase === "error" ? nextWord("terminalPhaseError") : phase === "denied" ? nextWord("terminalPhaseDenied") : nextWord("terminalPhaseLoading")
  const tone = phase === "typing" ? "ok" : phase === "watching" || phase === "loading" ? "idle" :
    phase === "offline" || phase === "error" || phase === "denied" ? "bad" : "warn"
  const typing = phase === "typing" && history === null
  const keyButton = ([name, value, spoken]: [string, string, Parameters<typeof nextWord>[0] | null]) =>
    <button key={name} className="cloud-chip" type="button" aria-label={spoken ? nextWord(spoken) : undefined}
      onPointerDown={(event) => event.preventDefault()} onClick={() => sendKey(value)}>{name}</button>
  const diagnosticsLink = <button className="cloud-link" type="button" onClick={copyDiagnostics}>{nextWord("terminalDiagnosticsCopy")}</button>

  return <div className="terminal-view cloud-terminal-view" data-holding={snapshot.canType ? "" : undefined}
    data-stale={phase === "reconnecting" || phase === "offline" ? "" : undefined} data-ended={phase === "ended" ? "" : undefined}
    onKeyDown={(event) => { if (isRegionKey(event.nativeEvent)) {
      event.preventDefault()
      if (history) {
        if (event.target === historyFocus.current) back.current?.focus()
        else historyFocus.current?.focus()
      } else terminal.current?.focus()
    } }}>
    <header className="terminal-head">
      <div className="terminal-head-row"><TerminalBackButton buttonRef={back} onClick={goBack} compact />
        <div className="cloud-title">
          <strong className="terminal-context" title={label}>{label}</strong>
          <span className="cloud-phase" data-tone={tone} role="status" aria-live="polite">{phaseWords}</span>
        </div>
        <details className="terminal-action-more" ref={actionMenu}><summary aria-label={nextWord("terminalMoreActions")} title={nextWord("terminalMoreActions")}>⋯</summary>
        <button className="terminal-menu-backdrop" type="button" aria-label={nextWord("terminalDismissMenu")}
          onClick={() => actionMenu.current?.removeAttribute("open")} />
        <div className="terminal-more-actions">
          <button className="board-button" type="button" disabled={!!busy || phase === "ended"} onClick={() => { actionMenu.current?.removeAttribute("open"); void run("history", async () => setHistory(history ? null : await session!.history())) }}>{history ? nextWord("terminalHistoryBack") : nextWord("terminalHistory")}</button>
          <button className="board-button terminal-reader" type="button" aria-pressed={reader} onClick={() => { actionMenu.current?.removeAttribute("open"); setReader((value) => !value) }}><span>{nextWord("terminalReaderMode")}</span><small>{nextWord("terminalReaderModeHelp")}</small></button>
          <button className="board-button" type="button" onClick={() => { actionMenu.current?.removeAttribute("open"); copyDiagnostics() }}>{nextWord("terminalDiagnosticsCopy")}</button>
          <button className="board-button terminal-danger" type="button" ref={closeOpener}
            disabled={!snapshot.canType || !!busy || closeRecord?.status === "pending" || closeRecord?.status === "unknown" || closeRecord?.status === "ok" || closeRecord?.status === "ended"}
            onClick={() => { actionMenu.current?.removeAttribute("open"); setCloseConfirm(true) }}>{nextWord("terminalTerminateHost")}</button>
        </div></details></div>
      <dialog ref={closeDialog} className="terminal-close-confirm" onCancel={() => { setCloseConfirm(false); closeOpener.current?.focus() }}
        aria-label={nextWord("terminalTerminateHost")}>
        <p>{nextWord("terminalTerminateAsk", { project: label, machine, id: terminalShortID(id) })}</p>
        <button className="board-button" type="button" ref={closeCancel} onClick={() => { setCloseConfirm(false); closeOpener.current?.focus() }}>{nextWord("terminalCancel")}</button>
        <button className="board-button terminal-danger" type="button" onClick={submitClose}>{nextWord("terminalTerminateHost")}</button>
      </dialog>
      {closeRecord && closeRecord.status !== "ok" && closeRecord.status !== "ended" && <div className="terminal-close-result">
        <p ref={closeMessage} tabIndex={-1} role={closeRecord.status === "refused" ? "alert" : "status"}>
          {nextWord(closeRecord.status === "pending" ? "terminalTerminatePending" :
            closeRecord.status === "unknown" ? "terminalTerminateUnknown" : "terminalCloudError",
            { code: terminalRefusalWords(closeRecord.error) })}
        </p>
        {closeRecord.status === "unknown" && <button className="board-button" type="button" disabled={!!busy} onClick={queryClose}>{nextWord("terminalTerminateQuery")}</button>}
      </div>}
    </header>
    {history && <section className="terminal-history" aria-label={nextWord("terminalHistoryTitle")}><h2>{nextWord("terminalHistoryTitle")}</h2>
      {history.truncated && <p className="terminal-note" role="status">{nextWord("terminalCloudHistoryTruncated", { lines: history.omitted_lines })}</p>}
      <div className="terminal-history-output" ref={historyFocus} tabIndex={0}>{history.lines.map((line, index) =>
        <div className="terminal-history-line" key={index}>{line || " "}</div>)}</div></section>}
    <div className="terminal-scroll" ref={scroller} hidden={history !== null}>{refusal && <div className="terminal-key-refused" role="alert">
      <p>{refusal.kind === "no_control" ? nextWord("terminalKeysNotSentNoControl") :
        refusal.kind === "other_holder" ? nextWord("terminalKeysNotSentOtherHolder", { holder }) :
          refusal.kind === "unknown" ? nextWord("terminalKeysNotSentUnknown") : nextWord("terminalKeysNotSent", { why: refusal.why })}</p>
      {refusal.action && <button className="board-button" type="button" disabled={!!busy || accessError || snapshot.state === "revoked"}
        onClick={() => refusal.action === "reconnect" ? reconnect() : takeInput()}>{nextWord(refusal.action === "acquire" ? "terminalAcquire" :
          refusal.action === "takeover" ? "terminalTypeHere" : refusal.action === "reacquire" ? "terminalNeedsReviewContinue" : "terminalCloudReconnect")}</button>}
    </div>}{firstFramePending && !error && phase !== "ended" && <TerminalSkeletonLines />}<div className="terminal-host" ref={screen} /></div>
    <div className="cloud-dock" hidden={history !== null}>
      {phase === "loading" && <TerminalSkeletonKeys />}
      {phase === "ended" && <section className="cloud-ended" aria-labelledby="cloud-ended-title">
        <h2 id="cloud-ended-title">{nextWord("terminalEndedTitle")}</h2>
        <p>{nextWord("terminalEndedHelp")}</p>
        <button className="cloud-primary" type="button" disabled={!!busy || !session} onClick={openNew}>{nextWord("terminalEndedOpenNew", { project: label })}</button>
        <button className="cloud-secondary" type="button" onClick={goBack}>{nextWord("terminalEndedBack")}</button>
        {diagnosticsLink}
      </section>}
      {(phase === "other_holder" || phase === "watching") && <div className="cloud-action">
        <p>{phase === "other_holder" ? nextWord("terminalOtherHolderHelp", { holder }) : nextWord("terminalWatchingHelp")}</p>
        <button className="cloud-primary" type="button" disabled={!!busy} onClick={takeInput}>{nextWord("terminalTypeHere")}</button>
      </div>}
      {phase === "needs_review" && <div className="cloud-action">
        <p>{nextWord("terminalNeedsReviewHelp")}</p>
        <button className="cloud-primary" type="button" disabled={!!busy} onClick={takeInput}>{nextWord("terminalNeedsReviewContinue")}</button>
      </div>}
      {phase === "reconnecting" && <div className="cloud-action">
        <p>{nextWord("terminalReconnectingHelp")}</p>
        <button className="cloud-secondary" type="button" disabled={!!busy} onClick={retryNow}>{nextWord("terminalRetryNow")}</button>
      </div>}
      {phase === "offline" && <div className="cloud-action">
        <p>{nextWord("terminalOfflineHelp")}</p>
        <button className="cloud-primary" type="button" disabled={!!busy} onClick={reconnect}>{nextWord("terminalCloudReconnect")}</button>
      </div>}
      {phase === "error" && <div className="cloud-action">
        <p role="alert">{frameTimedOut && !error ? nextWord("terminalCloudStaleHelp") : nextWord("terminalErrorHelp", { why: terminalRefusalWords(error) })}</p>
        <button className="cloud-primary" type="button" disabled={!!busy} onClick={reconnect}>{nextWord("terminalCloudReconnect")}</button>
        {diagnosticsLink}
      </div>}
      {phase === "denied" && <div className="cloud-action">
        <p role="alert">{snapshot.state === "revoked" ? nextWord("terminalCloudAccessRevoked") : nextWord("terminalCloudNotAuthorized", { code: error })}</p>
        {diagnosticsLink}
      </div>}
      {typing && keysOpen && <section className="cloud-keypanel" aria-label={nextWord("terminalMoreKeys")}>
        <div className="cloud-keypanel-head"><strong>{nextWord("terminalMoreKeys")}</strong>
          <button className="cloud-link" type="button" onClick={() => setKeysOpen(false)}>{nextWord("terminalKeysDone")}</button></div>
        <div className="cloud-keygrid">{COMBO_KEYS.map((key) =>
          <button key={key.name} className="cloud-key" data-danger={key.danger ? "" : undefined} type="button"
            onPointerDown={(event) => event.preventDefault()} onClick={() => sendKey(key.value)}>{key.name}<small>{nextWord(key.hint)}</small></button>)}
          {PANEL_KEYS.map(([name, value, spoken]) => <button key={name} className="cloud-key" type="button" aria-label={spoken ? nextWord(spoken) : undefined}
            onPointerDown={(event) => event.preventDefault()} onClick={() => sendKey(value)}>{name}</button>)}
          <button className="cloud-key" type="button" aria-pressed={ctrl} aria-label={nextWord("terminalKeyCtrl")}
            onPointerDown={(event) => event.preventDefault()} onClick={() => sendKey("ctrl")}>{nextWord("terminalKeyCtrlThen")}</button>
        </div>
      </section>}
      {typing && <div className="cloud-keyrow">
        <button className="cloud-round" type="button" aria-label={nextWord("terminalMoreKeys")} aria-expanded={keysOpen} onClick={() => setKeysOpen((open) => !open)}>
          <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false"><path d="M12 5v14M5 12h14" /></svg></button>
        <div className="cloud-quick" role="group" aria-label={nextWord("terminalKeys")}>
          {QUICK_KEYS.map(keyButton)}
          <button className="cloud-chip" type="button" disabled={pasting} aria-busy={pasting} onClick={() => void pasteFromClipboard()}>{nextWord("terminalPasteButton")}</button>
        </div>
        <button className="cloud-round" type="button" aria-label={nextWord("terminalShowKeyboard")} onClick={() => { terminal.current?.focus(); showInputLine() }}>
          <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false"><rect x="3" y="6" width="18" height="12" rx="2" /><path d="M7 10h1M11 10h1M15 10h1M8 14h8" /></svg></button>
      </div>}
      {dockNote && <p className="terminal-note" role="status">{dockNote}</p>}
      {diagnosticsText !== null && <pre className="terminal-diagnostics-text">{diagnosticsText}</pre>}
      {pasteMessage && <p className="terminal-note" role="status">{pasteMessage}</p>}
      {nativePaste && history === null && <PasteField failure={keyRefused?.why || pasteMessage} available={snapshot.canType} close={() => setNativePaste(false)} send={async text => {
        if (!session?.snapshot.canType || history !== null) { setPasteMessage(nextWord("terminalPasteUnavailable")); return false }
        recordPaste("send_started")
        try { await session.paste(text); recordPaste("sent"); keySent(); return true }
        catch (failure) { recordPaste("send_failed"); keyFailed(failure); return false }
      }} />}
    </div>
  </div>
}
