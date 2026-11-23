// The last quota reading this browser saw for each machine.
//
// The footer's reader is remounted whenever the machine changes, the phone's
// list is hidden and shown again, or the page reloads. A remounted poller starts
// with nothing, so a first read that failed (the Cloud line reconnecting after a
// daemon restart) left only "Quota could not be read." where the percentages
// had been. Keeping the last good snapshot here lets the footer show it, marked
// outdated, while the poller retries; only a machine never read in this browser
// shows the error alone (DG-7: unknown is not none).
//
// Bounds (`internal/domain/capacity`, console.quota_last_reading_machines and _age_seconds): 8 machines
// per browser, and a snapshot older than 24 hours is not shown — past that the
// windows it names have reset. Past the machine bound the oldest saved goes.

import type { ProviderQuotaSnapshot } from "@clawdline/contract"

export const LAST_READING_MACHINES = 8
export const LAST_READING_HOURS = 24
const HOUR_MS = 3_600_000
const KEY = "clawdline.quota-last-reading.v1"

interface Saved { at: number; snapshot: ProviderQuotaSnapshot }
type Record_ = Record<string, Saved>

function browserStore(): Storage | null {
  try { return typeof localStorage === "undefined" ? null : localStorage } catch { return null }
}

let memory: Record_ | null = null

function load(): Record_ {
  if (memory) return memory
  memory = {}
  try {
    const raw = browserStore()?.getItem(KEY)
    const parsed: unknown = raw ? JSON.parse(raw) : null
    if (parsed && typeof parsed === "object") {
      for (const [scope, v] of Object.entries(parsed as Record<string, unknown>)) {
        const s = v as Partial<Saved>
        if (typeof s?.at === "number" && s.snapshot && Array.isArray(s.snapshot.providers)) memory[scope] = s as Saved
      }
    }
  } catch { /* an unreadable record is no record */ }
  return memory
}

/** The last good snapshot for `scope`, or null when none is recent enough. */
export function lastQuotaReading(scope: string, now = Date.now()): ProviderQuotaSnapshot | null {
  const saved = load()[scope]
  if (!saved || now - saved.at > LAST_READING_HOURS * HOUR_MS) return null
  return saved.snapshot
}

/** Keep `snapshot` as the last good reading for `scope`. */
export function rememberQuotaReading(scope: string, snapshot: ProviderQuotaSnapshot, now = Date.now()): void {
  const record = load()
  if (record[scope]?.snapshot === snapshot) return
  record[scope] = { at: now, snapshot }
  const scopes = Object.keys(record).sort((a, b) => record[b].at - record[a].at)
  for (const old of scopes.slice(LAST_READING_MACHINES)) delete record[old]
  try { browserStore()?.setItem(KEY, JSON.stringify(record)) } catch { /* memory still holds it */ }
}
