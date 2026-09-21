import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { boardRegistration } from "./board-registration.ts"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { drawerEntries, pageReady } from "./registry.ts"

test("the Project Board is routable without becoming a drawer destination", () => {
  const modules = {
    board: { ...boardRegistration, Component: (() => null) as never },
  }
  const rows = [{ id: "projects" }, { id: "board" }]

  assert.equal(pageReady("board", false, modules), true)
  assert.deepEqual(drawerEntries(rows, modules), [{ id: "projects" }])
})
