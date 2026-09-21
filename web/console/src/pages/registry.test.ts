import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { drawerEntries, pageReady } from "./registry.ts"

// A page may have an address without being somewhere the drawer sends people:
// it is reached from inside another page. The Project Board was the one such
// page and has been removed, so the registration here is the test's own — the
// property outlived its only example, and deleting the test with the page
// would have taken the property's one guard with it.
test("a page can be routable without becoming a drawer destination", () => {
  const modules = {
    inner: { id: "inner", drawer: false, Component: (() => null) as never },
  }
  const rows = [{ id: "projects" }, { id: "inner" }]

  assert.equal(pageReady("inner", false, modules), true)
  assert.deepEqual(drawerEntries(rows, modules), [{ id: "projects" }])
})
