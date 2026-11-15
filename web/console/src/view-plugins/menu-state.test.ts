import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node executes source extensions.
import { nextMenuState } from "./menu-state.ts"
test("Remove asks before deleting: its first press only moves the menu to the question", () => {
  assert.equal(nextMenuState("closed", "toggle"), "open")
  assert.equal(nextMenuState("open", "remove"), "confirm")
  assert.equal(nextMenuState("confirm", "remove"), "confirm")
  assert.equal(nextMenuState("closed", "remove"), "closed")
})
test("Escape, an outside tap or the Manage button closes the menu and drops a pending question", () => {
  assert.equal(nextMenuState("open", "dismiss"), "closed")
  assert.equal(nextMenuState("confirm", "dismiss"), "closed")
  assert.equal(nextMenuState("open", "toggle"), "closed")
})
