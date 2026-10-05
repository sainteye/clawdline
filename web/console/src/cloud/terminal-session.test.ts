import assert from "node:assert/strict"
import test from "node:test"
import type { TerminalControl, TerminalFrame } from "@clawdline/contract"
// @ts-expect-error -- the test runner bundles this source file directly.
import { CloudTerminalSession, type TerminalWire } from "./terminal-session.ts"
import type { TerminalCarrier, TerminalChannelEvent, TerminalDirectOffer, TerminalEnvelope } from "./terminal-transport.js"
// @ts-expect-error -- the focused runner bundles TypeScript before Node executes it.
import { TerminalObservation } from "./terminal-observation.ts"
// @ts-expect-error -- focused runner bundles the TypeScript source.
import { terminalScreenHash } from "./terminal-delta.ts"

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
  closeResult: "ok" | "unknown" = "ok"
  incarnation = "first-machine-start"
  lease = held
  deltaSubscribed = false
  deltaMachine = false
  historyResult: Record<string, unknown> = { lines: [], truncated: false, omitted_lines: 0 }
  delayed = new Set<string>()
  replies: Array<() => void> = []
  /** Refusals by operation, or `rekey_connection:direct` for a rekey onto the DC. */
  refusals = new Map<string, string>()
  directMachine = false
  async subscribeTerminal(connection: string, _keyID: string, _raw: Uint8Array, listener: (event: TerminalChannelEvent) => void): Promise<void> {
    this.channels.set(connection, listener)
  }
  unsubscribeTerminal(connection: string): void { this.channels.delete(connection) }
  deltaAvailable(): boolean { return this.deltaSubscribed }
  observeTerminalFrame(envelope: TerminalEnvelope): void { this.observed.push(envelope) }
  async publishTerminal(request: Record<string, unknown>): Promise<{ sender: string; seq: number }> {
    this.requests.push(request)
    if ("previous_connection" in request) throw new Error("malformed_signed_request")
    const operation = request.operation as string
    const connection = request.connection as string
    if (operation === "rekey_connection" &&
      ((request.body as { old_connection?: unknown } | undefined)?.old_connection !== [...this.channels.keys()].at(-2))) {
      throw new Error("rekey_old_connection_missing")
    }
    const result = operation === "open_connection" || operation === "rekey_connection"
      ? { connection, key_id: request.key_id, expires_at: Date.now() / 1000 + 500, machine_incarnation: this.incarnation,
          ...(this.deltaMachine && (request.body as { frame_delta_v1?: boolean })?.frame_delta_v1 ? { frame_delta_v1: true } : {}),
          ...(this.directMachine && (request.body as { carrier?: string })?.carrier === "direct" ? { carrier: "direct" } : {}) }
      : operation === "direct_offer" ? { sdp: "v=0 answer" }
      : operation === "read" || operation === "open" ? { id: terminalID, project_id: "project", status: "running", control }
        : operation === "activate_connection" ? { connection, retired_connection: (request.body as { old_connection: string }).old_connection }
        : operation === "control" && request.body === undefined ? { machine_incarnation: this.incarnation, control: this.lease, input_state_unknown: false }
          : operation === "control" ? { control: this.lease }
            : operation === "list" ? { terminals: [] }
              : operation === "history" ? this.historyResult
                : operation === "input" ? { applied_through: request.seq, duplicate: false } : {}
    const refusal = this.refusals.get(operation + ((request.body as { carrier?: string } | undefined)?.carrier ? ":direct" : ""))
    const reply = () => this.emit(connection, "termr", {
      v: 1, type: "terminal_receipt", request_id: request.request_id, connection, operation,
      ...(request.terminal_id ? { terminal_id: request.terminal_id } : {}),
      ...(refusal ? { status: "refused", error: refusal } : {
        status: operation === "input" ? this.inputResult : operation === "close" ? this.closeResult : "ok", result,
        ...(operation === "input" && this.inputResult === "refused" ? { error: "terminal_forbidden" } : {}),
      }),
    })
    if (this.delayed.has(operation)) this.replies.push(reply)
    else queueMicrotask(reply)
    return { sender: viewer, seq: this.requests.length }
  }
  releaseReplies(): void { for (const reply of this.replies.splice(0)) reply() }
  emit(connection: string, kind: "term" | "termr" | "termd", plaintext: unknown): void {
    const envelope = { v: 1, ch: `${kind}/${machine}/${viewer}/${connection}`, seq: 1, ts: Date.now(), class: kind === "termr" ? "ctl" : "stream",
      key_id: "test", nonce: "", ct: "", sender: machine, sig: "" } satisfies TerminalEnvelope
    this.channels.get(connection)?.({ envelope, plaintext, realign: false })
  }
  latest(): string { return [...this.channels.keys()].at(-1)! }
  frame(connection: string, seq: number, rev: string): void {
    this.emit(connection, "term", { v: 1, type: "terminal_frame", terminal_id: terminalID, connection,
      frame_seq: seq, captured_at: Date.now() / 1000, frame: frame(rev) })
  }
}

