import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { acquireVisibleTerminal, beginCloudTerminal, cloudTerminalBody, cloudTerminalPhase, keyRefusalControl, keyRefusalNotice, reconnectCloudTerminal, shownKeyRefusal, type CloudTerminalStarter } from "./cloud-view.ts"

// `cloud/terminal-session.ts` uses constructor parameter properties, which
// Node's strip-types runner refuses, so the start path is driven through a fake
// session that records every operation it would put on the channel.
class CountingSession implements CloudTerminalStarter {
  operations: string[] = []
  projects: unknown[] = []
  async start(): Promise<void> { this.operations.push("open_connection") }
  async reconnect(terminal: string): Promise<void> { this.operations.push("rekey_connection", `read:${terminal}`, "capture") }
  async attach(terminal: string): Promise<{ result?: Record<string, unknown> }> {
    this.operations.push("read", "capture")
    return { result: { id: terminal, status: "running" } }
  }
  async request(operation: string, fields: Record<string, unknown> = {}): Promise<{ result?: Record<string, unknown> }> {
    this.operations.push(operation)
    if ("project_id" in fields) this.projects.push(fields.project_id)
    return { result: operation === "list" ? { terminals: [] } : {} }
  }
}

test("with no terminal host the page shows the offline note, never a live terminal", () => {
  assert.equal(cloudTerminalBody(null, "p-local-1", "trm_one"), "offline")
  assert.equal(cloudTerminalBody(null, "p-local-1", ""), "offline")
  assert.equal(cloudTerminalBody({}, "", "trm_one"), "no_project")
  assert.equal(cloudTerminalBody({}, "p-local-1", ""), "list")
  assert.equal(cloudTerminalBody({}, "p-local-1", "trm_one"), "terminal")
})

test("listing asks the channel by the machine-local Project id", async () => {
  const session = new CountingSession()
  assert.deepEqual(await beginCloudTerminal(session, "p-local-1", "", "tab-view"), { rows: [] })
  assert.deepEqual(session.projects, ["p-local-1"])
  assert.deepEqual(session.operations, ["open_connection", "list"])
})

test("a new session after a host change reads or lists, and never sends open or input", async () => {
  // The session before the host change opened a terminal and typed into it.
  const before = new CountingSession()
  await before.start()
  await before.request("open", { project_id: "p-local-1", body: { cols: 80, rows: 24 } })
  await before.request("input", { terminal_id: "trm_one", seq: 1 })

  // The page starts a new session for the returned host, on the terminal and on the list.
  for (const id of ["trm_one", ""]) {
    const after = new CountingSession()
    await beginCloudTerminal(after, "p-local-1", id, "tab-view")
    assert.ok(!after.operations.includes("open"), after.operations.join(","))
    assert.ok(!after.operations.includes("input"), after.operations.join(","))
    assert.ok(!after.operations.includes("paste"), after.operations.join(","))
  }
  assert.deepEqual(before.operations, ["open_connection", "open", "input"], "nothing was added to the old session")
})

test("reconnect reads and captures again without replaying open or input", async () => {
  const session = new CountingSession()
  await reconnectCloudTerminal(session, "trm_one")
  assert.deepEqual(session.operations, ["rekey_connection", "read:trm_one", "capture"])
})

test("entry acquires a free terminal or this tab's old lease, but never another viewer's lease or uncertain input", async () => {
  const calls: string[] = []
  let unknown = false
  const session = {
    async request() { calls.push("probe"); return { result: { input_state_unknown: unknown } } },
    async acquire() { calls.push("acquire") },
  }
  const free = { held: false, epoch: 0 }
  const mine = { held: true, epoch: 1, holder: { same_client: true, same_device: true, local: false, name: "this tab" } }
  const other = { held: true, epoch: 1, holder: { same_client: false, same_device: false, local: false, name: "other" } }
  assert.equal(await acquireVisibleTerminal(session, "trm_one", "tab", free, "live"), "acquired")
  assert.deepEqual(calls, ["acquire"])
  calls.length = 0
  assert.equal(await acquireVisibleTerminal(session, "trm_one", "tab", mine, "live"), "acquired")
  assert.deepEqual(calls, ["probe", "acquire"])
  calls.length = 0; unknown = true
  assert.equal(await acquireVisibleTerminal(session, "trm_one", "tab", mine, "live"), "needs_review")
  assert.deepEqual(calls, ["probe"])
  calls.length = 0
  assert.equal(await acquireVisibleTerminal(session, "trm_one", "tab", other, "live"), "other_holder")
  assert.equal(await acquireVisibleTerminal(session, "trm_one", "tab", mine, "unknown"), "unavailable")
  assert.deepEqual(calls, [])
})

