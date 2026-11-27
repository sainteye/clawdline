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

const deferred = <T = void>() => {
  let resolve!: (value: T) => void, reject!: (e: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
const panel = <T>(reads: ReturnType<typeof deferred<T>>[]) => {
  const state = { open: false, loading: false, values: [] as T[], failures: [] as unknown[] }
  const opener = memoryOpener(() => reads.shift()!.promise, {
    open: v => { state.open = v }, loading: v => { state.loading = v }, fail: e => { state.failures.push(e) },
    loaded: value => { state.values.push(value) },
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

test("a failed reread keeps the already open memory panel available for retry", async () => {
  const initial = deferred(), reload = deferred(), retry = deferred()
  const { state, opener } = panel([initial, reload, retry])
  const opened = opener.open()
  initial.resolve(); await opened
  const refreshed = opener.open()
  const error = new Error("read deadline")
  reload.reject(error); await refreshed
  assert.equal(state.open, true, "the failed reread keeps the existing memory visible")
  assert.deepEqual(state.failures, [error])
  const retried = opener.open()
  retry.resolve(); await retried
  assert.deepEqual([state.open, state.loading], [true, false])
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

test("late memory answers cannot clear a newer failure or report a failure after recovery", async () => {
  const [initial, staleSuccess, latestFailure, staleFailure, latestSuccess] = Array.from({length: 5}, () => deferred<number>())
  const { state, opener } = panel([initial, staleSuccess, latestFailure, staleFailure, latestSuccess])
  const opened = opener.open()
  initial.resolve(1); await opened

  const older = opener.open(), newer = opener.open()
  const error = new Error("latest failure")
  latestFailure.reject(error); await newer
  staleSuccess.resolve(2); await older
  assert.deepEqual(state.values, [1])
  assert.deepEqual(state.failures, [error])

  const pending = opener.open(), retry = opener.open()
  latestSuccess.resolve(3); await retry
  staleFailure.reject(new Error("late failure")); await pending
  assert.deepEqual(state.values, [1, 3])
  assert.deepEqual(state.failures, [error])
})

test("refresh within the memory panel reports its own error and can still publish a successful read", async () => {
  const initial = deferred<number>(), refused = deferred<number>(), retry = deferred<number>()
  const {state, opener} = panel([initial, refused, retry])
  const opened = opener.open()
  initial.resolve(1); await opened
  const refresh = opener.refresh(), error = new Error("unavailable")
  refused.reject(error)
  await assert.rejects(refresh, error)
  assert.deepEqual(state.failures, [])
  const refreshed = opener.refresh()
  retry.resolve(2); await refreshed
  assert.deepEqual(state.values, [1, 2])
})
