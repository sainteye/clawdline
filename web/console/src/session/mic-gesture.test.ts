import { test } from "node:test"
import assert from "node:assert/strict"

// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { DOUBLE_MS, HOLD_MS, MOVE_PX, MicGesture } from "./mic-gesture.ts"

/** A clock the test moves by hand, and a microphone that records what was asked of it. */
function rig(starts = true) {
  let now = 1000
  let pending: { at: number; run: () => void } | null = null
  const did: string[] = []
  const gesture = new MicGesture(
    {
      dictate: () => { did.push("dictate"); return starts },
      undo: () => { did.push("undo") },
      call: () => { did.push("call") },
      held: () => { did.push("held") },
    },
    {
      now: () => now,
      after: (ms: number, run: () => void) => { pending = { at: now + ms, run }; return pending },
      cancel: (h: unknown) => { if (pending === h) pending = null },
    },
  )
  const wait = (ms: number) => {
    now += ms
    if (pending && pending.at <= now) { const p = pending; pending = null; p.run() }
  }
  return { gesture, did, wait }
}

test("a tap dictates at once and nothing else", () => {
  const { gesture, did, wait } = rig()
  gesture.down(10, 10)
  wait(80)
  gesture.up()
  gesture.click()
  wait(DOUBLE_MS + 1)
  assert.deepEqual(did, ["dictate"])
})

test("a double press takes the dictation back and calls", () => {
  const { gesture, did, wait } = rig()
  gesture.click()
  wait(DOUBLE_MS - 50)
  gesture.click()
  assert.deepEqual(did, ["dictate", "undo", "call"])
})

test("two presses further apart than a double press are two dictation presses", () => {
  const { gesture, did, wait } = rig()
  gesture.click()
  wait(DOUBLE_MS + 1)
  gesture.click()
  assert.deepEqual(did, ["dictate", "dictate"])
})

test("a press that stopped a recording does not start a double press, so the words are kept", () => {
  const { gesture, did, wait } = rig(false)
  gesture.click()
  wait(100)
  gesture.click()
  assert.deepEqual(did, ["dictate", "dictate"])
})

test("a hold calls on the lift, and the click the lift produces does not dictate", () => {
  const { gesture, did, wait } = rig()
  gesture.down(10, 10)
  wait(HOLD_MS)
  assert.deepEqual(did, ["held"])
  gesture.up()
  gesture.click()
  assert.deepEqual(did, ["held", "call"])
  wait(1000)
  gesture.click()
  assert.deepEqual(did, ["held", "call", "dictate"], "the next real tap dictates again")
})

test("a hold whose click never arrives does not swallow a later tap", () => {
  const { gesture, did, wait } = rig()
  gesture.down(10, 10)
  wait(HOLD_MS)
  gesture.up()
  wait(2000)
  gesture.click()
  assert.deepEqual(did, ["held", "call", "dictate"])
})

test("a finger that slides away, or a press taken by the system, is not a hold", () => {
  const { gesture, did, wait } = rig()
  gesture.down(10, 10)
  gesture.move(10 + MOVE_PX + 1, 10)
  wait(HOLD_MS)
  gesture.up()
  gesture.down(10, 10)
  gesture.abort()
  wait(HOLD_MS)
  gesture.up()
  assert.deepEqual(did, [])
})