test("every refused key gets a notice beside the terminal with the way back to typing", () => {
  const free = { held: false, epoch: 3, expires_at: 0 }
  const mine = { held: true, epoch: 3, expires_at: 0, holder: { name: "tab", local: false, same_device: true, same_client: true } }
  const other = { ...mine, holder: { name: "phone", local: false, same_device: false, same_client: false } }
  assert.deepEqual(keyRefusalNotice("not_controller", free, "live"), { kind: "no_control", code: "not_controller", action: "acquire" })
  assert.deepEqual(keyRefusalNotice("not_controller", null, "live"), { kind: "no_control", code: "not_controller", action: "acquire" })
  assert.deepEqual(keyRefusalNotice("not_controller", other, "live"), { kind: "other_holder", code: "not_controller", action: "takeover" })
  assert.deepEqual(keyRefusalNotice("lease_expired", mine, "live"), { kind: "no_control", code: "lease_expired", action: "reacquire" })
  assert.deepEqual(keyRefusalNotice("terminal_input_state_unknown", mine, "unknown"),
    { kind: "unknown", code: "terminal_input_state_unknown", action: "reacquire" })
  assert.equal(keyRefusalNotice("terminal_stale", mine, "stale").action, "reconnect")
  assert.equal(keyRefusalNotice("machine_offline", mine, "offline").action, "reconnect")
  for (const code of ["terminal_input_paused", "input_too_large", "terminal_receipt_timeout", "terminal_access_revoked", "terminal_closed"])
    assert.deepEqual(keyRefusalNotice(code, mine, "live"), { kind: "other", code, action: null }, code)
})

test("a refused key's notice goes away once control changes", () => {
  const other = { held: true, epoch: 3, expires_at: 0, holder: { name: "", local: false, same_device: true, same_client: false } }
  const mine = { ...other, epoch: 4, holder: { ...other.holder, same_client: true } }
  const refusal = { ...keyRefusalNotice("not_controller", other, "live"), why: "", control: keyRefusalControl(other) }
  assert.equal(shownKeyRefusal(refusal, other), refusal)
  assert.equal(shownKeyRefusal(refusal, { ...other, expires_at: 99 }), refusal, "a renewal is not a change of holder")
  assert.equal(shownKeyRefusal(refusal, mine), null, "after this tab took over it no longer says another tab holds it")
  assert.equal(shownKeyRefusal(refusal, null), null)
  assert.equal(shownKeyRefusal(null, mine), null)
})

const live = { state: "live", error: "", status: "running", closeStatus: "", retrying: false, frameTimedOut: false, needsReview: false,
  canType: true, hasFrame: true, control: { held: true, epoch: 1, holder: { same_client: true } } } as Parameters<typeof cloudTerminalPhase>[0]

test("a terminal the machine says is gone is ended, whatever else the page holds", () => {
  // 2026-10-18: the shell had exited and the page offered Take control and Reconnect instead.
  assert.equal(cloudTerminalPhase({ ...live, error: "terminal_closed", canType: false }), "ended")
  assert.equal(cloudTerminalPhase({ ...live, state: "closed" }), "ended")
  assert.equal(cloudTerminalPhase({ ...live, status: "exited" }), "ended")
  assert.equal(cloudTerminalPhase({ ...live, closeStatus: "ok" }), "ended")
  assert.equal(cloudTerminalPhase({ ...live, error: "terminal_closed", state: "stale", retrying: true }), "ended")
})

test("each other situation has its own phase, in the order the page must say them", () => {
  assert.equal(cloudTerminalPhase(live), "typing")
  assert.equal(cloudTerminalPhase({ ...live, error: "terminal_access_revoked" }), "denied")
  assert.equal(cloudTerminalPhase({ ...live, state: "stale", canType: false }), "reconnecting")
  assert.equal(cloudTerminalPhase({ ...live, retrying: true, canType: false }), "reconnecting")
  assert.equal(cloudTerminalPhase({ ...live, state: "offline", canType: false }), "offline")
  assert.equal(cloudTerminalPhase({ ...live, error: "terminal_receipt_timeout", canType: false }), "error")
  assert.equal(cloudTerminalPhase({ ...live, frameTimedOut: true, canType: false }), "error")
  assert.equal(cloudTerminalPhase({ ...live, state: "unknown", canType: false }), "needs_review")
  assert.equal(cloudTerminalPhase({ ...live, canType: false, hasFrame: false, control: null }), "loading")
  assert.equal(cloudTerminalPhase({ ...live, canType: false, control: { held: true, epoch: 2, holder: { same_client: false, name: "iPad" } } } as typeof live), "other_holder")
  assert.equal(cloudTerminalPhase({ ...live, canType: false, control: { held: false, epoch: 2 } } as typeof live), "watching")
})


test("a deliberate display disconnect offers reconnect instead of an uncertain-input review", () => {
  assert.equal(cloudTerminalPhase({ ...live, state: "unknown", canType: false,
    reason: "terminal_connection_disconnected" }), "error")
})
