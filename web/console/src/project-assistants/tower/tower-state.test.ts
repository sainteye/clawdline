import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node executes source extensions.
import { assignFloors, floorOfBot, leadPlace, leadPoint, projectOf, idleMood, slotPoint, strollPoint, towerBots, towerMood, towerSummary, TowerRecentMs, TowerSleepMs, waitingList, activeWork, portalHelpers } from "./tower-state.ts"
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

test("floors follow a stable sort of Projects and wrap past the last floor", () => {
  const at = (project: string, id: string) => ({ ...(bot(id) as object), bot: { key: { machine_id: "m", bot_id: id }, name: id, project: { machine_id: "m", project_id: project } } }) as never
  const items = [at("zeta", "1"), at("alpha", "2"), at("mid", "3"), at("alpha", "4"), at("b", "5"), at("c", "6")]
  const floors = assignFloors(items, 4)
  assert.deepEqual([...floors.values()], [0, 1, 2, 3, 0])
  assert.equal(floors.get(projectOf(items[1])), floors.get(projectOf(items[3])))
  assert.deepEqual(assignFloors([...items].reverse(), 4), floors)
})

test("a placed Project keeps its floor and the others still follow the sort", () => {
  const at = (machine: string, project: string, id: string) => ({ ...(bot(id) as object), bot: { key: { machine_id: machine, bot_id: id }, name: id, project: { machine_id: machine, project_id: project } } }) as never
  const items = [at("m", "zeta", "1"), at("m", "alpha", "2"), at("m", "mid", "3"), at("n", "alpha", "4")]
  const auto = assignFloors(items, 4)
  const placed = new Map([[projectOf(items[0]), 3], [projectOf(items[3]), 1]])
  const floors = assignFloors(items, 4, placed)
  assert.equal(floors.get(projectOf(items[0])), 3)
  assert.equal(floors.get(projectOf(items[3])), 1, "the same Project id on another machine is a different Project")
  // The unplaced Projects take the default rule among themselves, in sort order.
  assert.deepEqual([floors.get(projectOf(items[1])), floors.get(projectOf(items[2]))], [0, 1])
  // A placement outside the building is ignored rather than drawn nowhere.
  assert.deepEqual(assignFloors(items, 4, new Map([[projectOf(items[0]), 4]])), auto)
  // A placement for a Project with no Bots here changes nothing.
  assert.deepEqual(assignFloors(items, 4, new Map([[JSON.stringify(["m", "gone"]), 2]])), auto)
})

test("the waiting list holds only waiting Bots, longest quiet first", () => {
  const at = (min: number) => new Date(now - min * 60_000).toISOString()
  const list = waitingList(towerBots([
    bot("fresh", { status: "needsInput", activity_at: at(2) }),
    bot("busy", { status: "working", activity_at: at(90) }),
    bot("old", { status: "needsInput", activity_at: at(40) }),
    bot("unknown", { status: "needsInput", activity_at: "" }),
    bot("tie-b", { status: "needsInput", activity_at: at(10) }),
    bot("tie-a", { pending_permissions: 1, status: undefined, activity_at: at(10) }),
  ], now, w))
  assert.deepEqual(list.map(b => b.item.bot.name), ["old", "tie-a", "tie-b", "fresh", "unknown"])
})

test("active work reads only whole positive counts, so an older source means none", () => {
  assert.equal(activeWork(bot("a", { active_work: 3 })), 3)
  assert.equal(activeWork(bot("a")), 0)
  for (const bad of [-1, 1.5, "2", null, Number.NaN]) assert.equal(activeWork(bot("a", { active_work: bad })), 0)
  assert.equal(towerSummary(towerBots([bot("a", { active_work: 2 }), bot("b", { active_work: 1 }), bot("c")], now, w)).helpers, 3)
})

test("portal helpers take turns between Bots and stop at the cap", () => {
  const bots = towerBots([bot("a", { active_work: 4 }), bot("b", { active_work: 1 }), bot("c")], now, w)
  assert.deepEqual(portalHelpers(bots, 6).map(h => `${h.bot.key.includes("a") ? "a" : "b"}${h.index}`), ["a0", "b0", "a1", "a2", "a3"])
  assert.equal(portalHelpers(bots, 2).length, 2)
  assert.deepEqual(portalHelpers(towerBots([bot("c")], now, w), 6), [])
})

test("the lead stands on the top floor whatever floor its Project has, in its own spot", () => {
  const at = (id: string, project: string, lead: boolean) => ({ ...(bot(id) as object), bot: { key: { machine_id: "m", bot_id: id }, name: id, project: { machine_id: "m", project_id: project }, lead } }) as never
  const items = [at("a", "alpha", false), at("b", "beta", false), at("lead", "beta", true)]
  const bots = towerBots(items, now, {})
  const floors = assignFloors(items, 4, new Map([[projectOf(items[1]), 2]]))
  const by = (id: string) => bots.find(b => b.item.bot.key.bot_id === id)!
  assert.equal(by("lead").lead, true)
  assert.equal(by("a").lead, false)
  assert.equal(floorOfBot(by("lead"), floors), 0, "the lead is on 4F although beta is on 2F")
  assert.equal(floorOfBot(by("b"), floors), 2)
  // A second machine's lead stands beside the first, then on the other side.
  assert.deepEqual([0, 1, 2].map(n => leadPoint([780, 332], n)), [[780, 332], [696, 332], [864, 332]])
})

test("the lead patrols its beat before the screen and stands still when it must be read", () => {
  const beat = [[620, 330], [940, 332], [780, 326]] as const
  const [lead] = towerBots([{ ...(bot("lead") as object), bot: { key: { machine_id: "m", bot_id: "lead" }, name: "lead", project: { machine_id: "m", project_id: "p" }, lead: true } } as never], now, {})
  const seen = new Set<string>()
  let walked = false
  for (let t = 0; t < 120_000; t += 500) {
    const p = leadPlace(beat, { ...lead, mood: "work" }, 0, now + t, false)
    walked ||= p.moving
    // Within the beat's span, give or take the stroll's small jitter.
    assert.ok(p.at[0] >= 590 && p.at[0] <= 970 && p.at[1] >= 314 && p.at[1] <= 344, `${p.at}`)
    seen.add(`${Math.round(p.at[0] / 40)}`)
  }
  assert.ok(walked && seen.size >= 3, "it walks between more than one spot")
  for (const mood of ["wait", "error", "sleep"] as const) {
    assert.deepEqual(leadPlace(beat, { ...lead, mood }, 0, now, false), { at: [940, 332], moving: false }, mood)
    assert.deepEqual(leadPlace(beat, { ...lead, mood }, 0, now + 7777, false), { at: [940, 332], moving: false }, mood)
  }
  assert.deepEqual(leadPlace(beat, { ...lead, mood: "work" }, 0, now + 4321, true), { at: [940, 332], moving: false }, "reduced motion stands")
})
