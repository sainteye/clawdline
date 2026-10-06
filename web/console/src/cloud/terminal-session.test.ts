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
// @ts-expect-error -- the focused runner bundles TypeScript before Node executes it.
import { until } from "./until.ts"

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
  /** The control a `read` answers with. */
  readControl = control
  deltaSubscribed = false
  deltaMachine = false
  historyResult: Record<string, unknown> = { lines: [], truncated: false, omitted_lines: 0 }
  delayed = new Set<string>()
  replies: Array<() => void> = []
  /** Refusals by operation, or `rekey_connection:direct` for a rekey onto the DC. */
  refusals = new Map<string, string>()
  directMachine = false
  /**
   * A machine that decides each numbered input as internal/domain/terminal/input.go does, one at a
   * time in arrival order, and answers each after `pacedMs`, the next only after the previous.
   */
  machineInputs: { applied: number; typed: string[]; pacedMs: number; queue: Array<() => void>; busy: boolean } | null = null
  private paceMachine(): void {
    const machine = this.machineInputs!
    if (machine.busy) return
    const next = machine.queue.shift()
    if (!next) return
    machine.busy = true
    setTimeout(() => { machine.busy = false; next(); this.paceMachine() }, machine.pacedMs)
  }
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
      : operation === "read" || operation === "open" ? { id: terminalID, project_id: "project", status: "running", control: this.readControl }
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
    if (operation === "input" && this.machineInputs) {
      const machine = this.machineInputs
      machine.queue.push(() => {
        const seq = request.seq as number
        const duplicate = seq <= machine.applied
        if (seq === machine.applied + 1) {
          machine.applied = seq
          machine.typed.push(atob((request.body as { data: string }).data))
        }
        const gap = seq > machine.applied
        this.emit(connection, "termr", { v: 1, type: "terminal_receipt", request_id: request.request_id, connection, operation,
          terminal_id: request.terminal_id, ...(gap ? { status: "refused", error: "input_gap" }
            : { status: "ok", result: { applied_through: machine.applied, duplicate } }) })
      })
      this.paceMachine()
    } else if (this.delayed.has(operation)) this.replies.push(reply)
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
      // A delta this connection may use is checked against its digest off the event loop;
      // one it may not use is dropped before the first await.
      if (relay && machineCap) await until(() => session.snapshot.frame?.rev === "second")
      else await new Promise<void>((resolve) => setTimeout(resolve, 0))
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
    await until(() => wire.latest() !== old && (session as unknown as { active: boolean }).active)
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
    const inputCount = () => wire.requests.filter((request) => request.operation === "input").length
    await until(() => inputCount() >= 4)
    await new Promise<void>((resolve) => setTimeout(resolve, 0))
    const sent = wire.requests.filter((request) => request.operation === "input")
    assert.equal(sent.length, 4)
    assert.deepEqual(sent.map((request) => request.seq), [1, 2, 3, 4])
    wire.releaseReplies()
    await until(() => inputCount() === 5)
    assert.equal(wire.requests.filter((request) => request.operation === "input").length, 5)
    wire.releaseReplies()
    await Promise.all(inputs)
  } finally { session.dispose() }
})

test("a burst longer than the in-flight window arrives complete and in order when receipts come back one by one", async () => {
  const wire = new Wire()
  const session = new CloudTerminalSession(wire, "stable-tab")
  try {
    await session.start()
    await session.attach(terminalID)
    await session.acquire("acquire")
    wire.frame(wire.latest(), 1, "ready")
    wire.machineInputs = { applied: 0, typed: [], pacedMs: 5, queue: [], busy: false }
    const text = "vim -R README.zh-TW.md\n"
    const inputs: Promise<void>[] = []
    for (const key of text) {
      const sent = session.input(new TextEncoder().encode(key))
      void sent.catch(() => undefined)
      inputs.push(sent)
      await new Promise<void>((resolve) => setTimeout(resolve, 2))
    }
    const outcomes = await Promise.allSettled(inputs)
    const seqs = wire.requests.filter((request) => request.operation === "input").map((request) => request.seq)
    assert.equal(wire.machineInputs.typed.join(""), text, `seqs sent ${seqs.join(",")}`)
    assert.deepEqual(seqs, Array.from({ length: text.length }, (_, i) => i + 1))
    assert.deepEqual(outcomes.filter((outcome) => outcome.status === "rejected"), [])
    assert.equal(session.snapshot.canType, true)
  } finally { session.dispose() }
})

