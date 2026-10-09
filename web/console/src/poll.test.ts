import test from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { Poller, type PollState } from "./poll.ts"

/** A poller whose reads are settled by hand and whose timer is one slot. */
function harness() {
  const reads: { resolve: (v: number) => void; reject: (e: unknown) => void }[] = []
  let timer: (() => void) | null = null
  let timersSet = 0
  let state: PollState<number> | null = null
  const poller = new Poller<number>({
    read: () => new Promise<number>((resolve, reject) => reads.push({ resolve, reject })),
    intervalMs: 4000,
    classify: () => "unanswered",
    onChange: (s) => (state = s),
    now: () => 1000,
    setTimer: (fn) => {
      timersSet++
      timer = fn
      return fn
    },
    clearTimer: () => (timer = null),
  })
  const settle = () => new Promise((r) => setImmediate(r))
  return {
    poller,
    reads,
    settle,
    state: () => state!,
    timer: () => timer,
    timersSet: () => timersSet,
  }
}

test("a retry while a read is out does not start a second read", async () => {
  const h = harness()
  h.poller.start()
  assert.equal(h.reads.length, 1)
  assert.equal(h.state().reading, true)
  h.poller.retry()
  h.poller.retry()
  assert.equal(h.reads.length, 1, "one read out, however often retry is pressed")
  h.reads[0].reject(new Error("timeout"))
  await h.settle()
  assert.equal(h.state().reading, false)
  assert.equal(h.state().failures, 1)
  assert.equal(h.state().failingSince, 1000)
  h.poller.stop()
})

test("a retry cancels the scheduled read and asks now", async () => {
  const h = harness()
  h.poller.start()
  h.reads[0].reject(new Error("timeout"))
  await h.settle()
  assert.ok(h.timer(), "the next read is scheduled")
  h.poller.retry()
  assert.equal(h.timer(), null, "and cancelled by the retry")
  assert.equal(h.reads.length, 2)
  h.reads[1].resolve(7)
  await h.settle()
  assert.equal(h.state().data, 7)
  assert.equal(h.state().failures, 0, "a success ends the run")
  assert.equal(h.state().failingSince, null)
  assert.equal(h.state().failureKind, null)
  assert.equal(h.timersSet(), 2, "one timer per settled read, none left over")
  h.poller.stop()
})

test("a stopped poller neither reads on retry nor schedules", async () => {
  const h = harness()
  h.poller.start()
  h.poller.stop()
  h.reads[0].resolve(1)
  await h.settle()
  assert.equal(h.timer(), null)
  h.poller.retry()
  assert.equal(h.reads.length, 1)
})

/** The harness with a page that can be hidden, and the waits it asked for. */
function visibleHarness(options: { backoff?: { firstMs: number; maxMs: number } } = {}) {
  const reads: { why: string; resolve: (v: number) => void; reject: (e: unknown) => void }[] = []
  let timer: (() => void) | null = null
  const waits: number[] = []
  let hidden = false
  const heard = new Set<() => void>()
  let state: PollState<number> | null = null
  const poller = new Poller<number>({
    read: (why) => new Promise<number>((resolve, reject) => reads.push({ why, resolve, reject })),
    intervalMs: 30000,
    classify: () => "unanswered",
    onChange: (s) => (state = s),
    visibility: {
      hidden: () => hidden,
      subscribe(fn) {
        heard.add(fn)
        return () => heard.delete(fn)
      },
    },
    backoff: options.backoff,
    setTimer: (fn, ms) => {
      waits.push(ms)
      timer = fn
      return fn
    },
    clearTimer: () => (timer = null),
  })
  return {
    poller,
    reads,
    waits,
    heard,
    state: () => state!,
    timer: () => timer,
    setHidden(next: boolean) {
      hidden = next
      for (const fn of heard) fn()
    },
    /** Hidden without the event: the timer can still fire first. */
    setHiddenQuietly(next: boolean) {
      hidden = next
    },
    settle: () => new Promise((r) => setImmediate(r)),
  }
}

test("nothing is read while the page is hidden, and one read is made when it is seen", async () => {
  const h = visibleHarness()
  h.poller.start()
  h.reads[0].resolve(1)
  await h.settle()
  h.setHidden(true)
  assert.equal(h.timer(), null, "the scheduled read is not left running for a hidden page")
  h.setHidden(false)
  assert.equal(h.reads.length, 2, "one read on coming back")
  assert.equal(h.reads[1].why, "visible")
  h.setHidden(false)
  assert.equal(h.reads.length, 2, "and not one per event")
  h.poller.stop()
  assert.equal(h.heard.size, 0, "stop stops listening")
})

test("a read that comes due while hidden waits for the page to be seen", async () => {
  const h = visibleHarness()
  h.poller.start()
  h.reads[0].resolve(1)
  await h.settle()
  h.setHiddenQuietly(true)
  h.timer()!()
  assert.equal(h.reads.length, 1, "a tick due while hidden is not read")
  h.setHidden(false)
  assert.equal(h.reads.length, 2, "it is read when the page is seen")
  assert.equal(h.reads[1].why, "visible")
  h.poller.stop()
})

test("a poller started on a hidden page reads nothing until it is seen", () => {
  const h = visibleHarness()
  h.setHidden(true)
  h.poller.start()
  assert.equal(h.reads.length, 0)
  h.setHidden(false)
  assert.equal(h.reads.length, 1)
  assert.equal(h.reads[0].why, "visible")
  h.poller.stop()
})

test("a poke during a read reads once more when it settles; two pokes are still one", async () => {
  const h = visibleHarness()
  h.poller.start()
  h.poller.poke()
  h.poller.poke()
  assert.equal(h.reads.length, 1)
  h.reads[0].resolve(1)
  await h.settle()
  assert.equal(h.reads.length, 2, "the change it was poked for is read")
  assert.equal(h.reads[1].why, "poke")
  h.reads[1].resolve(2)
  await h.settle()
  assert.equal(h.reads.length, 2)
  assert.ok(h.timer(), "and the interval resumes after it")
  h.poller.poke()
  assert.equal(h.reads.length, 3, "a poke between reads reads now")
  assert.equal(h.timer(), null, "replacing the scheduled read")
  h.poller.stop()
})

test("failed reads back off 2, 4, 8 seconds up to 30, and a success returns to the interval", async () => {
  const h = visibleHarness({ backoff: { firstMs: 2000, maxMs: 30000 } })
  h.poller.start()
  for (let i = 0; i < 6; i++) {
    h.reads[i].reject(new Error("timeout"))
    await h.settle()
    h.timer()!()
  }
  assert.deepEqual(h.waits, [2000, 4000, 8000, 16000, 30000, 30000])
  h.reads[6].resolve(1)
  await h.settle()
  assert.equal(h.waits.at(-1), 30000)
  h.poller.setIntervalMs(2000)
  assert.equal(h.reads.length, 7, "a new pace is not a read of its own")
  assert.equal(h.waits.at(-1), 2000, "it moves the scheduled read")
  h.poller.stop()
})
