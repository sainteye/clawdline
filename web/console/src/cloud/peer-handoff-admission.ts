import type { CloudClientHandle } from "./copied.js"
import { destinationKey, type SessionDestination, type SessionProjectionSource } from "./all-machine-sessions.js"

/** Recheck both the live machine roster and the exact status row before a peer write. */
export async function ensurePeerEndpointCurrent(destination: SessionDestination,
  source: SessionProjectionSource | null, client: Pick<CloudClientHandle, "machines"> | null): Promise<void> {
  if (!source || !client) throw new Error("peer_source_unavailable")
  const machines = await client.machines().catch(() => null)
  const machine = machines?.machines.find((entry) => entry.id === destination.machineID)
  if (!machine || machine.freshness !== "current" || machine.pairing !== "paired") {
    throw new Error("peer_machine_stale")
  }
  const reading = await source.readMachine(destination.machineID, new AbortController().signal)
  if (reading.kind !== "ready" || !reading.rows.some((row) => row.freshness === "current" &&
    destinationKey(row.destination) === destinationKey(destination))) {
    throw new Error("execution_generation_changed")
  }
}
