import type { CloudClientHandle } from "./copied.js"
import { destinationKey, type SessionDestination, type SessionProjectionSource } from "./all-machine-sessions.js"
import { STATUS_FRESH_MS } from "./status-projection.js"

export interface PeerPairStatus {
  pair_id: string
  source_machine_id: string
  target_machine_id: string
  source_fingerprint: string
  target_fingerprint: string
  state: "waiting_for_target" | "active"
  expires_at: string
}

export interface PeerGrantStatus {
  grant_id: string
  pair_id: string
  source: { machine_id: string; session_id: string; execution_generation: string }
  target: { machine_id: string; session_id: string; execution_generation: string }
  scopes: ("message" | "handoff")[]
  expires_at: string
}

export interface PeerAccessSnapshot {
  machineID: string
  observedAt: number
  pairs: PeerPairStatus[]
  grants: PeerGrantStatus[]
}

/** A checked box represents current machine authority, never a proposed grant. */
export function effectivePeerScopes(snapshot: PeerAccessSnapshot, grant: PeerGrantStatus,
  now = Date.now()): readonly ("message" | "handoff")[] {
  if (now < snapshot.observedAt || now - snapshot.observedAt > STATUS_FRESH_MS ||
    now >= Date.parse(grant.expires_at)) return []
  const pair = snapshot.pairs.find((entry) => entry.pair_id === grant.pair_id)
  if (!pair || pair.state !== "active" || now >= Date.parse(pair.expires_at) ||
    pair.source_machine_id !== grant.source.machine_id || pair.target_machine_id !== grant.target.machine_id) return []
  return grant.scopes
}

/** Never treat an old or malformed machine authority response as an empty list. */
export function checkedPeerAccessStatus(value: unknown, machineID: string, observedAt = Date.now()): PeerAccessSnapshot {
  const reply = value as Record<string, unknown> | null
  const pairs = reply?.pairs
  const grants = reply?.grants
  const endpoint = (item: unknown): boolean => {
    const part = item as Record<string, unknown> | null
    return !!part && typeof part.machine_id === "string" && !!part.machine_id &&
      typeof part.session_id === "string" && !!part.session_id &&
      typeof part.execution_generation === "string" && /^[0-9a-f]{32}$/u.test(part.execution_generation)
  }
  if (!reply || reply.action !== "status" || reply.state !== "identity_read" ||
    reply.local_machine_id !== machineID || !Array.isArray(pairs) || !Array.isArray(grants) ||
    !pairs.every((entry: PeerPairStatus) => entry && typeof entry.pair_id === "string" && !!entry.pair_id &&
      typeof entry.source_machine_id === "string" && !!entry.source_machine_id &&
      typeof entry.target_machine_id === "string" && !!entry.target_machine_id &&
      typeof entry.source_fingerprint === "string" && !!entry.source_fingerprint &&
      typeof entry.target_fingerprint === "string" && !!entry.target_fingerprint &&
      (entry.state === "waiting_for_target" || entry.state === "active") &&
      typeof entry.expires_at === "string" && Number.isFinite(Date.parse(entry.expires_at))) ||
    !grants.every((entry: PeerGrantStatus) => entry && typeof entry.grant_id === "string" && !!entry.grant_id &&
      typeof entry.pair_id === "string" && !!entry.pair_id && endpoint(entry.source) && endpoint(entry.target) &&
      Array.isArray(entry.scopes) && entry.scopes.length > 0 &&
      entry.scopes.every((scope) => scope === "message" || scope === "handoff") &&
      typeof entry.expires_at === "string" && Number.isFinite(Date.parse(entry.expires_at)))) {
    throw Object.assign(new Error("peer_access_bad_status"), { code: "peer_access_bad_status" })
  }
  return { machineID, observedAt, pairs, grants }
}

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
