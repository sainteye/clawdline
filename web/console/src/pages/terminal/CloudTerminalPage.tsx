import { useEffect, useRef, useState, useSyncExternalStore } from "react"
import type { Terminal as TerminalRow } from "@clawdline/contract"
import type { Terminal as XTerm } from "@xterm/xterm"
import type { FitAddon } from "@xterm/addon-fit"
import { nextWord } from "../../next-strings.js"
import { watchTerminalHost, type TerminalHost } from "../../cloud/terminal-host.js"
import { TerminalChannelTransport } from "../../cloud/terminal-transport.js"
import { TerminalObservation } from "../../cloud/terminal-observation.js"
import { CloudTerminalSession, type CloudTerminalSnapshot, type CloudTerminalHistory } from "../../cloud/terminal-session.js"
import { frameBytes, frameDeltaBytes } from "./frame-writer.js"
import { TAB } from "./tab.js"
import { openTerminalPage } from "./navigate.js"
import { firstSize } from "./TerminalProjectList.js"
import { KEY_ROW, bindTerminalKeyboard, isRegionKey, readClipboardPaste, withCtrl } from "./keys.js"
import { holderWords, terminalRefusalWords, terminalShortID } from "./words.js"
import { beginCloudTerminal, cloudTerminalBody, listCloudTerminals, reconnectCloudTerminal } from "./cloud-view.js"
import { sessionsPageHash } from "../../page-route.js"
import { requestPage } from "../../overlays/index.js"
import { beginTerminalClose, observeTerminalEnded, settleTerminalClose, terminalCloseState, watchTerminalClose } from "../../cloud/terminal-close-state.js"

const empty: CloudTerminalSnapshot = { state: "opening", frame: null, control: null, canType: false, hasLease: false, reason: "" }
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
  return code ?? (error instanceof Error ? error.message : "cloud_failed")
}
function stateWords(state: CloudTerminalSnapshot["state"]): string {
  switch (state) {
    case "opening": return nextWord("terminalConnecting")
    case "synchronizing": return nextWord("terminalCloudSyncing")
    case "just_synced": return nextWord("terminalCloudJustSynced")
    case "live": return nextWord("terminalCloudLive")
    case "stale": return nextWord("terminalCloudStale")
    case "offline": return nextWord("terminalCloudOffline")
    case "revoked": return nextWord("terminalCloudAccessRevoked")
    case "unknown": return nextWord("terminalCloudUnknown")
    case "closed": return nextWord("terminalRefusalClosed")
  }
}

/**
 * Hosted-only terminal page. The local TerminalView and its fetch/SSE route remain untouched.
 * `project` is the Cloud Project id the page address carries; `channelProject` is the same
 * Project's machine-local id, the only one the machine's terminal channel knows (cloud-project.ts).
 */
