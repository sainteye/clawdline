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

/** The connection light's pace while it is the only sign the host is there. */
export const HEALTH_MS = 15_000
/**
 * Its pace while this machine's own event stream is open. The stream already
 * says the line is up, so health is read only for what else it carries — the
 * version, and a browser that was signed out under the page (`door/Door.tsx`)
 * — and a dropped stream goes back to `HEALTH_MS`. Over Clawdline Cloud health
 * answers whether the chosen machine is current, which the relay's open socket
 * does not, and it is computed in the page; it keeps `HEALTH_MS` there. On a
 * phone, health at fifteen seconds was 13 of a list page's 25 requests in
 * three minutes.
 */
export const HEALTH_LIVE_MS = 90_000

/** The light's pace: slow only when this machine's own stream is up. */
export function healthPace(o: { live: boolean; answered: boolean; relayed: boolean }): number {
  return o.live && o.answered && !o.relayed ? HEALTH_LIVE_MS : HEALTH_MS
}

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
