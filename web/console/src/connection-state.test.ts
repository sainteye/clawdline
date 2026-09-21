import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { connectionLightState } from "./connection-state.ts"

test("an open relay stream is not a live machine until machine health answers", () => {
  assert.equal(connectionLightState(true, false, false), "connecting")
  assert.equal(connectionLightState(true, true, false), "live")
  assert.equal(connectionLightState(true, false, true), "offline")
})

test("health without the event stream is still reconnecting", () => {
  assert.equal(connectionLightState(false, true, false), "retrying")
})
