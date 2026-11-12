import { test } from "node:test"
import assert from "node:assert/strict"

// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { HOLD_MS, MOVE_PX, HoldGesture } from "./hold-gesture.ts"

/** A clock the test moves by hand, and an icon that records what was asked of it. */
function rig() {
  let now = 1000
  let pending: { at: number; run: () => void } | null = null
  const did: string[] = []
  const gesture = new HoldGesture(
    { call: () => { did.push("call") }, held: () => { did.push("held") } },
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
  const click = () => { if (gesture.tap()) did.push("snippets") }
  return { gesture, did, wait, click }
}

test("a tap on the icon does its ordinary job and places no call", () => {
  const { gesture, did, wait, click } = rig()
  gesture.down(10, 10)
  wait(120)
  gesture.up()
  click()
  assert.deepEqual(did, ["snippets"])
})

test("a hold calls on the lift, and the click the lift produces does not open Snippets", () => {
  const { gesture, did, wait, click } = rig()
  gesture.down(10, 10)
  wait(HOLD_MS)
  assert.deepEqual(did, ["held"])
  gesture.up()
  click()
  assert.deepEqual(did, ["held", "call"])
  wait(1000)
  click()
  assert.deepEqual(did, ["held", "call", "snippets"], "the next real tap opens Snippets again")
})

test("a hold whose click never arrives does not swallow a later tap", () => {
  const { gesture, did, wait, click } = rig()
  gesture.down(10, 10)
  wait(HOLD_MS)
  gesture.up()
  wait(2000)
  click()
  assert.deepEqual(did, ["held", "call", "snippets"])
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
