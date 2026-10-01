import assert from "node:assert/strict"
import test from "node:test"
import type { TerminalControl, TerminalFrame } from "@clawdline/contract"
// @ts-expect-error -- the test runner bundles this source file directly.
import { CloudTerminalSession, type TerminalWire } from "./terminal-session.ts"
import type { TerminalChannelEvent, TerminalEnvelope } from "./terminal-transport.js"

const terminalID = "trm_test"
const machine = "machine_test"
const viewer = "viewer_test"
const control: TerminalControl = { held: false, epoch: 1, expires_at: 0 }
const held: TerminalControl = { held: true, epoch: 2, holder: { name: "tab", local: false, same_device: true, same_client: true },
  expires_at: Date.now() / 1000 + 25, applied_through: 0 }
const frame = (rev: string): TerminalFrame => ({ rev, at: Date.now() / 1000, cols: 80, rows: 1,
  cursor: { x: 0, y: 0, visible: true }, modes: { app_cursor: false, app_keypad: false, mouse: "none", mouse_sgr: false, alt: false }, lines: [rev] })

class Wire implements TerminalWire {
  channels = new Map<string, (event: TerminalChannelEvent) => void>()
  observed: TerminalEnvelope[] = []
  requests: Record<string, unknown>[] = []
  inputResult: "ok" | "unknown" | "refused" = "ok"
  incarnation = "first-machine-start"
  lease = held
  async subscribeTerminal(connection: string, _keyID: string, _raw: Uint8Array, listener: (event: TerminalChannelEvent) => void): Promise<void> {
    this.channels.set(connection, listener)
  }
  unsubscribeTerminal(connection: string): void { this.channels.delete(connection) }
  observeTerminalFrame(envelope: TerminalEnvelope): void { this.observed.push(envelope) }
  async publishTerminal(request: Record<string, unknown>): Promise<{ sender: string; seq: number }> {
    this.requests.push(request)
    const operation = request.operation as string
    const connection = request.connection as string
    const result = operation === "open_connection" || operation === "rekey_connection"
      ? { connection, key_id: request.key_id, expires_at: Date.now() / 1000 + 500, machine_incarnation: this.incarnation }
      : operation === "read" || operation === "open" ? { id: terminalID, project_id: "project", status: "running", control }
        : operation === "activate_connection" ? { connection, retired_connection: (request.body as { old_connection: string }).old_connection }
        : operation === "control" && request.body === undefined ? { machine_incarnation: this.incarnation, control: this.lease, input_state_unknown: false }
          : operation === "control" ? { control: this.lease }
            : operation === "list" ? { terminals: [] }
              : operation === "history" ? { lines: [] }
                : operation === "input" ? { applied_through: request.seq, duplicate: false } : {}
    queueMicrotask(() => this.emit(connection, "termr", {
      v: 1, type: "terminal_receipt", request_id: request.request_id, connection, operation,
      ...(request.terminal_id ? { terminal_id: request.terminal_id } : {}),
      status: operation === "input" ? this.inputResult : "ok", result,
      ...(operation === "input" && this.inputResult === "refused" ? { error: "terminal_forbidden" } : {}),
    }))
    return { sender: viewer, seq: this.requests.length }
  }
  emit(connection: string, kind: "term" | "termr", plaintext: unknown): void {
    const envelope = { v: 1, ch: `${kind}/${machine}/${viewer}/${connection}`, seq: 1, ts: Date.now(), class: kind === "term" ? "stream" : "ctl",
      key_id: "test", nonce: "", ct: "", sender: machine, sig: "" } satisfies TerminalEnvelope
    this.channels.get(connection)?.({ envelope, plaintext, realign: false })
  }
  latest(): string { return [...this.channels.keys()].at(-1)! }
  frame(connection: string, seq: number, rev: string): void {
    this.emit(connection, "term", { v: 1, type: "terminal_frame", terminal_id: terminalID, connection,
      frame_seq: seq, captured_at: Date.now() / 1000, frame: frame(rev) })
  }
}

