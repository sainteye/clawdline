import type { MachineUsage, MachineUsageGroup, SessionRow } from "@clawdline/contract"

/**
 * What the machine dashboard says about `/v1/machine/usage`, with no DOM and
 * no fetch, so every sentence and every share can be checked by `node --test`.
 *
 * The dashboard answers one question first — is this machine the reason
 * everything is slow — and then who is using it. The first is `verdict`, read
 * from the same signals the 2026-09-26 investigation read by hand: memory the
 * kernel says is available, swap in use, the kernel's pressure-stall averages,
 * and the load against the cores.
 */

export type Level = "ok" | "warn" | "bad"

/** Words in both languages; the page's language picks one (`say`). */
export interface Said {
  en: string
  zh: string
}

export interface Verdict extends Said {
  level: Level
}

/** The dashboard's own words helper: the page's language decides, as `settings/capacity.ts` does. */
export function say(s: Said, zh: boolean): string {
  return zh ? s.zh : s.en
}

const GB = 1024 * 1024 * 1024
const MB = 1024 * 1024

/** Bytes as a person reads them: `1.9 GB`, `240 MB`. */
export function bytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return "0 MB"
  if (n >= GB) return `${(n / GB).toFixed(n >= 10 * GB ? 0 : 1)} GB`
  return `${Math.max(1, Math.round(n / MB))} MB`
}

/** A share of a whole, 0-100, never NaN. */
export function share(part: number, whole: number): number {
  if (!Number.isFinite(part) || !Number.isFinite(whole) || whole <= 0) return 0
  return Math.min(100, Math.max(0, (part / whole) * 100))
}

export function memoryPercent(u: MachineUsage): number {
  return share(u.memory_used_bytes, u.memory_total_bytes)
}

export function swapPercent(u: MachineUsage): number {
  return share(u.swap_used_bytes, u.swap_total_bytes)
}

/** How loud a gauge is. The thresholds are the ones `verdict` speaks at. */
export function cpuLevel(u: MachineUsage): Level {
  const load = u.load[0] ?? 0
  if (u.cpu_percent >= 90 || load >= u.cores * 2) return "bad"
  if (u.cpu_percent >= 70 || load >= u.cores) return "warn"
  return "ok"
}

export function memoryLevel(u: MachineUsage): Level {
  const p = memoryPercent(u)
  const stalled = u.pressure?.memory_some ?? 0
  if (p >= 90 || stalled >= 10) return "bad"
  if (p >= 75 || stalled >= 2) return "warn"
  return "ok"
}

export function swapLevel(u: MachineUsage): Level {
  if (u.swap_total_bytes <= 0) return "ok"
  const p = swapPercent(u)
  const stalled = u.pressure?.memory_some ?? 0
  if (p >= 50 && stalled >= 2) return "bad"
  if (p >= 25) return "warn"
  return "ok"
}

/**
 * The one sentence at the top: whether this machine is why things are slow,
 * and which resource. Memory is named before CPU because a machine short of
 * memory also shows a high load — it is waiting on the disk — and "CPU busy"
 * would send a person to close the wrong thing.
 */
export function verdict(u: MachineUsage): Verdict {
  const mem = memoryLevel(u)
  const swap = swapLevel(u)
  const cpu = cpuLevel(u)
  const avail = bytes(u.memory_available_bytes)
  if (mem === "bad" || swap === "bad") {
    return {
      level: "bad",
      en: `Memory is short: ${avail} left and ${bytes(u.swap_used_bytes)} swapped out. Everything waits on the disk — close idle sessions or stop a build.`,
      zh: `記憶體不夠：只剩 ${avail}，已有 ${bytes(u.swap_used_bytes)} 被換到 swap。所有東西都在等磁碟——關掉閒置的 session 或停掉編譯。`,
    }
  }
  if (cpu === "bad") {
    return {
      level: "bad",
      en: `The CPU is saturated: ${Math.round(u.cpu_percent)}% busy, load ${fixed(u.load[0])} on ${u.cores} cores. Work queues behind it.`,
      zh: `CPU 滿載：使用率 ${Math.round(u.cpu_percent)}%，load ${fixed(u.load[0])}（${u.cores} 核）。工作正在排隊。`,
    }
  }
  if (mem === "warn" || swap === "warn" || cpu === "warn") {
    return {
      level: "warn",
      en: `Busy but keeping up: ${Math.round(u.cpu_percent)}% CPU, ${avail} of memory available.`,
      zh: `有點忙，但還撐得住：CPU ${Math.round(u.cpu_percent)}%，記憶體還有 ${avail} 可用。`,
    }
  }
  return {
    level: "ok",
    en: `This machine has room: ${Math.round(u.cpu_percent)}% CPU, ${avail} of memory available.`,
    zh: `機器很從容：CPU ${Math.round(u.cpu_percent)}%，記憶體還有 ${avail} 可用。`,
  }
}

function fixed(n: number | undefined): string {
  return (n ?? 0).toFixed(1)
}

/**
 * The categorical slots, dark steps, in the fixed order validated against the
 * card surface (#16161a): every adjacent pair clears CVD ΔE 8.4 and
 * normal-vision ΔE 19.3, each at least 3:1. A ninth session is not given a
 * generated hue; it is drawn in the neutral "more" colour.
 */
