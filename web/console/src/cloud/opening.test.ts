import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- node's type-stripping runner resolves the source .ts file.
import { openingFor } from "./opening.ts"

const two = [{ id: "mac", selectable: true }, { id: "linux", selectable: true }]
const one = [{ id: "mac", selectable: true }]

test("a tab that has chosen nothing opens on every machine", () => {
  assert.deepEqual(openingFor({ machines: two }), { at: "console", machine: "mac", fleet: true, remember: false })
})

test("one readable machine still asks, because its row is where pairing and its name are", () => {
  assert.deepEqual(openingFor({ machines: one }), { at: "list" })
  assert.deepEqual(openingFor({ machines: [{ id: "mac", selectable: false }] }), { at: "list" })
  assert.deepEqual(openingFor({ machines: [] }), { at: "list" })
})

test("a machine this tab chose is the machine it opens, on its own", () => {
  assert.deepEqual(openingFor({ machines: two, remembered: "linux" }),
    { at: "console", machine: "linux", fleet: false, remember: true })
  assert.deepEqual(openingFor({ machines: one, remembered: "mac" }),
    { at: "console", machine: "mac", fleet: false, remember: true })
})

test("a choice the account no longer lists is not a choice", () => {
  assert.deepEqual(openingFor({ machines: two, remembered: "gone" }),
    { at: "console", machine: "mac", fleet: true, remember: false })
  assert.deepEqual(openingFor({ machines: one, remembered: "gone" }), { at: "list" })
})

test("the fleet address opens the fleet, reading the chosen machine underneath", () => {
  assert.deepEqual(openingFor({ machines: two, everyMachineAddressed: true, remembered: "linux" }),
    { at: "console", machine: "linux", fleet: true, remember: false })
  assert.deepEqual(openingFor({ machines: two, everyMachineAddressed: true }),
    { at: "console", machine: "mac", fleet: true, remember: false })
})

test("the fleet address with one machine to read is that machine, not a fleet of one", () => {
  assert.deepEqual(openingFor({ machines: one, everyMachineAddressed: true, remembered: "mac" }),
    { at: "console", machine: "mac", fleet: false, remember: true })
  assert.deepEqual(openingFor({ machines: one, everyMachineAddressed: true }), { at: "list" })
})

test("an addressed machine beats both the choice and the fleet default", () => {
  assert.deepEqual(openingFor({ machines: two, addressed: { machine: "linux", inFleet: true }, remembered: "mac" }),
    { at: "console", machine: "linux", fleet: true, remember: false })
  assert.deepEqual(openingFor({ machines: one, addressed: { machine: "mac", inFleet: true } }),
    { at: "console", machine: "mac", fleet: false, remember: true })
})

test("an address the fleet does not hold opens that machine's own page", () => {
  // A document link: the fleet would show the Session list over the document.
  assert.deepEqual(openingFor({ machines: two, addressed: { machine: "linux" }, remembered: "mac" }),
    { at: "console", machine: "linux", fleet: false, remember: true })
})

test("an address naming a machine this browser cannot read waits for it", () => {
  assert.deepEqual(openingFor({ machines: two, addressed: { machine: "other", inFleet: true }, remembered: "mac" }),
    { at: "list" })
  assert.deepEqual(openingFor({ machines: [...two, { id: "other", selectable: false }],
    addressed: { machine: "other", inFleet: true } }), { at: "list" })
})

/** Only an opening that reached a console says whether to write the machine down. */
function remembers(opening: ReturnType<typeof openingFor>): boolean {
  assert.equal(opening.at, "console")
  return opening.at === "console" && opening.remember
}

test("the machine under the fleet is this file's pick, not a choice to write down", () => {
  // Recording it made the fleet last one page load: the reload found a
  // remembered machine and opened that machine on its own.
  assert.equal(remembers(openingFor({ machines: two })), false)
  assert.equal(remembers(openingFor({ machines: two, everyMachineAddressed: true })), false)
  assert.equal(remembers(openingFor({ machines: two, everyMachineAddressed: true, remembered: "linux" })), false)
  assert.equal(remembers(openingFor({ machines: two, addressed: { machine: "linux", inFleet: true } })), false)
})

test("a machine opened on its own is the one to come back to", () => {
  assert.equal(remembers(openingFor({ machines: two, remembered: "linux" })), true)
  assert.equal(remembers(openingFor({ machines: one, addressed: { machine: "mac", inFleet: true } })), true)
  // A document address is one machine's page, and coming back to it is right.
  assert.equal(remembers(openingFor({ machines: two, addressed: { machine: "linux" } })), true)
})