test("a receipt alone never enables input; first and later full frames have distinct states", async () => {
  const wire = new Wire()
  const session = new CloudTerminalSession(wire, "stable-tab")
  await session.start()
  await session.attach(terminalID)
  await session.acquire("acquire")
  assert.equal(session.snapshot.canType, false)
  assert.equal(wire.observed.length, 0, "a receipt does not observe a frame")
  wire.emit(wire.latest(), "term", { v: 1, type: "terminal_frame", terminal_id: terminalID,
    connection: wire.latest(), frame_seq: 1, captured_at: Date.now() / 1000, frame: { ...frame("partial"), lines: [] } })
  assert.equal(wire.observed.length, 0, "an incomplete frame is not observed")
  wire.frame(wire.latest(), 1, "first")
  assert.equal(wire.observed.length, 1)
  assert.equal(session.snapshot.state, "just_synced")
  assert.equal(session.snapshot.canType, true)
  wire.frame(wire.latest(), 2, "later")
  assert.equal(session.snapshot.state, "live")
  session.dispose()
})

test("unchanged full frames keep an idle shell fresh and typable for 30 seconds", async () => {
  const realNow = Date.now
  let now = realNow()
  Date.now = () => now
  const wire = new Wire()
  wire.lease = { ...held, expires_at: now / 1000 + 90 }
  const session = new CloudTerminalSession(wire, "stable-tab")
  try {
    await session.start(); await session.attach(terminalID); await session.acquire("acquire")
    const connection = wire.latest()
    wire.frame(connection, 1, "unchanged")
    const originalRev = session.snapshot.frame?.rev
    for (let seq = 2; seq <= 11; seq++) {
      now += 3_000
      wire.frame(connection, seq, "unchanged")
      ;(session as unknown as { checkFreshness(): void }).checkFreshness()
      assert.equal(session.snapshot.state, "live")
      assert.equal(session.snapshot.canType, true, `input paused after ${(seq - 1) * 3} seconds`)
      assert.equal(session.snapshot.frame?.rev, originalRev)
    }
    assert.equal(wire.observed.length, 11, "each verified full frame is acknowledged despite the same revision")
  } finally {
    session.dispose()
    Date.now = realNow
  }
})

test("rekey keeps the lease client and checks a read-only high-water mark before typing", async () => {
  const wire = new Wire()
  const session = new CloudTerminalSession(wire, "stable-tab")
  await session.start(); await session.attach(terminalID); await session.acquire("acquire")
  wire.frame(wire.latest(), 1, "first")
  const old = wire.latest()
  await session.input(new TextEncoder().encode("a"))
  wire.lease = { ...held, applied_through: 1 }
  await session.start()
  const newConnection = wire.latest()
  assert.notEqual(newConnection, old)
  assert.equal(session.snapshot.canType, false, "new complete frame is still missing")
  assert.equal(wire.channels.has(old), true, "the old receipt channel stays until activation")
  const query = [...wire.requests].reverse().find((request) => request.operation === "control" && request.body === undefined)
  assert.equal(query?.client, "stable-tab")
  assert.equal(wire.requests.filter((request) => request.operation === "control" && (request.body as { action?: string } | undefined)?.action === "acquire").length, 1)
  wire.frame(newConnection, 1, "new connection")
  await new Promise<void>((resolve) => setTimeout(resolve, 0))
  assert.equal(wire.channels.has(old), false)
  assert.equal(wire.requests.filter((request) => request.operation === "activate_connection").length, 1)
  assert.equal(session.snapshot.canType, true)
  session.dispose()
})

test("an unknown machine input result stops typing without resending the same effect", async () => {
  const wire = new Wire()
  const session = new CloudTerminalSession(wire, "stable-tab")
  await session.start(); await session.attach(terminalID); await session.acquire("acquire")
  wire.frame(wire.latest(), 1, "screen")
  wire.inputResult = "unknown"
  await assert.rejects(session.input(new TextEncoder().encode("danger")))
  assert.equal(session.snapshot.state, "unknown")
  assert.equal(session.snapshot.canType, false)
  assert.equal(wire.requests.filter((request) => request.operation === "input").length, 1)
  session.dispose()
})

test("offline and revoked relay outcomes stop input even after a verified frame", async () => {
  const wire = new Wire()
  const session = new CloudTerminalSession(wire, "stable-tab")
  await session.start(); await session.attach(terminalID); await session.acquire("acquire")
  const connection = wire.latest()
  wire.frame(connection, 1, "screen")
  assert.equal(session.snapshot.canType, true)
  wire.channels.get(connection)?.({ error: "machine_offline" })
  assert.equal(session.snapshot.state, "offline")
  assert.equal(session.snapshot.canType, false)
  wire.channels.get(connection)?.({ error: "terminal_access_revoked" })
  assert.equal(session.snapshot.state, "revoked")
  assert.equal(session.snapshot.canType, false)
  session.dispose()
})

