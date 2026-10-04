import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- Node's strip-types runner loads the source directly.
import { acquireTerminalConnection, invalidateTerminalConnections, TERMINAL_IDLE_MS } from "./terminal-connection-owner.ts"
// @ts-expect-error -- Node's strip-types runner loads the source directly.
import { TerminalChannelTransport, type TerminalChannelEvent } from "./terminal-transport.ts"
// @ts-expect-error -- Node's strip-types runner loads the source directly.
import { setTerminalHost } from "./terminal-host.ts"

test("navigation, refresh and concurrent readers share one valid connection", async (t) => {
  const channels = new Map<string, (event: TerminalChannelEvent) => void>()
  const requests: Record<string, unknown>[] = []
  t.mock.method(TerminalChannelTransport.prototype, "subscribeTerminal", async (connection: string, _id: string,
    _key: Uint8Array, listener: (event: TerminalChannelEvent) => void) => { channels.set(connection, listener) })
  t.mock.method(TerminalChannelTransport.prototype, "unsubscribeTerminal", (connection: string) => { channels.delete(connection) })
  t.mock.method(TerminalChannelTransport.prototype, "dispose", () => undefined)
  t.mock.method(TerminalChannelTransport.prototype, "publishTerminal", async (request: Record<string, unknown>) => {
    requests.push(request)
    const connection = request.connection as string
    queueMicrotask(() => channels.get(connection)?.({ envelope: { ch: `termr/machine/viewer/${connection}` } as never,
      plaintext: { v: 1, type: "terminal_receipt", request_id: request.request_id, connection,
        operation: request.operation, status: "ok", result: request.operation === "open_connection"
          ? { connection, key_id: request.key_id, expires_at: Date.now() / 1000 + 500, machine_incarnation: "start-1" }
          : { terminals: [] } }, realign: false }))
    return { sender: "viewer", seq: requests.length }
  })
  const client = () => ({ deviceID: "viewer", devicePrivateKey: null, ready: true, retired: false,
    _outboundMachinePairing: async () => ({}) as never, _receiveEnvelope: async () => undefined,
    events: () => () => undefined }) as never
  const first = { client: client(), machine: "machine" }
  setTerminalHost(first)
  try {
    const [list, detail] = await Promise.all([
      acquireTerminalConnection(first, "machine", "tab"), acquireTerminalConnection(first, "machine", "tab")])
    assert.equal(list.session, detail.session)
    assert.equal(requests.filter((request) => request.operation === "open_connection").length, 1)
    await list.session.request("list")
    list.release(); detail.release()
    const refresh = await acquireTerminalConnection(first, "machine", "tab")
    assert.equal(refresh.session, list.session)
    refresh.release()
    const second = { client: client(), machine: "machine" }
    setTerminalHost(second)
    const renewed = await acquireTerminalConnection(second, "machine", "tab")
    assert.notEqual(renewed.session, list.session)
    assert.equal(requests.filter((request) => request.operation === "open_connection").length, 2)
    const active = [...channels.keys()].at(-1)!
    channels.get(active)?.({ error: "terminal_access_revoked" })
    renewed.release()
    const replaced = await acquireTerminalConnection(second, "machine", "tab")
    assert.notEqual(replaced.session, renewed.session)
    t.mock.timers.enable({ apis: ["setTimeout"] })
    replaced.release()
    assert.equal(requests.filter((request) => request.operation === "open_connection").length, 3)
    t.mock.timers.tick(TERMINAL_IDLE_MS)
    await new Promise<void>((resolve) => setImmediate(resolve))
    assert.equal(requests.filter((request) => request.operation === "release_connection").length, 1)
  } finally { t.mock.timers.reset(); setTerminalHost(null); invalidateTerminalConnections() }
})