export const SLOTS = ["#3987e5", "#d95926", "#199e70", "#c98500", "#d55181", "#008300", "#9085e9", "#e66767"] as const
export const MORE = "#6d6a64"

/**
 * A colour per session that follows the session, not its rank: a session keeps
 * its slot while it lives, and a new one takes the first free slot. `held` is
 * the previous assignment; the result is the next one.
 */
export function assignSlots(ids: string[], held: ReadonlyMap<string, number>): Map<string, number> {
  const out = new Map<string, number>()
  const taken = new Set<number>()
  for (const id of ids) {
    const slot = held.get(id)
    if (slot !== undefined && !taken.has(slot)) {
      out.set(id, slot)
      taken.add(slot)
    }
  }
  for (const id of ids) {
    if (out.has(id)) continue
    const free = SLOTS.findIndex((_, i) => !taken.has(i))
    if (free < 0) continue
    out.set(id, free)
    taken.add(free)
  }
  return out
}

export type Sort = "memory" | "cpu"

export interface Row {
  key: string
  kind: "session" | "daemon"
  id: string
  title: string
  detail: string
  color: string
  cpu: number
  rss: number
  swap: number
  processes: number
  /** Resident memory as a share of the machine's. */
  memoryShare: number
}

/**
 * The rows under the gauges: each session with the name its own row on the
 * session list carries, then this daemon. A session the list does not know
 * (it opened a moment ago) keeps the scan's name, else its terminal.
 */
export function rows(u: MachineUsage, sessions: readonly SessionRow[], slots: ReadonlyMap<string, number>, sort: Sort, zh: boolean): Row[] {
  const byId = new Map(sessions.map((s) => [s.id, s]))
  const out: Row[] = u.groups.map((g) => {
    const known = byId.get(g.id)
    const slot = slots.get(g.id)
    const daemon = g.kind === "daemon"
    return {
      key: daemon ? "daemon" : `session:${g.id}`,
      kind: g.kind,
      id: g.id,
      title: daemon ? "Clawdline" : known?.label || g.label || terminal(g) || g.id,
      detail: daemon ? (zh ? "這個服務本身" : "this service itself") : detail(g, known, zh),
      color: daemon ? MORE : slot === undefined ? MORE : SLOTS[slot],
      cpu: g.cpu_percent,
      rss: g.rss_bytes,
      swap: g.swap_bytes,
      processes: g.processes,
      memoryShare: share(g.rss_bytes, u.memory_total_bytes),
    }
  })
  const weight = (r: Row) => (sort === "cpu" ? r.cpu : r.rss + r.swap)
  return out.sort((a, b) => {
    // The daemon is last: it is the reader, not a candidate to close.
    if (a.kind !== b.kind) return a.kind === "daemon" ? 1 : -1
    return weight(b) - weight(a) || a.title.localeCompare(b.title)
  })
}

function terminal(g: MachineUsageGroup): string {
  return (g.tty ?? "").replace(/^\/dev\//, "")
}

function detail(g: MachineUsageGroup, known: SessionRow | undefined, zh: boolean): string {
  const parts = [terminal(g) || known?.tty?.replace(/^\/dev\//, "") || ""]
  parts.push(zh ? `${g.processes} 個程序` : `${g.processes} ${g.processes === 1 ? "process" : "processes"}`)
  return parts.filter(Boolean).join(" · ")
}

/** One segment of the memory bar. */
export interface Segment {
  key: string
  label: string
  color: string
  bytes: number
  percent: number
}

/**
 * The machine's memory as one bar: each session's resident memory in its
 * colour, this daemon and everything else in neutral, and what is left empty.
 * Resident pages shared between processes are counted in each, so the parts
 * can sum past what is used; they are then scaled to it, and "other" is only
 * ever what is left over, never negative.
 */
export function memorySegments(u: MachineUsage, list: Row[], zh: boolean): Segment[] {
  const used = Math.max(0, u.memory_used_bytes)
  const total = Math.max(1, u.memory_total_bytes)
  const parts = list.filter((r) => r.rss > 0)
  const counted = parts.reduce((n, r) => n + r.rss, 0)
  const scale = counted > used && counted > 0 ? used / counted : 1
  const out: Segment[] = parts.map((r) => ({
    key: r.key,
    label: r.title,
    color: r.color,
    bytes: r.rss * scale,
    percent: share(r.rss * scale, total),
  }))
  const other = Math.max(0, used - counted * scale)
  if (other > 0) {
    out.push({ key: "other", label: zh ? "其他程序與系統" : "other processes and the system", color: "#3a3a44", bytes: other, percent: share(other, total) })
  }
  return out
}

/** A short rolling history for the sparklines, newest last. */
export function remember(history: readonly number[], value: number, keep = 40): number[] {
  const next = [...history, value]
  return next.length > keep ? next.slice(next.length - keep) : next
}

/** Points for an SVG polyline across `width`×`height`, 0-100 upward. */
export function sparkPoints(values: readonly number[], width: number, height: number, keep = 40): string {
  if (values.length === 0) return ""
  const step = width / Math.max(1, keep - 1)
  const start = width - step * (values.length - 1)
  return values
    .map((v, i) => {
      const x = start + step * i
      const y = height - (Math.min(100, Math.max(0, v)) / 100) * (height - 2) - 1
      return `${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(" ")
}
