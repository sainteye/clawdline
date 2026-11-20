import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node executes source extensions.
import { idleMood, slotPoint, strollPoint, towerBots, towerMood, towerSummary, TowerRecentMs, TowerSleepMs } from "./tower-state.ts"
// @ts-expect-error Node executes source extensions.
import { words } from "../words.ts"

const now = Date.parse("2026-11-20T12:00:00Z")
const live = { fresh: true, state: "online", readable: true, access: "interactive" }
const bot = (id: string, over: Record<string, unknown> = {}) => ({
  bot: { key: { machine_id: "m", bot_id: id }, name: id, project: { machine_id: "m", project_id: "p" } },
  latest_message: "Last reply", activity: "", status: "idle", activity_at: new Date(now - 5 * 60_000).toISOString(),
  pending_permissions: 0, unread_count: 0, source: live, ...over,
}) as never
const w = words("en") as unknown as Record<string, string>

test("placement follows the daemon status, not the running caption", () => {
  assert.equal(towerMood(bot("a", { status: "working", activity: "Read notes.md" }), now, w).mood, "work")
  assert.equal(towerMood(bot("a", { status: "working", activity: "Read notes.md" }), now, w).caption, "Read notes.md")
  assert.equal(towerMood(bot("a", { status: "working", activity: "writing" }), now, w).mood, "work")
  assert.equal(towerMood(bot("a", { status: "needsInput", activity: "Needs approval: git push" }), now, w).mood, "wait")
  assert.equal(towerMood(bot("a", { status: "error" }), now, w).mood, "error")
  assert.equal(towerMood(bot("a", { status: "idle", unread_count: 2 }), now, w).mood, "recent")
})

test("older daemons without status keep the state-word fallback", () => {
  assert.equal(towerMood(bot("a", { status: undefined, activity: "running" }), now, w).mood, "work")
  assert.equal(towerMood(bot("a", { status: undefined, pending_permissions: 1 }), now, w).mood, "wait")
  assert.equal(towerMood(bot("a", { status: "somethingNew" }), now, w).mood, "recent")
})

test("a cached source never shows a busy pose and is marked as not live", () => {
  const m = towerMood(bot("a", { status: "working", source: { ...live, fresh: false } }), now, w)
  assert.equal(m.live, false)
  assert.notEqual(m.mood, "work")
  assert.equal(towerSummary(towerBots([bot("a", { source: { ...live, fresh: false } }), bot("b")], now, w)).stale, true)
})

test("idle Bots age from just finished to idle to asleep", () => {
  const at = (ms: number) => new Date(now - ms).toISOString()
  assert.equal(idleMood(at(TowerRecentMs - 1), now), "recent")
  assert.equal(idleMood(at(TowerRecentMs), now), "idle")
  assert.equal(idleMood(at(TowerSleepMs - 1), now), "idle")
  assert.equal(idleMood(at(TowerSleepMs), now), "sleep")
  assert.equal(idleMood(new Date(now + 60_000).toISOString(), now), "recent")
  assert.equal(idleMood("0001-01-01T00:00:00Z", now), "sleep")
  assert.equal(idleMood("", now), "sleep")
})

test("the summary names the most recently active Bot and counts busy states", () => {
  const s = towerSummary(towerBots([
    bot("old", { activity_at: new Date(now - 3 * 3600_000).toISOString() }),
    bot("new", { activity_at: new Date(now - 60_000).toISOString(), status: "working" }),
    bot("zero", { activity_at: "0001-01-01T00:00:00Z" }),
  ], now, w))
  assert.equal(s.total, 3)
  assert.equal(s.work, 1)
  assert.equal(s.latest?.item.bot.name, "new")
})

test("slots repeat the spots and line extra Bots up beside them", () => {
  const spots = [[10, 10], [20, 20]] as const
  assert.deepEqual(slotPoint(spots, 1), [20, 20])
  assert.deepEqual(slotPoint(spots, 2), [54, 24])
})

test("a stroll is a pure function of time and stands still under reduced motion", () => {
  const spots = [[100, 100], [300, 100], [500, 300], [700, 300]] as const
  assert.deepEqual(strollPoint(spots, 12345, now, false), strollPoint(spots, 12345, now, false))
  const still = strollPoint(spots, 12345, now, true)
  assert.equal(still.moving, false)
  assert.deepEqual(strollPoint(spots, 12345, now + 99_999, true), still)
  let moved = false
  for (let t = 0; t < 40_000; t += 500) if (strollPoint(spots, 12345, now + t, false).moving) moved = true
  assert.equal(moved, true)
})

test("unread replies mark a Bot without moving it", () => {
  const [b] = towerBots([bot("a", { unread_count: 101, activity_at: new Date(now - 3 * 3600_000).toISOString() })], now, w)
  assert.equal(b.mood, "idle")
  assert.equal(b.unread, true)
  assert.equal(towerSummary([b]).unread, 1)
})
