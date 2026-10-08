/** Notify open composers only after an authenticated pairing handover completes. */
const listeners = new Set<(machine: string) => void>()

export function watchMachinePaired(listener: (machine: string) => void): () => void {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}

export function machinePaired(machine: string): void {
  for (const listener of listeners) listener(machine)
}
