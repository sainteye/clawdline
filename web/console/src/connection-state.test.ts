import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { connectionLightState, connectionLightWords } from "./connection-state.ts"

test("an open relay stream is not a live machine until machine health answers", () => {
  assert.equal(connectionLightState(true, false, false), "connecting")
  assert.equal(connectionLightState(true, true, false), "live")
  assert.equal(connectionLightState(true, false, true), "offline")
})

test("health without the event stream is still reconnecting", () => {
  assert.equal(connectionLightState(false, true, false), "retrying")
})

test("the light's word and tip follow its state, and the version only when live", () => {
  const words = {
    webConnLive: "live",
    webConnConnecting: "connecting",
    webConnOffline: "offline",
    webConnTipLive: "Streaming from the app",
    webConnTipDown: "Not connected — click to try now",
  }
  assert.deepEqual(connectionLightWords("live", words, "1.2"), { label: "live", tip: "Streaming from the app · 1.2" })
  assert.deepEqual(connectionLightWords("live", words, undefined), { label: "live", tip: "Streaming from the app" })
  assert.deepEqual(connectionLightWords("retrying", words, "1.2"), {
    label: "connecting",
    tip: "Not connected — click to try now",
  })
  assert.deepEqual(connectionLightWords("offline", words, null).label, "offline")
  // A catalog still loading has no word yet; the state names itself.
  assert.deepEqual(connectionLightWords("connecting", {}, null).label, "connecting")
})