test("delta capability requires both relay and machine confirmation", async () => {
  for (const [relay, machineCap] of [[false, false], [true, false], [true, true]]) {
    const wire = new Wire()
    wire.deltaSubscribed = relay; wire.deltaMachine = machineCap
    const session = new CloudTerminalSession(wire, "stable-tab")
    try {
      await session.start()
      const open = wire.requests.find((request) => request.operation === "open_connection")!
      assert.equal((open.body as { frame_delta_v1?: boolean } | undefined)?.frame_delta_v1, relay ? true : undefined)
      await session.attach(terminalID)
      wire.frame(wire.latest(), 1, "first")
      const at = Date.now() / 1000
      wire.emit(wire.latest(), "termd", { v: 1, type: "terminal_frame_delta", terminal_id: terminalID,
        connection: wire.latest(), frame_seq: 2, base_seq: 1, base_rev: "first", captured_at: at,
        rev: "second", at, cols: 80, rows: 1, dead: false, cursor: frame("second").cursor,
        modes: frame("second").modes, changed_rows: [{ row: 0, line: "second" }],
        screen_hash: await terminalScreenHash(["second"]) })
      await new Promise<void>((resolve) => setTimeout(resolve, 0))
      assert.equal(session.snapshot.frame?.rev, relay && machineCap ? "second" : "first")
    } finally { session.dispose() }
  }
})

test("a corrupt delta discards the connection and never replays input", async () => {
  const wire = new Wire()
  wire.deltaSubscribed = true; wire.deltaMachine = true
  const session = new CloudTerminalSession(wire, "stable-tab")
  try {
    await session.start(); await session.attach(terminalID); await session.acquire("acquire")
    const old = wire.latest()
    wire.frame(old, 1, "first")
    const at = Date.now() / 1000
    wire.emit(old, "termd", { v: 1, type: "terminal_frame_delta", terminal_id: terminalID,
      connection: old, frame_seq: 2, base_seq: 1, base_rev: "first", captured_at: at,
      rev: "second", at, cols: 80, rows: 1, dead: false, cursor: frame("second").cursor,
      modes: frame("second").modes, changed_rows: [{ row: 0, line: "second" }], screen_hash: "0".repeat(64) })
    await new Promise<void>((resolve) => setTimeout(resolve, 10))
    assert.notEqual(wire.latest(), old)
    assert.equal(session.snapshot.canType, false)
    assert.equal(wire.requests.filter((request) => request.operation === "input" || request.operation === "paste").length, 0)
    wire.frame(wire.latest(), 1, "restored")
    assert.equal(session.snapshot.frame?.rev, "restored")
  } finally { session.dispose() }
})

