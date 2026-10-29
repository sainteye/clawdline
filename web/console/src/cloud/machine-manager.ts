// What the Devices page's machine sheet may do to a machine, installed by the gate.
//
// The page is drawn outside the gate (`pages/devices.tsx`), and everything it
// acts through — the connected client, the account roster the gate keeps, the
// forget path that also leaves a console reading the forgotten machine — is
// the gate's. So the gate installs this while it holds a client and removes it
// when it lets go, as it does the machine source in `legacy/devices-bridge.ts`.
import type { CarrierReading } from "./carrier-state.js"
import type { CloudMachine } from "./copied.js"
import type { DirectTry } from "./direct-carrier.js"
import type { ForgetOutcome } from "./forget.js"
import type { RenameOutcome } from "./rename.js"

export interface MachineManager {
  /** The account's API, for the device list (`viewer-devices.ts`). */
  apiOrigin: string
  /** This browser's own device id, so the list can say which row is this one. */
  deviceID(): string | null
  /** Which road this page's reads of `machine` take now. It opens nothing. */
  carrier(machine: string): CarrierReading | null
  /** One direct attempt the person asked for. */
  tryDirect(machine: string): Promise<DirectTry>
  sameNetworkAvailable?(machine: string): boolean
  enableSameNetwork?(machine: string): Promise<void>
  rename(machine: CloudMachine, name: string): Promise<RenameOutcome>
  forget(machine: CloudMachine): Promise<ForgetOutcome>
  /** Opens pairing for a named machine, or the target picker when null. */
  pair(machine: string | null): void
}

let installed: MachineManager | null = null
const listeners = new Set<() => void>()

export function setMachineManager(manager: MachineManager | null): void {
  installed = manager
  for (const listener of [...listeners]) listener()
}

export function machineManager(): MachineManager | null {
  return installed
}

export function watchMachineManager(listener: () => void): () => void {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}
