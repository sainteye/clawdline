import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- Node's strip-types runner loads source paths.
import { manageTerminalConnections } from "./terminal-connections.ts"
import type { RelayEvent } from "./terminal-transport.js"

test("a full terminal rail can list and disconnect through authenticated metadata without acquiring a slot", async () => {
  const listeners = new Set<(event: RelayEvent) => void>()
  const sent: Record<string, unknown>[] = []
  let rows = [{ machine: "machine", connection: "existing", opened_at: 1, expires_at: 2, active: true }]
  const client = { ready: true, retired: false,
    events: (listener: (event: RelayEvent) => void) => { listeners.add(listener); return () => listeners.delete(listener) },
    _send: (request: Record<string, unknown>) => {
      sent.push(request)
      assert.equal(request.type, "terminal_connections")
      if (request.action === "disconnect") rows = []
      queueMicrotask(() => {
        for (const listener of listeners) {
          listener({ type: "terminal_connections_result", request_id: "another-request", action: request.action as string, code: "forbidden" })
          listener({ type: "terminal_connections_result", request_id: request.request_id as string, action: request.action as string, connections: rows, limit: 16 })
        }
      })
    } }
  const result = await manageTerminalConnections(client, "list")
  assert.equal(result.connections.length, 1)
  assert.equal(listeners.size, 0)
  assert.equal((await manageTerminalConnections(client, "disconnect", result.connections[0])).connections.length, 0)
  assert.equal(sent.length, 2)
  assert.equal(sent[1].machine, "machine")
  assert.equal(sent[1].connection, "existing")
  assert.equal("viewer" in sent[1], false, "the relay derives the viewer identity")
  assert.equal(listeners.size, 0)
})

test("malformed metadata fails closed and a lost disconnect result is never retried", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] })
  const listeners = new Set<(event: RelayEvent) => void>()
  const requests: Record<string, unknown>[] = []
  const client = { ready: true, retired: false,
    events: (listener: (event: RelayEvent) => void) => { listeners.add(listener); return () => listeners.delete(listener) },
    _send: (request: Record<string, unknown>) => { requests.push(request) } }
  const reading = manageTerminalConnections(client, "list")
  for (const listener of listeners) listener({ type: "terminal_connections_result", request_id: requests[0].request_id as string,
    action: "list", limit: 16, connections: [{ machine: "machine" }] })
  await assert.rejects(reading, { code: "terminal_bad_receipt" })
  const disconnect = manageTerminalConnections(client, "disconnect", { machine: "machine", connection: "existing" })
  const refused = assert.rejects(disconnect, { code: "terminal_connection_disconnect_unknown" })
  t.mock.timers.tick(10_000)
  await refused
  assert.equal(requests.length, 2)
  assert.equal(listeners.size, 0)
})
