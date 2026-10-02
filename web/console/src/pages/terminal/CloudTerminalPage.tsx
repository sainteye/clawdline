import { useEffect, useRef, useState } from "react"
import type { Terminal as TerminalRow } from "@clawdline/contract"
import type { Terminal as XTerm } from "@xterm/xterm"
import type { FitAddon } from "@xterm/addon-fit"
import { nextWord } from "../../next-strings.js"
import { watchTerminalHost, type TerminalHost } from "../../cloud/terminal-host.js"
import { TerminalChannelTransport } from "../../cloud/terminal-transport.js"
import { TerminalObservation } from "../../cloud/terminal-observation.js"
import { CloudTerminalSession, type CloudTerminalSnapshot } from "../../cloud/terminal-session.js"
import { frameBytes } from "./frame-writer.js"
import { TAB } from "./tab.js"
import { openTerminalPage } from "./navigate.js"
import { firstSize } from "./TerminalProjectList.js"
import { KEY_ROW, bindTerminalKeyboard, isRegionKey, withCtrl } from "./keys.js"
import { holderWords, terminalShortID } from "./words.js"
import { beginCloudTerminal, cloudTerminalBody, listCloudTerminals } from "./cloud-view.js"

const empty: CloudTerminalSnapshot = { state: "opening", frame: null, control: null, canType: false, hasLease: false, reason: "" }
function reason(error: unknown): string { return (error as { code?: string })?.code ?? (error instanceof Error ? error.message : "cloud_failed") }
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
export function CloudTerminalPage({ project, channelProject, label, id, shown, from }: {
  project: string; channelProject: string; label: string; id: string; shown: boolean; from: "" | "projects" | "work" | "sessions"
}) {
  const [host, setHost] = useState<TerminalHost | null>(null)
  const [session, setSession] = useState<CloudTerminalSession | null>(null)
  const [snapshot, setSnapshot] = useState<CloudTerminalSnapshot>(empty)
  const [rows, setRows] = useState<TerminalRow[]>([])
  const [meta, setMeta] = useState<TerminalRow | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState("")
  const [history, setHistory] = useState<string[] | null>(null)
  const [reader, setReader] = useState(false)
  const [ctrl, setCtrl] = useState(false)
  const ctrlArmed = useRef(false)
  const [confirm, setConfirm] = useState<"takeover" | "close" | "reacquire" | null>(null)
  const screen = useRef<HTMLDivElement>(null)
  const terminal = useRef<XTerm | null>(null)
  const fit = useRef<FitAddon | null>(null)
  const lastRev = useRef("")
  const back = useRef<HTMLButtonElement>(null)
  const historyFocus = useRef<HTMLPreElement>(null)
  useEffect(() => watchTerminalHost(setHost), [])

  useEffect(() => {
    if (!shown || !host || !channelProject) return
    let live = true
    const observation = new TerminalObservation()
    const transport = new TerminalChannelTransport(host.client, host.machine, observation)
    const next = new CloudTerminalSession(transport, TAB, observation)
    const stop = next.subscribe((value) => live && setSnapshot(value))
    setSession(next); setSnapshot(empty); setLoading(true); setError(""); setMeta(null); setRows([])
    void (async () => {
      try {
        const begun = await beginCloudTerminal(next, channelProject, id, TAB)
        if (live) { if ("meta" in begun) setMeta(begun.meta); else setRows(begun.rows) }
      } catch (e) { if (live) setError(reason(e)) }
      finally { if (live) setLoading(false) }
    })()
    return () => { live = false; stop(); next.dispose(); transport.dispose(); setSession(null) }
  }, [host, channelProject, id, shown])

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
        if (current) { term.resize(current.cols, current.rows); term.write(frameBytes(current)); lastRev.current = current.rev }
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
      terminal.current?.dispose(); terminal.current = null; fit.current = null; lastRev.current = "" }
  }, [id, shown, session, label])

  useEffect(() => {
    const term = terminal.current
    const frame = snapshot.frame
    if (!term || !frame || frame.rev === lastRev.current) return
    lastRev.current = frame.rev
    if (term.cols !== frame.cols || term.rows !== frame.rows) term.resize(frame.cols, frame.rows)
    term.write(frameBytes(frame))
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
  const goBack = () => openTerminalPage(project, "", from)
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
      {!accessError && <button className="board-button" type="button" disabled={!!busy} onClick={reloadList}>{nextWord("terminalCloudReloadList")}</button>}</div>}
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
      <div className="terminal-head-row"><button className="board-button" type="button" ref={back} onClick={goBack}>{nextWord("terminalBack")}</button>
        <dl className="terminal-facts"><div><dt>{nextWord("terminalEntry")}</dt><dd>{terminalShortID(id)}</dd></div>
          <div><dt>{nextWord("terminalMachine")}</dt><dd>{host.machine}</dd></div>
          <div><dt>{nextWord("terminalFresh", { time: "" }).trim()}</dt><dd className="terminal-fresh">{status}</dd></div>
          <div><dt>{nextWord("terminalControlLabel")}</dt><dd>{holder}</dd></div></dl></div>
      <div className="terminal-actions" role="group" aria-label={nextWord("terminalControlLabel")}>
        {!snapshot.control?.holder?.same_client && <button className="board-button" type="button" disabled={!!busy || loading || accessError || snapshot.state === "revoked"}
          onClick={() => snapshot.control?.held ? setConfirm("takeover") : void run("acquire", () => session!.acquire("acquire"))}>
          {snapshot.control?.held ? nextWord("terminalTakeover") : nextWord("terminalAcquire")}</button>}
        {!accessError && (snapshot.state === "unknown" || (snapshot.control?.holder?.same_client && !snapshot.hasLease)) && <button className="board-button" type="button" disabled={!!busy}
          onClick={() => setConfirm("reacquire")}>{nextWord("terminalCloudReacquire")}</button>}
        {(snapshot.state === "stale" || snapshot.state === "offline") && <button className="board-button" type="button" disabled={!!busy}
          onClick={() => void run("reconnect", () => session!.start())}>{nextWord("terminalCloudReconnect")}</button>}
        {snapshot.control?.holder?.same_client && snapshot.hasLease && <button className="board-button" type="button" disabled={!!busy}
          onClick={() => void run("release", () => session!.release())}>{nextWord("terminalRelease")}</button>}
        <button className="board-button" type="button" disabled={!!busy} onClick={() => void run("history", async () => setHistory(history ? null : await session!.history()))}>{history ? nextWord("terminalHistoryBack") : nextWord("terminalHistory")}</button>
        <button className="board-button" type="button" aria-pressed={reader} onClick={() => setReader((value) => !value)}>{nextWord("terminalReaderMode")}</button>
        <button className="board-button" type="button" disabled={!snapshot.canType || !!busy} onClick={() => setConfirm("close")}>{nextWord("terminalClose")}</button>
      </div>
      {confirm && <div className="terminal-ask" role="group" aria-label={confirm === "close" ? nextWord("terminalClose") : nextWord("terminalTakeover")}>
        <p>{confirm === "reacquire" ? nextWord("terminalCloudReacquireAsk") : nextWord(confirm === "close" ? "terminalCloseAsk" : "terminalTakeoverAsk", { holder })}</p>
        <button className="board-button terminal-danger" type="button" disabled={!!busy} onClick={() => void run(confirm, async () => {
          if (confirm === "close") { await session!.close(); goBack() } else await session!.acquire(confirm === "reacquire" ? "acquire" : "takeover")
          setConfirm(null)
        })}>{nextWord(confirm === "close" ? "terminalCloseConfirm" : confirm === "reacquire" ? "terminalCloudReacquire" : "terminalTakeoverConfirm")}</button>
        <button className="board-button" type="button" onClick={() => setConfirm(null)}>{nextWord("terminalCancel")}</button>
      </div>}
      <p className="terminal-status-line" role="status" aria-live="polite">{loading ? nextWord("terminalConnecting") :
        error ? accessError ? nextWord("terminalCloudNotAuthorized", { code: error }) : nextWord("terminalCloudError", { code: error }) : snapshot.state === "offline" ? nextWord("terminalCloudOffline") :
          snapshot.state === "stale" ? nextWord("terminalCloudStaleHelp") : snapshot.state === "unknown" ? nextWord("terminalCloudUnknown") :
          snapshot.state === "revoked" ? nextWord("terminalCloudAccessRevoked") :
            !snapshot.canType ? nextWord("terminalCloudInputPaused") : ""}</p>
      {error && !accessError && snapshot.state !== "stale" && snapshot.state !== "offline" && <button className="board-button" type="button" disabled={!!busy} onClick={() => void run("reconnect", () => session!.start())}>{nextWord("terminalCloudReconnect")}</button>}
      {meta && meta.status !== "running" && <p role="alert">{nextWord("terminalExited")}</p>}
    </header>
    {history && <section className="terminal-history" aria-label={nextWord("terminalHistoryTitle")}><h2>{nextWord("terminalHistoryTitle")}</h2><pre ref={historyFocus} tabIndex={0}>{history.join("\n")}</pre></section>}
    <div className="terminal-scroll" hidden={history !== null}>{loading && <p className="terminal-note">{nextWord("terminalConnecting")}</p>}<div className="terminal-host" ref={screen} /></div>
    <div className="terminal-keys" role="group" aria-label={nextWord("terminalKeys")} hidden={history !== null}>
      {KEY_ROW.map(([name, value, spoken]) =>
        <button key={name} className="terminal-key" type="button" disabled={!snapshot.canType} aria-label={spoken ? nextWord(spoken) : undefined}
          aria-pressed={value === "ctrl" ? ctrl : undefined} onPointerDown={(event) => event.preventDefault()} onClick={() => sendKey(value)}>{name}</button>)}
    </div>
  </div>
}
