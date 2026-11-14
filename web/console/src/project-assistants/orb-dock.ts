/** Where the Bots orb sits, per browser: floating at an offset from the Session
 * list's bottom-right corner, or docked as a small icon in the quota bar. A
 * per-viewer convenience kept in localStorage; it falls back to floating. */
export interface OrbPlace { docked: boolean; right: number; bottom: number }

const KEY = "bots_orb_place"
const DEFAULT: OrbPlace = { docked: false, right: 16, bottom: 72 }
const listeners = new Set<() => void>()
let cached: OrbPlace | null = null

function read(): OrbPlace {
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) ?? "null") as Partial<OrbPlace> | null
    if (raw && typeof raw === "object") {
      return {
        docked: raw.docked === true,
        right: Number.isFinite(raw.right) ? Math.max(0, Number(raw.right)) : DEFAULT.right,
        bottom: Number.isFinite(raw.bottom) ? Math.max(0, Number(raw.bottom)) : DEFAULT.bottom,
      }
    }
  } catch { /* Private windows and blocked storage keep the default. */ }
  return DEFAULT
}

export function orbPlace(): OrbPlace {
  if (!cached) cached = read()
  return cached
}

export function setOrbPlace(next: OrbPlace): void {
  cached = next
  try { localStorage.setItem(KEY, JSON.stringify(next)) } catch { /* The place still holds for this page. */ }
  for (const listener of listeners) listener()
}

export function subscribeOrbPlace(listener: () => void): () => void {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}

/** The quota bar's top edge, when the bar is on screen; a drop below it docks. */
export function quotaBarTop(): number | null {
  const bar = document.querySelector(".provider-quota-bar")
  if (!bar) return null
  const box = bar.getBoundingClientRect()
  return box.height > 0 ? box.top : null
}
