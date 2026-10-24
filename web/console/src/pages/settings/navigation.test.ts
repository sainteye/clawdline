import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { nextSettingsTab, settingsPanes } from "./navigation.ts"

test("local settings expose each machine pane beside browser, work and status", () => {
  assert.deepEqual(settingsPanes(false), ["browser", "work", "status", "workflow", "general", "voice", "remote", "orchestrator", "dispatch-policy"])
})

test("Cloud navigation exposes no machine settings", () => {
  assert.deepEqual(settingsPanes(true), ["browser", "work", "status", "workflow"])
})

test("keyboard navigation reaches every pane and wraps in both transports", () => {
  for (const cloud of [true, false]) {
    const panes = settingsPanes(cloud)
    let index = 0
    const visited = []
    for (let i = 0; i < panes.length; i++) {
      visited.push(panes[index])
      index = nextSettingsTab(index, "ArrowRight", panes.length)!
    }
    assert.deepEqual(visited, panes)
    assert.equal(index, 0)
    assert.equal(nextSettingsTab(0, "ArrowLeft", panes.length), panes.length - 1)
    assert.equal(nextSettingsTab(1, "Home", panes.length), 0)
    assert.equal(nextSettingsTab(0, "End", panes.length), panes.length - 1)
    assert.equal(nextSettingsTab(0, "Tab", panes.length), null)
  }
})
