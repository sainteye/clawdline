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
