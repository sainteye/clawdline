import { useEffect, useRef, useState } from "react"
import { nextWord } from "../next-strings.js"
import type { CloudClientHandle } from "./copied.js"
import type { DetailActionContext, FleetMachine, SessionDestination, SessionProjectionSource } from "./all-machine-sessions.js"
import { checkedPeerAccessStatus, ensurePeerEndpointCurrent, type PeerAccessSnapshot } from "./peer-handoff-admission.js"
import { STATUS_FRESH_MS } from "./status-projection.js"
import "./peer-handoff.css"

type Control = { action: string; pair_id?: string; grant_id?: string; target_machine_id?: string;
  compared_fingerprint?: string; source_session_id?: string; source_execution_generation?: string;
  target_session_id?: string; target_execution_generation?: string; scopes?: string[] }
type ControlAnswer = { pair_id?: string; grant_id?: string; local_fingerprint?: string; peer_fingerprint?: string; state?: string }
type InboxItem = { request: { request_id: string; kind: string; source: SessionDestinationWire; target: SessionDestinationWire };
  body: string; receipt: { machine_execution: string; session_delivered: string; agent_observed: string; agent_acknowledged: string } }
type InboxWireItem = Omit<InboxItem, "body"> & { body_base64: string }
type SessionDestinationWire = { machine_id: string; session_id: string; execution_generation: string }
type Revocation = { machineID: string; action: "revoke_pair" | "revoke_grant"; id: string; context: string }

function RevokeConfirmation({ pending, onCancel, onConfirm }: {
  pending: Revocation; onCancel: () => void; onConfirm: () => void
}) {
  const dialog = useRef<HTMLDialogElement>(null)
  const cancel = useRef<HTMLButtonElement>(null)
  const confirmed = useRef(false)
  useEffect(() => {
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null
    dialog.current?.showModal()
    cancel.current?.focus()
    return () => { dialog.current?.close(); previous?.focus() }
  }, [])
  return <dialog ref={dialog} className="cloud-peer-confirm" onCancel={(event) => { event.preventDefault(); onCancel() }}>
    <h3>{nextWord("cloudPeerRevokeConfirmTitle")}</h3>
    <p>{nextWord("cloudPeerRevokeConfirmBody", { machine: pending.machineID,
      kind: pending.action === "revoke_pair" ? nextWord("cloudPeerPairID") : nextWord("cloudPeerGrantID"),
      id: pending.id, context: pending.context })}</p>
    <div className="cloud-peer-buttons">
      <button ref={cancel} type="button" onClick={onCancel}>{nextWord("cloudPeerRevokeCancel")}</button>
      <button type="button" onClick={() => { if (confirmed.current) return; confirmed.current = true; onConfirm() }}>
        {nextWord("cloudPeerRevokeConfirm")}</button>
    </div>
  </dialog>
}

function wire(destination: SessionDestination): SessionDestinationWire {
  return { machine_id: destination.machineID, session_id: destination.sessionID,
    execution_generation: destination.executionGeneration }
}

function problem(error: unknown): string {
  const code = (error as { code?: unknown } | null)?.code
  return typeof code === "string" ? code : error instanceof Error ? error.message : String(error)
}

function decodeInboxBody(encoded: string): string {
  const binary = atob(encoded)
  const bytes = new Uint8Array(binary.length)
  for (let index = 0; index < binary.length; index++) bytes[index] = binary.charCodeAt(index)
  return new TextDecoder().decode(bytes)
}

