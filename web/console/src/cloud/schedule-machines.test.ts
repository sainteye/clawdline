import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { fleetReaderMachine } from "./schedule-machines.ts"

const pilot = { id: "linux-pilot", online: false }
const mac = { id: "mac", online: true }

test("every-machine scope does not read through a machine that stopped reporting in while another is online", () => {
  // The roster lists the offline machine first, so it was the one being read.
  assert.equal(fleetReaderMachine("linux-pilot", [pilot, mac]), "mac")
})

test("a reading machine the roster no longer offers gives way to an online one", () => {
  assert.equal(fleetReaderMachine("forgotten", [pilot, mac]), "mac")
})

test("when the offline machine reports in again it is simply kept; nothing moves back and forth", () => {
  assert.equal(fleetReaderMachine("mac", [{ ...pilot, online: true }, mac]), "mac")
})

test("control: an online reading machine is kept", () => {
  assert.equal(fleetReaderMachine("mac", [pilot, mac]), "mac")
  assert.equal(fleetReaderMachine("linux-pilot", [{ ...pilot, online: true }, mac]), "linux-pilot")
})

test("with no machine online there is none better to read, so the reading one stays", () => {
  assert.equal(fleetReaderMachine("linux-pilot", [pilot, { ...mac, online: false }]), "linux-pilot")
  assert.equal(fleetReaderMachine(null, []), null)
})
