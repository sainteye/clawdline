import "./workflow-settings.css"
import "./window/window.css"
import { useEffect, useState } from "react"
import { catalogWord } from "../../catalog.js"
import { failureSentence } from "../../refusals/refusal-text.js"
import { nativePairing, nativePairingAvailable, type NativePairingReply } from "../../cloud/native-pairing.js"
import { localPairing } from "../../cloud/pair-agent.js"
import { PairingQr } from "./window/PairingQr.js"
import type { CloudPairing } from "./cloud.js"

const word = (key: string) => catalogWord("browserPairing", key)

/** A website entry to the local handover, still confirmed by the owning Mac. */
export function BrowserPairingBlock({ shown }: { shown: boolean }) {
  const available = nativePairingAvailable()
  const [machine, setMachine] = useState<NativePairingReply | null>(null)
  const [pairing, setPairing] = useState<CloudPairing | null>(null)
  const [code, setCode] = useState("")
  const [busy, setBusy] = useState(false)
  const [said, setSaid] = useState("")
  useEffect(() => {
    if (!shown || !available) return
    let live = true
    void nativePairing({ kind: "status" }).then(value => { if (live) setMachine(value) },
      error => { if (live) setSaid(failureSentence(error, word("failed"))) })
    return () => { live = false }
  }, [shown, available])
  useEffect(() => {
    if (!shown || !pairing || (pairing.phase !== "waiting" && pairing.phase !== "sealing")) return
    let live = true
    let pending = false
    const timer = setInterval(() => {
      if (pending) return
      pending = true
      void nativePairing({ kind: "invitation-status" }).then(value => {
        if (live && value.pairing) setPairing(value.pairing)
      }, error => { if (live) setSaid(failureSentence(error, word("failed"))) }).finally(() => { pending = false })
    }, 2000)
    return () => { live = false; clearInterval(timer) }
  }, [shown, pairing?.phase])
  const run = async (work: () => Promise<void>) => {
    if (busy) return
    setBusy(true)
    setSaid("")
    try { await work() }
    catch (error) { setSaid(failureSentence(error, word("failed"))) }
    finally { setBusy(false) }
  }
  const invite = () => void run(async () => {
    const result = await nativePairing({ kind: "invitation", machine_id: machine?.machine_id })
    if (result.pairing) setPairing(result.pairing)
  })
  return <div className="block settings-browser-pairing">
    <b>{word("title")}</b>
    <p className="say">{word("intro")}</p>
    {!available ? <p className="say">{word("localOnly")}</p> : <>
      <p className="say">{machine?.machine_name}</p>
      {machine?.pairing_invitation ? <button type="button" className="chip" disabled={busy} onClick={invite}>{word("qr")}</button>
        : machine && <p className="say">{word("update")}</p>}
      {(pairing?.machine_fingerprint || machine?.fingerprint) && <p>{word("machineFingerprint")}: <code>{pairing?.machine_fingerprint || machine?.fingerprint}</code></p>}
      {pairing?.viewer_fingerprint && <p>{word("viewerFingerprint")}: <code>{pairing.viewer_fingerprint}</code></p>}
      {pairing && <PairingQr pairing={pairing} busy={busy} onRenew={invite} automaticRenew={false} />}
      {pairing?.phase === "paired" && <p role="status">{word("paired")}</p>}
      <label htmlFor="settings-browser-pairing-code">{word("code")}</label>
      <textarea id="settings-browser-pairing-code" rows={3} value={code} disabled={busy}
        spellCheck={false} autoCapitalize="off" autoCorrect="off" onChange={event => setCode(event.currentTarget.value)} />
      <button type="button" className="chip" disabled={busy || !machine?.machine_id || !code.trim()}
        onClick={() => void run(async () => {
          const result = await localPairing({ machineID: machine?.machine_id, offer: code.trim() })
          if (!result.ok) throw Object.assign(new Error(result.error), { code: result.error })
          setCode("")
          setSaid(word("paired"))
        })}>{word("submit")}</button>
    </>}
    <p className="said" role="status">{said}</p>
  </div>
}
