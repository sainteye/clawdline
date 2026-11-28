import type * as W from "@clawdline/contract"
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node executes source extensions.
import { botRosterActivity, dispatchedCount, rosterMood } from "./working.ts"
// @ts-expect-error Node executes source extensions.
import { dispatchedRows, dispatchedCounts, dispatchedMoreLabel, dispatchedReason, dispatchedStrip, DispatchedStripRows } from "./dispatched-work.ts"
// @ts-expect-error Node executes source extensions.
import { words } from "./words.ts"

const w = words("zh-Hant")
const live = {fresh:true,state:"online",readable:true,access:"interactive"}
const row = (extra: Record<string, unknown> = {}) => ({status:"idle",activity:"",latest_message:"Last reply",pending_permissions:0,unread_count:0,source:live,...extra}) as unknown as W.ProjectAssistantBotSummary
const work = (running: number, needs_you = 0, delivered = 0) => ({dispatched_work:{running,needs_you,delivered}})

test("an idle Bot with running dispatched Sessions waits on them, with a caption that counts them", () => {
  assert.deepEqual(botRosterActivity(row(work(3, 0, 1)), w),
    {state:"waitingOnWork", caption:"等 3 個分身完成", live:true, queuedOnly:false})
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

test("the strip lists only active links in their latest state: needs you, then running longest first", () => {
  const link = (id: string, state: string, extra: Record<string, unknown> = {}) => ({id, task_id:id, state, ...extra}) as unknown as W.ProjectAssistantWorkLink
  const rows = dispatchedRows([
    link("a", "accepted"), link("a", "executing", {started_at:"2026-11-23T09:50:00Z"}),
    link("b", "executing", {started_at:"2026-11-23T09:00:00Z"}),
    link("c", "waiting_for_person"),
    link("d", "completed", {finished_at:"2026-11-23T08:00:00Z", landing:"pending"}),
    link("e", "completed", {finished_at:"2026-11-20T08:00:00Z", landing:"pending"}),
    link("f", "completed"),
    link("g", "failed"), link("g2", "failed", {landing:"pending", landing_note:"timed out with commits"}),
    link("h", "landed"), link("i", "unknown"),
    link("j", "completed", {landing:"abandoned"}), link("k", "completed", {landing:"nothing_to_land"}),
    link("l", "failed", {landing:"abandoned"}),
  ])
  assert.deepEqual(rows.map((r: {link:{id:string}}) => r.link.id), ["c", "b", "a"])
  assert.deepEqual(dispatchedCounts(rows), {running:2, needsYou:1})
  assert.deepEqual(dispatchedRows([link("g", "failed"), link("h", "landed")]), [])
})

test("a link leaves the strip on delivery, even while its landing is pending", () => {
  const running = {id:"x", task_id:"x", state:"executing"} as W.ProjectAssistantWorkLink
  const delivered = {...running, state:"completed", landing:"pending"} as W.ProjectAssistantWorkLink
  assert.equal(dispatchedRows([running]).length, 1)
  assert.deepEqual(dispatchedRows([running, delivered]), [])
})

test("every listed state says in plain words what it waits for", () => {
  const reasonOf = (state: string) => dispatchedReason(dispatchedRows([{id:"x", task_id:"x", state} as W.ProjectAssistantWorkLink])[0], w)
  assert.equal(reasonOf("waiting_for_person"), "等你回答分身的問題")
  assert.equal(reasonOf("executing"), "執行中")
  assert.equal(reasonOf("accepted"), "啟動中")
})

test("the strip shares active counts with the roster when only some work links have loaded", () => {
  const item = row(work(2, 0, 14))
  const loaded = [{id:"run", task_id:"run", state:"executing"},
    {id:"d0", task_id:"d0", state:"completed", landing:"pending"}] as W.ProjectAssistantWorkLink[]
  const strip = dispatchedStrip(loaded, item.dispatched_work)
  assert.deepEqual(strip.counts, {running:2, needsYou:0})
  assert.equal(strip.counts.running + strip.counts.needsYou, dispatchedCount(item))
  assert.equal(botRosterActivity(item, w).caption, "等 2 個分身完成")
  assert.deepEqual(strip.rows.map(r => r.link.id), ["run"])
  assert.equal(strip.more, 1)
})

test("the strip animates only when the summary confirms execution", () => {
  const queued = {id:"queued", task_id:"queued", state:"accepted"} as W.ProjectAssistantWorkLink
  const executing = {...queued, state:"executing"} as W.ProjectAssistantWorkLink
  const summary = {running:2, needs_you:0, delivered:4}
  assert.equal(dispatchedStrip([queued], {...summary, executing:1}).showWorking, true)
  assert.equal(dispatchedStrip([executing], {...summary, executing:0}).showWorking, false)
  assert.equal(dispatchedStrip([], {...summary, running:0, executing:0}).showWorking, false)
  assert.equal(dispatchedStrip([executing], row({dispatched_work:summary}).dispatched_work).showWorking, false)
  assert.equal(dispatchedStrip([executing], undefined).showWorking, false)
})

test("past the row limit the strip lists the limit and says how many more there are", () => {
  const link = (id: string, state: string, extra: Record<string, unknown> = {}) => ({id, task_id:id, state, ...extra}) as unknown as W.ProjectAssistantWorkLink
  const loaded = Array.from({length: 9}, (_, i) => link(`r${i}`, "executing"))
  const strip = dispatchedStrip(loaded, {running:14, executing:14, needs_you:0, delivered:14})
  assert.equal(strip.rows.length, DispatchedStripRows)
  assert.equal(strip.more, 14 - DispatchedStripRows)
  const older = dispatchedStrip(loaded, undefined)
  assert.deepEqual(older.counts, {running:9, needsYou:0})
  assert.equal(older.rows.length + older.more, 9)
  // A fresh active count can supersede the chat's older snapshot.
  assert.equal(dispatchedStrip(loaded, {running:0, executing:0, needs_you:0, delivered:14}).rows.length, 0)
})

test("when no rows are loaded the strip points directly to Sessions instead of saying more", () => {
  assert.equal(dispatchedMoreLabel(0, 4, w), "到 Sessions 查看這 4 個工作")
  assert.doesNotMatch(dispatchedMoreLabel(0, 4, w), /還有/)
  assert.equal(dispatchedMoreLabel(0, 4, words("en")), "View 4 in Sessions")
  assert.equal(dispatchedMoreLabel(2, 3, w), "還有 3 個，到 Sessions 查看")
  assert.equal(dispatchedMoreLabel(2, 3, words("en")), "3 more in Sessions")
  assert.equal(dispatchedMoreLabel(2, 0, w), "")
  // Snapshot active links from older pages appear in the strip and clear once a newer update arrives.
  const olderActive = {id:"old-run", task_id:"old-run", state:"executing", title:"Older task"} as W.ProjectAssistantWorkLink
  const strip = dispatchedStrip([olderActive], {running:1, executing:1, needs_you:0, delivered:0})
  assert.deepEqual(strip.rows.map(r => r.link.id), ["old-run"])
  assert.equal(strip.more, 0)
  const closed = {...olderActive, state:"unknown"} as W.ProjectAssistantWorkLink
  assert.deepEqual(dispatchedStrip([olderActive, closed], {running:0, executing:0, needs_you:0, delivered:0}).rows, [])
})
