import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- node's type-stripping runner resolves the source .ts file.
import { openingFor } from "./opening.ts"

const two = [{ id: "mac", selectable: true }, { id: "linux", selectable: true }]
const one = [{ id: "mac", selectable: true }]

test("a tab that has chosen nothing opens on every machine", () => {
  assert.deepEqual(openingFor({ machines: two }), { at: "console", machine: "mac", fleet: true })
})

test("one readable machine still asks, because its row is where pairing and its name are", () => {
  assert.deepEqual(openingFor({ machines: one }), { at: "list" })
  assert.deepEqual(openingFor({ machines: [{ id: "mac", selectable: false }] }), { at: "list" })
  assert.deepEqual(openingFor({ machines: [] }), { at: "list" })
})

test("a machine this tab chose is the machine it opens, on its own", () => {
  assert.deepEqual(openingFor({ machines: two, remembered: "linux" }),
    { at: "console", machine: "linux", fleet: false })
  assert.deepEqual(openingFor({ machines: one, remembered: "mac" }),
    { at: "console", machine: "mac", fleet: false })
})

test("a choice the account no longer lists is not a choice", () => {
  assert.deepEqual(openingFor({ machines: two, remembered: "gone" }),
    { at: "console", machine: "mac", fleet: true })
  assert.deepEqual(openingFor({ machines: one, remembered: "gone" }), { at: "list" })
})

test("the fleet address opens the fleet, reading the chosen machine underneath", () => {
  assert.deepEqual(openingFor({ machines: two, everyMachineAddressed: true, remembered: "linux" }),
    { at: "console", machine: "linux", fleet: true })
  assert.deepEqual(openingFor({ machines: two, everyMachineAddressed: true }),
    { at: "console", machine: "mac", fleet: true })
})

test("the fleet address with one machine to read is that machine, not a fleet of one", () => {
  assert.deepEqual(openingFor({ machines: one, everyMachineAddressed: true, remembered: "mac" }),
    { at: "console", machine: "mac", fleet: false })
  assert.deepEqual(openingFor({ machines: one, everyMachineAddressed: true }), { at: "list" })
})

test("an addressed machine beats both the choice and the fleet default", () => {
  assert.deepEqual(openingFor({ machines: two, addressed: { machine: "linux", inFleet: true }, remembered: "mac" }),
    { at: "console", machine: "linux", fleet: true })
  assert.deepEqual(openingFor({ machines: one, addressed: { machine: "mac", inFleet: true } }),
    { at: "console", machine: "mac", fleet: false })
})

test("an address the fleet does not hold opens that machine's own page", () => {
  // A document link: the fleet would show the Session list over the document.
  assert.deepEqual(openingFor({ machines: two, addressed: { machine: "linux" }, remembered: "mac" }),
    { at: "console", machine: "linux", fleet: false })
})

test("an address naming a machine this browser cannot read waits for it", () => {
  assert.deepEqual(openingFor({ machines: two, addressed: { machine: "other", inFleet: true }, remembered: "mac" }),
    { at: "list" })
  assert.deepEqual(openingFor({ machines: [...two, { id: "other", selectable: false }],
    addressed: { machine: "other", inFleet: true } }), { at: "list" })
})
