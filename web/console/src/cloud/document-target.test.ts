import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node strips the TypeScript source for this repository's tests.
import { machineForAddress } from "./document-target.ts"

test("a complete document link selects its machine before a remembered choice", () => {
  const link = "#document=1&machine=mac-a&session=pane-1&scope=project&path=demo.md"
  assert.equal(machineForAddress(link, "mac-b"), "mac-a")
  assert.equal(machineForAddress("#document=1&machine=mac-a&session=pane-1&scope=project&path=../secret.md", "mac-b"), "mac-b")
  assert.equal(machineForAddress("#session=pane-1", "mac-b"), "mac-b")
})