test("a key typed while the tab is still taking control on entry waits and goes out once it holds control", async () => {
  const wire = new Wire()
  const session = new CloudTerminalSession(wire, "stable-tab")
  try {
    await session.start(); await session.attach(terminalID)
    wire.frame(wire.latest(), 1, "watching")
    wire.delayed.add("control")
    const acquiring = session.acquire("acquire")
    await until(() => wire.requests.some((request) => request.operation === "control"))
    assert.equal(session.snapshot.typeAhead, true)
    const key = session.input(new TextEncoder().encode("l"))
    wire.delayed.delete("control")
    wire.releaseReplies()
    await acquiring
    assert.equal(wire.requests.filter((request) => request.operation === "input").length, 0, "no key before a current screen")
    wire.frame(wire.latest(), 2, "controlled")
    await key
    assert.deepEqual(wire.requests.filter((request) => request.operation === "input").map((request) => request.seq), [1])
  } finally { session.dispose() }
})

test("a key typed while taking control that another tab holds is refused as not_controller, never sent", async () => {
  const wire = new Wire()
  wire.lease = { ...held, holder: { name: "other", local: false, same_device: true, same_client: false } }
  const session = new CloudTerminalSession(wire, "stable-tab")
  try {
    await session.start(); await session.attach(terminalID)
    wire.frame(wire.latest(), 1, "watching")
    wire.delayed.add("control")
    const acquiring = session.acquire("acquire")
    void acquiring.catch(() => undefined)
    await until(() => wire.requests.some((request) => request.operation === "control"))
    const key = session.input(new TextEncoder().encode("l"))
    void key.catch(() => undefined)
    wire.releaseReplies()
    await assert.rejects(acquiring, /not_controller/)
    await assert.rejects(key, /not_controller/)
    assert.equal(wire.requests.filter((request) => request.operation === "input").length, 0)
    assert.equal(session.snapshot.typeAhead, false)
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
    await until(() => wire.requests.filter((request) => request.operation === "input").length >= 4)
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
    await until(() => wire.requests.some((request) => request.operation === "capture"))
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
    await until(() => wire.requests.some((request) => request.operation === "capture"))
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
    await until(() => wire.requests.some((request) => request.operation === "open"))
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
  await until(() => !wire.channels.has(old) && session.snapshot.canType)
  assert.equal(wire.channels.has(old), false)
  assert.equal(wire.requests.filter((request) => request.operation === "activate_connection").length, 1)
  assert.equal(session.snapshot.canType, true)
  session.dispose()
})

test("a key still awaiting its receipt across a rekey keeps its number; the next key gets the one after", async () => {
  const wire = new Wire()
  const session = new CloudTerminalSession(wire, "stable-tab")
  try {
    await session.start(); await session.attach(terminalID); await session.acquire("acquire")
    wire.frame(wire.latest(), 1, "first")
    wire.delayed.add("input")
    // Its receipt is never released here: only the numbering is under test.
    void session.input(new TextEncoder().encode("a")).catch(() => undefined)
    await until(() => wire.requests.some((request) => request.operation === "input"))
    // The machine has not decided "a" yet: the lease proof still says nothing was applied.
    await session.start()
    wire.frame(wire.latest(), 1, "new connection")
    await until(() => session.snapshot.canType)
    void session.input(new TextEncoder().encode("b")).catch(() => undefined)
    await until(() => wire.requests.filter((request) => request.operation === "input").length === 2)
    assert.deepEqual(wire.requests.filter((request) => request.operation === "input").map((request) => request.seq), [1, 2])
  } finally { session.dispose() }
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
    await until(() => wire.requests.some((request) => request.operation === "rekey_connection"))
    const current = wire.latest()
    assert.notEqual(current, old)
    wire.frame(current, 1, "new screen")
    assert.equal(session.snapshot.canType, false)
    assert.equal(wire.observed.length, 1, "the new frame waits for a verified rekey receipt")
    wire.releaseReplies()
    await reconnect
    await until(() => !wire.channels.has(old) && session.snapshot.canType)
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
  await until(() => !(session as unknown as { renewing: boolean }).renewing)
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
    assert.equal((session as unknown as { renewing: boolean }).renewing, true, "the renewal is out")
    await until(() => !(session as unknown as { renewing: boolean }).renewing)
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
  await assert.rejects(session.input(new TextEncoder().encode("do not type")), /terminal_access_revoked/)
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
  await until(() => session.snapshot.canType)
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
    await until(() => wire.requests.some((request) => request.operation === "capture"))
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
    await until(() => wire.requests.some((item) => item.operation === "list"))
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
    await until(() => wire.requests.some((item) => item.operation === "capture"))
    const first = wire.requests.filter((item) => item.operation === "capture").at(-1)!
    wire.channels.get(wire.latest())?.({ error: "rate_limited", requestID: first.request_id as string })
    await attaching
    assert.equal(session.reusable, true)
    const acquiring = session.acquire("acquire")
    await until(() => wire.requests.filter((item) => item.operation === "capture").length === 2)
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
  await until(() => session.snapshot.carrier === "direct")
  const direct = wire.latest()
  wire.frame(direct, 1, "direct")
  await until(() => !wire.channels.has(relay) && session.snapshot.canType)
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

test("a terminal this tab does not hold stays on the relay until the tab takes control", async () => {
  // Only a tab holding the lease upgrades; a watching tab has no keys to speed up.
  const wire = new DirectWire()
  const session = new CloudTerminalSession(wire, "stable-tab")
  try {
    await session.start(); await session.attach(terminalID)
    const relay = wire.latest()
    wire.frame(relay, 1, "watching")
    await settle()
    assert.equal(wire.offers, 0)
    assert.equal(session.snapshot.carrier, "relay")
    await session.acquire("acquire")
    wire.frame(relay, 2, "held")
    await until(() => lastRequest(wire, "rekey_connection")?.connection === wire.latest())
    assert.equal(wire.offers, 1)
    assert.equal(lastRequest(wire, "rekey_connection")?.connection, wire.latest())
  } finally { session.dispose() }
})

test("a direct frame that arrives while the lease is being checked is drawn, acknowledged and activates", async () => {
  const wire = new DirectWire()
  const session = new CloudTerminalSession(wire, "stable-tab")
  try {
    await session.start(); await session.attach(terminalID); await session.acquire("acquire")
    const relay = wire.latest()
    wire.delayed.add("control")
    wire.frame(relay, 1, "relay")
    await until(() => wire.latest() !== relay && lastRequest(wire, "control")?.connection === wire.latest())
    const direct = wire.latest()
    assert.notEqual(direct, relay)
    assert.equal(lastRequest(wire, "control")?.connection, direct, "the lease check is out")
    wire.frame(direct, 1, "direct")
    await settle()
    assert.equal(lastRequest(wire, "activate_connection"), undefined, "activation waits for the lease check")
    wire.releaseReplies()
    await until(() => lastRequest(wire, "activate_connection")?.connection === direct && session.snapshot.carrier === "direct")
    assert.equal(lastRequest(wire, "activate_connection")?.connection, direct)
    assert.equal(session.snapshot.carrier, "direct")
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
    const stages: string[] = []
    const session = new CloudTerminalSession(wire, "stable-tab", new TerminalObservation((row) => stages.push(row.stage)))
    try {
      await session.start(); await session.attach(terminalID); await session.acquire("acquire")
      const relay = wire.latest()
      wire.frame(relay, 1, "ready")
      await until(() => stages.includes("direct_failed"))
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

test("a relay too busy for the upgrade is tried again in seconds, a machine refusal only after the long wait", async () => {
  const realNow = Date.now
  let skew = 0
  Date.now = () => realNow() + skew
  try {
    for (const [code, retried] of [["rate_limited", true], ["over_capacity", true], ["terminal_direct_disabled", false]] as const) {
      skew = 0
      const wire = new DirectWire()
      wire.refusals.set("direct_offer", code)
      const stages: string[] = []
      const session = new CloudTerminalSession(wire, "stable-tab", new TerminalObservation((row) => stages.push(row.stage)))
      try {
        await session.start(); await session.attach(terminalID); await session.acquire("acquire")
        const relay = wire.latest()
        wire.frame(relay, 1, "ready")
        await until(() => stages.includes("direct_failed"))
        assert.equal(wire.offers, 1, code)
        wire.refusals.delete("direct_offer")
        skew = 6_000
        wire.frame(relay, 2, "later")
        if (retried) await until(() => wire.offers === 2)
        else await settle()
        assert.equal(wire.offers, retried ? 2 : 1, code)
      } finally { session.dispose() }
    }
  } finally { Date.now = realNow }
})

test("a closed DC rekeys back to the relay naming the direct connection and never replays input", async () => {
  const wire = new DirectWire()
  const stages: string[] = []
  const { session, direct } = await upgraded(wire, new TerminalObservation((row) => stages.push(`${row.stage}:${row.code ?? ""}`)))
  try {
    wire.delayed.add("input")
    const typed = session.input(new TextEncoder().encode("a"))
    void typed.catch(() => undefined)
    await until(() => operations(wire).includes("input"))
    assert.equal(operations(wire).filter((operation) => operation === "input").length, 1)
    wire.dropDC()
    await until(() => session.snapshot.carrier === "relay")
    const rekey = lastRequest(wire, "rekey_connection")!
    assert.deepEqual(rekey.body, { old_connection: direct })
    assert.equal(session.snapshot.carrier, "relay")
    assert.equal(session.snapshot.canType, false, "input waits for the relay connection's first frame")
    wire.releaseReplies()
    await typed
    const relay = wire.latest()
    wire.frame(relay, 1, "back")
    await until(() => lastRequest(wire, "activate_connection")?.connection === relay && session.snapshot.canType)
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
    await until(() => operations(wire).slice(before).includes("capture"))
    const after = wire.requests.slice(before)
    assert.deepEqual(after.map((request) => request.operation), ["rekey_connection", "open_connection", "control", "read", "capture"])
    assert.equal(after[2].body, undefined, "the lease is proved by a read-only control query")
    assert.equal(wire.channels.has(direct), false)
    wire.frame(wire.latest(), 1, "reopened")
    await until(() => session.snapshot.frame?.rev === "reopened" && session.snapshot.hasLease)
    assert.equal(session.snapshot.carrier, "relay")
    assert.equal(session.snapshot.hasLease, true, "the proved lease is kept")
    assert.equal(session.snapshot.frame?.rev, "reopened")
    assert.equal(operations(wire).includes("input"), false)
    assert.equal(wire.requests.slice(before).some((request) => request.operation === "activate_connection"), false)
  } finally { session.dispose() }
})

// Mid-session pauses: a person typing into a healthy terminal must never have a key refused
// by a pause that ends by itself. Each test is a sequence seen on a live machine.

/** `until` for a test that has frozen `Date.now`: a bounded number of event-loop turns. */
async function turns(done: () => boolean, count = 2_000): Promise<void> {
  for (let turn = 0; turn < count && !done(); turn++) await new Promise<void>((resolve) => setImmediate(resolve))
}

/** A frame captured by a machine whose clock is `skewMs` behind this browser's. */
function skewedFrame(wire: Wire, connection: string, seq: number, rev: string, skewMs: number): void {
  const at = (Date.now() - skewMs) / 1000
  wire.emit(connection, "term", { v: 1, type: "terminal_frame", terminal_id: terminalID, connection,
    frame_seq: seq, captured_at: at, frame: { ...frame(rev), at } })
}

test("a still screen on a machine whose clock is behind keeps beating and stays typable", async () => {
  const realNow = Date.now
  let now = realNow()
  Date.now = () => now
  const wire = new Wire()
  wire.lease = { ...held, expires_at: now / 1000 + 90 }
  const session = new CloudTerminalSession(wire, "stable-tab")
  try {
    await session.start(); await session.attach(terminalID); await session.acquire("acquire")
    const connection = wire.latest()
    // The machine's heartbeat recaptures a still screen every 3 s; its clock reads 3.5 s behind ours.
    skewedFrame(wire, connection, 1, "still", 3_500)
    for (let seq = 2; seq <= 6; seq++) {
      now += 2_900
      ;(session as unknown as { checkFreshness(): void }).checkFreshness()
      assert.equal(session.snapshot.state === "stale", false, `a beating screen was called stale before beat ${seq}`)
      assert.equal(session.snapshot.canType, true)
      now += 100
      skewedFrame(wire, connection, seq, "still", 3_500)
    }
  } finally { session.dispose(); Date.now = realNow }
})

test("a key typed while a still screen's next beat is late waits for it instead of being refused", async () => {
  const realNow = Date.now
  let now = realNow()
  Date.now = () => now
  const wire = new Wire()
  wire.lease = { ...held, expires_at: now / 1000 + 90 }
  const session = new CloudTerminalSession(wire, "stable-tab")
  try {
    await session.start(); await session.attach(terminalID); await session.acquire("acquire")
    const connection = wire.latest()
    wire.frame(connection, 1, "still")
    now += 6_500
    ;(session as unknown as { checkFreshness(): void }).checkFreshness()
    assert.equal(session.snapshot.canType, false, "no key goes out on a screen this old")
    const typed = session.input(new TextEncoder().encode("k"))
    let refused: unknown = null
    void typed.catch((error) => { refused = error })
    await settle()
    assert.equal(refused, null, "the key waits for the next frame")
    assert.equal(wire.requests.some((request) => request.operation === "input"), false, "and is not sent before it")
    wire.frame(connection, 2, "still")
    await typed
    assert.equal(wire.requests.filter((request) => request.operation === "input").length, 1)
  } finally { session.dispose(); Date.now = realNow }
})

test("a lease renewal whose receipt never arrives does not take the keyboard away", async (t) => {
  const wire = new Wire()
  wire.lease = { ...held, expires_at: Date.now() / 1000 + 25 }
  const session = new CloudTerminalSession(wire, "stable-tab")
  try {
    await session.start(); await session.attach(terminalID); await session.acquire("acquire")
    wire.frame(wire.latest(), 1, "screen")
    wire.delayed.add("control")
    t.mock.timers.enable({ apis: ["setTimeout"] })
    ;(session as unknown as { lastRenew: number }).lastRenew = Date.now() - 11_000
    ;(session as unknown as { checkFreshness(): void }).checkFreshness()
    await until(() => wire.requests.some((request) => (request.body as { action?: string } | undefined)?.action === "renew"))
    t.mock.timers.tick(10_000)
    await until(() => !(session as unknown as { renewing: boolean }).renewing)
    wire.frame(wire.latest(), 2, "screen")
    assert.notEqual(session.snapshot.state, "unknown")
    assert.equal(session.snapshot.hasLease, true, "one lost renewal receipt says nothing about the lease or the input")
    assert.equal(session.snapshot.canType, true)
    wire.delayed.delete("control")
    await session.input(new TextEncoder().encode("k"))
  } finally { t.mock.timers.reset(); session.dispose() }
})

test("a renewal the machine is too busy to answer is retried, not treated as a lost lease", async () => {
  const wire = new Wire()
  const session = new CloudTerminalSession(wire, "stable-tab")
  try {
    await session.start(); await session.attach(terminalID); await session.acquire("acquire")
    wire.frame(wire.latest(), 1, "screen")
    wire.refusals.set("control", "terminal_busy")
    ;(session as unknown as { lastRenew: number }).lastRenew = Date.now() - 11_000
    ;(session as unknown as { checkFreshness(): void }).checkFreshness()
    await until(() => !(session as unknown as { renewing: boolean }).renewing)
    assert.equal(session.snapshot.hasLease, true)
    assert.equal(session.snapshot.canType, true)
    wire.refusals.delete("control")
    await session.input(new TextEncoder().encode("k"))
  } finally { session.dispose() }
})

test("a key typed as the DC closes waits and goes out once on the relay connection", async () => {
  const wire = new ClosingDirectWire()
  const { session, direct } = await upgraded(wire)
  try {
    const before = wire.requests.length
    // The channel is already closing (readyState is no longer "open") before its close event fires.
    wire.dcOpen = false
    const typed = session.input(new TextEncoder().encode("k"))
    let refused: unknown = null
    void typed.catch((error) => { refused = error })
    await until(() => session.snapshot.carrier === "relay")
    assert.equal(refused, null, "the key was never published, so it waits")
    assert.equal(lastRequest(wire, "rekey_connection")?.connection, wire.latest())
    const relay = wire.latest()
    assert.notEqual(relay, direct)
    wire.frame(relay, 1, "back")
    await typed
    const inputs = wire.requests.slice(before).filter((request) => request.operation === "input")
    assert.deepEqual(inputs.map((request) => request.connection), [relay], "published once, on the relay")
    assert.equal(wire.thrown, 1, "the closing DC refused it before sending")
  } finally { session.dispose() }
})

test("a key rotation the relay refuses for a moment is tried again before the key expires", async () => {
  const realNow = Date.now
  let now = realNow()
  Date.now = () => now
  const wire = new Wire()
  wire.lease = { ...held, expires_at: now / 1000 + 900 }
  const session = new CloudTerminalSession(wire, "stable-tab")
  try {
    await session.start(); await session.attach(terminalID); await session.acquire("acquire")
    const first = wire.latest()
    wire.frame(first, 1, "screen")
    ;(session as unknown as { lastRenew: number }).lastRenew = now
    wire.refusals.set("rekey_connection", "over_capacity")
    now += 450_000
    wire.frame(first, 2, "screen")
    ;(session as unknown as { checkFreshness(): void }).checkFreshness()
    await turns(() => !(session as unknown as { openingNew: boolean }).openingNew && operations(wire).includes("rekey_connection"))
    wire.refusals.delete("rekey_connection")
    now += 6_000
    ;(session as unknown as { lastRenew: number }).lastRenew = now
    wire.frame(first, 3, "screen")
    ;(session as unknown as { checkFreshness(): void }).checkFreshness()
    await turns(() => operations(wire).filter((operation) => operation === "rekey_connection").length === 2)
    assert.equal(operations(wire).filter((operation) => operation === "rekey_connection").length, 2)
  } finally { session.dispose(); Date.now = realNow }
})

test("a key refused for good says why instead of a generic pause", async () => {
  const wire = new Wire()
  const session = new CloudTerminalSession(wire, "stable-tab")
  try {
    await session.start(); await session.attach(terminalID); await session.acquire("acquire")
    wire.frame(wire.latest(), 1, "screen")
    wire.inputResult = "unknown"
    await assert.rejects(session.input(new TextEncoder().encode("a")))
    wire.inputResult = "ok"
    await assert.rejects(session.input(new TextEncoder().encode("b")), /terminal_input_state_unknown/)
  } finally { session.dispose() }
})

/** A DirectWire whose transport refuses a direct connection's request once the DC is not open. */
class ClosingDirectWire extends DirectWire {
  thrown = 0
  override async publishTerminal(request: Record<string, unknown>): Promise<{ sender: string; seq: number }> {
    if (this.carriers.get(request.connection as string) === "direct" && !this.dcOpen && request.operation !== "activate_connection") {
      this.thrown++
      throw Object.assign(new Error("terminal_direct_closed"), { code: "terminal_direct_closed" })
    }
    return super.publishTerminal(request)
  }
}

const delta = async (connection: string, seq: number, baseSeq: number, baseRev: string, rev: string) => {
  const at = Date.now() / 1000
  return { v: 1, type: "terminal_frame_delta", terminal_id: terminalID, connection, frame_seq: seq, base_seq: baseSeq,
    base_rev: baseRev, captured_at: at, rev, at, cols: 80, rows: 1, dead: false, cursor: frame(rev).cursor,
    modes: frame(rev).modes, changed_rows: [{ row: 0, line: rev }], screen_hash: await terminalScreenHash([rev]) }
}

test("taking over a terminal whose screen arrives as deltas makes the tab typable on the same connection", async () => {
  // 2026-10-06, app.clawdline.com: after 接手 the machine's next screen was a delta on the base it
  // had already delivered; the tab had dropped that base when it took control, read the delta as
  // corrupt, threw the connection away and opened one it never attached. Keys never reached it.
  const wire = new Wire()
  wire.deltaSubscribed = true; wire.deltaMachine = true
  const session = new CloudTerminalSession(wire, "stable-tab")
  try {
    await session.start(); await session.attach(terminalID)
    const connection = wire.latest()
    wire.frame(connection, 1, "first")
    await session.acquire("takeover")
    wire.emit(connection, "termd", await delta(connection, 2, 1, "first", "second"))
    await until(() => session.snapshot.canType)
    assert.equal(wire.latest(), connection)
    assert.equal(wire.requests.filter((request) => request.operation === "open_connection").length, 1)
    await session.input(new TextEncoder().encode("x"))
    assert.equal(wire.requests.filter((request) => request.operation === "input" && request.connection === connection).length, 1)
  } finally { session.dispose() }
})

test("a connection discarded for a bad delta reads its terminal again and proves the lease on the new one", async () => {
  const wire = new Wire()
  wire.deltaSubscribed = true; wire.deltaMachine = true
  const session = new CloudTerminalSession(wire, "stable-tab")
  try {
    await session.start(); await session.attach(terminalID); await session.acquire("acquire")
    const old = wire.latest()
    wire.frame(old, 1, "first")
    await until(() => session.snapshot.canType)
    wire.readControl = held
    wire.emit(old, "termd", { ...(await delta(old, 2, 1, "first", "second")), screen_hash: "0".repeat(64) })
    await until(() => wire.latest() !== old && wire.requests.some((request) => request.operation === "read" && request.connection === wire.latest()))
    const fresh = wire.latest()
    // The discarded connection was given back before the new one was asked for: the machine
    // allows two per viewer and refused the third as busy while the old one lingered.
    const order = wire.requests.map((request) => `${request.operation}@${request.connection === old ? "old" : request.connection === fresh ? "new" : "?"}`)
    assert.ok(order.indexOf("release_connection@old") >= 0 && order.indexOf("release_connection@old") < order.indexOf("open_connection@new"), order.join(" "))
    // The new connection proved the same lease rather than asking the person to take control again.
    await until(() => wire.requests.some((request) => request.operation === "control" && request.connection === fresh && request.body === undefined))
    wire.frame(fresh, 1, "restored")
    await until(() => session.snapshot.canType)
    await session.input(new TextEncoder().encode("y"))
    assert.equal(wire.requests.filter((request) => request.operation === "input" && request.connection === fresh).length, 1)
  } finally { session.dispose() }
})

test("keys whose envelopes the machine drops unanswered are each refused, none left waiting unsaid", async (t) => {
  // 2026-10-06 21:01: the machine dropped a page's envelopes as replays and answered nothing.
  // The only way the page can know is the receipt timeout; every key from then on must be refused.
  const wire = new Wire()
  const session = new CloudTerminalSession(wire, "stable-tab")
  try {
    await session.start(); await session.attach(terminalID); await session.acquire("acquire")
    wire.frame(wire.latest(), 1, "ready")
    wire.delayed.add("input")
    t.mock.timers.enable({ apis: ["setTimeout"] })
    const outcomes = Array.from({ length: 6 }, (_, i) => session.input(new TextEncoder().encode(String(i))).then(() => "sent", (error) => (error as Error).message))
    for (let i = 0; i < 20; i++) await new Promise<void>((resolve) => setImmediate(resolve))
    t.mock.timers.tick(10_000)
    for (let i = 0; i < 20; i++) await new Promise<void>((resolve) => setImmediate(resolve))
    t.mock.timers.tick(10_000)
    const settled = await Promise.all(outcomes)
    assert.deepEqual(settled, Array(6).fill("terminal_input_state_unknown"))
    assert.equal(wire.requests.filter((request) => request.operation === "input").length, 4, "nothing past the in-flight window was sent")
  } finally { t.mock.timers.reset(); session.dispose() }
})

test("a lease that lapsed while the tab was idle is let go cleanly: the screen stays and taking control again types", async () => {
  // A background tab's timers slept past the 30-second lease (2026-10-06, 22:50). Nothing was
  // typed under it since the last receipt, so no input is in doubt: the tab no longer holds the
  // terminal, and says so, instead of "input may already have been applied".
  const wire = new Wire()
  const session = new CloudTerminalSession(wire, "stable-tab")
  try {
    await session.start(); await session.attach(terminalID); await session.acquire("acquire")
    wire.frame(wire.latest(), 1, "screen")
    await session.input(new TextEncoder().encode("k"))
    assert.equal(session.snapshot.canType, true)
    wire.refusals.set("control", "lease_expired")
    ;(session as unknown as { lastRenew: number }).lastRenew = Date.now() - 31_000
    ;(session as unknown as { checkFreshness(): void }).checkFreshness()
    await until(() => !(session as unknown as { renewing: boolean }).renewing)
    assert.equal(session.snapshot.hasLease, false)
    assert.equal(session.snapshot.canType, false)
    assert.notEqual(session.snapshot.state, "unknown", "a lapsed idle lease is not an input in doubt")
    assert.equal(session.snapshot.reason, "lease_expired")
    assert.ok(session.snapshot.frame, "the screen stays drawn")
    wire.refusals.delete("control")
    await session.acquire("acquire")
    wire.frame(wire.latest(), 2, "screen again")
    assert.equal(session.snapshot.canType, true)
    await session.input(new TextEncoder().encode("j"))
  } finally { session.dispose() }
})

test("an activation the machine refuses is not asked again on every frame; a fresh connection reads the terminal", async () => {
  // 2026-10-06 22:50-22:53: the upgrade's rekey was answered, its activation refused
  // terminal_invalid, and the tab sent activate_connection again with every frame for minutes.
  const wire = new DirectWire()
  const session = new CloudTerminalSession(wire, "stable-tab")
  try {
    await session.start(); await session.attach(terminalID); await session.acquire("acquire")
    const relay = wire.latest()
    wire.refusals.set("activate_connection", "terminal_invalid")
    wire.frame(relay, 1, "relay")
    await until(() => lastRequest(wire, "rekey_connection") !== undefined && wire.latest() !== relay)
    const direct = wire.latest()
    wire.frame(direct, 1, "direct 1")
    await until(() => lastRequest(wire, "activate_connection") !== undefined)
    await settle()
    for (let seq = 2; seq <= 5; seq++) { wire.frame(direct, seq, `direct ${seq}`); await settle() }
    const activations = wire.requests.filter((request) => request.operation === "activate_connection").length
    assert.equal(activations, 1, "a refused activation was asked again")
    await until(() => lastRequest(wire, "open_connection")?.connection === wire.latest() && wire.latest() !== direct)
    const fresh = wire.latest()
    await until(() => lastRequest(wire, "read")?.connection === fresh)
    wire.frame(fresh, 1, "fresh")
    await until(() => session.snapshot.state === "just_synced" || session.snapshot.state === "live")
    assert.ok(session.snapshot.frame)
    assert.equal(wire.channels.has(relay), false, "the old relay connection is let go")
    assert.equal(wire.channels.has(direct), false, "the refused connection is let go")
  } finally { session.dispose() }
})
