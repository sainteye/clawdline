// The page's clock for when a session last moved: `node --test web/console/src/session/*.test.ts`.
// The `.ts` import is for node, as in `order.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
import type { SessionRow } from "@clawdline/contract"
// @ts-expect-error -- a `.ts` path, for node; see `order.test.ts`.
import { ActivityClock, movementOf, type ActivityStore } from "./activity.ts"

function row(id: string, state: string, extra: Partial<SessionRow> = {}): SessionRow {
  return {
    id,
    state,
    label: id,
    backend: "iterm",
    work_state: "unknown",
    evidence: "screen",
    isClaude: true,
    closeability: { activity_generation: 0, observed_at: 0 },
    ...extra,
  } as SessionRow
}

function memory(initial: string | null = null): ActivityStore & { value: string | null; writes: number } {
  const store = {
    value: initial,
    writes: 0,
    read: () => store.value,
    write: (v: string) => {
      store.value = v
      store.writes++
    },
  }
  return store
}

test("rows in the first list have no time; working rows are moving now", () => {
  const clock = new ActivityClock()
  clock.observe([row("idle", "idle"), row("busy", "working")], 1_000)
  assert.equal(clock.movedAt("idle"), 0)
  assert.equal(clock.movedAt("busy"), 1_000)
})

test("a session that stops working keeps the moment it was first seen stopped", () => {
  const clock = new ActivityClock()
  clock.observe([row("a", "working")], 1_000)
  clock.observe([row("a", "working")], 2_000)
  assert.equal(clock.movedAt("a"), 2_000)
  clock.observe([row("a", "idle")], 3_000)
  assert.equal(clock.movedAt("a"), 3_000)
  // Nothing changed after that, so later looks do not move it.
  clock.observe([row("a", "idle")], 9_000)
  assert.equal(clock.movedAt("a"), 3_000)
})

test("an idle row that did nothing keeps its time; one that did something gets a new one", () => {
  const clock = new ActivityClock()
  clock.observe([row("quiet", "idle"), row("busy", "idle")], 1_000)
  clock.observe([row("quiet", "idle"), row("busy", "idle", { shells: [{ id: "x", at: 1_700 }] })], 2_000)
  assert.equal(clock.movedAt("quiet"), 0)
  assert.equal(clock.movedAt("busy"), 2_000)
})

test("a new question on a waiting row is movement; the reading's own time is not", () => {
  const clock = new ActivityClock()
  const asked = (line: string, observed: number) =>
    row("w", "waiting", { line, closeability: { activity_generation: 0, observed_at: observed } as SessionRow["closeability"] })
  clock.observe([asked("rev-1", 10)], 1_000)
  clock.observe([asked("rev-1", 11)], 2_000)
  assert.equal(clock.movedAt("w"), 0)
  clock.observe([asked("rev-2", 12)], 3_000)
  assert.equal(clock.movedAt("w"), 3_000)
})

test("the working line's own clock is not part of the movement", () => {
  const a = movementOf(row("a", "working", { line: "Thinking… (3s)" }))
  const b = movementOf(row("a", "working", { line: "Thinking… (4s)" }))
  assert.equal(a, b)
})

test("a row that turns up after the first list has just appeared", () => {
  const clock = new ActivityClock()
  clock.observe([row("a", "idle")], 1_000)
  clock.observe([row("a", "idle"), row("new", "idle")], 2_000)
  assert.equal(clock.movedAt("new"), 2_000)
  // An empty first look (no fleet yet) does not count as having looked.
  const early = new ActivityClock()
  early.observe([], 500)
  early.observe([row("b", "idle")], 1_000)
  assert.equal(early.movedAt("b"), 0)
})

test("the clock carries over a reload, and what moved while away is stamped when seen", () => {
  const store = memory()
  const first = new ActivityClock(store)
  first.observe([row("a", "idle"), row("b", "working")], 1_000)
  first.observe([row("a", "idle"), row("b", "idle")], 2_000)
  assert.ok(store.writes > 0)

  const second = new ActivityClock(store)
  assert.equal(second.movedAt("b"), 2_000)
  second.observe([row("a", "working"), row("b", "idle"), row("c", "idle")], 5_000)
  assert.equal(second.movedAt("a"), 5_000)
  assert.equal(second.movedAt("b"), 2_000)
  // Remembered rows mean this is not a first look, so an unknown row is new.
  assert.equal(second.movedAt("c"), 5_000)
})

test("storage that throws or holds rubbish leaves a working clock", () => {
  const broken: ActivityStore = {
    read: () => {
      throw new Error("blocked")
    },
    write: () => {
      throw new Error("full")
    },
  }
  const clock = new ActivityClock(broken)
  clock.observe([row("a", "working")], 1_000)
  assert.equal(clock.movedAt("a"), 1_000)
  for (const junk of ["not json", "[]", '{"rows":{"a":[1,2,3]}}', '{"rows":{"a":["sig",1]}}']) {
    const c = new ActivityClock(memory(junk))
    assert.equal(c.movedAt("a"), 0)
  }
})

test("rows not seen for a week are forgotten when the clock is written", () => {
  const store = memory()
  const clock = new ActivityClock(store)
  const week = 7 * 24 * 60 * 60 * 1000
  clock.observe([row("old", "working")], 1_000)
  clock.observe([row("fresh", "working")], 1_000 + week + 1)
  const saved = JSON.parse(store.value ?? "{}") as { rows: Record<string, unknown> }
  assert.deepEqual(Object.keys(saved.rows), ["fresh"])
})