export function PeerHandoffPanel({ context, machines, source, current }: {
  context: DetailActionContext
  machines: readonly FleetMachine[]
  source: SessionProjectionSource | null
  current: () => CloudClientHandle | null
}) {
  const [sources, setSources] = useState<SessionDestination[]>([])
  const [selected, setSelected] = useState("")
  const [pairID, setPairID] = useState("")
  const [grantID, setGrantID] = useState("")
  const [targetFingerprint, setTargetFingerprint] = useState("")
  const [sourceFingerprint, setSourceFingerprint] = useState("")
  const [fingerprintHints, setFingerprintHints] = useState<{ source: string; target: string } | null>(null)
  const [sourceCompared, setSourceCompared] = useState(false)
  const [targetCompared, setTargetCompared] = useState(false)
  const [kind, setKind] = useState<"message" | "handoff">("message")
  const [allowMessage, setAllowMessage] = useState(true)
  const [allowHandoff, setAllowHandoff] = useState(false)
  const [body, setBody] = useState("")
  const [answer, setAnswer] = useState("")
	const [lastRequest, setLastRequest] = useState("")
  const [busy, setBusy] = useState(false)
  const [inbox, setInbox] = useState<InboxItem[]>([])
  const [inboxBefore, setInboxBefore] = useState("")
  const [pendingRevoke, setPendingRevoke] = useState<Revocation | null>(null)
  const target = context.destination

  useEffect(() => {
    setPairID("")
    setGrantID("")
    setTargetFingerprint("")
    setSourceFingerprint("")
    setFingerprintHints(null)
    setSourceCompared(false)
    setTargetCompared(false)
    setLastRequest("")
    setInbox([])
    setInboxBefore("")
  }, [selected, target.machineID, target.sessionID, target.executionGeneration])

  useEffect(() => {
    if (!source) { setSources([]); return }
    const abort = new AbortController()
    void Promise.all(machines.filter((machine) => machine.id !== target.machineID && machine.freshness === "current")
      .map(async (machine) => ({ machine, reading: await source.readMachine(machine.id, abort.signal) })))
      .then((rows) => {
        if (abort.signal.aborted) return
        setSources(rows.flatMap(({ reading }) => reading.kind === "ready" ? reading.rows
          .filter((row) => row.freshness === "current").map((row) => row.destination) : []))
      }).catch(() => { if (!abort.signal.aborted) setSources([]) })
    return () => abort.abort()
  }, [source, machines.map((machine) => machine.id + ":" + machine.freshness).join("\0"), target.machineID])

  const chosen = sources.find((entry) => JSON.stringify(entry) === selected)
  const sourceMachine = machines.find((machine) => machine.id === chosen?.machineID)

  function canWrite(machineID: string | undefined, command: string): boolean {
    const client = current()
    return !!machineID && !!client?.allowWrites && !!client._machineRequestAs &&
      !!client.viewerVerified?.has(machineID) && !!client.machineDescriptor?.(machineID)?.machine?.commands?.includes(command)
  }
  const sourceCanControl = canWrite(chosen?.machineID, "peer-control")
  const targetCanControl = canWrite(target.machineID, "peer-control")
  const sourceCanSend = canWrite(chosen?.machineID, "peer-send")

  function clientFor(machineID: string, command: string): CloudClientHandle {
    const client = current()
    const commands = client?.machineDescriptor?.(machineID)?.machine?.commands
    if (!client || !client._machineRequestAs || !client.allowWrites ||
      !commands?.includes(command) || !client.viewerVerified?.has(machineID)) {
      throw Object.assign(new Error("peer_capability_unavailable"), { code: "peer_capability_unavailable" })
    }
    return client
  }

  async function control(machineID: string, input: Control) {
    if (busy) return
    setBusy(true)
    try {
      if (input.action === "grant") {
        if (!chosen) throw new Error("peer_source_unavailable")
        await Promise.all([ensurePeerEndpointCurrent(chosen, source, current()), ensurePeerEndpointCurrent(target, source, current())])
      }
      const client = clientFor(machineID, "peer-control")
      const request = crypto.randomUUID()
      const response = await client._machineRequestAs!(request, machineID, "peer-control", { control: input }, "action", 30_000) as ControlAnswer
      if (response.pair_id) setPairID(response.pair_id)
      if (response.grant_id) setGrantID(response.grant_id)
      setAnswer(nextWord("cloudPeerControlResult", { state: response.state ?? "unknown", id: response.grant_id ?? response.pair_id ?? "" }))
    } catch (error) { setAnswer(nextWord("cloudPeerRefused", { code: problem(error) })) }
    finally { setBusy(false) }
  }

  function askRevoke(machineID: string, action: Revocation["action"], id: string) {
    if (busy || !id) return
    setPendingRevoke({ machineID, action, id, context: `${chosen?.machineID ?? "?"}/${chosen?.sessionID ?? "?"} → ${target.machineID}/${target.sessionID}` })
  }

  async function readFingerprints() {
    if (!chosen || busy) return
    setBusy(true)
    try {
      const sourceClient = clientFor(chosen.machineID, "peer-control")
      const targetClient = clientFor(target.machineID, "peer-control")
      const [sourceAnswer, targetAnswer] = await Promise.all([
        sourceClient._machineRequestAs!(crypto.randomUUID(), chosen.machineID, "peer-control", { control: { action: "status" } }, "action", 30_000),
        targetClient._machineRequestAs!(crypto.randomUUID(), target.machineID, "peer-control", { control: { action: "status" } }, "action", 30_000),
      ]) as [ControlAnswer, ControlAnswer]
      if (!sourceAnswer.local_fingerprint || !targetAnswer.local_fingerprint) throw new Error("peer_fingerprint_unknown")
      setFingerprintHints({ source: sourceAnswer.local_fingerprint, target: targetAnswer.local_fingerprint })
      setSourceCompared(false); setTargetCompared(false)
    } catch (error) { setAnswer(nextWord("cloudPeerRefused", { code: problem(error) })) }
    finally { setBusy(false) }
  }

  async function send() {
    if (busy || !chosen || !body.trim() || !grantID) return
    setBusy(true)
    try {
      await Promise.all([ensurePeerEndpointCurrent(chosen, source, current()), ensurePeerEndpointCurrent(target, source, current())])
      const client = clientFor(chosen.machineID, "peer-send")
      const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(body))
      const hex = [...new Uint8Array(digest)].map((byte) => byte.toString(16).padStart(2, "0")).join("")
      const request = crypto.randomUUID()
      setLastRequest(request)
      const header = { request_id: request, kind, source: wire(chosen), target: wire(target),
        grant_id: grantID, body_digest: hex }
      const result = await client._machineRequestAs!(request, chosen.machineID, "peer-send", { header, body }, "action", 30_000) as
        { relay_accepted?: string; machine_execution?: string; code?: string }
      if (result.code && result.code !== "ok") throw Object.assign(new Error(result.code), { code: result.code })
      setAnswer(nextWord("cloudPeerSendResult", { request, relay: result.relay_accepted ?? "unknown",
        execution: result.machine_execution ?? "unknown" }))
    } catch (error) { setAnswer(nextWord("cloudPeerRefused", { code: problem(error) })) }
    finally { setBusy(false) }
  }

  async function readInbox(before = "") {
    if (busy) return
    setBusy(true)
    try {
      const client = current()
      const commands = client?.machineDescriptor?.(target.machineID)?.machine?.commands
      const capability = (client as CloudClientHandle & { readContentCapabilities?: ReadonlyMap<string,
        { at?: number; supported?: boolean }> } | null)?.readContentCapabilities?.get(target.machineID)
      if (!client?._read || !commands?.includes("peer-inbox") || !client.viewerVerified?.has(target.machineID))
        throw new Error("peer_capability_unavailable")
      if (capability?.supported !== true || !Number.isFinite(capability.at) ||
        Math.abs(Date.now() - capability.at! * 1000) > STATUS_FRESH_MS) throw new Error("old_version")
      const request = crypto.randomUUID()
      const result = await client._read({ machine: target.machineID, session: target.sessionID }, "peer-inbox",
        { request, machine_id: target.machineID, expected_generation: target.executionGeneration,
          ...(before ? { before } : {}) }, "read:" + request) as
        { machine_id?: string; session_id?: string; expected_generation?: string; items?: InboxWireItem[]; next_before?: string }
      if (result.machine_id !== target.machineID || result.session_id !== target.sessionID ||
        result.expected_generation !== target.executionGeneration || !Array.isArray(result.items)) {
        throw new Error("peer_inbox_target_mismatch")
      }
      const items = result.items.map((item) => ({ ...item, body: decodeInboxBody(item.body_base64) }))
      setInbox(before ? (prior) => [...prior, ...items] : items)
      setInboxBefore(result.next_before ?? "")
      setAnswer(nextWord("cloudPeerInboxRead"))
    } catch (error) { setAnswer(nextWord("cloudPeerRefused", { code: problem(error) })) }
    finally { setBusy(false) }
  }

  async function checkRelay() {
	if (!chosen || !lastRequest || busy) return
	setBusy(true)
	try {
		const client = current()
		const commands = client?.machineDescriptor?.(chosen.machineID)?.machine?.commands
		if (!client?._machineRequestAs || !commands?.includes("peer-outbox")) throw new Error("peer_capability_unavailable")
		const result = await client._machineRequestAs(crypto.randomUUID(), chosen.machineID, "peer-outbox",
			{ target_request: lastRequest }, "read", 30_000) as { relay_accepted?: string; relay_delivered?: string }
		setAnswer(nextWord("cloudPeerRelayReceipt", { request: lastRequest,
			accepted: result.relay_accepted ?? "unknown", delivered: result.relay_delivered ?? "unknown" }))
	} catch (error) { setAnswer(nextWord("cloudPeerRefused", { code: problem(error) })) }
	finally { setBusy(false) }
  }

  return <section className="cloud-peer-panel" aria-label={nextWord("cloudPeerHeading")}>
    <h2>{nextWord("cloudPeerHeading")}</h2>
    <p>{nextWord("cloudPeerTarget", { machine: `${context.machine.name} (${target.machineID})`, session: target.sessionID,
      generation: target.executionGeneration })}</p>
    <label>{nextWord("cloudPeerSource")}
      <select value={selected} disabled={busy} onChange={(event) => setSelected(event.target.value)}>
        <option value="">{nextWord("cloudPeerChooseSource")}</option>
        {sources.map((entry) => <option key={JSON.stringify(entry)} value={JSON.stringify(entry)}>
          {machines.find((machine) => machine.id === entry.machineID)?.name ?? entry.machineID} ({entry.machineID}) / {entry.sessionID} / {entry.executionGeneration}
        </option>)}
      </select>
    </label>
    {chosen && !sourceCanControl && <p role="status">{nextWord("cloudPeerWriteUnavailable", { machine: chosen.machineID })}</p>}
    {!targetCanControl && <p role="status">{nextWord("cloudPeerWriteUnavailable", { machine: target.machineID })}</p>}
    <p>{nextWord("cloudPeerCompareInstruction")}</p>
    <button disabled={busy || !chosen || !sourceCanControl || !targetCanControl} onClick={() => void readFingerprints()}>{nextWord("cloudPeerReadFingerprints")}</button>
    {fingerprintHints && <dl className="cloud-peer-fingerprints">
      <div><dt>{nextWord("cloudPeerSourceFingerprint")}</dt><dd>{fingerprintHints.source}</dd></div>
      <div><dt>{nextWord("cloudPeerTargetFingerprint")}</dt><dd>{fingerprintHints.target}</dd></div>
    </dl>}
    <label>{nextWord("cloudPeerTargetFingerprint")}<input value={targetFingerprint} onChange={(event) => setTargetFingerprint(event.target.value)} /></label>
    <label>{nextWord("cloudPeerSourceFingerprint")}<input value={sourceFingerprint} onChange={(event) => setSourceFingerprint(event.target.value)} /></label>
    {fingerprintHints && <div className="cloud-peer-compare">
      <label><input type="checkbox" checked={sourceCompared} onChange={(event) => setSourceCompared(event.target.checked)} />{nextWord("cloudPeerSourceCompared")}</label>
      <label><input type="checkbox" checked={targetCompared} onChange={(event) => setTargetCompared(event.target.checked)} />{nextWord("cloudPeerTargetCompared")}</label>
    </div>}
    <label>{nextWord("cloudPeerPairID")}<input value={pairID} onChange={(event) => setPairID(event.target.value)} /></label>
    <div className="cloud-peer-buttons">
      <button disabled={busy || !chosen || !sourceCanControl || !targetCompared ||
        targetFingerprint !== fingerprintHints?.target} onClick={() => chosen && void control(chosen.machineID,
        { action: "start", target_machine_id: target.machineID, compared_fingerprint: targetFingerprint })}>{nextWord("cloudPeerStart")}</button>
      <button disabled={busy || !targetCanControl || !pairID || !sourceCompared ||
        sourceFingerprint !== fingerprintHints?.source} onClick={() => void control(target.machineID,
        { action: "accept", pair_id: pairID, compared_fingerprint: sourceFingerprint })}>{nextWord("cloudPeerAccept")}</button>
      <button disabled={busy || !chosen || !sourceCanControl || !pairID || !targetCompared ||
        targetFingerprint !== fingerprintHints?.target} onClick={() => chosen && void control(chosen.machineID,
        { action: "sync", pair_id: pairID, compared_fingerprint: targetFingerprint })}>{nextWord("cloudPeerSync")}</button>
      <button disabled={busy || !chosen || !sourceCanControl || !pairID} onClick={() => chosen && askRevoke(chosen.machineID,
        "revoke_pair", pairID)}>{nextWord("cloudPeerRevokePairSource")}</button>
      <button disabled={busy || !targetCanControl || !pairID} onClick={() => askRevoke(target.machineID,
        "revoke_pair", pairID)}>{nextWord("cloudPeerRevokePair")}</button>
    </div>
    <label>{nextWord("cloudPeerGrantID")}<input value={grantID} disabled={busy} onChange={(event) => setGrantID(event.target.value)} /></label>
    <div className="cloud-peer-scopes" role="group" aria-label={nextWord("cloudPeerScopes")}>
      <label><input type="checkbox" checked={allowMessage} onChange={(event) => setAllowMessage(event.target.checked)} />{nextWord("cloudPeerMessage")}</label>
      <label><input type="checkbox" checked={allowHandoff} onChange={(event) => setAllowHandoff(event.target.checked)} />{nextWord("cloudPeerHandoff")}</label>
    </div>
    <div className="cloud-peer-buttons">
      <button disabled={busy || !chosen || !targetCanControl || !pairID || !allowMessage && !allowHandoff ||
        sourceMachine?.freshness !== "current" || context.machine.freshness !== "current"} onClick={() => chosen && void control(target.machineID,
        { action: "grant", pair_id: pairID, source_session_id: chosen.sessionID,
          source_execution_generation: chosen.executionGeneration, target_session_id: target.sessionID,
          target_execution_generation: target.executionGeneration,
          scopes: [...(allowMessage ? ["message"] : []), ...(allowHandoff ? ["handoff"] : [])] })}>{nextWord("cloudPeerGrant")}</button>
      <button disabled={busy || !chosen || !sourceCanControl || !grantID} onClick={() => chosen && void control(chosen.machineID,
        { action: "grant_sync", grant_id: grantID })}>{nextWord("cloudPeerGrantSync")}</button>
      <button disabled={busy || !targetCanControl || !grantID} onClick={() => askRevoke(target.machineID,
        "revoke_grant", grantID)}>{nextWord("cloudPeerRevokeGrant")}</button>
    </div>
    <div className="cloud-peer-compose">
      <label>{nextWord("cloudPeerKind")}<select value={kind} disabled={busy} onChange={(event) => setKind(event.target.value as typeof kind)}>
        <option value="message">{nextWord("cloudPeerMessage")}</option><option value="handoff">{nextWord("cloudPeerHandoff")}</option>
      </select></label>
      <label>{nextWord("cloudPeerBody")}<textarea value={body} disabled={busy} onChange={(event) => setBody(event.target.value)} rows={4} /></label>
      <button disabled={busy || !chosen || !sourceCanSend || !grantID || !body.trim() || sourceMachine?.freshness !== "current" ||
        context.machine.freshness !== "current"}
        onClick={() => void send()}>{nextWord("cloudPeerSend")}</button>
    </div>
    <button disabled={busy} onClick={() => void readInbox()}>{nextWord("cloudPeerReadInbox")}</button>
    {inboxBefore && <button disabled={busy} onClick={() => void readInbox(inboxBefore)}>{nextWord("cloudPeerOlderInbox")}</button>}
    <button disabled={busy || !chosen || !lastRequest} onClick={() => void checkRelay()}>{nextWord("cloudPeerCheckRelay")}</button>
    {answer && <p role="status">{answer}</p>}
    <ul>{inbox.map((item) => <li key={item.request.request_id}>
      <strong>{item.request.kind}</strong> {item.request.source.machine_id}/{item.request.source.session_id} →
      {item.request.target.machine_id}/{item.request.target.session_id}: {item.body}
      <p>{nextWord("cloudPeerTargetReceipt", { execution: item.receipt?.machine_execution ?? "unknown",
        delivered: item.receipt?.session_delivered ?? "unknown", observed: item.receipt?.agent_observed ?? "unknown",
        acknowledged: item.receipt?.agent_acknowledged ?? "unknown" })}</p>
    </li>)}</ul>
    <p>{nextWord("cloudPeerObservationUnknown")}</p>
    {pendingRevoke && <RevokeConfirmation pending={pendingRevoke} onCancel={() => setPendingRevoke(null)}
      onConfirm={() => { const pending = pendingRevoke; setPendingRevoke(null); void control(pending.machineID,
        { action: pending.action, ...(pending.action === "revoke_pair" ? { pair_id: pending.id } : { grant_id: pending.id }) }) }} />}
  </section>
}

