import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { acquireVisibleTerminal, beginCloudTerminal, cloudTerminalBody, keyRefusalExplained, reconnectCloudTerminal, type CloudTerminalStarter } from "./cloud-view.ts"

// `cloud/terminal-session.ts` uses constructor parameter properties, which
// Node's strip-types runner refuses, so the start path is driven through a fake
// session that records every operation it would put on the channel.
class CountingSession implements CloudTerminalStarter {
  operations: string[] = []
  projects: unknown[] = []
  async start(): Promise<void> { this.operations.push("open_connection") }
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
  assert.deepEqual(session.operations, ["open_connection", "read", "capture"])
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

test("a refused key repeats as an error only when the status line cannot already say why", () => {
  // The session's state says these, with the button that recovers; an error would outlive the recovery.
  for (const code of ["terminal_input_state_unknown", "not_controller", "terminal_stale", "machine_offline", "terminal_access_revoked"])
    assert.equal(keyRefusalExplained(code), true, code)
  // A pause that outlasted its bound dropped keys the state no longer shows, and a send failure is news.
  for (const code of ["terminal_input_paused", "input_too_large", "terminal_receipt_timeout"])
    assert.equal(keyRefusalExplained(code), false, code)
})
