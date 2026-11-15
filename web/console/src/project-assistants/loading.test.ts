import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node executes source extensions.
import { memoryOpener, showAfter, SkeletonDelayMs } from "./loading.ts"

test("a placeholder appears only after the wait has lasted 150ms", t => {
  t.mock.timers.enable({ apis: ["setTimeout"] })
  const seen: boolean[] = []
  const gate = showAfter(SkeletonDelayMs, shown => seen.push(shown))
  gate.set(true)
  t.mock.timers.tick(149)
  assert.deepEqual(seen, [])
  t.mock.timers.tick(1)
  assert.deepEqual(seen, [true])
  gate.set(false)
  assert.deepEqual(seen, [true, false])
})

test("a read that answers inside 150ms never shows a placeholder", t => {
  t.mock.timers.enable({ apis: ["setTimeout"] })
  const seen: boolean[] = []
  const gate = showAfter(SkeletonDelayMs, shown => seen.push(shown))
  gate.set(true)
  t.mock.timers.tick(100)
  gate.set(false)
  t.mock.timers.tick(500)
  assert.deepEqual(seen, [false])
  gate.set(true)
  gate.stop()
  t.mock.timers.tick(500)
  assert.deepEqual(seen, [false])
})

const deferred = () => {
  let resolve!: () => void, reject!: (e: unknown) => void
  const promise = new Promise<void>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
const panel = (reads: ReturnType<typeof deferred>[]) => {
  const state = { open: false, loading: false, failures: [] as unknown[] }
  const opener = memoryOpener(() => reads.shift()!.promise, {
    open: v => { state.open = v }, loading: v => { state.loading = v }, fail: e => { state.failures.push(e) },
  })
  return { state, opener }
}

test("memory opens at once while loading and settles to its content", async () => {
  const read = deferred(), { state, opener } = panel([read])
  const done = opener.open()
  assert.deepEqual([state.open, state.loading], [true, true])
  read.resolve(); await done
  assert.deepEqual([state.open, state.loading, state.failures.length], [true, false, 0])
})

test("a failed memory read closes the panel onto the error line, never a stuck placeholder", async () => {
  const read = deferred(), { state, opener } = panel([read])
  const done = opener.open()
  read.reject(new Error("unavailable")); await done
  assert.deepEqual([state.open, state.loading, state.failures.length], [false, false, 1])
})

test("closing memory while it loads keeps it closed when the read answers", async () => {
  const first = deferred(), failed = deferred(), { state, opener } = panel([first, failed])
  const done = opener.open()
  opener.close()
  first.resolve(); await done
  assert.deepEqual([state.open, state.loading], [false, false])
  const again = opener.open()
  opener.close()
  failed.reject(new Error("late")); await again
  assert.deepEqual([state.open, state.loading, state.failures.length], [false, false, 0])
})

test("only the latest opening settles the memory panel", async () => {
  const older = deferred(), newer = deferred(), { state, opener } = panel([older, newer])
  const first = opener.open()
  opener.close()
  const second = opener.open()
  older.resolve(); await first
  assert.deepEqual([state.open, state.loading], [true, true])
  newer.resolve(); await second
  assert.deepEqual([state.open, state.loading], [true, false])
})
