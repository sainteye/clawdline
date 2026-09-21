import type { CloudMachine } from "./copied.js"

export interface MachineAnswer {
  machines: CloudMachine[]
  syncing: boolean
  retryAfterMs: number
}

interface MachineCapabilityClient {
  readonly viewerVerified?: ReadonlyMap<string, unknown>
  forgetMachinePairingAnswer?(machine: string): void
  machines(): Promise<MachineAnswer>
}

/**
 * A successfully opened pairing handover or a verified and decrypted envelope
 * is the pairing capability. The handover is the bootstrap proof: it was
 * authenticated with the out-of-band offer and stored this machine's keys,
 * before a fresh browser could possibly have opened one of its envelopes.
 *
 * The copied client separately remembers failed pairing lookups. A renewal or
 * another tab completing a pairing can therefore leave both facts in memory,
 * and its row calculation currently lets the older negative fact win. Repair
 * that contradiction at the typed boundary: clear the stale negative memory
 * and draw the machine from the cryptographic proof.
 */
export async function machinesByCapability(
  client: MachineCapabilityClient,
  claimed: ReadonlySet<string> = new Set(),
): Promise<MachineAnswer> {
  const answer = await client.machines()
  const verified = client.viewerVerified
  if (!verified && claimed.size === 0) return answer

  let changed = false
  const machines = answer.machines.map((machine) => {
    if (machine.pairing === "paired" || (!claimed.has(machine.id) && !verified?.has(machine.id))) return machine
    changed = true
    client.forgetMachinePairingAnswer?.(machine.id)
    return { ...machine, pairing: "paired" as const, selectable: true }
  })
  return changed ? { ...answer, machines } : answer
}
