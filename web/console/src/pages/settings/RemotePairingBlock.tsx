import { useSyncExternalStore } from "react"
import { LocalMachineSetup } from "../../cloud/LocalMachineSetup.js"
import { machineManager, watchMachineManager } from "../../cloud/machine-manager.js"
import { BrowserPairingBlock } from "./BrowserPairingBlock.js"

/** Remote pairing belongs here in both local and hosted Settings. */
export function RemotePairingBlock() {
  const manager = useSyncExternalStore(watchMachineManager, machineManager)
  return <LocalMachineSetup onPair={manager ? machine => manager.pair(machine.id) : undefined}>
    <BrowserPairingBlock shown onPair={manager ? () => manager.pair(null) : undefined} />
  </LocalMachineSetup>
}
