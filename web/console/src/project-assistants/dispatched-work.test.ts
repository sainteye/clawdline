import type * as W from "@clawdline/contract"
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node executes source extensions.
import { botRosterActivity, dispatchedCount, rosterMood } from "./working.ts"
// @ts-expect-error Node executes source extensions.
import { dispatchedRows, dispatchedCounts, DeliveredWorkMs } from "./dispatched-work.ts"
// @ts-expect-error Node executes source extensions.
import { words } from "./words.ts"

const w = words("zh-Hant")
const live = {fresh:true,state:"online",readable:true,access:"interactive"}
const row = (extra: Record<string, unknown> = {}) => ({status:"idle",activity:"",latest_message:"Last reply",pending_permissions:0,unread_count:0,source:live,...extra}) as unknown as W.ProjectAssistantBotSummary
const work = (running: number, needs_you = 0, delivered = 0) => ({dispatched_work:{running,needs_you,delivered}})

test("an idle Bot with running dispatched Sessions waits on them, with a caption that counts them", () => {
  assert.deepEqual(botRosterActivity(row(work(3, 0, 1)), w),
    {state:"waitingOnWork", caption:"等 3 個分身完成 · 1 個待整合", live:true, queuedOnly:false})
  assert.equal(rosterMood(row(work(3)), true), "waitingOnWork")
  // Delivered work alone does not keep a Bot waiting.
  assert.equal(botRosterActivity(row(work(0, 0, 2)), w).state, "idle")
})

test("precedence is needsInput, error, working, waitingOnWork, idle", () => {
  // A dispatched Session waiting for the person lifts the row to needsInput.
  assert.deepEqual(botRosterActivity(row(work(1, 1)), w).state, "needsInput")
  assert.equal(botRosterActivity(row(work(1, 1)), w).caption, w.rosterDispatchedNeedsYou)
  // The Bot's own failure is not hidden by work running elsewhere.
  assert.equal(botRosterActivity(row({status:"error", ...work(2), background_work:{working:1,waiting:0,needs_input:0}}), w).state, "error")
  // The Bot's own turn wins over its clones; the count still says how many.
  const busy = row({status:"working", activity:"Read notes.md", ...work(2)})
  assert.equal(botRosterActivity(busy, w).state, "working")
  assert.equal(dispatchedCount(busy), 2)
  // Background threads keep today's working state above waitingOnWork.
  assert.equal(botRosterActivity(row({...work(2), background_work:{working:1,waiting:0,needs_input:0}}), w).state, "working")
  assert.equal(botRosterActivity(row({status:"needsInput", ...work(2)}), w).state, "needsInput")
})

test("with the setting off, or from an older source, the row is what it was", () => {
  for (const item of [
    row({status:"idle"}), row({status:"error"}), row({status:"working", activity:"Read notes.md"}),
    row({background_work:{working:1,waiting:0,needs_input:0}}), row({status:"error", background_work:{working:1,waiting:0,needs_input:0}}),
  ]) {
    const withWork = {...item, ...work(2, 1, 1)} as W.ProjectAssistantBotSummary
    assert.deepEqual(botRosterActivity(withWork, w, false), botRosterActivity(item, w, true), JSON.stringify(item))
    assert.deepEqual(botRosterActivity(withWork, w, false), botRosterActivity(item, w, false))
    assert.equal(rosterMood(withWork), rosterMood(item))
  }
  // An older source sends no dispatched_work: nothing is counted.
  assert.equal(dispatchedCount(row()), 0)
  assert.equal(botRosterActivity(row(), w).state, "idle")
  // A cached (offline) source never animates or counts.
  assert.equal(botRosterActivity(row({...work(2), source:{...live, fresh:false}}), w).state, "idle")
})

test("the clone count is running plus needing you, and zero without dispatched work", () => {
  assert.equal(dispatchedCount(row(work(2, 1, 5))), 3)
  assert.equal(dispatchedCount(row(work(0, 0, 4))), 0)
  assert.equal(dispatchedCount(row({dispatched_work:{running:-1,needs_you:1.5,delivered:0}})), 0)
})

test("the strip lists each link once at its latest state: needs you, running longest first, then recent deliveries", () => {
  const now = Date.parse("2026-11-23T10:00:00Z")
  const link = (id: string, state: string, extra: Record<string, unknown> = {}) => ({id, task_id:id, state, ...extra}) as unknown as W.ProjectAssistantWorkLink
  const rows = dispatchedRows([
    link("a", "accepted"), link("a", "executing", {started_at:"2026-11-23T09:50:00Z"}),
    link("b", "executing", {started_at:"2026-11-23T09:00:00Z"}),
    link("c", "waiting_for_person"),
    link("d", "completed", {finished_at:"2026-11-23T08:00:00Z"}),
    link("e", "completed", {finished_at:new Date(now - DeliveredWorkMs - 1000).toISOString()}),
    link("f", "completed"), link("g", "failed"), link("h", "landed"), link("i", "unknown"),
  ], now)
  assert.deepEqual(rows.map((r: {link:{id:string}}) => r.link.id), ["c", "b", "a", "d"])
  assert.deepEqual(dispatchedCounts(rows), {running:2, needsYou:1, delivered:1})
  assert.deepEqual(dispatchedRows([link("g", "failed"), link("h", "landed")], now), [])
})
