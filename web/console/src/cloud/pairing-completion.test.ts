import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node strips types from this source file in the test.
import { machinePaired, watchMachinePaired } from "./pairing-completion.ts"

test("a pairing completion names its machine and stops notifying an unmounted composer", () => {
  const seen: string[] = []
  const stop = watchMachinePaired((machine) => seen.push(machine))
  machinePaired("machine-two")
  machinePaired("machine-one")
  assert.deepEqual(seen, ["machine-two", "machine-one"])
  stop()
  machinePaired("machine-one")
  assert.equal(seen.length, 2)
})
