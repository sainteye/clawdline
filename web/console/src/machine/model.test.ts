import { test } from "node:test"
import assert from "node:assert/strict"
import type { MachineUsage, SessionRow } from "@clawdline/contract"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { assignSlots, bytes, memorySegments, rows, SLOTS, MORE, sparkPoints, remember, verdict } from "./model.ts"

const GB = 1024 * 1024 * 1024
const MB = 1024 * 1024

function usage(over: Partial<MachineUsage> = {}): MachineUsage {
  return {
    at: 1,
    interval_ms: 3000,
    cores: 2,
    cpu_percent: 20,
    load: [0.4, 0.3, 0.2],
    memory_total_bytes: 2 * GB,
    memory_used_bytes: 0.6 * GB,
    memory_available_bytes: 1.4 * GB,
    swap_total_bytes: 2 * GB,
    swap_used_bytes: 0,
    groups: [],
    others: [],
    ...over,
  }
}

test("the verdict names memory before CPU, because a swapping machine also shows a high load", () => {
  // The machine of 2026-09-26: 1.9 GB, 290 MB available, 1.1 GB swapped, load 14 on two cores.
  const v = verdict(
    usage({
      cpu_percent: 100,
      load: [14.1, 5.6, 2.2],
      memory_total_bytes: 1.9 * GB,
      memory_used_bytes: 1.6 * GB,
      memory_available_bytes: 290 * MB,
      swap_used_bytes: 1.1 * GB,
      pressure: { cpu_some: 92, memory_some: 36, memory_full: 18, io_some: 53 },
    }),
  )
  assert.equal(v.level, "bad")
  assert.match(v.zh, /^記憶體不夠/)
  assert.match(v.zh, /290 MB/)
  assert.match(v.en, /^Memory is short/)
})

test("a saturated CPU with memory to spare is called the CPU", () => {
  const v = verdict(usage({ cpu_percent: 97, load: [5, 3, 2] }))
  assert.equal(v.level, "bad")
  assert.match(v.zh, /^CPU 滿載/)
})

test("a quiet machine says it has room, and a busy one that copes says so", () => {
  assert.equal(verdict(usage()).level, "ok")
  assert.equal(verdict(usage({ cpu_percent: 75 })).level, "warn")
  assert.equal(verdict(usage({ memory_used_bytes: 1.6 * GB, memory_available_bytes: 0.4 * GB })).level, "warn")
})

test("bytes read as a person reads them", () => {
  assert.equal(bytes(0), "0 MB")
  assert.equal(bytes(240 * MB), "240 MB")
  assert.equal(bytes(1.9 * GB), "1.9 GB")
  assert.equal(bytes(16 * GB), "16 GB")
  assert.equal(bytes(Number.NaN), "0 MB")
})

test("a session keeps its colour while it lives, and a new one takes the first free slot", () => {
  const first = assignSlots(["a", "b", "c"], new Map())
  assert.deepEqual([...first], [["a", 0], ["b", 1], ["c", 2]])
  // b ends, d arrives: a and c keep theirs, d takes b's.
  const second = assignSlots(["c", "d", "a"], first)
  assert.equal(second.get("a"), 0)
  assert.equal(second.get("c"), 2)
  assert.equal(second.get("d"), 1)
  // A ninth session is not given a generated hue.
  const many = assignSlots(["1", "2", "3", "4", "5", "6", "7", "8", "9"], new Map())
  assert.equal(many.size, SLOTS.length)
  assert.equal(many.has("9"), false)
})

test("rows carry the session list's name, heaviest first, and the daemon last", () => {
  const u = usage({
    groups: [
      { kind: "daemon", id: "", pid: 1, processes: 1, cpu_percent: 1, rss_bytes: 20 * MB, swap_bytes: 0 },
      { kind: "session", id: "%1", label: "scan name", tty: "/dev/pts/1", pid: 10, processes: 3, cpu_percent: 5, rss_bytes: 100 * MB, swap_bytes: 0 },
      { kind: "session", id: "%2", tty: "/dev/pts/2", pid: 20, processes: 9, cpu_percent: 80, rss_bytes: 300 * MB, swap_bytes: 50 * MB },
      { kind: "session", id: "%3", pid: 30, processes: 1, cpu_percent: 0, rss_bytes: 10 * MB, swap_bytes: 0 },
    ],
  })
  const list = [{ id: "%1", label: "Machine load" } as SessionRow]
  const slots = assignSlots(["%1", "%2", "%3"], new Map())
  const byMemory = rows(u, list, slots, "memory", true)
  assert.deepEqual(
    byMemory.map((r) => r.title),
    ["pts/2", "Machine load", "%3", "Clawdline"],
  )
  assert.equal(byMemory[0].color, SLOTS[1])
  assert.equal(byMemory[3].color, MORE)
  assert.equal(byMemory[0].detail, "pts/2 · 9 個程序")
  const byCPU = rows(u, list, slots, "cpu", false)
  assert.deepEqual(
    byCPU.map((r) => r.id),
    ["%2", "%1", "%3", ""],
  )
  assert.equal(byCPU[1].detail, "pts/1 · 3 processes")
})

test("the memory bar never shows more than is used, and other is what is left over", () => {
  const u = usage({ memory_total_bytes: 1000, memory_used_bytes: 600 })
  const list = [
    { key: "a", kind: "session" as const, id: "a", title: "A", detail: "", color: SLOTS[0], cpu: 0, rss: 500, swap: 0, processes: 1, memoryShare: 50 },
    { key: "b", kind: "session" as const, id: "b", title: "B", detail: "", color: SLOTS[1], cpu: 0, rss: 300, swap: 0, processes: 1, memoryShare: 30 },
  ]
  // Shared pages counted twice: 800 counted of 600 used, scaled to fit.
  const over = memorySegments(u, list, true)
  assert.equal(over.length, 2)
  assert.equal(Math.round(over.reduce((n: number, s: { bytes: number }) => n + s.bytes, 0)), 600)
  const under = memorySegments(usage({ memory_total_bytes: 1000, memory_used_bytes: 900 }), list, false)
  assert.equal(under.at(-1)?.key, "other")
  assert.equal(Math.round(under.at(-1)!.bytes), 100)
  assert.equal(Math.round(under.reduce((n: number, s: { percent: number }) => n + s.percent, 0)), 90)
})

test("the sparkline is right-aligned and clamped", () => {
  assert.equal(sparkPoints([], 100, 20), "")
  const pts = sparkPoints([0, 150], 100, 20, 3).split(" ")
  assert.equal(pts.length, 2)
  assert.equal(pts[1], "100.0,1.0")
  assert.equal(pts[0], "50.0,19.0")
  assert.deepEqual(remember([1, 2, 3], 4, 3), [2, 3, 4])
})
