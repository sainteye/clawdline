import { useState } from "react"
import { nextWord } from "../next-strings.js"
import { failureSentence } from "../refusals/refusal-text.js"
import type { CloudClientHandle } from "./copied.js"
import { destinationKey, type FleetMachine, type ProjectedSession, type SessionDestination,
  type SessionProjectionSource } from "./all-machine-sessions.js"
import { checkedPeerAccessStatus, ensurePeerEndpointCurrent } from "./peer-handoff-admission.js"

type Control = { action: string; pair_id?: string; grant_id?: string; target_machine_id?: string;
  compared_fingerprint?: string; source_session_id?: string; source_execution_generation?: string;
  target_session_id?: string; target_execution_generation?: string; scopes?: string[] }
type Answer = { pair_id?: string; grant_id?: string; local_fingerprint?: string; state?: string }
type Step = "endpoints" | "fingerprints" | "scopes"
const command = "clawdline cloud peer fingerprint"

export function PeerAccessWizard({ machines, sessions, source, current, machineName, sessionName,
  onCancel, onCreated }: {
  machines: readonly FleetMachine[]
  sessions: Readonly<Record<string, ProjectedSession[]>>
  source: SessionProjectionSource | null
  current: () => CloudClientHandle | null
  machineName: (id: string) => string
  sessionName: (row: ProjectedSession) => string
  onCancel: () => void
  onCreated: () => void
}) {
  const [step, setStep] = useState<Step>("endpoints")
  const [sourceMachine, setSourceMachine] = useState("")
  const [targetMachine, setTargetMachine] = useState("")
  const [sourceKey, setSourceKey] = useState("")
  const [targetKey, setTargetKey] = useState("")
  const [fingerprints, setFingerprints] = useState<{ source: string; target: string } | null>(null)
  const [sourceInput, setSourceInput] = useState("")
  const [targetInput, setTargetInput] = useState("")
  const [pairID, setPairID] = useState("")
  const [pairPhase, setPairPhase] = useState(0)
  const [grantID, setGrantID] = useState("")
  const [grantPhase, setGrantPhase] = useState(0)
  const [messageScope, setMessageScope] = useState(true)
  const [handoffScope, setHandoffScope] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const available = machines.filter((machine) => machine.freshness === "current" && machine.paired !== false)
  const allRows = Object.values(sessions).flat()
  const from = allRows.find((row) => destinationKey(row.destination) === sourceKey)?.destination
  const to = allRows.find((row) => destinationKey(row.destination) === targetKey)?.destination
  const sourceMatches = !!fingerprints && !!sourceInput.trim() && sourceInput.trim() === fingerprints.source
  const targetMatches = !!fingerprints && !!targetInput.trim() && targetInput.trim() === fingerprints.target

  function resetAgreement() {
    setFingerprints(null); setSourceInput(""); setTargetInput("")
    setPairID(""); setPairPhase(0); setGrantID(""); setGrantPhase(0); setError("")
  }

  function clientFor(machineID: string): CloudClientHandle {
    const client = current()
    if (!client?.allowWrites || !client._machineRequestAs || !client.viewerVerified?.has(machineID) ||
      !client.machineDescriptor?.(machineID)?.machine?.commands?.includes("peer-control"))
      throw new Error("peer_capability_unavailable")
    return client
  }
  async function control(machineID: string, input: Control): Promise<Answer> {
    const client = clientFor(machineID)
    return await client._machineRequestAs!(crypto.randomUUID(), machineID, "peer-control",
      { control: input }, "action", 30_000) as Answer
  }
  async function readFingerprints() {
    if (!from || !to || busy) return
    setBusy(true); setError(""); setFingerprints(null); setSourceInput(""); setTargetInput("")
    try {
      const [first, second] = await Promise.all([
        control(from.machineID, { action: "status" }),
        control(to.machineID, { action: "status" }),
      ])
      checkedPeerAccessStatus(first, from.machineID)
      checkedPeerAccessStatus(second, to.machineID)
      if (!first.local_fingerprint || !second.local_fingerprint) throw new Error("peer_fingerprint_unknown")
      setFingerprints({ source: first.local_fingerprint, target: second.local_fingerprint })
      setStep("fingerprints")
    } catch (error) { setError(failureSentence(error, nextWord("cloudPeerAccessFingerprintUnread"))) }
    finally { setBusy(false) }
  }
  async function buildPair() {
    if (!from || !to || !sourceMatches || !targetMatches || busy) return
    setBusy(true); setError("")
    let id = pairID
    try {
      if (pairPhase < 1) {
        const answer = await control(from.machineID, { action: "start", target_machine_id: to.machineID,
          compared_fingerprint: fingerprints!.target })
        if (!answer.pair_id || answer.state !== "waiting_for_target") throw new Error("peer_pair_start_unconfirmed")
        id = answer.pair_id
        setPairID(id); setPairPhase(1)
      }
      if (pairPhase < 2) {
        const answer = await control(to.machineID, { action: "accept", pair_id: id,
          compared_fingerprint: fingerprints!.source })
        if (answer.pair_id !== id || answer.state !== "active_target") throw new Error("peer_pair_accept_unconfirmed")
        setPairPhase(2)
      }
      if (pairPhase < 3) {
        const answer = await control(from.machineID, { action: "sync", pair_id: id,
          compared_fingerprint: fingerprints!.target })
        if (answer.pair_id !== id || answer.state !== "active_source") throw new Error("peer_pair_sync_unconfirmed")
        setPairPhase(3)
      }
      setStep("scopes")
    } catch (error) { setError(failureSentence(error, nextWord("cloudPeerAccessPairUnconfirmed"))) }
    finally { setBusy(false) }
  }
  async function buildGrant() {
    if (!from || !to || !pairID || busy || !messageScope && !handoffScope) return
    setBusy(true); setError("")
    let id = grantID
    try {
      await Promise.all([ensurePeerEndpointCurrent(from, source, current()),
        ensurePeerEndpointCurrent(to, source, current())])
      if (grantPhase < 1) {
        const answer = await control(to.machineID, { action: "grant", pair_id: pairID,
          source_session_id: from.sessionID, source_execution_generation: from.executionGeneration,
          target_session_id: to.sessionID, target_execution_generation: to.executionGeneration,
          scopes: [...(messageScope ? ["message"] : []), ...(handoffScope ? ["handoff"] : [])] })
        if (!answer.grant_id || answer.state !== "grant_active_target") throw new Error("peer_grant_unconfirmed")
        id = answer.grant_id
        setGrantID(id); setGrantPhase(1)
      }
      if (grantPhase < 2) {
        const answer = await control(from.machineID, { action: "grant_sync", grant_id: id })
        if (answer.grant_id !== id || answer.state !== "grant_active_source") throw new Error("peer_grant_sync_unconfirmed")
        setGrantPhase(2)
      }
      onCreated()
    } catch (error) { setError(failureSentence(error, nextWord("cloudPeerAccessGrantUnconfirmed"))) }
    finally { setBusy(false) }
  }
  const endpointPicker = (role: "source" | "target") => {
    const machineID = role === "source" ? sourceMachine : targetMachine
    const setMachine = role === "source" ? setSourceMachine : setTargetMachine
    const key = role === "source" ? sourceKey : targetKey
    const setKey = role === "source" ? setSourceKey : setTargetKey
    const rows = sessions[machineID] ?? []
    return <div className="cloud-peer-endpoint">
      <h4>{nextWord(role === "source" ? "cloudPeerFromWhere" : "cloudPeerToWhere")}</h4>
      <label>{nextWord(role === "source" ? "cloudPeerAccessSourceMachine" : "cloudPeerAccessTargetMachine")}
        <select value={machineID} disabled={busy} onChange={(event) => {
          setMachine(event.target.value); setKey(""); resetAgreement()
          if (role === "source" && event.target.value === targetMachine) {
            setTargetMachine(""); setTargetKey("")
          }
        }}>
          <option value="">{nextWord("cloudPeerChooseMachine")}</option>
          {available.filter((machine) => role === "source" || machine.id !== sourceMachine).map((machine) =>
            <option key={machine.id} value={machine.id}>{machineName(machine.id)}</option>)}
        </select>
      </label>
      <label>{nextWord(role === "source" ? "cloudPeerSource" : "cloudPeerAccessTargetSession")}
        <select value={key} disabled={!machineID || busy} onChange={(event) => {
          setKey(event.target.value); resetAgreement()
        }}>
          <option value="">{nextWord("cloudPeerAccessChooseSession")}</option>
          {rows.map((row) => <option key={destinationKey(row.destination)}
            value={destinationKey(row.destination)}>{sessionName(row)}</option>)}
        </select>
      </label>
      {machineID && rows.length === 0 && <p>{nextWord("cloudPeerAccessNoCurrentSessions")}</p>}
    </div>
  }
  return <div className="cloud-peer-wizard">
    <div className="cloud-peer-wizard-top">
      <p className="cloud-peer-wizard-progress">{nextWord("cloudPeerAccessStep", {
        number: step === "endpoints" ? "1" : step === "fingerprints" ? "2" : "3",
      })}</p>
      <button type="button" className="peer-secondary" disabled={busy} onClick={onCancel}>{nextWord("cloudPeerAccessCancel")}</button>
    </div>
    {step === "endpoints" && <>
      <h3>{nextWord("cloudPeerStepEndpoints")}</h3>
      <p>{nextWord("cloudPeerAccessEndpointsWhat")}</p>
      <div className="cloud-peer-endpoints">{endpointPicker("source")}
        <span className="cloud-peer-route-arrow" aria-hidden="true">→</span>{endpointPicker("target")}</div>
      <button type="button" className="peer-primary" disabled={!from || !to || from.machineID === to.machineID || busy}
        onClick={() => void readFingerprints()}>{nextWord("cloudPeerNextFingerprint")}</button>
    </>}
    {step === "fingerprints" && <>
      <h3>{nextWord("cloudPeerStepFingerprint")}</h3>
      <p>{nextWord("cloudPeerFingerprintWhy")}</p>
      {([["source", from, sourceInput, setSourceInput, sourceMatches],
        ["target", to, targetInput, setTargetInput, targetMatches]] as const).map(([role, destination, value, setValue, matched], index) =>
        destination && <div className="cloud-peer-fingerprint" key={role}>
          <h4>{index + 1}. {machineName(destination.machineID)}</h4>
          <p>{nextWord("cloudPeerFingerprintRun")}</p>
          <div className="cloud-peer-command"><code>{command}</code>
            <button className="peer-secondary" type="button" onClick={() => void navigator.clipboard.writeText(command)}>
              {nextWord("cloudPeerCopy")}</button></div>
          <label>{nextWord("cloudPeerFingerprintPaste")}
            <input value={value} onChange={(event) => setValue(event.target.value)} disabled={busy}
              autoCapitalize="off" autoComplete="off" spellCheck={false} /></label>
          {value.trim() && <p className={matched ? "cloud-peer-match" : "cloud-peer-mismatch"} role="status">
            {nextWord(matched ? "cloudPeerFingerprintMatch" : "cloudPeerFingerprintDiffers")}</p>}
        </div>)}
      <div className="cloud-peer-access-actions">
        <button type="button" className="peer-secondary" disabled={busy || pairPhase > 0}
          onClick={() => setStep("endpoints")}>{nextWord("cloudPeerAccessBack")}</button>
        <button type="button" className="peer-primary" disabled={!sourceMatches || !targetMatches || busy}
          onClick={() => void buildPair()}>{nextWord("cloudPeerPairBuild")}</button>
      </div>
      {pairPhase > 0 && <p role="status">{nextWord("cloudPeerAccessPairProgress", { step: String(pairPhase) })}</p>}
    </>}
    {step === "scopes" && <>
      <h3>{nextWord("cloudPeerStepScopes")}</h3>
      <p>{nextWord("cloudPeerAccessScopesWhat")}</p>
      <div className="cloud-peer-scope-cards">
        <label><input type="checkbox" checked={messageScope} disabled={busy || grantPhase > 0}
          onChange={(event) => setMessageScope(event.target.checked)} />
          <span><strong>{nextWord("cloudPeerMessage")}</strong><small>{nextWord("cloudPeerScopeMessageWhat")}</small></span></label>
        <label><input type="checkbox" checked={handoffScope} disabled={busy || grantPhase > 0}
          onChange={(event) => setHandoffScope(event.target.checked)} />
          <span><strong>{nextWord("cloudPeerHandoff")}</strong><small>{nextWord("cloudPeerScopeHandoffWhat")}</small></span></label>
      </div>
      <button className="peer-primary" type="button" disabled={busy || !messageScope && !handoffScope}
        onClick={() => void buildGrant()}>{nextWord("cloudPeerGrantBuild")}</button>
      {grantPhase > 0 && <p role="status">{nextWord("cloudPeerAccessGrantProgress", { step: String(grantPhase) })}</p>}
    </>}
    {error && <p className="cloud-peer-wizard-error" role="alert">{error}</p>}
  </div>
}
