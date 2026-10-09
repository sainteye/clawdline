// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { catalogFormat } from "../../catalog.ts"
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { catalogWord, currentCatalogTag } from "../../catalog.ts"
import type { CapacityEntry, CapacityPanel, CapacityState } from "@clawdline/contract"
import { RefusalError, TransportError, isRefusal } from "@clawdline/core"
import { client } from "../../client.js"

/**
 * `/v1/capacity` and the words the Settings page's capacity block says about
 * it (docs/limits.md §4.5 "the screen", design-decisions C4).
 *
 * Asked the way `api.ts` asks `/v1/settings`: through `client.url` and the
 * page's `fetch`, which a console reading a machine through Clawdline Cloud
 * answers from the relay (`cloud/install.ts`) as the `capacity` word. So the
 * block a capacity push names opens on the phone that got the push. The read
 * is spelled beside its method on one line because `cloud/carry.test.ts`
 * reads it there.
 *
 * Nothing here touches the DOM, so every sentence can be checked without one.
 */

/** Existing callers pass source English and the current catalog's translated copy. */
export function words(_english: string, localized: string): string {
  return localized
}

export function readCapacity(): Promise<CapacityPanel> {
  return call("GET", "/v1/capacity")
}

async function call(method: string, path: string): Promise<CapacityPanel> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), 15_000)
  let res: Response
  try {
    res = await fetch(client.url(path), { method, credentials: "same-origin", signal: controller.signal })
  } catch (cause) {
    throw new TransportError(`${method} ${path} did not complete`, cause)
  } finally {
    clearTimeout(timer)
  }
  const text = await res.text()
  let parsed: unknown
  try {
    parsed = text ? JSON.parse(text) : null
  } catch (cause) {
    throw new TransportError(`${path} answered with something that is not JSON`, cause)
  }
  if (!res.ok) {
    if (isRefusal(parsed)) throw new RefusalError(res.status, parsed, path)
    throw new TransportError(`${path} answered ${res.status} with no refusal in it`)
  }
  return parsed as CapacityPanel
}

const STATE_RANK: Record<CapacityState, number> = { full: 0, critical: 1, unknown: 2, warn: 3, ok: 4 }

/** Fullest first; a row that could not be measured sits above one that only warns. */
export function bySeverity(a: CapacityEntry, b: CapacityEntry): number {
  return STATE_RANK[a.state] - STATE_RANK[b.state] || (b.ratio ?? 0) - (a.ratio ?? 0) || a.name.localeCompare(b.name)
}

export function stateWord(state: CapacityState): string {
  switch (state) {
    case "ok":
      return catalogWord("literal", "499be85f5b3b")
    case "warn":
      return catalogWord("literal", "7c2553d0f0f2")
    case "critical":
      return catalogWord("literal", "959dd5060ea6")
    case "full":
      return catalogWord("literal", "6a0d24a2797e")
    default:
      return catalogWord("literal", "74d2b141f0a6")
  }
}

/**
 * A reading in its row's unit: bytes in binary units, the others as the count
 * their register row names — the words `capacity.amountOf` gives a push.
 */
export function amount(unit: CapacityEntry["unit"], n: number): string {
  if (unit === "characters") return words(`${n} chars`, catalogFormat("template", "5c5e1d37c1ed", [n]))
  if (unit === "seconds") return words(`${n} s`, catalogFormat("template", "49c70f219483", [n]))
  if (unit === "rows") return words(`${n}`, catalogFormat("template", "86ef4744daea", [n]))
  const k = 1024
  if (n >= k * k * k) return `${(n / (k * k * k)).toFixed(1)} GiB`
  if (n >= k * k) return `${(n / (k * k)).toFixed(1)} MiB`
  if (n >= k) return `${(n / k).toFixed(1)} KiB`
  return `${n} B`
}

/** "used / limit · pct%", or the limit alone when nothing was read: unknown is not empty. */
export function reading(row: CapacityEntry): string {
  const limit = amount(row.unit, row.limit)
  if (row.used == null) return words(`limit ${limit}`, catalogFormat("template", "cad071062b9f", [limit]))
  const pct = row.ratio == null ? "" : ` · ${Math.floor(row.ratio * 100)}%`
  return `${amount(row.unit, row.used)} / ${limit}${pct}`
}

