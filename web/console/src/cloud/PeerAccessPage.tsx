import { useEffect, useRef, useState } from "react"
import { nextWord } from "../next-strings.js"
import { failureSentence } from "../refusals/refusal-text.js"
import type { CloudClientHandle } from "./copied.js"
import { destinationKey, type FleetMachine, type ProjectedSession, type SessionDestination,
  type SessionProjectionSource } from "./all-machine-sessions.js"
import { checkedPeerAccessStatus, effectivePeerScopes, type PeerAccessSnapshot } from "./peer-handoff-admission.js"
import { STATUS_FRESH_MS } from "./status-projection.js"
import { PeerAccessWizard } from "./PeerAccessWizard.js"
import "./peer-access-page.css"

type Reading = { kind: "ready"; snapshot: PeerAccessSnapshot } |
  { kind: "unavailable"; reason: "offline" | "unpaired" | "view_only" | "unreadable" }
type Revoke = { machineID: string; action: "revoke_grant" | "revoke_pair"; id: string; label: string }
const short = (id: string) => id.slice(0, 8)
const dateTime = (value: string) => new Date(value).toLocaleString(
  typeof document === "undefined" ? undefined : document.documentElement.lang)
const usable = (name: string | undefined, id: string) => !!name?.trim() && name.trim() !== id &&
  !/^[0-9a-f]{8}-[0-9a-f-]{20,}$/iu.test(name.trim())

function Confirmation({ revoke, busy, cancel, confirm }: {
  revoke: Revoke; busy: boolean; cancel: () => void; confirm: () => void
}) {
  const dialog = useRef<HTMLDialogElement>(null)
  const first = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null
    dialog.current?.showModal()
    first.current?.focus()
    return () => { dialog.current?.close(); previous?.focus() }
  }, [])
  return <dialog ref={dialog} className="cloud-peer-confirm" onCancel={(event) => {
    event.preventDefault(); if (!busy) cancel()
  }}>
    <h2>{nextWord("cloudPeerRevokeConfirmTitle")}</h2>
    <p>{nextWord("cloudPeerAccessRevokeQuestion", { name: revoke.label })}</p>
    <div className="cloud-peer-access-actions">
      <button ref={first} type="button" disabled={busy} onClick={cancel}>{nextWord("cloudPeerRevokeCancel")}</button>
      <button className="peer-danger" type="button" disabled={busy} onClick={confirm}>{nextWord("cloudPeerRevokeConfirm")}</button>
    </div>
  </dialog>
}

