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

/** `words(en, zh)`, as `BoardBlock` says it: the page's language decides. */
export function words(en: string, zh: string): string {
  const lang = typeof document === "undefined" ? "" : document.documentElement.lang
  const nav = typeof navigator === "undefined" ? "" : navigator.language
  return /^zh/i.test(lang || nav || "") ? zh : en
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
      return words("OK", "正常")
    case "warn":
      return words("Filling", "注意")
    case "critical":
      return words("Nearly full", "快滿了")
    case "full":
      return words("Full", "滿了")
    default:
      return words("Not measured", "量不到")
  }
}

/**
 * A reading in its row's unit: bytes in binary units, the others as the count
 * their register row names — the words `capacity.amountOf` gives a push.
 */
export function amount(unit: CapacityEntry["unit"], n: number): string {
  if (unit === "characters") return words(`${n} chars`, `${n} 字`)
  if (unit === "seconds") return words(`${n} s`, `${n} 秒`)
  if (unit === "rows") return words(`${n}`, `${n} 筆`)
  const k = 1024
  if (n >= k * k * k) return `${(n / (k * k * k)).toFixed(1)} GiB`
  if (n >= k * k) return `${(n / (k * k)).toFixed(1)} MiB`
  if (n >= k) return `${(n / k).toFixed(1)} KiB`
  return `${n} B`
}

/** "used / limit · pct%", or the limit alone when nothing was read: unknown is not empty. */
export function reading(row: CapacityEntry): string {
  const limit = amount(row.unit, row.limit)
  if (row.used == null) return words(`limit ${limit}`, `上限 ${limit}`)
  const pct = row.ratio == null ? "" : ` · ${Math.floor(row.ratio * 100)}%`
  return `${amount(row.unit, row.used)} / ${limit}${pct}`
}

/** Who lets go of what when the row is full: the person, or the daemon by its rule. */
export function evicts(row: CapacityEntry): string {
  if (row.evicted_by === "person") {
    switch (row.at_limit) {
      case "rotate":
        return words("Rotates when full; only you delete old segments", "滿了輪替，舊的分段只有你能刪")
      case "refuse":
        return words("Refuses new entries when full; only you can make room", "滿了拒絕新的，只有你能騰出空間")
      default:
        return words("Only reports when full, refuses nothing yet; only you can make room", "滿了只回報、還不會拒絕，只有你能騰出空間")
    }
  }
  switch (row.at_limit) {
    case "refuse":
      return words("Refuses new entries when full; keeps what it has", "滿了拒絕新的，daemon 不丟已有的")
    case "evict_oldest":
      return words("The daemon drops the oldest when full", "滿了由 daemon 淘汰最舊的")
    case "expire":
      return words("The daemon expires entries past their window", "過了視窗由 daemon 讓它過期")
    case "rotate":
      return words("The daemon rotates and deletes the oldest segment", "滿了由 daemon 輪替、刪最舊的分段")
    case "coalesce":
      return words("Keeps only the latest value", "滿了只留最新的值")
    case "disconnect":
      return words("Disconnects readers that cannot keep up", "滿了斷開跟不上的讀者")
    case "summarize":
      return words("Summarizes before moving entries out", "滿了先摘要再移走")
    default:
      return words("Only reports when full", "滿了只回報，不拒絕也不淘汰")
  }
}

function pushWord(push: NonNullable<CapacityEntry["last_push"]>["push"]): string {
  switch (push) {
    case "pending":
      return words("push queued", "推播待送")
    case "sending":
      return words("push sending", "推播送出中")
    case "pushed":
      return words("pushed", "已推播")
    case "not_subscribed":
      return words("no device subscribed to pushes", "沒有裝置訂閱推播")
    case "failed":
      return words("push could not be sent", "推播送不出去")
    default:
      return words("not known whether the push went out", "不確定推播有沒有送出")
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
  return words(`${span} ago`, `${span} 前`)
}

/** When the row last told anybody, and whether that reached a push service. */
export function lastAlert(row: CapacityEntry, now = Date.now()): string {
  const tells = row.told.includes("notice")
  const pushed = row.last_push?.at ?? 0
  const noticed = row.last_notice_at ?? 0
  if (!pushed && !noticed) {
    return tells ? words("Never alerted", "沒有告警過") : words("Not pushed; recorded in diagnostics only", "不推播，只記在 diagnostics")
  }
  const at = Math.max(pushed, noticed)
  const push = row.last_push ? words(", ", "，") + pushWord(row.last_push.push) : tells ? "" : words(", not pushed", "，不推播")
  const held =
    row.notices_suppressed > 0
      ? words(` (${row.notices_suppressed} more held by the one-a-day rule)`, `（另有 ${row.notices_suppressed} 則被一天一則擋下）`)
      : ""
  return words("Last alert ", "上次告警 ") + ago(at, now) + push + held
}

/** What the row has let go of or refused since counting began. Empty when nothing. */
export function counters(row: CapacityEntry): string {
  const parts: string[] = []
  const add = (n: number, en: string, zh: string) => {
    if (n) parts.push(words(`${en} ${n}`, `${zh} ${n}`))
  }
  add(row.refused, "refused", "拒絕")
  add(row.evicted, "evicted", "淘汰")
  add(row.expired, "expired", "過期")
  add(row.rotated, "rotated", "輪替")
  add(row.dropped, "dropped", "丟掉")
  add(row.coalesced, "coalesced", "合併")
  add(row.disconnected, "disconnected", "斷線")
  add(row.write_errors, "write errors", "寫入失敗")
  return parts.join(" · ")
}

/** When the row fills at its current rate, in this browser's clock. */
export function fullBy(unix: number): string {
  const at = new Date(unix * 1000).toLocaleString([], { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" })
  return words(`Full by ${at} at this rate`, `照目前速度 ${at} 會滿`)
}

/** The block's one-line count: how many rows want a look, or that all of them are fine. */
export function summary(rows: CapacityEntry[]): string {
  const loud = rows.filter((r) => r.state !== "ok").length
  if (loud > 0) return words(`${loud} of ${rows.length} rows need a look`, `${rows.length} 列裡有 ${loud} 列要注意`)
  return words(`All ${rows.length} rows are fine`, `${rows.length} 列都正常`)
}