export function CloudTerminalPage({ project, channelProject, machine, label, id, shown, from }: {
  project: string; channelProject: string; machine: string; label: string; id: string; shown: boolean; from: "" | "projects" | "work" | "sessions"
}) {
  const [host, setHost] = useState<TerminalHost | null>(null)
  const [session, setSession] = useState<CloudTerminalSession | null>(null)
  const [snapshot, setSnapshot] = useState<CloudTerminalSnapshot>(empty)
  const [rows, setRows] = useState<TerminalRow[]>([])
  const [meta, setMeta] = useState<TerminalRow | null>(null)
  const [loading, setLoading] = useState(true)
  const [firstFramePending, setFirstFramePending] = useState(true)
  const [frameTimedOut, setFrameTimedOut] = useState(false)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState("")
  const [history, setHistory] = useState<CloudTerminalHistory | null>(null)
  const [reader, setReader] = useState(false)
  const [ctrl, setCtrl] = useState(false)
  const [pasteMessage, setPasteMessage] = useState("")
  const [pasting, setPasting] = useState(false)
  const pasteBusy = useRef(false)
  const ctrlArmed = useRef(false)
  const [confirm, setConfirm] = useState<"takeover" | "reacquire" | null>(null)
  const [closeConfirm, setCloseConfirm] = useState(false)
  const closeDialog = useRef<HTMLDialogElement>(null)
  const closeOpener = useRef<HTMLButtonElement>(null)
  const closeCancel = useRef<HTMLButtonElement>(null)
  const closeMessage = useRef<HTMLParagraphElement>(null)
  const closeRecord = useSyncExternalStore(watchTerminalClose, () => terminalCloseState(machine, id), () => null)
  const screen = useRef<HTMLDivElement>(null)
  const terminal = useRef<XTerm | null>(null)
  const fit = useRef<FitAddon | null>(null)
  const lastRev = useRef("")
  const drawnFrame = useRef<CloudTerminalSnapshot["frame"]>(null)
  const back = useRef<HTMLButtonElement>(null)
  const historyFocus = useRef<HTMLPreElement>(null)
  const observed = useRef<TerminalObservation | null>(null)
  useEffect(() => watchTerminalHost(setHost), [])
  useEffect(() => {
    const dialog = closeDialog.current
    if (!dialog) return
    if (closeConfirm && !dialog.open) { dialog.showModal(); closeCancel.current?.focus() }
    if (!closeConfirm && dialog.open) dialog.close()
  }, [closeConfirm])
  useEffect(() => {
    if (closeRecord)
      closeMessage.current?.focus()
  }, [closeRecord])

  useEffect(() => {
    if (!shown || !host || !channelProject) return
    let live = true
    const observation = new TerminalObservation()
    observed.current = observation
    const transport = new TerminalChannelTransport(host.client, machine, observation)
    const next = new CloudTerminalSession(transport, TAB, observation)
    const stop = next.subscribe((value) => live && setSnapshot(value))
    setSession(next); setSnapshot(empty); setLoading(true); setFirstFramePending(!!id); setFrameTimedOut(false); setError(""); setMeta(null); setRows([])
    void (async () => {
      try {
        const begun = await beginCloudTerminal(next, channelProject, id, TAB)
        if (live) { if ("meta" in begun) setMeta(begun.meta); else setRows(begun.rows) }
      } catch (e) { if (live) { if (id && reason(e) === "terminal_closed") observeTerminalEnded(machine, id); setError(reason(e)) } }
      finally { if (live) setLoading(false) }
    })()
    return () => { live = false; stop(); next.dispose(); transport.dispose(); setSession(null) }
  }, [host, machine, channelProject, id, shown])

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
          void session?.input(new TextEncoder().encode(out)).catch((e) => setError(reason(e)))
        })
        term.onBinary((data) => { const bytes = Uint8Array.from(data, (char) => char.charCodeAt(0) & 0xff)
          void session?.input(bytes).catch((e) => setError(reason(e))) })
        terminal.current = term
        fit.current = addon
        term.options.disableStdin = !session?.snapshot.canType
        const current = session?.snapshot.frame
        if (current) { term.resize(current.cols, current.rows); term.write(frameBytes(current)); lastRev.current = current.rev; drawnFrame.current = current }
      } catch (e) { if (!cancelled) setError(reason(e)) }
    })()
    const element = screen.current
    const paste = (event: ClipboardEvent) => {
      event.preventDefault(); event.stopImmediatePropagation()
      const text = event.clipboardData?.getData("text/plain") ?? ""
      if (text) void session?.paste(text).catch((e) => setError(reason(e)))
    }
    const unbindKeys = bindTerminalKeyboard(element, () => back.current?.focus())
    element.addEventListener("paste", paste, true)
    return () => { cancelled = true; element.removeEventListener("paste", paste, true); unbindKeys()
      terminal.current?.dispose(); terminal.current = null; fit.current = null; lastRev.current = ""; drawnFrame.current = null }
  }, [id, shown, session, label])

  useEffect(() => {
    const term = terminal.current
    const frame = snapshot.frame
    if (!term || !frame || frame.rev === lastRev.current) return
    lastRev.current = frame.rev
    if (term.cols !== frame.cols || term.rows !== frame.rows) term.resize(frame.cols, frame.rows)
    term.write(frameDeltaBytes(drawnFrame.current, frame))
    drawnFrame.current = frame
  }, [snapshot.frame])
  useEffect(() => { if (terminal.current) terminal.current.options.disableStdin = !snapshot.canType }, [snapshot.canType])
  useEffect(() => { if (terminal.current) terminal.current.options.screenReaderMode = reader }, [reader])
  useEffect(() => {
    const box = screen.current?.parentElement
    if (!box || !session || !snapshot.canType || typeof ResizeObserver === "undefined") return
    let timer: ReturnType<typeof setTimeout> | null = null
    const suggest = () => {
      if (timer) clearTimeout(timer)
      timer = setTimeout(() => {
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
  const reloadList = () => void run("list", async () => {
    if (!session) return
    setLoading(true)
    try {
      setRows(await listCloudTerminals(session, channelProject, TAB))
    } finally { setLoading(false) }
  })
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
    openTerminalPage(project, next, from)
  })
  const goBack = () => {
    if (from !== "sessions") { openTerminalPage(project, "", from); return }
    const address = sessionsPageHash(true)
    try { window.history.replaceState(window.history.state, "", address) } catch { location.hash = address }
    requestPage({ page: "sessions", hash: false })
    window.dispatchEvent(new HashChangeEvent("hashchange"))
  }
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
  const queryClose = () => void run("close-read", async () => {
    if (!session) return
    try {
      const answer = await session.request("read", { terminal_id: id, client: TAB })
      const status = (answer.result as { status?: unknown } | undefined)?.status
      if (status === "closed") observeTerminalEnded(machine, id)
    } catch (failure) {
      if (reason(failure) === "terminal_closed") observeTerminalEnded(machine, id)
      else throw failure
    }
  })
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
    void session?.input(new TextEncoder().encode(out)).catch((e) => setError(reason(e)))
    terminal.current?.focus()
  }
  const pasteFromClipboard = async () => {
    if (pasteBusy.current) return
    pasteBusy.current = true; setPasting(true); setPasteMessage("")
    try {
      const outcome = await readClipboardPaste(navigator.clipboard,
        () => session?.snapshot.canType && !history ? null : "terminalPasteUnavailable",
        text => session!.paste(text))
      if (outcome) setPasteMessage(nextWord(outcome))
    } catch (failure) { setError(reason(failure)) }
    finally { pasteBusy.current = false; setPasting(false) }
  }
  const status = stateWords(snapshot.state)
  const accessError = error === "forbidden" || error === "terminal_forbidden" || error === "terminal_access_revoked"
  const holder = !snapshot.control?.held ? nextWord("terminalControlNobody") :
    (snapshot.control.holder?.name || snapshot.control.holder?.same_device)
      ? holderWords(snapshot.control.holder)
      : nextWord("terminalControlOtherDevice")
  const body = cloudTerminalBody(host, channelProject, id)
  if (body === "offline" || !host) return <p className="terminal-note" role="status">{nextWord("terminalCloudLineReconnecting")}</p>
  if (body === "no_project") return <p className="terminal-note" role="alert">{nextWord("terminalNoProject")}</p>
  if (body === "list") return <section className="terminal-wrap" aria-label={nextWord("terminalListTitle", { project: label })}>
    <div className="terminal-note-row"><h2>{nextWord("terminalListTitle", { project: label })}</h2>
      <button className="board-button" type="button" disabled={!!busy || loading || !!error} aria-busy={busy === "open"} onClick={openNew}>{nextWord("terminalOpenNew")}</button></div>
    {loading && <p className="terminal-note" role="status">{nextWord("terminalListLoading")}</p>}
    {error && <div><p className="terminal-note" role="alert">{accessError ? nextWord("terminalCloudNotAuthorized", { code: error }) :
      error === "terminal_open_state_unknown" ? nextWord("terminalCloudOpenUnknown") : nextWord("terminalListFailed", { why: error })}</p>
      {!accessError && <button className="board-button" type="button" disabled={!!busy} onClick={reloadList}>{nextWord("terminalCloudReloadList")}</button>}
      <TerminalDiagnostics observation={observed} /></div>}
    {!loading && !error && rows.length === 0 && <p className="terminal-note">{nextWord("terminalListEmpty")}</p>}
    <ul className="terminal-rows">{rows.map((row) => <li key={row.id}>
      <button className="terminal-row" type="button" onClick={() => openTerminalPage(project, row.id, from)}>
        {nextWord("terminalIdentity", { id: terminalShortID(row.id) })} · {row.status}
      </button></li>)}</ul>
  </section>

  return <div className="terminal-view" data-holding={snapshot.canType ? "" : undefined} data-stale={snapshot.state === "stale" || snapshot.state === "offline" ? "" : undefined}
    onKeyDown={(event) => { if (isRegionKey(event.nativeEvent)) {
      event.preventDefault()
      if (history) {
        if (event.target === historyFocus.current) back.current?.focus()
        else historyFocus.current?.focus()
      } else terminal.current?.focus()
    } }}>
    <header className="terminal-head">
      <div className="terminal-head-row"><button className="board-button" type="button" ref={back} onClick={goBack}>{nextWord(from === "sessions" ? "terminalBackSessions" : "terminalBack")}</button>
        <strong className="terminal-context" title={label}>{label}</strong>
        <dl className="terminal-facts">
          <div><dt>{nextWord("terminalFresh", { time: "" }).trim()}</dt><dd className="terminal-fresh">{status}</dd></div>
          <div><dt>{nextWord("terminalControlLabel")}</dt><dd>{holder}</dd></div></dl>
        <details className="terminal-more terminal-fact-more"><summary>{nextWord("terminalDetails")}</summary>
          <dl className="terminal-facts"><div><dt>{nextWord("terminalEntry")}</dt><dd>{terminalShortID(id)}</dd></div>
            <div><dt>{nextWord("terminalMachine")}</dt><dd>{machine}</dd></div></dl>
        </details></div>
      <p className="terminal-status-line" role="status" aria-live="polite">{loading && !snapshot.frame ? nextWord("terminalConnecting") :
        error ? accessError ? nextWord("terminalCloudNotAuthorized", { code: error }) : nextWord("terminalCloudError", { code: error }) : snapshot.state === "offline" ? nextWord("terminalCloudOffline") :
          snapshot.state === "stale" ? nextWord("terminalCloudStaleHelp") : snapshot.state === "unknown" ? nextWord("terminalCloudUnknown") :
          snapshot.state === "revoked" ? nextWord("terminalCloudAccessRevoked") :
            frameTimedOut ? nextWord("terminalCloudStaleHelp") : firstFramePending ? "" :
            !snapshot.canType ? snapshot.control?.held ? nextWord("terminalKeyboardPaused") : nextWord("terminalTerminateNeedsControl") : ""}</p>
      <div className="terminal-actions" role="group" aria-label={nextWord("terminalControlLabel")}>
        {!snapshot.control?.holder?.same_client && <button className="board-button" type="button" disabled={!!busy || loading || accessError || snapshot.state === "revoked"}
          onClick={() => snapshot.control?.held ? setConfirm("takeover") : void run("acquire", () => session!.acquire("acquire"))}>
          {snapshot.control?.held ? nextWord("terminalTakeover") : nextWord("terminalAcquire")}</button>}
        {!accessError && (snapshot.state === "unknown" || (snapshot.control?.holder?.same_client && !snapshot.hasLease)) && <button className="board-button" type="button" disabled={!!busy}
          onClick={() => setConfirm("reacquire")}>{nextWord("terminalCloudReacquire")}</button>}
        {(snapshot.state === "stale" || snapshot.state === "offline") && <button className="board-button" type="button" disabled={!!busy}
          onClick={reconnect}>{nextWord("terminalCloudReconnect")}</button>}
        {snapshot.control?.holder?.same_client && snapshot.hasLease && <button className="board-button" type="button" disabled={!!busy}
          onClick={() => void run("release", () => session!.release())}>{nextWord("terminalRelease")}</button>}
        <details className="terminal-more terminal-action-more"><summary>{nextWord("terminalMoreActions")}</summary><div className="terminal-more-actions">
        <button className="board-button" type="button" disabled={!!busy} onClick={() => void run("history", async () => setHistory(history ? null : await session!.history()))}>{history ? nextWord("terminalHistoryBack") : nextWord("terminalHistory")}</button>
        <button className="board-button" type="button" aria-pressed={reader} onClick={() => setReader((value) => !value)}>{nextWord("terminalReaderMode")}</button>
        <button className="board-button terminal-danger" type="button" ref={closeOpener}
          disabled={!snapshot.canType || !!busy || closeRecord?.status === "pending" || closeRecord?.status === "unknown" || closeRecord?.status === "ok" || closeRecord?.status === "ended"}
          onClick={() => setCloseConfirm(true)}>{nextWord("terminalTerminateHost")}</button>
        </div></details>
      </div>
      <dialog ref={closeDialog} className="terminal-close-confirm" onCancel={() => { setCloseConfirm(false); closeOpener.current?.focus() }}
        aria-label={nextWord("terminalTerminateHost")}>
        <p>{nextWord("terminalTerminateAsk", { project: label, machine, id: terminalShortID(id) })}</p>
        <button className="board-button" type="button" ref={closeCancel} onClick={() => { setCloseConfirm(false); closeOpener.current?.focus() }}>{nextWord("terminalCancel")}</button>
        <button className="board-button terminal-danger" type="button" onClick={submitClose}>{nextWord("terminalTerminateHost")}</button>
      </dialog>
      {closeRecord && <div className="terminal-close-result">
        <p ref={closeMessage} tabIndex={-1} role={closeRecord.status === "refused" ? "alert" : "status"}>
          {nextWord(closeRecord.status === "pending" ? "terminalTerminatePending" :
            closeRecord.status === "unknown" ? "terminalTerminateUnknown" :
              closeRecord.status === "ended" ? "terminalTerminateEnded" :
                closeRecord.status === "ok" ? "terminalTerminateSucceeded" : "terminalCloudError",
            { code: terminalRefusalWords(closeRecord.error) })}
        </p>
        {closeRecord.status === "unknown" && <button className="board-button" type="button" disabled={!!busy} onClick={queryClose}>{nextWord("terminalTerminateQuery")}</button>}
      </div>}
      {confirm && <div className="terminal-ask" role="group" aria-label={nextWord("terminalTakeover")}>
        <p>{confirm === "reacquire" ? nextWord("terminalCloudReacquireAsk") : nextWord("terminalTakeoverAsk", { holder })}</p>
        <button className="board-button terminal-danger" type="button" disabled={!!busy} onClick={() => void run(confirm, async () => {
          await session!.acquire(confirm === "reacquire" ? "acquire" : "takeover")
          setConfirm(null)
        })}>{nextWord(confirm === "reacquire" ? "terminalCloudReacquire" : "terminalTakeoverConfirm")}</button>
        <button className="board-button" type="button" onClick={() => setConfirm(null)}>{nextWord("terminalCancel")}</button>
      </div>}
      {(frameTimedOut || (error && !accessError && snapshot.state !== "stale" && snapshot.state !== "offline")) && <button className="board-button" type="button" disabled={!!busy} onClick={reconnect}>{nextWord("terminalCloudReconnect")}</button>}
      {meta && meta.status !== "running" && <p role="alert">{nextWord("terminalExited")}</p>}
      {(error || frameTimedOut || snapshot.state === "unknown" || snapshot.state === "revoked") && <TerminalDiagnostics observation={observed} />}
    </header>
    {history && <section className="terminal-history" aria-label={nextWord("terminalHistoryTitle")}><h2>{nextWord("terminalHistoryTitle")}</h2>
      {history.truncated && <p className="terminal-note" role="status">{nextWord("terminalCloudHistoryTruncated", { lines: history.omitted_lines })}</p>}
      <pre ref={historyFocus} tabIndex={0}>{history.lines.join("\n")}</pre></section>}
    <div className="terminal-scroll" hidden={history !== null}>{firstFramePending && !error && <p className="terminal-loading terminal-wait" role="status" aria-live="polite"><span className="terminal-wait-indicator" aria-hidden="true" />{nextWord("terminalCloudSyncing")}</p>}<div className="terminal-host" ref={screen} /></div>
    <button className="terminal-keyboard board-button" type="button" disabled={!snapshot.canType || history !== null}
      title={!snapshot.canType ? nextWord("terminalKeyboardPaused") : undefined}
      onClick={() => terminal.current?.focus()}>{nextWord(snapshot.canType ? "terminalShowKeyboard" : "terminalKeyboardPaused")}</button>
    <div className="terminal-keys" role="group" aria-label={nextWord("terminalKeys")} hidden={history !== null}>
      <button className="terminal-key terminal-paste" type="button" disabled={!snapshot.canType || pasting || history !== null}
        aria-busy={pasting} onClick={() => void pasteFromClipboard()}>{nextWord("terminalPasteButton")}</button>
      {KEY_ROW.map(([name, value, spoken]) =>
        <button key={name} className="terminal-key" type="button" disabled={!snapshot.canType} aria-label={spoken ? nextWord(spoken) : undefined}
          aria-pressed={value === "ctrl" ? ctrl : undefined} onPointerDown={(event) => event.preventDefault()} onClick={() => sendKey(value)}>{name}</button>)}
    </div>
    {pasteMessage && <p className="terminal-note" role="status">{pasteMessage}</p>}
  </div>
}