/** Reads each machine's authority; proposed scopes are never shown as effective here. */
export function PeerAccessPage({ machines, source, current, active = true }: {
  machines: readonly FleetMachine[]
  source: SessionProjectionSource | null
  current: () => CloudClientHandle | null
  active?: boolean
}) {
  const [readings, setReadings] = useState<Record<string, Reading>>({})
  const [sessions, setSessions] = useState<Record<string, ProjectedSession[]>>({})
  const [titles, setTitles] = useState<Record<string, string>>({})
  const [loading, setLoading] = useState(true)
  const [revision, setRevision] = useState(0)
  const [creating, setCreating] = useState(false)
  const [pending, setPending] = useState<Revoke | null>(null)
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState("")
  const [now, setNow] = useState(Date.now())
  const machineKey = machines.map((machine) =>
    [machine.id, machine.name, machine.freshness, machine.paired].join(":")).join("\0")

  useEffect(() => {
    if (!active) return
    const abort = new AbortController()
    setReadings({}); setSessions({}); setTitles({}); setLoading(true)
    void (async () => {
      const client = current()
      for (const machine of machines) {
        if (abort.signal.aborted) return
        let reading: Reading
        if (machine.freshness !== "current") reading = { kind: "unavailable", reason: "offline" }
        else if (machine.paired === false) reading = { kind: "unavailable", reason: "unpaired" }
        else if (!client?.allowWrites || !client._machineRequestAs ||
          !client.viewerVerified?.has(machine.id) ||
          !client.machineDescriptor?.(machine.id)?.machine?.commands?.includes("peer-control")) {
          reading = { kind: "unavailable", reason: "view_only" }
        } else {
          try {
            const value = await client._machineRequestAs(crypto.randomUUID(), machine.id,
              "peer-control", { control: { action: "status" } }, "action", 30_000)
            reading = { kind: "ready", snapshot: checkedPeerAccessStatus(value, machine.id) }
          } catch { reading = { kind: "unavailable", reason: "unreadable" } }
        }
        if (!abort.signal.aborted) {
          setReadings((before) => ({ ...before, [machine.id]: reading }))
          setNow(Date.now())
        }
        if (machine.freshness !== "current" || machine.paired === false || !source) continue
        try {
          const projection = await source.readMachine(machine.id, abort.signal)
          if (abort.signal.aborted || projection.kind !== "ready") continue
          setSessions((before) => ({ ...before, [machine.id]: projection.rows.filter((row) => row.freshness === "current") }))
          if (source.readMachinePresentations) {
            const names = await source.readMachinePresentations(machine.id, abort.signal)
            if (!abort.signal.aborted && names) setTitles((before) => ({ ...before,
              ...Object.fromEntries(names.map((entry) => [destinationKey(entry.destination), entry.title])) }))
          }
        } catch { /* Name reads may fail without changing the authority result. */ }
      }
      if (!abort.signal.aborted) setLoading(false)
    })()
    return () => abort.abort()
  }, [active, machineKey, source, revision])

  useEffect(() => {
    const deadlines = Object.values(readings).flatMap((reading) => reading.kind === "ready"
      ? [reading.snapshot.observedAt + STATUS_FRESH_MS,
        ...reading.snapshot.pairs.map((pair) => Date.parse(pair.expires_at)),
        ...reading.snapshot.grants.map((grant) => Date.parse(grant.expires_at))] : [])
    const next = Math.min(...deadlines.filter((time) => time > now))
    if (!Number.isFinite(next)) return
    const timer = window.setTimeout(() => setNow(Date.now()), Math.max(1, next - Date.now() + 1))
    return () => window.clearTimeout(timer)
  }, [readings, now])

  const machineName = (id: string) => {
    const name = machines.find((machine) => machine.id === id)?.name
    return usable(name, id) ? name! : nextWord("cloudPeerAccessUnnamedMachine", { id: short(id) })
  }
  const sessionName = (destination: SessionDestination) => {
    const row = sessions[destination.machineID]?.find((entry) =>
      destinationKey(entry.destination) === destinationKey(destination))
    const name = titles[destinationKey(destination)] || row?.title
    if (!usable(name, destination.sessionID))
      return nextWord("cloudPeerAccessUnnamedSession", { id: short(destination.sessionID) })
    const sameName = (sessions[destination.machineID] ?? []).filter((entry) => {
      const other = titles[destinationKey(entry.destination)] || entry.title
      return other === name
    })
    if (sameName.length <= 1) return name!
    const sameID = sameName.filter((entry) => entry.destination.sessionID === destination.sessionID)
    return `${name} · ${short(destination.sessionID)}${sameID.length > 1 ? ` / ${short(destination.executionGeneration)}` : ""}`
  }
  const nameWire = (entry: { machine_id: string; session_id: string; execution_generation: string }) =>
    sessionName({ machineID: entry.machine_id, sessionID: entry.session_id, executionGeneration: entry.execution_generation })
  const grants = Object.values(readings).flatMap((reading) => reading.kind === "ready"
    ? reading.snapshot.grants.map((grant) => ({ grant, snapshot: reading.snapshot })) : [])
  grants.sort((a, b) => Number(effectivePeerScopes(b.snapshot, b.grant, now).length > 0) -
    Number(effectivePeerScopes(a.snapshot, a.grant, now).length > 0) ||
    Date.parse(a.grant.expires_at) - Date.parse(b.grant.expires_at))
  const pairs = new Map<string, { pair: PeerAccessSnapshot["pairs"][number]; holders: string[]; observedAt: number }>()
  for (const reading of Object.values(readings)) {
    if (reading.kind !== "ready") continue
    for (const pair of reading.snapshot.pairs) {
      const prior = pairs.get(pair.pair_id)
      if (prior) prior.holders.push(reading.snapshot.machineID)
      else pairs.set(pair.pair_id, { pair, holders: [reading.snapshot.machineID], observedAt: reading.snapshot.observedAt })
    }
  }
  const readyCount = Object.values(readings).filter((reading) => reading.kind === "ready").length
  const canCreate = readyCount >= 2
  async function revoke(entry: Revoke) {
    if (busy) return
    setBusy(true); setMessage("")
    try {
      const client = current()
      if (!client?.allowWrites || !client._machineRequestAs || !client.viewerVerified?.has(entry.machineID) ||
        !client.machineDescriptor?.(entry.machineID)?.machine?.commands?.includes("peer-control"))
        throw new Error("peer_capability_unavailable")
      const answer = await client._machineRequestAs(crypto.randomUUID(), entry.machineID, "peer-control",
        { control: { action: entry.action, ...(entry.action === "revoke_pair"
          ? { pair_id: entry.id } : { grant_id: entry.id }) } }, "action", 30_000) as { state?: string }
      if (answer.state !== "revoked") throw new Error("peer_revocation_unconfirmed")
      setMessage(nextWord("cloudPeerAccessRevokeDone"))
    } catch (error) { setMessage(failureSentence(error, nextWord("cloudPeerAccessRevokeUnconfirmed"))) }
    finally { setBusy(false); setPending(null); setRevision((before) => before + 1) }
  }

  return <div className="cloud-peer-access-sheet">
    <header className="cloud-peer-access-header">
      <h1 id="peer-access-title">{nextWord("cloudPeerAccessNav")}</h1>
      <p>{nextWord("cloudPeerAccessPurpose")}</p>
    </header>
    <section className="cloud-peer-access-section" aria-labelledby="peer-current-title">
      <div className="cloud-peer-access-section-head">
        <div><h2 id="peer-current-title">{nextWord("cloudPeerAccessCurrent")}</h2>
          <p>{loading ? nextWord("cloudPeerAccessLoading") :
            nextWord("cloudPeerAccessReadFrom", { count: String(readyCount), total: String(machines.length) })}</p></div>
        <button className="peer-secondary" type="button" disabled={loading || busy}
          onClick={() => setRevision((before) => before + 1)}>{nextWord("cloudPeerAccessRefresh")}</button>
      </div>
      {machines.some((machine) => readings[machine.id]?.kind === "unavailable") &&
        <div className="cloud-peer-access-notices" role="status">{machines.map((machine) => {
          const reading = readings[machine.id]
          return reading?.kind === "unavailable" && <p key={machine.id}><strong>{machineName(machine.id)}</strong>
            {" · "}{nextWord(reading.reason === "offline" ? "cloudPeerAccessOffline" :
              reading.reason === "unpaired" ? "cloudPeerAccessUnpaired" :
                reading.reason === "view_only" ? "cloudPeerAccessViewOnly" : "cloudPeerAccessUnreadSimple")}</p>
        })}</div>}
      {!loading && grants.length === 0 && <div className="cloud-peer-access-empty">
        <h3>{nextWord(readyCount === machines.length && readyCount > 0
          ? "cloudPeerAccessNone" : "cloudPeerAccessPartialNone", { count: String(readyCount) })}</h3>
        <p>{readyCount === 0 ? nextWord("cloudPeerAccessNoReadableMachine") :
          readyCount === machines.length ? nextWord("cloudPeerAccessNoneWhat") :
            nextWord("cloudPeerAccessPartialWhat")}</p>
        {readyCount > 0 && !canCreate && <p>{nextWord("cloudPeerAccessNeedsTwo")}</p>}
        {canCreate && <button className="peer-primary" type="button"
          onClick={() => setCreating(true)}>{nextWord("cloudPeerAccessCreateFirst")}</button>}
      </div>}
      <div className="cloud-peer-grants">{grants.map(({ grant, snapshot }) => {
        const effective = effectivePeerScopes(snapshot, grant, now)
        const pair = snapshot.pairs.find((entry) => entry.pair_id === grant.pair_id)
        const state = now - snapshot.observedAt > STATUS_FRESH_MS ? "stale" :
          effective.length ? "active" : pair?.state === "waiting_for_target" ? "waiting" :
            now >= Date.parse(grant.expires_at) || pair && now >= Date.parse(pair.expires_at)
              ? "expired" : "inactive"
        const label = `${nameWire(grant.source)} → ${nameWire(grant.target)}`
        return <article className="cloud-peer-grant" key={snapshot.machineID + grant.grant_id}>
          <div className="cloud-peer-grant-route">
            <div><h3>{nameWire(grant.source)}</h3><p>{machineName(grant.source.machine_id)}</p></div>
            <span className="cloud-peer-route-arrow" aria-hidden="true">→</span>
            <div><h3>{nameWire(grant.target)}</h3><p>{machineName(grant.target.machine_id)}</p></div>
          </div>
          <p className="cloud-peer-grant-status" data-state={state}>
            {nextWord(state === "active" ? "cloudPeerAccessInEffect" : state === "waiting" ?
              "cloudPeerAccessWaitingTarget" : state === "expired" ? "cloudPeerAccessExpired" :
                state === "stale" ? "cloudPeerAccessStaleRecord" : "cloudPeerAccessPairGone")}
            {effective.length > 0 && <span className="cloud-peer-scope-tags">{effective.map((scope) =>
              <span key={scope}>{nextWord(scope === "message" ? "cloudPeerMessage" : "cloudPeerHandoff")}</span>)}</span>}
            {" · "}{nextWord("cloudPeerAccessExpiresAt", { time: dateTime(grant.expires_at) })}
          </p>
          <div className="cloud-peer-grant-footer">
            <details><summary>{nextWord("cloudPeerAccessTechnical")}</summary>
              <dl><dt>{nextWord("cloudPeerGrantID")}</dt><dd>{grant.grant_id}</dd>
                <dt>{nextWord("cloudPeerPairID")}</dt><dd>{grant.pair_id}</dd>
                <dt>{nextWord("cloudPeerSource")}</dt><dd>{grant.source.machine_id} / {grant.source.session_id} / {grant.source.execution_generation}</dd>
                <dt>{nextWord("cloudPeerAccessTargetSession")}</dt><dd>{grant.target.machine_id} / {grant.target.session_id} / {grant.target.execution_generation}</dd></dl>
            </details>
            <button className="peer-danger" type="button" disabled={busy || loading}
              onClick={() => setPending({ machineID: snapshot.machineID, action: "revoke_grant",
                id: grant.grant_id, label })}>{nextWord("cloudPeerAccessRevokeOne")}</button>
          </div>
        </article>
      })}</div>
      {pairs.size > 0 && <details className="cloud-peer-pair-summary">
        <summary>{nextWord("cloudPeerAccessPairs")} ({pairs.size})</summary>
        <ul>{[...pairs.values()].map(({ pair, holders, observedAt }) => <li key={pair.pair_id}>
          <span>{machineName(pair.source_machine_id)} → {machineName(pair.target_machine_id)}</span>
          <span>{now >= observedAt && now - observedAt <= STATUS_FRESH_MS &&
            now < Date.parse(pair.expires_at) && pair.state === "active"
            ? nextWord("cloudPeerAccessActive") : nextWord("cloudPeerAccessInactive")}</span>
          {holders.map((id) => <button key={id} className="peer-danger" type="button" disabled={busy || loading}
            onClick={() => setPending({ machineID: id, action: "revoke_pair", id: pair.pair_id,
              label: `${machineName(pair.source_machine_id)} → ${machineName(pair.target_machine_id)} · ${machineName(id)}` })}>
            {nextWord("cloudPeerAccessRevokePairHere", { machine: machineName(id) })}</button>)}
        </li>)}</ul>
      </details>}
    </section>
    <section className="cloud-peer-access-section" aria-labelledby="peer-create-title">
      <div className="cloud-peer-access-section-head"><div>
        <h2 id="peer-create-title">{nextWord("cloudPeerAccessCreate")}</h2>
        <p>{nextWord("cloudPeerAccessCreateWhat")}</p>
      </div>{!canCreate && <p>{nextWord("cloudPeerAccessNeedsTwo")}</p>}
        {grants.length > 0 && !creating && <button className="peer-primary" type="button" disabled={!canCreate}
        onClick={() => setCreating(true)}>{nextWord("cloudPeerAccessCreateOpen")}</button>}</div>
      {creating && <PeerAccessWizard machines={machines} sessions={sessions} source={source} current={current}
        machineName={machineName} sessionName={(row) => sessionName(row.destination)}
        onCancel={() => setCreating(false)}
        onCreated={() => { setCreating(false); setRevision((before) => before + 1) }} />}
    </section>
    {message && <p className="cloud-peer-access-message" role="status">{message}</p>}
    {pending && <Confirmation revoke={pending} busy={busy} cancel={() => setPending(null)}
      confirm={() => void revoke(pending)} />}
  </div>
}