test("an authorized connection remains reusable for repeated reads until revoked", async () => {
  const wire = new Wire()
  const session = new CloudTerminalSession(wire, "stable-tab")
  try {
    assert.equal(session.reusable, false)
    await session.start()
    assert.equal(session.reusable, true)
    await session.request("list", { client: "stable-tab" })
    await session.request("list", { client: "stable-tab" })
    assert.equal(wire.requests.filter((request) => request.operation === "open_connection").length, 1)
    wire.channels.get(wire.latest())?.({ error: "terminal_access_revoked" })
    assert.equal(session.reusable, false)
  } finally { session.dispose() }
})

test("a close exposes its request id and an unknown receipt does not claim success", async () => {
  const wire = new Wire()
  const session = new CloudTerminalSession(wire, "stable-tab")
  try {
    await session.start()
    await session.attach(terminalID)
    await session.acquire("acquire")
    wire.frame(wire.latest(), 1, "ready")
    wire.closeResult = "unknown"
    let requestID = ""
    await assert.rejects(() => session.close((id) => { requestID = id }), /terminal_result_unknown/)
    assert.equal(requestID, wire.requests.find((request) => request.operation === "close")?.request_id)
    assert.notEqual(session.snapshot.state, "closed")
  } finally { session.dispose() }
})

test("several numbered keys publish before receipts while later keys wait for the bounded window", async () => {
  const wire = new Wire()
  const session = new CloudTerminalSession(wire, "stable-tab")
  try {
    await session.start()
    await session.attach(terminalID)
    await session.acquire("acquire")
    wire.frame(wire.latest(), 1, "ready")
    wire.delayed.add("input")
    const inputs = Array.from({ length: 5 }, (_, i) => session.input(new TextEncoder().encode(String(i))))
    await new Promise<void>((resolve) => setTimeout(resolve, 0))
    const sent = wire.requests.filter((request) => request.operation === "input")
    assert.equal(sent.length, 4)
    assert.deepEqual(sent.map((request) => request.seq), [1, 2, 3, 4])
    wire.releaseReplies()
    await new Promise<void>((resolve) => setTimeout(resolve, 0))
    assert.equal(wire.requests.filter((request) => request.operation === "input").length, 5)
    wire.releaseReplies()
    await Promise.all(inputs)
  } finally { session.dispose() }
})

test("an unknown pipelined input outcome stops the waiting key without replay", async () => {
  const wire = new Wire()
  const session = new CloudTerminalSession(wire, "stable-tab")
  try {
    await session.start()
    await session.attach(terminalID)
    await session.acquire("acquire")
    wire.frame(wire.latest(), 1, "ready")
    wire.delayed.add("input")
    const inputs = Array.from({ length: 5 }, (_, i) => session.input(new TextEncoder().encode(String(i))))
    for (const pending of inputs) void pending.catch(() => undefined)
    await new Promise<void>((resolve) => setTimeout(resolve, 0))
    wire.inputResult = "unknown"
    wire.releaseReplies()
    await Promise.allSettled(inputs)
    assert.equal(wire.requests.filter((request) => request.operation === "input").length, 4)
    assert.equal(session.snapshot.canType, false)
  } finally { session.dispose() }
})

test("a signed receipt with a different request id is diagnosed and cannot settle capture", async () => {
  const wire = new Wire()
  wire.delayed.add("capture")
  const stages: Array<{ stage: string; code?: string; operation?: string }> = []
  const session = new CloudTerminalSession(wire, "stable-tab", new TerminalObservation((row) => stages.push(row)))
  try {
    await session.start()
    const attaching = session.attach(terminalID)
    void attaching.catch(() => undefined)
    await new Promise<void>((resolve) => setTimeout(resolve, 0))
    wire.emit(wire.latest(), "termr", { v: 1, type: "terminal_receipt", request_id: crypto.randomUUID(),
      connection: wire.latest(), operation: "capture", terminal_id: terminalID, status: "ok" })
    assert.equal(stages.at(-1)?.stage, "pending_miss")
    assert.equal(stages.at(-1)?.code, "request_unknown")
    wire.releaseReplies()
    await attaching
    assert.deepEqual(stages.filter((row) => row.operation === "capture").map((row) => row.stage),
      ["request_pending", "pending_miss", "pending_match", "session_settled"])
  } finally { session.dispose() }
})

