import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { carrierReading } from "./carrier-state.ts"

test("an open channel is the direct road, whatever else is known", () => {
  assert.equal(carrierReading({ known: true, supported: true, open: true }).show, "direct")
  // The descriptor can arrive after the channel this page opened.
  assert.equal(carrierReading({ known: false, supported: false, open: true }).show, "direct")
})

test("a machine this browser has not read yet is unknown, not relay", () => {
  const reading = carrierReading({ known: false, supported: false, open: false })
  assert.equal(reading.show, "unknown")
  assert.equal(reading.word, "cloudCarrierUnknown")
})

test("relay says which of the two reasons it is", () => {
  assert.deepEqual(carrierReading({ known: true, supported: false, open: false }),
    { show: "relay", word: "cloudCarrierRelayMachineOff" })
  assert.deepEqual(carrierReading({ known: true, supported: true, open: false }),
    { show: "relay", word: "cloudCarrierRelayNotOpen" })
})
