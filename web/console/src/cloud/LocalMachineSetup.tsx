import { useEffect, useState, type ReactNode } from "react"
import { catalogWord } from "../catalog.js"
import { nativePairing, nativePairingAvailable, type NativePairingReply } from "./native-pairing.js"

const word = (key: string) => catalogWord("browserPairing", key)

/** The native owner can connect an unregistered machine before this viewer has a key. */
export function LocalMachineSetup({ onPair, children }: { onPair: (machine: { id: string; name: string }) => void; children?: ReactNode }) {
  const [status, setStatus] = useState<NativePairingReply | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const available = nativePairingAvailable()
  useEffect(() => {
    if (!available) return
    let live = true
    void nativePairing({ kind: "status" }).then(value => { if (live) setStatus(value) },
      reason => { if (live) setError(String(reason?.code || "connection_failed")) })
    return () => { live = false }
  }, [available])
  const waiting = status?.connection?.phase === "waiting"
  useEffect(() => {
    if (!waiting) return
    let live = true
    let pending = false
    const read = () => {
      if (pending) return
      pending = true
      void nativePairing({ kind: "connection-status" }).then(value => {
        if (live) { setStatus(value); setError("") }
      }, reason => { if (live) setError(String(reason?.code || "connection_failed")) }).finally(() => { pending = false })
    }
    const timer = setInterval(read, 2000)
    return () => { live = false; clearInterval(timer) }
  }, [waiting])
  if (!available) return children ?? null
  if (!status) return error ? <div role="region" aria-label={word("connect")}>
    <p role="status">{word("connectFailed")} ({error})</p>
    <button type="button" className="chip" disabled={busy} onClick={() => {
      setBusy(true)
      void nativePairing({ kind: "status" }).then(value => { setStatus(value); setError("") },
        reason => setError(String(reason?.code || "connection_failed"))).finally(() => setBusy(false))
    }}>{word("retry")}</button>
  </div> : null
  if (!status.native_setup) return children ?? null
  const ready = !!status.configured && !!status.enabled && !!status.machine_id
  async function connect() {
    if (busy) return
    setBusy(true)
    setError("")
    try { setStatus(await nativePairing({ kind: "connect" })) }
    catch (reason) { setError(String((reason as { code?: string })?.code || "connection_failed")) }
    finally { setBusy(false) }
  }
  return <div role="region" className="cloud-local-setup" aria-label={word("connect")}>
    <p className="fine">{word(ready ? "connected" : "connectIntro")}</p>
    {waiting ? <>
      <p role="status">{word("approve")}</p>
      {status.connection?.user_code && <p><code>{status.connection.user_code}</code></p>}
      {status.connection?.verification_url && <a className="chip" href={status.connection.verification_url} target="_blank" rel="noreferrer">{word("approvalPage")}</a>}
      <button type="button" className="chip" onClick={() => {
        void nativePairing({ kind: "connection-cancel" }).then(value => { setStatus(value); setError("") },
          reason => setError(String(reason?.code || "connection_failed")))
      }}>{word("stopConnecting")}</button>
    </> : !ready ? <button className="go" type="button" disabled={busy} onClick={() => void connect()}>{word("connect")}</button> : <>
      <p>{status.machine_name}</p>
      <button className="go" type="button" onClick={() => onPair({ id: status.machine_id!, name: status.machine_name || status.machine_id! })}>{word("pairApp")}</button>
      {children}
    </>}
    {status.connection?.phase === "failed" && <p role="status">{word("connectFailed")} ({status.connection.code})</p>}
    {error && <p role="status">{word("connectFailed")} ({error})</p>}
  </div>
}
