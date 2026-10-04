import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { beginCloudTerminal, cloudTerminalBody, reconnectCloudTerminal, type CloudTerminalStarter } from "./cloud-view.ts"

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
