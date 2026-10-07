import assert from "node:assert/strict"
import test, { mock } from "node:test"
// @ts-expect-error -- Node strips TypeScript for this focused test.
import { beginTerminalClose, observeTerminalEnded, observeTerminalRunning, settleTerminalClose, terminalCloseState, TERMINAL_CLOSE_UNKNOWN_MS } from "./terminal-close-state.ts"

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

test("an unknown close resolves when a read finds the terminal still running", () => {
  beginTerminalClose("mac", "t3", "request-c")
  settleTerminalClose("mac", "t3", "request-c", "unknown", "terminal_receipt_timeout")
  observeTerminalRunning("mac", "t3")
  // No record left: the close button is offered again.
  assert.equal(terminalCloseState("mac", "t3"), null)
})

test("a running read does not erase a close that is still in flight or already confirmed", () => {
  beginTerminalClose("mac", "t4", "request-d")
  observeTerminalRunning("mac", "t4")
  assert.equal(terminalCloseState("mac", "t4")?.status, "pending")
  settleTerminalClose("mac", "t4", "request-d", "refused", "terminal_forbidden")
  observeTerminalRunning("mac", "t4")
  assert.equal(terminalCloseState("mac", "t4")?.status, "refused")
})

test("an unknown close that nothing resolves expires after a bounded time", () => {
  mock.timers.enable({ apis: ["setTimeout", "Date"] })
  try {
    beginTerminalClose("mac", "t5", "request-e")
    settleTerminalClose("mac", "t5", "request-e", "unknown", "terminal_receipt_timeout")
    mock.timers.tick(TERMINAL_CLOSE_UNKNOWN_MS - 1)
    assert.equal(terminalCloseState("mac", "t5")?.status, "unknown")
    mock.timers.tick(1)
    assert.equal(terminalCloseState("mac", "t5"), null)
  } finally { mock.timers.reset() }
})

test("an expiry armed for an earlier request does not clear a newer close", () => {
  mock.timers.enable({ apis: ["setTimeout", "Date"] })
  try {
    beginTerminalClose("mac", "t6", "request-f")
    settleTerminalClose("mac", "t6", "request-f", "unknown")
    mock.timers.tick(TERMINAL_CLOSE_UNKNOWN_MS / 2)
    beginTerminalClose("mac", "t6", "request-g")
    settleTerminalClose("mac", "t6", "request-g", "unknown")
    mock.timers.tick(TERMINAL_CLOSE_UNKNOWN_MS / 2)
    assert.equal(terminalCloseState("mac", "t6")?.requestID, "request-g")
    assert.equal(terminalCloseState("mac", "t6")?.status, "unknown")
  } finally { mock.timers.reset() }
})
