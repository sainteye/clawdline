import { useEffect, useState } from "react"
import { nextWord } from "../next-strings.js"
import { failureSentence } from "../refusals/refusal-text.js"
import type { CloudClientHandle } from "./copied.js"
import type { FleetMachine, SessionProjectionSource } from "./all-machine-sessions.js"
import { checkedPeerAccessStatus, type PeerAccessSnapshot } from "./peer-handoff-admission.js"
import { STATUS_FRESH_MS } from "./status-projection.js"
import "./peer-access-page.css"

type Reading = { kind: "ready"; snapshot: PeerAccessSnapshot; fingerprint: string } |
  { kind: "unavailable"; reason: "offline" | "unpaired" | "view_only" | "unreadable" }
type Answer = { pair_id?: string; state?: string; local_fingerprint?: string }
const usable = (name: string | undefined, id: string) => !!name?.trim() && name.trim() !== id &&
  !/^[0-9a-f]{8}-[0-9a-f-]{20,}$/iu.test(name.trim())

/** One switch controls both directed, signed machine pairs. No Session grant is created. */
export function PeerAccessPage({ machines, current, active = true }: {
  machines: readonly FleetMachine[]
  source: SessionProjectionSource | null
  current: () => CloudClientHandle | null
  active?: boolean
}) {
  const [readings, setReadings] = useState<Record<string, Reading>>({})
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState("")
  const [message, setMessage] = useState("")
  const [revision, setRevision] = useState(0)
  const [now, setNow] = useState(Date.now())
  const machineKey = machines.map((machine) =>
    [machine.id, machine.name, machine.freshness, machine.paired].join(":")).join("\0")

  function clientFor(machineID: string): CloudClientHandle {
    const client = current()
    if (!client?.allowWrites || !client._machineRequestAs || !client.viewerVerified?.has(machineID) ||
      !client.machineDescriptor?.(machineID)?.machine?.commands?.includes("peer-control"))
      throw new Error("peer_capability_unavailable")
    return client
  }
  async function control(machineID: string, input: Record<string, string>): Promise<Answer> {
    const client = clientFor(machineID)
    return await client._machineRequestAs!(crypto.randomUUID(), machineID, "peer-control",
      { control: input }, "action", 30_000) as Answer
  }
  async function read(machine: FleetMachine): Promise<Reading> {
    if (machine.freshness !== "current") return { kind: "unavailable", reason: "offline" }
    if (machine.paired === false) return { kind: "unavailable", reason: "unpaired" }
    try { clientFor(machine.id) } catch { return { kind: "unavailable", reason: "view_only" } }
    try {
      const answer = await control(machine.id, { action: "status" })
      const snapshot = checkedPeerAccessStatus(answer, machine.id)
      if (!answer.local_fingerprint) throw new Error("peer_fingerprint_unknown")
      return { kind: "ready", snapshot, fingerprint: answer.local_fingerprint }
    } catch { return { kind: "unavailable", reason: "unreadable" } }
  }
  useEffect(() => {
    if (!active) return
    let cancelled = false
    setLoading(true)
    void Promise.all(machines.map(async (machine) => [machine.id, await read(machine)] as const))
      .then((rows) => { if (!cancelled) { setReadings(Object.fromEntries(rows)); setNow(Date.now()); setLoading(false) } })
    return () => { cancelled = true }
  }, [active, machineKey, revision])
  useEffect(() => {
    const timeout = window.setTimeout(() => setNow(Date.now()), STATUS_FRESH_MS + 100)
    return () => window.clearTimeout(timeout)
  }, [readings])

  const name = (machine: FleetMachine) => usable(machine.name, machine.id)
    ? machine.name : nextWord("cloudPeerMachineUnnamed", { number: String(machines.indexOf(machine) + 1) })
  const pairs = machines.flatMap((first, index) => machines.slice(index + 1).map((second) => ({ first, second })))
  const activePairs = (from: string, to: string) => {
    const reading = readings[from]
    if (reading?.kind !== "ready") return []
    return reading.snapshot.pairs.filter((pair) => pair.source_machine_id === from &&
      pair.target_machine_id === to && pair.state === "active" && now < Date.parse(pair.expires_at))
  }
  const records = (machineID: string, otherID: string) => {
    const reading = readings[machineID]
    return reading?.kind === "ready" ? reading.snapshot.pairs.filter((pair) =>
      pair.source_machine_id === otherID && pair.target_machine_id === machineID ||
      pair.source_machine_id === machineID && pair.target_machine_id === otherID) : []
  }
  const ready = (id: string) => {
    const reading = readings[id]
    return reading?.kind === "ready" && now >= reading.snapshot.observedAt &&
      now - reading.snapshot.observedAt < STATUS_FRESH_MS
  }
  const directionActive = (from: string, to: string) => activePairs(from, to).some((pair) => {
    const source = readings[from], target = readings[to]
    if (source?.kind !== "ready" || target?.kind !== "ready" ||
      pair.source_fingerprint !== source.fingerprint || pair.target_fingerprint !== target.fingerprint) return false
    return records(to, from).some((record) => record.pair_id === pair.pair_id &&
      record.state === "active" && record.expires_at === pair.expires_at &&
      record.source_fingerprint === pair.source_fingerprint &&
      record.target_fingerprint === pair.target_fingerprint && now < Date.parse(record.expires_at))
  })
  const enabled = (a: string, b: string) => ready(a) && ready(b) &&
    directionActive(a, b) && directionActive(b, a)
  const unavailableReason = (machine: FleetMachine) => {
    const reading = readings[machine.id]
    const reason = reading?.kind === "unavailable" ? reading.reason : "stale"
    const key = {
      offline: "cloudPeerMachineOffline", unpaired: "cloudPeerMachineUnpaired",
      view_only: "cloudPeerMachineViewOnly", unreadable: "cloudPeerMachineUnreadable",
      stale: "cloudPeerMachineStale",
    } as const
    return nextWord(key[reason], { machine: name(machine) })
  }

  async function openDirection(from: string, to: string) {
    const first = readings[from], second = readings[to]
    if (first?.kind !== "ready" || second?.kind !== "ready") throw new Error("peer_status_unavailable")
    const started = await control(from, { action: "start", target_machine_id: to,
      compared_fingerprint: second.fingerprint })
    if (!started.pair_id || started.state !== "waiting_for_target") throw new Error("peer_pair_start_unconfirmed")
    const accepted = await control(to, { action: "accept", pair_id: started.pair_id,
      compared_fingerprint: first.fingerprint })
    if (accepted.pair_id !== started.pair_id || accepted.state !== "active_target")
      throw new Error("peer_pair_accept_unconfirmed")
    const synced = await control(from, { action: "sync", pair_id: started.pair_id,
      compared_fingerprint: second.fingerprint })
    if (synced.pair_id !== started.pair_id || synced.state !== "active_source")
      throw new Error("peer_pair_sync_unconfirmed")
  }
  async function change(first: FleetMachine, second: FleetMachine, turnOn: boolean) {
    if (busy || !ready(first.id) || !ready(second.id)) return
    setBusy(`${first.id}:${second.id}`); setMessage("")
    try {
      const [a, b] = [first.id, second.id]
      if (turnOn) {
        if (!directionActive(a, b)) await openDirection(a, b)
        if (!directionActive(b, a)) await openDirection(b, a)
      } else {
        // Revoke Cloud first, then each local pin. Repeating a revoke is safe.
        const seen = new Set<string>()
        for (const holder of [a, b]) for (const pair of records(holder, holder === a ? b : a)) {
          const key = `${holder}:${pair.pair_id}`
          if (seen.has(key)) continue
          seen.add(key)
          const answer = await control(holder, { action: "revoke_pair", pair_id: pair.pair_id })
          if (answer.state !== "revoked") throw new Error("peer_revocation_unconfirmed")
        }
      }
    } catch (error) {
      setMessage(failureSentence(error, nextWord("cloudPeerMachineChangeFailed")))
    } finally { setBusy(""); setRevision((value) => value + 1) }
  }
  return <div className="cloud-peer-access-sheet">
    <header className="cloud-peer-access-header"><h1 id="peer-access-title">{nextWord("cloudPeerAccessNav")}</h1>
      <p>{nextWord("cloudPeerAccessPurpose")}</p></header>
    <div className="cloud-peer-access-section-head"><div><h2>{nextWord("cloudPeerMachineList")}</h2>
      <p>{nextWord("cloudPeerMachineHint")}</p></div>
      <button className="peer-secondary" type="button" disabled={loading || !!busy}
        onClick={() => setRevision((value) => value + 1)}>{nextWord("cloudPeerAccessRefresh")}</button></div>
    {loading && <p role="status">{nextWord("cloudPeerAccessLoading")}</p>}
    {!loading && pairs.length === 0 && <p className="cloud-peer-access-empty">{nextWord("cloudPeerMachineNeedsTwo")}</p>}
    {!loading && <div className="cloud-peer-machine-list">{pairs.map(({ first, second }) => {
      const key = `${first.id}:${second.id}`
      const available = ready(first.id) && ready(second.id)
      const on = available && enabled(first.id, second.id)
      const partial = available && !on && (records(first.id, second.id).length > 0 || records(second.id, first.id).length > 0)
      return <article className="cloud-peer-machine-row" key={key}>
        <div className="cloud-peer-machine-names"><h3>{name(first)} <span aria-hidden="true">↔</span> {name(second)}</h3>
          <p>{!available ? nextWord("cloudPeerMachineUnknown") : partial
            ? nextWord("cloudPeerMachinePartial") : on ? nextWord("cloudPeerMachineOn") : nextWord("cloudPeerMachineOff")}</p>
          {!available && <p className="cloud-peer-machine-reasons">{[first, second].filter((machine) => !ready(machine.id))
            .map((machine) => <span key={machine.id}>{unavailableReason(machine)}</span>)}</p>}</div>
        {partial && <button className="peer-secondary" type="button" disabled={!!busy}
          onClick={() => void change(first, second, false)}>{nextWord("cloudPeerMachineClear")}</button>}
        {available ? <button role="switch" type="button" aria-checked={!!on} aria-label={nextWord("cloudPeerMachineSwitch", {
          first: name(first), second: name(second) })} disabled={!!busy}
          className="cloud-peer-machine-switch" onClick={() => void change(first, second, !on)}>
          <span aria-hidden="true"><svg viewBox="0 0 20 20" fill="none" focusable="false">
            <path d="m4.5 10 3.7 3.7 7.3-7.4" stroke="currentColor" strokeWidth="2.4"
              strokeLinecap="round" strokeLinejoin="round" />
          </svg></span></button> : <span className="cloud-peer-machine-unknown" aria-hidden="true">—</span>}
      </article>
    })}</div>}
    {message && <p className="cloud-peer-access-message" role="alert">{message}</p>}
  </div>
}
