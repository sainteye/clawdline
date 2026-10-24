import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { startedRow } from "./started-row.ts"

test("a confirmed pane opens before inventory without inventing permission to close it", () => {
  const row = startedRow({ id: "%42", backend: "tmux", assistant: "codex", cwd: "/project" },
    { id: "project", label: "Project", path: "/project" })
  assert.equal(row?.id, "%42")
  assert.equal(row?.cwd, "/project")
  assert.equal(row?.closeability.state, "unknown")
  assert.equal(row?.closeability.version, "")
  assert.equal(row?.state, "unknown")
  assert.equal(row?.identity, "no_record")
})

test("a reply without a pane backend or assistant waits for the inventory", () => {
  assert.equal(startedRow({ id: "%42" }, null), null)
  assert.equal(startedRow({ backend: "tmux", assistant: "codex" }, null), null)
})

test("an Antigravity start keeps its provider without inventing conversation or close permission", () => {
  const row = startedRow({ id: "%fixture", backend: "tmux", assistant: "antigravity" }, null)
  assert.equal(row?.assistant, "antigravity")
  assert.equal(row?.isClaude, false)
  assert.equal(row?.identity, "no_record")
  assert.equal(row?.state, "unknown")
  assert.equal(row?.closeability.state, "unknown")
})