/** Who lets go of what when the row is full: the person, or the daemon by its rule. */
export function evicts(row: CapacityEntry): string {
  if (row.evicted_by === "person") {
    switch (row.at_limit) {
      case "rotate":
        return catalogWord("literal", "a409c44179a1")
      case "refuse":
        return catalogWord("literal", "a7632813e591")
      default:
        return catalogWord("literal", "24be634bc511")
    }
  }
  switch (row.at_limit) {
    case "refuse":
      return catalogWord("literal", "56ce6ed5b735")
    case "evict_oldest":
      return catalogWord("literal", "fd71625e8c36")
    case "expire":
      return catalogWord("literal", "41b7b78d5589")
    case "rotate":
      return catalogWord("literal", "64f36331b69f")
    case "coalesce":
      return catalogWord("literal", "2f810d174492")
    case "disconnect":
      return catalogWord("literal", "603cabc2847f")
    case "summarize":
      return catalogWord("literal", "8eb312e5c44d")
    default:
      return catalogWord("literal", "b2f7a693e69e")
  }
}

function pushWord(push: NonNullable<CapacityEntry["last_push"]>["push"]): string {
  switch (push) {
    case "pending":
      return catalogWord("literal", "f8d802ee3b49")
    case "sending":
      return catalogWord("literal", "4abc45706b7c")
    case "pushed":
      return catalogWord("literal", "2c5109559831")
    case "not_subscribed":
      return catalogWord("literal", "682e873e7cdf")
    case "failed":
      return catalogWord("literal", "f90cd1fa096f")
    default:
      return catalogWord("literal", "7ddacedc1cff")
  }
}

function age(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`
  return `${Math.floor(seconds / 86400)}d`
}

function ago(unix: number, now: number): string {
  const span = age(Math.max(0, Math.floor(now / 1000) - unix))
  return words(`${span} ago`, catalogFormat("template", "4d35c17ea7eb", [span]))
}

/** When the row last told anybody, and whether that reached a push service. */
export function lastAlert(row: CapacityEntry, now = Date.now()): string {
  const tells = row.told.includes("notice")
  const pushed = row.last_push?.at ?? 0
  const noticed = row.last_notice_at ?? 0
  if (!pushed && !noticed) {
    return tells ? catalogWord("literal", "e0390e103f88") : catalogWord("literal", "79be3075d69a")
  }
  const at = Math.max(pushed, noticed)
  const separator = currentCatalogTag().startsWith("zh") ? "，" : ", "
  const push = row.last_push ? separator + pushWord(row.last_push.push) : tells ? "" : catalogWord("literal", "78f1213394fc")
  const held =
    row.notices_suppressed > 0
      ? words(` (${row.notices_suppressed} more held by the one-a-day rule)`, catalogFormat("template", "0ebfc47d96d4", [row.notices_suppressed]))
      : ""
  return catalogWord("literal", "9034aceb6633") + ago(at, now) + push + held
}

/** What the row has let go of or refused since counting began. Empty when nothing. */
export function counters(row: CapacityEntry): string {
  const parts: string[] = []
  const add = (n: number, en: string, zh: string) => {
    if (n) parts.push(words(`${en} ${n}`, `${zh} ${n}`))
  }
  add(row.refused, "refused", catalogWord("literal", "ac2ddfb95017"))
  add(row.evicted, "evicted", catalogWord("literal", "3d1666fc258f"))
  add(row.expired, "expired", catalogWord("literal", "88fefceaa267"))
  add(row.rotated, "rotated", catalogWord("literal", "55c4eda8a6e9"))
  add(row.dropped, "dropped", catalogWord("literal", "ee94960e6f12"))
  add(row.coalesced, "coalesced", catalogWord("literal", "c4fd4486315f"))
  add(row.disconnected, "disconnected", catalogWord("literal", "9c405ef85bd2"))
  add(row.write_errors, "write errors", catalogWord("literal", "d8007d80c5de"))
  return parts.join(" · ")
}

/** When the row fills at its current rate, in this browser's clock. */
export function fullBy(unix: number): string {
  const at = new Date(unix * 1000).toLocaleString([], { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" })
  return words(`Full by ${at} at this rate`, catalogFormat("template", "73fb5e35d2a0", [at]))
}

/** The block's one-line count: how many rows want a look, or that all of them are fine. */
export function summary(rows: CapacityEntry[]): string {
  const loud = rows.filter((r) => r.state !== "ok").length
  if (loud > 0) return words(`${loud} of ${rows.length} rows need a look`, catalogFormat("template", "211ae3476e5f", [rows.length, loud]))
  return words(`All ${rows.length} rows are fine`, catalogFormat("template", "614443bb7374", [rows.length]))
}