/** Pair and grant revocation remain reachable after a Session disappears. */
export function PeerRevocationPanel({ machines, current }: {
  machines: readonly FleetMachine[]
  current: () => CloudClientHandle | null
}) {
  const [machineID, setMachineID] = useState("")
  const [pairID, setPairID] = useState("")
  const [grantID, setGrantID] = useState("")
  const [busy, setBusy] = useState(false)
  const [answer, setAnswer] = useState("")
  const [access, setAccess] = useState<PeerAccessSnapshot | null>(null)
  const [pendingRevoke, setPendingRevoke] = useState<Revocation | null>(null)
  const machine = machines.find((entry) => entry.id === machineID)
  const viewer = current()
  const canManage = !!machineID && !!viewer?.allowWrites && !!viewer._machineRequestAs &&
    !!viewer.viewerVerified?.has(machineID) &&
    !!viewer.machineDescriptor?.(machineID)?.machine?.commands?.includes("peer-control")

  async function clientForAccess(): Promise<CloudClientHandle> {
    const client = current()
    const roster = await client?.machines().catch(() => null)
    const currentMachine = roster?.machines.find((entry) => entry.id === machineID)
    if (!client || !currentMachine || currentMachine.freshness !== "current" ||
      currentMachine.pairing !== "paired") throw new Error("peer_machine_stale")
    if (!client.allowWrites || !client._machineRequestAs || !client.viewerVerified?.has(machineID) ||
      !client.machineDescriptor?.(machineID)?.machine?.commands?.includes("peer-control")) {
      throw new Error("peer_capability_unavailable")
    }
    return client
  }

  async function readAccess() {
    if (busy || !machineID) return
    setBusy(true)
    setAccess(null)
    try {
      const client = await clientForAccess()
      const response = await client._machineRequestAs!(crypto.randomUUID(), machineID, "peer-control",
        { control: { action: "status" } }, "action", 30_000)
      setAccess(checkedPeerAccessStatus(response, machineID))
      setAnswer("")
    } catch (error) { setAnswer(nextWord("cloudPeerRefused", { code: problem(error) })) }
    finally { setBusy(false) }
  }

  async function revoke(action: "revoke_pair" | "revoke_grant", id: string) {
    if (busy || !machineID || !id) return
    setBusy(true)
    setAccess(null)
    try {
      const client = await clientForAccess()
      const response = await client._machineRequestAs!(crypto.randomUUID(), machineID, "peer-control",
        { control: { action, ...(action === "revoke_pair" ? { pair_id: id } : { grant_id: id }) } },
        "action", 30_000) as ControlAnswer
      if (response.state !== "revoked") throw new Error("peer_revocation_unconfirmed")
      setAnswer(nextWord("cloudPeerControlResult", { state: response.state, id }))
      if (action === "revoke_pair") setPairID("")
      else setGrantID("")
    } catch (error) { setAnswer(nextWord("cloudPeerRefused", { code: problem(error) })) }
    finally { setBusy(false) }
  }

  return <details className="cloud-peer-panel cloud-peer-revocation">
    <summary>{nextWord("cloudPeerManageAccess")}</summary>
    <label>{nextWord("cloudPeerAccessMachine")}
      <select value={machineID} disabled={busy} onChange={(event) => {
        setMachineID(event.target.value); setPairID(""); setGrantID(""); setAccess(null); setAnswer("")
      }}>
        <option value="">{nextWord("cloudPeerChooseMachine")}</option>
        {machines.map((entry) => <option key={entry.id} value={entry.id}>{entry.name || entry.id} · {entry.id}</option>)}
      </select>
    </label>
    {machineID && !canManage && <p role="status">{nextWord("cloudPeerWriteUnavailable", { machine: machineID })}</p>}
    <button type="button" disabled={busy || !canManage || machine?.freshness !== "current"}
      onClick={() => void readAccess()}>{nextWord("cloudPeerAccessRefresh")}</button>
    {access && <div className="cloud-peer-access-list">
      <p>{nextWord("cloudPeerAccessObservedAt", { time: new Date(access.observedAt).toLocaleString() })}</p>
      {access.pairs.length === 0 && access.grants.length === 0 && <p>{nextWord("cloudPeerAccessEmpty")}</p>}
      {access.pairs.length > 0 && <section aria-label={nextWord("cloudPeerAccessPairs")}>
        <h3>{nextWord("cloudPeerAccessPairs")}</h3>
        <ul>{access.pairs.map((pair) => <li key={pair.pair_id}>
          <strong>{pair.pair_id}</strong>
          <span>{pair.source_machine_id} → {pair.target_machine_id} · {pair.state} · {new Date(pair.expires_at).toLocaleString()}</span>
          <button type="button" disabled={busy || !canManage || machine?.freshness !== "current"}
            onClick={() => setPendingRevoke({ machineID, action: "revoke_pair", id: pair.pair_id,
              context: `${pair.source_machine_id} → ${pair.target_machine_id}` })}>{nextWord("cloudPeerRevokePair")}</button>
        </li>)}</ul>
      </section>}
      {access.grants.length > 0 && <section aria-label={nextWord("cloudPeerAccessGrants")}>
        <h3>{nextWord("cloudPeerAccessGrants")}</h3>
        <ul>{access.grants.map((grant) => <li key={grant.grant_id}>
          <strong>{grant.grant_id}</strong>
          <span>{grant.source.machine_id}/{grant.source.session_id}/{grant.source.execution_generation} →
            {grant.target.machine_id}/{grant.target.session_id}/{grant.target.execution_generation} ·
            {grant.scopes.join(", ")} · {new Date(grant.expires_at).toLocaleString()}</span>
          <button type="button" disabled={busy || !canManage || machine?.freshness !== "current"}
            onClick={() => setPendingRevoke({ machineID, action: "revoke_grant", id: grant.grant_id,
              context: `${grant.source.machine_id}/${grant.source.session_id} → ${grant.target.machine_id}/${grant.target.session_id} · ${grant.scopes.join(", ")}` })}>{nextWord("cloudPeerRevokeGrant")}</button>
        </li>)}</ul>
      </section>}
    </div>}
    <label>{nextWord("cloudPeerPairID")}<input value={pairID} disabled={busy} onChange={(event) => setPairID(event.target.value)} /></label>
    <button type="button" disabled={busy || !canManage || machine?.freshness !== "current" || !pairID}
      onClick={() => setPendingRevoke({ machineID, action: "revoke_pair", id: pairID,
        context: nextWord("cloudPeerAccessMachine") })}>{nextWord("cloudPeerRevokePair")}</button>
    <label>{nextWord("cloudPeerGrantID")}<input value={grantID} disabled={busy} onChange={(event) => setGrantID(event.target.value)} /></label>
    <button type="button" disabled={busy || !canManage || machine?.freshness !== "current" || !grantID}
      onClick={() => setPendingRevoke({ machineID, action: "revoke_grant", id: grantID,
        context: nextWord("cloudPeerAccessMachine") })}>{nextWord("cloudPeerRevokeGrant")}</button>
    {answer && <p role="status">{answer}</p>}
    {pendingRevoke && <RevokeConfirmation pending={pendingRevoke} onCancel={() => setPendingRevoke(null)}
      onConfirm={() => { const pending = pendingRevoke; setPendingRevoke(null); void revoke(pending.action, pending.id) }} />}
  </details>
}
