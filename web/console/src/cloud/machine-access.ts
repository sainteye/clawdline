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

/** Unknown is a pending observation, never evidence that a pairing is missing. */
export function needsMachinePairing(pairing: CloudMachine["pairing"]): boolean {
  return pairing === "not_paired"
}

/** Restore each account-listed machine independently, including after a PWA return. */
export function watchStoredMachinePairings(
  client: { restoreMachinePairing?: (machine: string) => Promise<void> },
  machines: readonly string[],
  changed: () => void,
  page: { document: EventTarget & { readonly hidden?: boolean }; window: EventTarget },
): () => void {
  if (!client.restoreMachinePairing) return () => {}
  let stopped = false
  // Scoped to this account roster and client; bursts share each outstanding local read.
  const pending = new Set<string>()
  const started = new Set<string>()
  const returned = new Set<string>()
  const read = (machine: string) => {
    pending.add(machine)
    void Promise.resolve().then(() => {
      if (!stopped) {
        started.add(machine)
        return client.restoreMachinePairing!(machine)
      }
    }).then(
      () => { if (!stopped) changed() },
      () => { /* A failed key-store read remains unknown; a return retries it. */ },
    ).finally(() => {
      pending.delete(machine)
      started.delete(machine)
      // A return during an interrupted IndexedDB read must not be lost.
      if (returned.delete(machine) && !stopped && !page.document.hidden) read(machine)
    })
  }
  const restore = () => {
    if (stopped || page.document.hidden) return
    for (const machine of machines) {
      if (pending.has(machine)) {
        if (started.has(machine)) returned.add(machine)
      }
      else read(machine)
    }
  }
  page.document.addEventListener("visibilitychange", restore)
  page.window.addEventListener("pageshow", restore)
  page.window.addEventListener("online", restore)
  restore()
  return () => {
    stopped = true
    page.document.removeEventListener("visibilitychange", restore)
    page.window.removeEventListener("pageshow", restore)
    page.window.removeEventListener("online", restore)
  }
}

/** A verified machine report can change presence on every console view. */
export function reportsMachinePresence(event: { type: string }): boolean {
  return event.type === "sessions" || event.type === "orchestrator" || event.type === "session_status" ||
    event.type === "machine_reachability"
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
