import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- Node strips TypeScript for this focused test.
import { beginTerminalClose, observeTerminalEnded, settleTerminalClose, terminalCloseState } from "./terminal-close-state.ts"

test("a close outcome follows its exact machine, terminal, and request", () => {
  beginTerminalClose("mac", "t1", "request-a")
  assert.equal(terminalCloseState("mac", "t1")?.status, "pending")
  assert.equal(terminalCloseState("linux", "t1"), null)
  assert.equal(settleTerminalClose("mac", "t1", "another-request", "ok"), false)
  assert.equal(terminalCloseState("mac", "t1")?.status, "pending")
  assert.equal(settleTerminalClose("mac", "t1", "request-a", "unknown"), true)
  assert.equal(terminalCloseState("mac", "t1")?.status, "unknown")
  observeTerminalEnded("mac", "t1")
  assert.equal(terminalCloseState("mac", "t1")?.status, "ended")
})

test("a verified close receipt is not downgraded by a later absent read", () => {
  beginTerminalClose("linux", "t2", "request-b")
  settleTerminalClose("linux", "t2", "request-b", "ok")
  observeTerminalEnded("linux", "t2")
  assert.equal(terminalCloseState("linux", "t2")?.status, "ok")
})
