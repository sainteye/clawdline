/*
 * The one `/v1/health` reading the page takes, handed to whoever else needs it.
 *
 * The connection light (`App.tsx`, `useConnectionLight`) reads health on its
 * own lane, paused while the page is hidden. The door (`door/Door.tsx`) needs
 * the same answer to notice a browser that was signed out under a running
 * console; it used to read the same route again on a 30-second timer of its
 * own. Now, while the console is drawn, it listens here instead, and only the
 * door alone — no console behind it — reads for itself.
 *
 * Nothing is imported at run time, so `node --test` loads this file as it is.
 */

export type HealthListener = (health: unknown) => void

const listeners = new Set<HealthListener>()

/** Hand one successful health answer to every listener. */
export function publishHealth(health: unknown): void {
  for (const fn of listeners) fn(health)
}

/** Hear every health answer the console reads, until the returned function is called. */
export function onHealth(fn: HealthListener): () => void {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}
