// A manual status refresh re-reads availability; it never replays a command.
const listeners = new Set<() => void>()
export function onStatusRefresh(listener: () => void): () => void {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}
export function requestStatusRefresh(): void {
  for (const listener of listeners) listener()
}