test("a slow read does not hold back the capture request or first frame", async () => {
  const wire = new Wire()
  wire.delayed.add("read")
  const session = new CloudTerminalSession(wire, "stable-tab")
  try {
    await session.start()
    const attaching = session.attach(terminalID)
    await new Promise<void>((resolve) => setTimeout(resolve, 0))
    assert.deepEqual(wire.requests.slice(-2).map((request) => request.operation), ["read", "capture"])
    wire.frame(wire.latest(), 1, "early screen")
    assert.equal(session.snapshot.frame?.rev, "early screen")
    wire.releaseReplies()
    await attaching
  } finally { session.dispose() }
})

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

test("Cloud history preserves an explicit partial-result marker", async () => {
  const wire = new Wire()
  wire.historyResult = { lines: ["newest"], truncated: true, omitted_lines: 19 }
  const session = new CloudTerminalSession(wire, "stable-tab")
  try {
    await session.start()
    await session.attach(terminalID)
    assert.deepEqual(await session.history(), { lines: ["newest"], truncated: true, omitted_lines: 19 })
    wire.historyResult = { lines: ["newest"], truncated: false, omitted_lines: 19 }
    await assert.rejects(() => session.history(), /terminal_bad_receipt/)
  } finally {
    session.dispose()
  }
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

test("an open frame arriving before its open receipt is applied only after terminal identity is known", async () => {
  const wire = new Wire()
  wire.delayed.add("open")
  const session = new CloudTerminalSession(wire, "stable-tab")
  try {
    await session.start()
    const opening = session.create("project", 80, 1)
    await new Promise<void>((resolve) => setTimeout(resolve, 0))
    assert.equal(wire.requests.some((request) => request.operation === "open"), true)
    wire.frame(wire.latest(), 1, "first")
    wire.frame(wire.latest(), 2, "newest")
    assert.equal(wire.observed.length, 0, "an unidentified frame cannot be acknowledged")
    wire.releaseReplies()
    await opening
    assert.equal(session.snapshot.frame?.rev, "newest")
    assert.equal(session.snapshot.state, "just_synced")
    assert.equal(wire.observed.length, 1, "the early-frame buffer retains only the newest full frame")
  } finally { session.dispose() }
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
  const rekey = wire.requests.find((request) => request.operation === "rekey_connection")
  assert.deepEqual(rekey?.body, { old_connection: old })
  assert.equal(Object.hasOwn(rekey ?? {}, "previous_connection"), false)
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

test("a rekey frame arriving before its receipt waits for lease proof and then activates", async () => {
  const wire = new Wire()
  const session = new CloudTerminalSession(wire, "stable-tab")
  try {
    await session.start(); await session.attach(terminalID); await session.acquire("acquire")
    const old = wire.latest()
    wire.frame(old, 1, "old screen")
    wire.delayed.add("rekey_connection")
    const reconnect = session.start()
    await new Promise<void>((resolve) => setTimeout(resolve, 0))
    const current = wire.latest()
    assert.notEqual(current, old)
    wire.frame(current, 1, "new screen")
    assert.equal(session.snapshot.canType, false)
    assert.equal(wire.observed.length, 1, "the new frame waits for a verified rekey receipt")
    wire.releaseReplies()
    await reconnect
    await new Promise<void>((resolve) => setTimeout(resolve, 0))
    assert.equal(session.snapshot.frame?.rev, "new screen")
    assert.equal(wire.observed.length, 2)
    assert.equal(wire.requests.filter((request) => request.operation === "activate_connection").length, 1)
    assert.equal(wire.channels.has(old), false)
    assert.equal(session.snapshot.canType, true)
  } finally { session.dispose() }
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

test("an omitted zero input watermark keeps a renewed idle lease typable", async () => {
  const wire = new Wire()
  wire.lease = { ...held, applied_through: undefined }
  const session = new CloudTerminalSession(wire, "stable-tab")
  try {
    await session.start(); await session.attach(terminalID); await session.acquire("acquire")
    wire.frame(wire.latest(), 1, "screen")
    assert.equal(session.snapshot.canType, true)
    ;(session as unknown as { lastRenew: number }).lastRenew = Date.now() - 11_000
    ;(session as unknown as { checkFreshness(): void }).checkFreshness()
    await new Promise<void>((resolve) => setTimeout(resolve, 0))
    assert.equal(session.snapshot.canType, true)
  } finally { session.dispose() }
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

// Offline fault injection on the session side: the receipt was opened, and
// then either matched no pending request or never arrived at all.
test("fault injection: a receipt for another request stops at pending_match", async () => {
  const wire = new Wire()
  wire.delayed.add("capture")
  const observation = new TerminalObservation(() => {})
  const session = new CloudTerminalSession(wire, "stable-tab", observation)
  try {
    await session.start()
    const attaching = session.attach(terminalID)
    void attaching.catch(() => undefined)
    await new Promise<void>((resolve) => setTimeout(resolve, 0))
    const stray = crypto.randomUUID()
    wire.emit(wire.latest(), "termr", { v: 1, type: "terminal_receipt", request_id: stray,
      connection: wire.latest(), operation: "capture", terminal_id: terminalID, status: "ok" })
    const text = observation.text()
    assert.match(text.split("\n")[1]!, /^stopped phase=pending_match stage=pending_miss code=request_unknown seq=\d+$/)
    for (const secret of [stray, wire.latest(), terminalID]) assert.equal(text.includes(secret), false)
    wire.releaseReplies()
    await attaching
  } finally { session.dispose() }
})

test("fault injection: a receipt that never arrives stops at receipt_timeout", async (t) => {
  const wire = new Wire()
  const observation = new TerminalObservation(() => {})
  const session = new CloudTerminalSession(wire, "stable-tab", observation)
  try {
    await session.start()
    await session.attach(terminalID)
    wire.delayed.add("history")
    t.mock.timers.enable({ apis: ["setTimeout"] })
    const reading = session.history()
    await new Promise<void>((resolve) => queueMicrotask(resolve))
    await new Promise<void>((resolve) => queueMicrotask(resolve))
    t.mock.timers.tick(10_000)
    await assert.rejects(reading, /terminal_receipt_timeout/)
    assert.equal(session.reusable, false)
    assert.match(observation.text().split("\n")[1]!, /^stopped phase=receipt_timeout stage=receipt_timeout code=- seq=\d+$/)
    assert.match(observation.text(), /stage=request_pending phase=- conn=1 req=\d+ ch=- op=history/)
  } finally { t.mock.timers.reset(); session.dispose() }
})

test("a correlated relay refusal settles the list immediately and leaves the connection reusable", async () => {
  const wire = new Wire()
  wire.delayed.add("list")
  const session = new CloudTerminalSession(wire, "stable-tab")
  try {
    await session.start()
    const reading = session.request("list", { client: "stable-tab" })
    await new Promise<void>((resolve) => queueMicrotask(resolve))
    const request = wire.requests.find((item) => item.operation === "list")!
    wire.channels.get(wire.latest())?.({ error: "rate_limited", requestID: request.request_id as string })
    await assert.rejects(reading, /rate_limited/)
    assert.equal(session.reusable, true)
  } finally { session.dispose() }
})

test("a rate-limited capture cannot undo a successful read or block later verified typing", async () => {
  const wire = new Wire()
  wire.delayed.add("capture")
  const session = new CloudTerminalSession(wire, "stable-tab")
  try {
    await session.start()
    const attaching = session.attach(terminalID)
    await new Promise<void>((resolve) => setTimeout(resolve, 0))
    const first = wire.requests.filter((item) => item.operation === "capture").at(-1)!
    wire.channels.get(wire.latest())?.({ error: "rate_limited", requestID: first.request_id as string })
    await attaching
    assert.equal(session.reusable, true)
    const acquiring = session.acquire("acquire")
    await new Promise<void>((resolve) => setTimeout(resolve, 0))
    const second = wire.requests.filter((item) => item.operation === "capture").at(-1)!
    wire.channels.get(wire.latest())?.({ error: "rate_limited", requestID: second.request_id as string })
    await acquiring
    wire.frame(wire.latest(), 1, "ready")
    assert.equal(session.snapshot.canType, true)
  } finally { session.dispose() }
})

/** A wire whose DC opens on the answer; `dropDC` is the DC closing under the session. */
class DirectWire extends Wire {
  dcOpen = false
  offers = 0
  carriers = new Map<string, TerminalCarrier>()
  private down: ((code: string) => void) | null = null
  constructor() { super(); this.directMachine = true }
  directSupported(): boolean { return true }
  async prepareDirect(_connection: string, onDown: (code: string) => void): Promise<TerminalDirectOffer> {
    this.offers++
    this.down = onDown
    const wire = this
    return { sealed: "sealed-offer", get open() { return wire.dcOpen }, answer: async () => { wire.dcOpen = true },
      close: () => wire.dropDC("terminal_direct_released") }
  }
  setCarrier(connection: string, carrier: TerminalCarrier): void { this.carriers.set(connection, carrier) }
  override async publishTerminal(request: Record<string, unknown>): Promise<{ sender: string; seq: number }> {
    if ((request.body as { carrier?: string } | undefined)?.carrier === "direct" && !this.dcOpen) throw new Error("terminal_direct_closed")
    return super.publishTerminal(request)
  }
  dropDC(code = "terminal_direct_closed"): void {
    const down = this.down
    this.down = null
    this.dcOpen = false
    down?.(code)
  }
}
const settle = () => new Promise<void>((resolve) => setTimeout(resolve, 10))
const operations = (wire: Wire) => wire.requests.map((request) => request.operation)
const lastRequest = (wire: Wire, operation: string) => [...wire.requests].reverse().find((request) => request.operation === operation)

/** A held, live relay terminal that has upgraded to a direct connection and retired its relay one. */
async function upgraded(wire: DirectWire, observation?: TerminalObservation) {
  const session = new CloudTerminalSession(wire, "stable-tab", observation)
  await session.start(); await session.attach(terminalID); await session.acquire("acquire")
  const relay = wire.latest()
  wire.frame(relay, 1, "relay")
  await settle()
  const direct = wire.latest()
  wire.frame(direct, 1, "direct")
  await settle()
  return { session, relay, direct }
}

test("a live relay terminal upgrades through a sealed offer, a direct rekey and an activation", async () => {
  const wire = new DirectWire()
  const stages: string[] = []
  const { session, relay, direct } = await upgraded(wire, new TerminalObservation((row) => stages.push(`${row.stage}:${row.code ?? ""}`)))
  try {
    const offer = wire.requests.find((request) => request.operation === "direct_offer")!
    assert.equal(offer.connection, relay)
    assert.deepEqual(offer.body, { sealed: "sealed-offer" })
    const rekey = lastRequest(wire, "rekey_connection")!
    assert.equal(rekey.connection, direct)
    assert.deepEqual(rekey.body, { old_connection: relay, carrier: "direct" })
    assert.equal(wire.carriers.get(direct), "direct")
    assert.equal(session.snapshot.carrier, "direct")
    const activate = lastRequest(wire, "activate_connection")!
    assert.deepEqual(activate.body, { old_connection: relay, first_frame_seq: 1 })
    assert.equal(wire.channels.has(relay), false)
    assert.equal(session.snapshot.canType, true)
    assert.ok(stages.includes("carrier_changed:direct"))
  } finally { session.dispose() }
})

test("a direct connection's key rotation is a direct rekey", async () => {
  const wire = new DirectWire()
  const { session, direct } = await upgraded(wire)
  try {
    await session.start()
    const rekey = lastRequest(wire, "rekey_connection")!
    assert.deepEqual(rekey.body, { old_connection: direct, carrier: "direct" })
    assert.equal(session.snapshot.carrier, "direct")
  } finally { session.dispose() }
})

test("a machine refusing the direct path leaves the terminal on the relay silently and waits before trying again", async () => {
  for (const [operation, code] of [["direct_offer", "terminal_invalid"], ["direct_offer", "terminal_direct_disabled"],
    ["direct_offer", "terminal_direct_unavailable"], ["rekey_connection:direct", "terminal_direct_unavailable"]]) {
    const wire = new DirectWire()
    wire.refusals.set(operation, code)
    const session = new CloudTerminalSession(wire, "stable-tab")
    try {
      await session.start(); await session.attach(terminalID); await session.acquire("acquire")
      const relay = wire.latest()
      wire.frame(relay, 1, "ready")
      await settle()
      assert.equal(wire.offers, 1, code)
      assert.equal(session.snapshot.carrier, "relay", code)
      assert.equal(session.snapshot.canType, true, code)
      assert.equal(session.snapshot.state, "just_synced", code)
      assert.equal(wire.latest(), relay, code)
      assert.equal(wire.dcOpen, false, `${code}: the unused peer is closed`)
      wire.frame(relay, 2, "later")
      await settle()
      assert.equal(wire.offers, 1, `${code}: no second attempt within the retry wait`)
      assert.equal(session.snapshot.state, "live", code)
    } finally { session.dispose() }
  }
})

test("a closed DC rekeys back to the relay naming the direct connection and never replays input", async () => {
  const wire = new DirectWire()
  const stages: string[] = []
  const { session, direct } = await upgraded(wire, new TerminalObservation((row) => stages.push(`${row.stage}:${row.code ?? ""}`)))
  try {
    wire.delayed.add("input")
    const typed = session.input(new TextEncoder().encode("a"))
    void typed.catch(() => undefined)
    await settle()
    assert.equal(operations(wire).filter((operation) => operation === "input").length, 1)
    wire.dropDC()
    await settle()
    const rekey = lastRequest(wire, "rekey_connection")!
    assert.deepEqual(rekey.body, { old_connection: direct })
    assert.equal(session.snapshot.carrier, "relay")
    assert.equal(session.snapshot.canType, false, "input waits for the relay connection's first frame")
    wire.releaseReplies()
    await typed
    const relay = wire.latest()
    wire.frame(relay, 1, "back")
    await settle()
    assert.equal(lastRequest(wire, "activate_connection")?.connection, relay)
    assert.equal(operations(wire).filter((operation) => operation === "input").length, 1)
    assert.equal(wire.offers, 1, "no new upgrade inside the retry wait")
    assert.equal(session.snapshot.canType, true)
    assert.ok(stages.includes("carrier_changed:relay"))
  } finally { session.dispose() }
})

test("when the machine already retired the direct connection, the fallback opens a new one and proves the lease", async () => {
  const wire = new DirectWire()
  const { session, direct } = await upgraded(wire)
  try {
    wire.refusals.set("rekey_connection", "terminal_old_connection")
    const before = wire.requests.length
    wire.dropDC()
    await settle()
    const after = wire.requests.slice(before)
    assert.deepEqual(after.map((request) => request.operation), ["rekey_connection", "open_connection", "control", "read", "capture"])
    assert.equal(after[2].body, undefined, "the lease is proved by a read-only control query")
    assert.equal(wire.channels.has(direct), false)
    wire.frame(wire.latest(), 1, "reopened")
    await settle()
    assert.equal(session.snapshot.carrier, "relay")
    assert.equal(session.snapshot.hasLease, true, "the proved lease is kept")
    assert.equal(session.snapshot.frame?.rev, "reopened")
    assert.equal(operations(wire).includes("input"), false)
    assert.equal(wire.requests.slice(before).some((request) => request.operation === "activate_connection"), false)
  } finally { session.dispose() }
})
