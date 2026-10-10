import { useEffect, useState } from "react"
import { nextWord } from "../next-strings.js"
import type { CloudClientHandle } from "./copied.js"
import { PeerHandoffPanel, PeerRevocationPanel } from "./PeerHandoffPanel.js"
import type { FleetMachine, MachineSessionProjection, SessionProjectionSource } from "./all-machine-sessions.js"
import "./peer-access-page.css"

/** Machine authority stays on each machine; this page only reads or requests changes. */
export function PeerAccessPage({ machines, source, current }: {
  machines: readonly FleetMachine[]
  source: SessionProjectionSource | null
  current: () => CloudClientHandle | null
}) {
  const [machineID, setMachineID] = useState("")
  const [sessionID, setSessionID] = useState("")
  const [projection, setProjection] = useState<MachineSessionProjection | null>(null)
  const machine = machines.find((entry) => entry.id === machineID)

  useEffect(() => {
    setProjection(null)
    setSessionID("")
    if (!machine || machine.freshness !== "current" || machine.paired === false) return
    if (!source) { setProjection({ kind: "unavailable", reason: "old_version" }); return }
    const abort = new AbortController()
    void source.readMachine(machine.id, abort.signal).then((answer) => {
      if (!abort.signal.aborted) setProjection(answer)
    }).catch(() => {
      if (!abort.signal.aborted) setProjection({ kind: "unavailable", reason: "unknown" })
    })
    return () => abort.abort()
  }, [machineID, machine?.freshness, machine?.paired, source])

  const rows = projection?.kind === "ready" ? projection.rows.filter((row) => row.freshness === "current") : []
  const selected = rows.find((row) => row.destination.sessionID === sessionID)

  return <div className="sheet cloud-peer-access-sheet">
    <h2 id="peer-access-title">{nextWord("cloudPeerAccessNav")}</h2>
    <p>{nextWord("cloudPeerAccessIntro")}</p>
    <section className="cloud-peer-access-create" aria-label={nextWord("cloudPeerAccessTargetMachine")}>
      <label>{nextWord("cloudPeerAccessTargetMachine")}
        <select value={machineID} onChange={(event) => setMachineID(event.target.value)}>
          <option value="">{nextWord("cloudPeerChooseMachine")}</option>
          {machines.map((entry) => <option key={entry.id} value={entry.id}>{entry.name || entry.id} · {entry.id}</option>)}
        </select>
      </label>
      {machine && machine.freshness !== "current" && <p role="status">{nextWord("cloudPeerAccessOffline")}</p>}
      {machine?.paired === false && <p role="status">{nextWord("cloudPeerAccessUnpaired")}</p>}
      {machine?.freshness === "current" && projection?.kind === "unavailable" &&
        <p role="status">{nextWord("cloudPeerAccessUnread", { code: projection.reason })}</p>}
      {machine?.freshness === "current" && machine.paired !== false && !projection && <p role="status">{nextWord("cloudPeerAccessLoading")}</p>}
      {machineID && <PeerRevocationPanel machines={machines} current={current} selectedMachineID={machineID} />}
    </section>
    <section className="cloud-peer-access-create" aria-labelledby="peer-access-create-title">
      <h3 id="peer-access-create-title">{nextWord("cloudPeerAccessCreate")}</h3>
      {projection?.kind === "ready" && <label>{nextWord("cloudPeerAccessTargetSession")}
        <select value={sessionID} onChange={(event) => setSessionID(event.target.value)}>
          <option value="">{nextWord("cloudPeerAccessChooseSession")}</option>
          {rows.map((row) => <option key={row.destination.sessionID + row.destination.executionGeneration}
            value={row.destination.sessionID}>{row.title || row.destination.sessionID} · {row.destination.sessionID}</option>)}
        </select>
      </label>}
      {selected && machine && projection?.kind === "ready" &&
        <PeerHandoffPanel key={selected.destination.machineID + selected.destination.sessionID + selected.destination.executionGeneration}
          context={{ destination: selected.destination, machine, row: selected, projection, content: null }}
          machines={machines} source={source} current={current} accessOnly />}
    </section>
  </div>
}
