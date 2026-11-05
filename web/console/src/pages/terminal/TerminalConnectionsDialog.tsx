import { useEffect, useRef, useState } from "react"
import type { TerminalHost } from "../../cloud/terminal-host.js"
import { manageTerminalConnections, type TerminalConnections, type TerminalConnectionRow } from "../../cloud/terminal-connections.js"
import { showModalWithoutChoice, focusModalContainer } from "../../modal-focus.js"
import { nextWord } from "../../next-strings.js"
import { terminalRefusalWords } from "./words.js"

/** Disconnects viewer-owned display connections; the shell is never terminated here. */
export function TerminalConnectionsDialog({ host, machine, close, focusTarget }: { host: TerminalHost; machine: string; close: () => void; focusTarget?: HTMLElement | null }) {
  const dialog = useRef<HTMLDialogElement>(null)
  const confirmation = useRef<HTMLElement>(null)
  const [listing, setListing] = useState<TerminalConnections | null>(null)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState("list")
  const [selected, setSelected] = useState<TerminalConnectionRow | null>(null)
  const [notice, setNotice] = useState("")
  const run = async (row?: TerminalConnectionRow) => {
    setBusy(row ? row.machine + "/" + row.connection : "list"); setError("")
    try {
      setListing(await manageTerminalConnections(host.client, row ? "disconnect" : "list", row))
      setSelected(null)
      if (row) setNotice(nextWord("terminalConnectionReleased"))
    } catch (failure) { setError((failure as { code?: string }).code ?? "terminal_send_failed") }
    finally { setBusy("") }
  }
  useEffect(() => {
    const opener = focusTarget ?? document.activeElement as HTMLElement | null
    showModalWithoutChoice(dialog.current)
    void run()
    return () => { if (opener?.isConnected && opener.getClientRects().length) opener.focus() }
  }, [host])
  useEffect(() => { if (selected) { focusModalContainer(confirmation.current); confirmation.current?.scrollIntoView({ block: "nearest" }) } }, [selected])
  return <dialog ref={dialog} className="terminal-connections-dialog" aria-labelledby="terminal-connections-title" onCancel={(event) => { event.preventDefault(); close() }}>
    <div className="terminal-connections-heading"><h2 id="terminal-connections-title">{nextWord("terminalConnectionsTitle")}</h2>
      <button className="board-button" type="button" onClick={close}>{nextWord("terminalConnectionsDone")}</button></div>
    <p>{nextWord("terminalConnectionsHelp")}</p>
    {listing && <p className="terminal-note">{nextWord("terminalConnectionsCount", { used: listing.connections.length, limit: listing.limit })}</p>}
    {error && <p role="alert">{terminalRefusalWords(error)}</p>}
    {notice && <p role="status">{notice}</p>}
    {busy === "list" && <p role="status">{nextWord("terminalConnectionsLoading")}</p>}
    {listing?.connections.length === 0 && <p>{nextWord("terminalConnectionsEmpty")}</p>}
    <ul className="terminal-connections-list">{listing?.connections.map((row) => <li key={row.machine + "/" + row.connection}>
      <div><strong>{row.machine === machine ? nextWord("terminalConnectionsThisMachine") : row.machine}</strong>
        <span>{host.client.socketSubscriptions.has(`term/${row.machine}/${host.client.deviceID}/${row.connection}`) && <>{nextWord("terminalConnectionsThisTab")} · </>}{nextWord(row.active ? "terminalConnectionActive" : "terminalConnectionPending")} · {row.connection.slice(-8)}</span>
        <time dateTime={new Date(row.opened_at).toISOString()}>{new Date(row.opened_at).toLocaleString()}</time></div>
      <button className="board-button" type="button" disabled={!!busy} onClick={() => setSelected(row)}>{nextWord("terminalConnectionDisconnect")}</button>
      {selected?.machine === row.machine && selected.connection === row.connection && <section ref={confirmation} className="terminal-connection-confirm" aria-label={nextWord("terminalConnectionDisconnect")}>
        <p>{nextWord("terminalConnectionDisconnectAsk", { id: row.connection.slice(-8) })}</p>
        <button className="board-button" type="button" disabled={!!busy} onClick={() => setSelected(null)}>{nextWord("terminalCancel")}</button>
        <button className="board-button terminal-danger" type="button" disabled={!!busy} aria-busy={!!busy} onClick={() => void run(row)}>{nextWord("terminalConnectionDisconnect")}</button>
      </section>}
    </li>)}</ul>
    <button className="board-button" type="button" disabled={!!busy} onClick={() => void run()}>{nextWord("terminalConnectionsRefresh")}</button>
  </dialog>
}
