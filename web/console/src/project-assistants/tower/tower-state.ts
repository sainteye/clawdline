import type * as W from "@clawdline/contract"
// @ts-expect-error -- Node's strip-types runner uses source extensions.
import { rosterActivity } from "../working.ts"
// @ts-expect-error -- Node's strip-types runner uses source extensions.
import { botIdentity } from "../view-state.ts"

/** Where a Bot stands in the tower and why. The daemon's `status` decides the
 * busy states (via rosterActivity, which also covers older daemons); `activity`
 * is a running caption such as a tool title and never decides placement. */
export type TowerMood = "wait" | "work" | "error" | "unread" | "recent" | "idle" | "sleep"
export type TowerZone = "queue" | "desk" | "repair" | "snack" | "lounge" | "bed"

/** Presentation thresholds for an idle Bot's last activity, not capacity bounds. */
export const TowerRecentMs = 30 * 60_000
export const TowerSleepMs = 24 * 60 * 60_000

export interface TowerBot {
  key: string
  item: W.ProjectAssistantBotSummary
  mood: TowerMood
  zone: TowerZone
  caption: string
  /** False when the source is not fresh: the pose is a cached guess. */
  live: boolean
  seed: number
}

export const zoneOf: Record<TowerMood, TowerZone> = {
  wait: "queue", work: "desk", error: "repair", unread: "snack", recent: "lounge", idle: "lounge", sleep: "bed",
}

/** A stable small integer per Bot, so routes and blinks repeat across reloads. */
export function seedOf(key: string): number {
  let h = 2166136261
  for (let i = 0; i < key.length; i++) h = Math.imul(h ^ key.charCodeAt(i), 16777619)
  return h >>> 0
}

/** Zero, unparsable or pre-epoch timestamps read as long asleep; a timestamp
 * ahead of this clock reads as just finished. */
export function idleMood(activityAt: string, now: number): "recent" | "idle" | "sleep" {
  const at = Date.parse(activityAt)
  if (!Number.isFinite(at) || at <= 0) return "sleep"
  const age = now - at
  return age < TowerRecentMs ? "recent" : age < TowerSleepMs ? "idle" : "sleep"
}

export function towerMood(item: W.ProjectAssistantBotSummary, now: number, w: Record<string, string>): { mood: TowerMood, caption: string, live: boolean } {
  const shown = rosterActivity(item, w) as { state: string, caption: string, live: boolean }
  if (shown.state === "needsInput") return { mood: "wait", caption: shown.caption, live: shown.live }
  if (shown.state === "working") return { mood: "work", caption: shown.caption, live: shown.live }
  if (shown.state === "error") return { mood: "error", caption: shown.caption, live: shown.live }
  if (item.unread_count > 0) return { mood: "unread", caption: item.latest_message, live: shown.live }
  // An unknown status is idle, as the roster shows it.
  return { mood: idleMood(item.activity_at, now), caption: item.latest_message, live: shown.live }
}

export function towerBots(items: readonly W.ProjectAssistantBotSummary[], now: number, w: Record<string, string>): TowerBot[] {
  return items.map(item => {
    const key = botIdentity(item.bot.key) as string, m = towerMood(item, now, w)
    return { key, item, mood: m.mood, zone: zoneOf[m.mood], caption: m.caption, live: m.live, seed: seedOf(key) }
  }).sort((a, b) => a.key < b.key ? -1 : a.key > b.key ? 1 : 0)
}

export interface TowerSummary {
  total: number
  wait: number
  work: number
  error: number
  unread: number
  stale: boolean
  /** The Bot with the most recent valid activity. */
  latest?: TowerBot
}

export function towerSummary(bots: readonly TowerBot[]): TowerSummary {
  const count = (m: TowerMood) => bots.filter(b => b.mood === m).length
  let latest: TowerBot | undefined, best = -Infinity
  for (const b of bots) {
    const at = Date.parse(b.item.activity_at)
    if (Number.isFinite(at) && at > 0 && at > best) { best = at; latest = b }
  }
  return { total: bots.length, wait: count("wait"), work: count("work"), error: count("error"), unread: count("unread"), stale: bots.some(b => !b.live), latest }
}

type Point = readonly [number, number]
/** The n-th Bot of a zone takes the n-th spot; extra Bots line up beside it. */
export function slotPoint(spots: readonly Point[], index: number): Point {
  const base = spots[index % spots.length], ring = Math.floor(index / spots.length)
  return [base[0] + ring * 44, base[1] + ring * 14]
}

/** Idle Bots stroll between lounge spots on a fixed timetable derived from the
 * seed, so a frame is a pure function of time: no state, no timers. */
export function strollPoint(spots: readonly Point[], seed: number, now: number, still: boolean): { at: Point, moving: boolean } {
  const n = spots.length, start = seed % n, step = 1 + (seed >>> 4) % Math.max(1, n - 1)
  const jitter: Point = [((seed >>> 8) % 61) - 30, ((seed >>> 14) % 25) - 12]
  const place = (i: number): Point => { const p = spots[(start + i * step) % n]; return [p[0] + jitter[0], p[1] + jitter[1]] }
  if (still) return { at: place(0), moving: false }
  const period = 9000 + (seed >>> 20) % 7000, t = now + (seed % period), leg = Math.floor(t / period), f = (t % period) / period
  const from = place(leg), to = place(leg + 1)
  if (f < 0.72) return { at: from, moving: false }
  const k = (f - 0.72) / 0.28
  return { at: [from[0] + (to[0] - from[0]) * k, from[1] + (to[1] - from[1]) * k], moving: true }
}