test("the lease renews on its own cadence and an expired lease cannot type", async () => {
  const wire = new Wire()
  const session = new CloudTerminalSession(wire, "stable-tab")
  await session.start(); await session.attach(terminalID); await session.acquire("acquire")
  wire.frame(wire.latest(), 1, "screen")
  ;(session as unknown as { lastRenew: number }).lastRenew = Date.now() - 11_000
  ;(session as unknown as { checkFreshness(): void }).checkFreshness()
  await new Promise<void>((resolve) => setTimeout(resolve, 0))
  assert.equal(wire.requests.filter((request) => (request.body as { action?: string } | undefined)?.action === "renew").length, 1)
  ;(session as unknown as { set(value: object): void }).set({ control: { ...held, expires_at: Date.now() / 1000 - 1 } })
  assert.equal(session.snapshot.canType, false)
  session.dispose()
})

test("a current signed revocation notice clears the lease and stays revoked", async () => {
  const wire = new Wire()
  const session = new CloudTerminalSession(wire, "stable-tab")
  await session.start(); await session.attach(terminalID); await session.acquire("acquire")
  const connection = wire.latest()
  wire.frame(connection, 1, "screen")
  assert.equal(session.snapshot.canType, true)
  const notice = { v: 1, type: "terminal_notice", connection, code: "terminal_access_revoked",
    machine_incarnation: wire.incarnation }
  wire.emit(connection, "term", notice)
  assert.equal(session.snapshot.canType, true, "a notice on the frame route is ignored")
  wire.emit(connection, "termr", { ...notice, request_id: crypto.randomUUID() })
  assert.equal(session.snapshot.canType, true, "a notice cannot masquerade as a receipt")
  wire.emit(connection, "termr", notice)
  assert.equal(session.snapshot.state, "revoked")
  assert.equal(session.snapshot.hasLease, false)
  assert.equal(session.snapshot.canType, false)
  await assert.rejects(session.input(new TextEncoder().encode("do not type")), /terminal_input_paused/)
  wire.frame(connection, 2, "later")
  assert.equal(session.snapshot.state, "revoked")
  session.dispose()
})

test("a different incarnation fails closed and an old connection cannot revoke the new one", async () => {
  const wire = new Wire()
  const session = new CloudTerminalSession(wire, "stable-tab")
  await session.start(); await session.attach(terminalID); await session.acquire("acquire")
  wire.frame(wire.latest(), 1, "old")
  const old = wire.latest()
  await session.start()
  const current = wire.latest()
  wire.emit(old, "termr", { v: 1, type: "terminal_notice", connection: old,
    code: "terminal_access_revoked", machine_incarnation: wire.incarnation })
  assert.notEqual(session.snapshot.state, "revoked")
  wire.frame(current, 1, "new")
  await new Promise<void>((resolve) => setTimeout(resolve, 0))
  assert.equal(session.snapshot.canType, true)
  wire.emit(current, "termr", { v: 1, type: "terminal_notice", connection: current,
    code: "terminal_access_revoked", machine_incarnation: "another-start" })
  assert.equal(session.snapshot.state, "revoked")
  assert.equal(session.snapshot.hasLease, false)
  assert.equal(session.snapshot.canType, false)
  session.dispose()
})

test("a machine terminal-forbidden receipt revokes a previously held lease", async () => {
  const wire = new Wire()
  const session = new CloudTerminalSession(wire, "stable-tab")
  await session.start(); await session.attach(terminalID); await session.acquire("acquire")
  wire.frame(wire.latest(), 1, "screen")
  assert.equal(session.snapshot.canType, true)
  wire.inputResult = "refused"
  await assert.rejects(session.input(new TextEncoder().encode("x")), /terminal_forbidden/)
  assert.equal(session.snapshot.state, "revoked")
  assert.equal(session.snapshot.hasLease, false)
  assert.equal(session.snapshot.canType, false)
  session.dispose()
})
