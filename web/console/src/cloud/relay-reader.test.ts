// The relay seam's rules: `node --test web/console/src/cloud/*.test.ts`.
//
// A fake CloudClient stands in for the copied one, holding rows the way it
// does (`_sessionResponse`) and counting transcript asks, which is the number
// that costs an envelope each way across the relay.
import { test } from "node:test"
import assert from "node:assert/strict"
import type { SessionsSnapshot, TranscriptPage } from "@clawdline/contract"
import { documentIdentityForSession } from "../legacy/js/net/document-links.js"
// @ts-expect-error -- Node runs the source test with type stripping.
import { arrangeSessions } from "../session/order.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { RelayReader, headerReadDiagnostics, ROW_RESTATE_MS, TRANSCRIPT_LINE_REREAD_MS, TRANSCRIPT_MAX_REUSE_MS, type CloudEvent, type CloudIdentity, type CloudReadClient, type CloudRow, type CloudSchedules, type CloudSnippets } from "./relay-reader.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { acceptSnapshot } from "../../../core/src/fleet-accept.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { fetchWithDeadline } from "../pages/work/fetch-deadline.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { SESSION_LIST_READ } from "../pages/work/session-assignment.ts"
import { SESSION_INVENTORY_ID } from "../legacy/js/net/client.js"
// @ts-expect-error -- Node runs the source test with type stripping.
import { listRunningAgents, backgroundCounts, showsBackground } from "../session/background.ts"

test("Cloud restores provider work from the rich row when its display list omits agents", async () => {
  for (const fleet of [false, true]) {
    for (const projected of [false, true]) {
      const client = new FakeClient()
      const now = 1_000_000
      const execution = "a".repeat(32)
      const destination = { machineID: "mac-a", sessionID: "s1", executionGeneration: execution }
      const projection = { destination, assistant: "claude" as const, state: "working", freshness: "current" as const, observedAt: now }
      const agents = Array.from({ length: 6 }, (_, i) => ({ id: "agent-" + i, type: "Explore",
        what: "Inspect fixture " + i, state: "running", depth: 1, at: now / 1000 }))
      const rich = (extra: Record<string, unknown> = {}) => ({ machine: "mac-a", session: "s1",
        execution_generation: execution, state: "working", agents, agents_reading: { state: "complete" },
        source: { freshness: "current", observed_at: now / 1000 }, ...extra })
      const held = new Map<string, unknown>([["mac-a\u0000s1", rich()]])
      Object.assign(client, fleet ? { detailSnapshots: held } : { sessionSnapshots: held })
      client.statusSnapshots = new Map([
        [JSON.stringify(["mac-a", SESSION_INVENTORY_ID]), { payload: { at: now / 1000, complete: true,
          snapshot_generation: execution, inventory: { version: 1, sessions: ["s1"] } } }],
        [JSON.stringify(["mac-a", "s1"]), { payload: { machine_id: "mac-a", session_id: "s1",
          execution_generation: execution, snapshot_generation: execution, state: "working", assistant: "claude",
          source: { freshness: "current", observed_at: now / 1000 } } }],
      ])
      const reader = new RelayReader("mac-a", { classicStatus: true, now: () => now,
        ...(fleet ? { fleetTarget: () => destination } : {}),
        ...(projected ? { machineProjection: async () => ({ kind: "ready" as const, complete: true, observedAt: now,
          snapshotGeneration: execution, rows: [projection] }) } : {}),
        // This is the daemon's compact wire: provider work is absent here.
        listPresentations: async () => [{ destination, title: "Fixture Session", assistant: "claude",
          status: { state: "working" }, projection }],
      })
      reader.attach(client)
      const drawn = async () => (await reader.snapshot()).sessions[0]
      assert.deepEqual(backgroundCounts(await drawn()), { shells: 0, running: 6, listed: 6, truncated: 0 })
      assert.equal(showsBackground(backgroundCounts(await drawn())), true)
      held.set("mac-a\u0000s1", rich({ agents: [{ ...agents[0], state: "done" }],
        agents_reading: { state: "complete", truncated: 2 } }))
      assert.deepEqual(backgroundCounts(await drawn()), { shells: 0, running: 0, listed: 1, truncated: 2 })
      held.set("mac-a\u0000s1", rich({ agents_reading: { state: "unknown", reason: "unreadable" } }))
      assert.equal(backgroundCounts(await drawn()).running, null)
      held.set("mac-a\u0000s1", rich({ agents: undefined }))
      assert.equal(showsBackground(backgroundCounts(await drawn())), false, "complete omitted agents means zero")
      for (const extra of [{ machine: "mac-b" }, { session: "s2" },
        { execution_generation: "b".repeat(32) },
        { source: { freshness: "unverified", observed_at: now / 1000 } },
        { source: { freshness: "current", observed_at: (now - 600_000) / 1000 } }]) {
        held.set("mac-a\u0000s1", rich(extra))
        assert.equal(showsBackground(backgroundCounts(await drawn())), false,
          "another or stale reading cannot populate this execution")
      }
      held.set("mac-a\u0000s1", rich({ state: "idle" }))
      assert.equal(backgroundCounts(await drawn()).running, 6, "provider work survives a parent state transition")
      assert.equal(client.reads.length, 0, "held work never starts another read")
    }
  }
})

test("an opened working Cloud Session recovers a missing provider reading without repeated redraw requests", async () => {
  const client = new FakeClient()
  let now = 1_000_000
  const execution = "a".repeat(32)
  const destination = { machineID: "mac-a", sessionID: "s1", executionGeneration: execution }
  const projection = { destination, assistant: "claude" as const, state: "working", freshness: "current" as const, observedAt: now }
  const held = new Map<string, unknown>()
  Object.assign(client, { detailSnapshots: held,
    _machineImplements: (_machine: string, word: string) => word === "sessions.row" ? "yes" : "unknown" })
  const reader = new RelayReader("mac-a", { classicStatus: true, now: () => now, fleetTarget: () => destination,
    machineProjection: async () => ({ kind: "ready", complete: true, observedAt: now,
      snapshotGeneration: execution, rows: [projection] }),
    listPresentations: async () => [{ destination, title: "Fixture Session", assistant: "claude", status: { state: "working" }, projection }],
  })
  reader.attach(client)
  await reader.snapshot()
  await reader.snapshot()
  assert.deepEqual(client.reads, [{ machine: "mac-a", word: "sessions.row", body: { row: "s1" } }])
  now += 6_001
  await reader.snapshot()
  assert.equal(client.reads.length, 2, "a missing reply can be recovered after the existing restatement window")
  held.set("mac-a\u0000s1", { machine: "mac-a", session: "s1", execution_generation: execution,
    state: "working", source: { freshness: "current", observed_at: now / 1000 },
    agents_reading: { state: "complete" }, agents: [{ id: "agent-1", state: "running" }] })
  now += 6_001
  assert.equal(backgroundCounts((await reader.snapshot()).sessions[0]).running, 1)
  assert.equal(client.reads.length, 2, "a held current reading needs no recovery request")
})


test("a diagnostic press waits for a receipt from the selected machine and propagates failure", async () => {
  const reader = new RelayReader("mac-a")
  await assert.rejects(reader.recordSendDiagnostic("unexpected_error"), { code: "cloud_not_carried" })
  const client = new FakeClient()
  let finish: () => void = () => {}
  const seen: unknown[] = []
  Object.assign(client, { sendDiagnostic(machine: string, data: Record<string, unknown>) {
    seen.push({ machine, data })
    return new Promise<void>((resolve) => { finish = resolve })
  } })
  reader.attach(client)
  let delivered = false
  const sending = reader.recordSendDiagnostic("unexpected_error").then(() => { delivered = true })
  await Promise.resolve()
  assert.equal(delivered, false)
  assert.equal((seen[0] as { machine: string }).machine, "mac-a")
  assert.equal(JSON.stringify(seen).includes("message"), false)
  finish()
  await sending
  assert.equal(delivered, true)
  Object.assign(client, { sendDiagnostic: async () => { throw Object.assign(new Error("lost receipt"), { code: "cloud_read_timeout" }) } })
  await assert.rejects(reader.recordSendDiagnostic("unexpected_error"), { code: "cloud_read_timeout" })
})

test("Session header trail distinguishes started, observed, canceled, unavailable and reconnect without identifiers", async () => {
  const saved = globalThis.sessionStorage
  const entries = new Map<string, string>()
  Object.defineProperty(globalThis, "sessionStorage", { configurable: true, value: {
    getItem: (key: string) => entries.get(key) ?? null,
    setItem: (key: string, value: string) => { entries.set(key, value) },
  } })
  try {
    const clock = { t: 100000 }
    const client = new FakeClient()
    const r = reader(client, clock)
    await r.fetch("/v1/work/v2/session-todos/private-id")
    assert.deepEqual(headerReadDiagnostics(clock.t).map((row) => row.stage), ["ask_started", "answer_observed"])
    client.readAnswer = async () => { throw Object.assign(new Error("private payload"), { code: "cloud_read_timeout" }) }
    await assert.rejects(r.fetch("/v1/work/v2/session-todos/private-id"))
    assert.deepEqual(headerReadDiagnostics(clock.t).slice(-2).map((row) => [row.stage, row.code]),
      [["ask_started", "none"], ["read_failed", "cloud_read_timeout"]])
    let release: (value: unknown) => void = () => {}
    client.readAnswer = () => new Promise((resolve) => { release = resolve })
    const controller = new AbortController()
    const pending = r.fetch("/v1/work/v2/human-interventions/conversation%3A10000000-0000-4000-8000-000000000002", { signal: controller.signal })
    await Promise.resolve()
    controller.abort()
    await assert.rejects(pending, { name: "AbortError" })
    release({})
    const offline = new RelayReader("mac-a", { now: () => clock.t })
    await offline.fetch("/v1/work/v2/session-todos/private-id")
    const renewed = new FakeClient()
    renewed.ready = false
    Object.assign(renewed, { lifecycle: () => true })
    const waiting = new RelayReader("mac-a", { now: () => clock.t, reconnectWaitMs: 100 })
    waiting.attach(renewed)
    const next = waiting.fetch("/v1/work/v2/session-todos/private-id")
    waiting.attach(new FakeClient())
    await next
    const trail = headerReadDiagnostics(clock.t)
    assert.ok(trail.some((row) => row.stage === "caller_canceled"))
    assert.ok(trail.some((row) => row.stage === "connection_unavailable"))
    assert.equal(trail.at(-1)?.stage, "answer_observed")
    assert.doesNotMatch(JSON.stringify(trail), /private-id|mac-a|sender|payload/)
  } finally {
    Object.defineProperty(globalThis, "sessionStorage", { configurable: true, value: saved })
  }
})

class FakeClient implements CloudReadClient {
  ready = true
  viewerEvents?: { record(event: string, data: Record<string, unknown>, key?: string): void | { n: number } }
  statusSnapshots?: ReadonlyMap<string, unknown>
  sessionInventoryByMachine = new Map<string, unknown>()
  machineRows: { id: string; freshness: "current" | "stale" | "unknown" }[] = [
    { id: "mac-a", freshness: "current" },
  ]
  rows: CloudRow[] = []
  recovering: string[] = []
  /** Set by the tests that need the one retained ss/ row a read is pinned to. */
  recoverStatusRow?: (machineID: string, sessionID: string, snapshotGeneration: string) => Promise<boolean>
  /** The pinned read a classic page makes; the destination it was given. */
  pinnedReads: { machineID: string; sessionID: string; executionGeneration: string }[] = []
  transcriptForGeneration?: (destination: { machineID: string; sessionID: string; executionGeneration: string }) => Promise<unknown> =
    async (destination) => {
      this.pinnedReads.push(destination)
      return this.answer()
    }
  asks = 0
  answer: () => Promise<unknown> = async () => ({ id: "s1", entries: [{ role: "user", text: "hi" }], signature: "1-1", evidence: "transcript" })
  private listeners = new Set<(event: CloudEvent) => void>()
  events(listener: (event: CloudEvent) => void) {
    this.listeners.add(listener)
    return () => this.listeners.delete(listener)
  }
  async machines() {
    return { machines: this.machineRows, syncing: false, retryAfterMs: 0 }
  }
  emit(event: CloudEvent) {
    for (const fn of this.listeners) fn(event)
  }
  async sessions() {
    return { sessions: this.rows, at: 100, scan: { emptyAuthoritative: this.recovering.length === 0, recovering: this.recovering, failures: [] } }
  }
  transcript() {
    this.asks += 1
    return this.answer()
  }
  olderAsks: number[] = []
  sessionPageReads: { identity: CloudIdentity; word: string; body: Record<string, unknown>; answer: string }[] = []
  _read = async (identity: CloudIdentity, word: string, reqBody: Record<string, unknown>, answer: string) => {
    this.sessionPageReads.push({ identity, word, body: reqBody, answer })
    this.olderAsks.push(reqBody.before as number)
    return { id: "s1", entries: [{ role: "user", text: "earlier" }], signature: "1-1", evidence: "transcript" }
  }
  pushKeyAsks = 0
  pushKey?: () => Promise<unknown> = async () => {
    this.pushKeyAsks += 1
    return { key: "BPk" }
  }
  /** Every reading of the schedule list, with the options it was asked under. */
  scheduleAsks: ({ fresh?: boolean; machine?: string } | undefined)[] = []
  scheduleAnswer: (options?: { fresh?: boolean; machine?: string }) => Promise<CloudSchedules> = async () => ({ schedules: [], at: 0 })
  schedules?: (options?: { fresh?: boolean; machine?: string }) => Promise<CloudSchedules> = (options) => {
    this.scheduleAsks.push(options)
    return this.scheduleAnswer(options)
  }
  /** Every reading of the snippet list: the identity it named and the options. */
  snippetAsks: { identity: CloudIdentity; options?: { fresh?: boolean } }[] = []
  snippetAnswer: () => Promise<CloudSnippets> = async () => ({ snippets: [], at: 0 })
  snippets?: (identity: CloudIdentity, options?: { fresh?: boolean }) => Promise<CloudSnippets> = (identity, options) => {
    this.snippetAsks.push({ identity, options })
    return this.snippetAnswer()
  }
  // The two shapes below stand for two different client methods and are both
  // kept: `snippets()` is the copied client's own named reader, and
  // `_machineRequest` is the generic underneath every word that has no named
  // method. A test that asks for the snippet list reads `snippetAsks`; a test
  // that asks for a work-system word reads `reads`.
  /** Every read sent by word, with the machine and the body it carried. */
  reads: { machine: string; word: string; body: Record<string, unknown> }[] = []
  readAnswer: (word: string) => Promise<unknown> = async (word) => ({ read: word })
  _machineRequest?: (machine: string, word: string, body: Record<string, unknown>, kind: "read") => Promise<unknown> = (
    machine,
    word,
    reqBody,
  ) => {
    this.reads.push({ machine, word, body: reqBody })
    return this.readAnswer(word)
  }
  /** Every session read by its own method: the word, the identity and the id. */
  sessionReads: { word: string; identity: CloudIdentity; id: string }[] = []
  agent?: (identity: CloudIdentity, agent: string) => Promise<unknown> = (identity, agent) => {
    this.sessionReads.push({ word: "agent", identity, id: agent })
    return this.readAnswer("agent")
  }
  shell?: (identity: CloudIdentity, shell: string) => Promise<unknown> = (identity, shell) => {
    this.sessionReads.push({ word: "shell", identity, id: shell })
    return this.readAnswer("shell")
  }
  _place(value: unknown) {
    if (value === "cloud-p1") return { machine: "mac-a", id: "p1", path: "/repo" }
    throw Object.assign(new Error("Project not found"), { code: "not_found", status: 404 })
  }
}

test("older transcript reads name their cursor and do not replace the newest cache", async () => {
  const client = new FakeClient()
  const r = reader(client, { t: 10_000 })
  const older = await body<TranscriptPage>(await r.fetch("/v1/transcript?session=s1&limit=200&before=123"))
  assert.equal(older.entries[0].text, "earlier")
  assert.deepEqual(client.olderAsks, [123])
  assert.deepEqual(client.sessionPageReads, [{
    identity: { machine: "mac-a", session: "s1" }, word: "transcript",
    body: { limit: 200, before: 123, priority: "foreground" }, answer: "transcript.before.123",
  }])
  const latest = await body<TranscriptPage>(await r.fetch("/v1/transcript?session=s1&limit=200"))
  assert.equal(latest.entries[0].text, "hi")
  assert.equal(client.asks, 1)
})

test("same-offset older reads carry distinct held revisions through the relay", async () => {
  const client = new FakeClient()
  const r = reader(client, { t: 10_000 })
  for (const revision of ["agy1:a:123:abc", "agy1:b:123:def"]) {
    await r.fetch(`/v1/transcript?session=s1&before=123&revision=${encodeURIComponent(revision)}`)
  }
  assert.deepEqual(client.sessionPageReads.map((read) => read.body.revision), ["agy1:a:123:abc", "agy1:b:123:def"])
  assert.notEqual(client.sessionPageReads[0].answer, client.sessionPageReads[1].answer)
})

test("an equal-offset page from another conversation cannot be drawn as this Session", async () => {
  const client = new FakeClient()
  client._read = async () => ({ id: "other", entries: [{ role: "user", text: "another conversation" }], signature: "agy1:other", evidence: "transcript" })
  const r = reader(client, { t: 10_000 })
  const response = await r.fetch("/v1/transcript?session=s1&before=123&revision=agy1:held")
  assert.equal(response.status, 502)
  assert.equal((await response.json()).error, "bad_payload")
})

/** A snippet row as the copied client tags it, with nothing of anybody's in it. */
function snippet(machine: string, id: string, extra: Record<string, unknown> = {}) {
  return { id, machine, title: "a title", body: "a body", scope: "global", position: 0, ...extra }
}

/** A schedule row as the copied client tags it: the machine's own row, plus the machine. */
function schedule(machine: string, id: string, extra: Record<string, unknown> = {}) {
  return { id, machine, title: "a schedule", enabled: true, project_dir: "/tmp/p", ...extra }
}

function row(machine: string, session: string, extra: Record<string, unknown> = {}): CloudRow {
  return { id: session, machine, session, identity: { machine, session }, state: "idle", work_state: "ready", ...extra }
}

function inventory(machine: string, ...sessions: string[]) {
  return { ids: new Set(sessions.map((session) => machine + "\u0000" + session)) }
}

function reader(client: FakeClient, clock: { t: number }) {
  const r = new RelayReader("mac-a", { now: () => clock.t })
  r.attach(client)
  return r
}

async function body<T>(res: Response): Promise<T> {
  return (await res.json()) as T
}

test("the list keeps each selected row's document identity", async () => {
  const client = new FakeClient()
  client.rows = [row("mac-a", "s1"), row("mac-b", "s2"), row("mac-a", "s3")]
  const r = reader(client, { t: 1000 })
  const snap = await body<SessionsSnapshot>(await r.fetch("/v1/sessions"))
  assert.deepEqual(snap.sessions.map((s) => s.id), ["s1", "s3"])
  assert.equal((snap.sessions[0] as unknown as { machine: string }).machine, "mac-a")
  assert.deepEqual(snap.sessions[0].identity, { machine: "mac-a", session: "s1" })
  assert.deepEqual(documentIdentityForSession(snap.sessions, "s1", "cloud"), { machine: "mac-a", session: "s1" })
})

test("Cloud update status reads the selected machine and preserves its answer", async () => {
  const client = new FakeClient()
  const status = {
    state: "update_available",
    running: { version: "v0.10.0", stamp: "old" },
    latest: { version: "v0.11.0", stamp: "new" },
    apply: { state: "idle" },
  }
  client.readAnswer = async (word) => word === "update" ? status : { read: word }
  const r = reader(client, { t: 1000 })
  assert.deepEqual(await body(await r.fetch("/v1/update")), status)
  assert.deepEqual(client.reads, [{ machine: "mac-a", word: "update", body: {} }])
  const extra = await r.fetch("/v1/update?machine=mac-b")
  assert.equal(extra.status, 501, "a query must not silently read another machine")
  assert.equal((await body<{ error: string }>(extra)).error, "cloud_not_carried")
  assert.equal(client.reads.length, 1)
})

test("the original page keeps rich titles only for current executions named by ss", async () => {
  const client = new FakeClient()
  const generation = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  client.rows = [row("mac-a", "s1", { label: "Project title", execution_generation: generation }),
    row("mac-a", "s2", { label: "Old execution", execution_generation: generation }),
    row("mac-b", "s1", { label: "Other machine", execution_generation: generation })]
  client.statusSnapshots = new Map([
    [JSON.stringify(["mac-a", "__clawdline_inventory_v1__"]), { payload: {
      complete: true, inventory: { version: 1, sessions: ["s1", "s2"] },
    } }],
    [JSON.stringify(["mac-a", "s1"]), { payload: { execution_generation: generation } }],
    [JSON.stringify(["mac-a", "s2"]), { payload: { execution_generation: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" } }],
  ])
  const r = new RelayReader("mac-a", { classicStatus: true, now: () => 1000 })
  r.attach(client)
  const snap = await r.snapshot()
  assert.deepEqual(snap.sessions.map((session) => session.label), ["Project title"])
  assert.equal(snap.scan.complete, false)
  assert.equal(snap.scan.emptyAuthoritative, false)
})

/** The ss/ pass as a machine publishes it: the marker, then one row per Session. */
function statusPass(machine: string, generation: string, ...sessions: string[]) {
  return new Map<string, unknown>([
    [JSON.stringify([machine, "__clawdline_inventory_v1__"]),
      { payload: { complete: true, snapshot_generation: generation, inventory: { version: 1, sessions } } }],
    ...sessions.map((session) => [JSON.stringify([machine, session]),
      { payload: { execution_generation: generation } }] as [string, unknown]),
  ])
}

const settled = () => new Promise((resolve) => setTimeout(resolve, 0))

test("the row under an opened Session is asked for, not waited for", async () => {
  const client = new FakeClient()
  const generation = "a".repeat(32)
  // The machine's pass names both Sessions; only one rich row is held, and the
  // one a person has open is the other.
  client.rows = [row("mac-a", "s1", { execution_generation: generation })]
  client.statusSnapshots = statusPass("mac-a", generation, "s1", "s2")
  const clock = { t: 1000 }
  const r = new RelayReader("mac-a", { classicStatus: true, now: () => clock.t,
    fleetTarget: () => ({ machineID: "mac-a", sessionID: "s2", executionGeneration: generation }) })
  r.attach(client)
  await r.snapshot()
  await settled()
  assert.deepEqual(client.reads, [{ machine: "mac-a", word: "sessions.snapshot", body: {} }],
    "the opened Session's row is asked for")

  await r.snapshot()
  await settled()
  assert.equal(client.reads.length, 1, "and not asked for again within ROW_RESTATE_MS")

  clock.t += ROW_RESTATE_MS
  await r.snapshot()
  await settled()
  assert.equal(client.reads.length, 2, "and asked again once that has passed")

  client.rows = [...client.rows, row("mac-a", "s2", { execution_generation: generation })]
  clock.t += ROW_RESTATE_MS
  const snap = await r.snapshot()
  await settled()
  assert.deepEqual(snap.sessions.map((session) => session.id), ["s1", "s2"])
  assert.equal(client.reads.length, 2, "a row that is here is not asked for")
})

test("an authorized fleet presentation opens a conversation when rich replay is missing", async () => {
  const client = new FakeClient()
  const generation = "a".repeat(32)
  const pass = "b".repeat(32)
  const target = { machineID: "mac-a", sessionID: "s2", executionGeneration: generation }
  client.statusSnapshots = new Map([
    [JSON.stringify(["mac-a", "__clawdline_inventory_v1__"]), { payload: {
      complete: true, snapshot_generation: pass, inventory: { version: 1, sessions: ["s2"] },
    } }],
    [JSON.stringify(["mac-a", "s2"]), { payload: {
      machine_id: "mac-a", session_id: "s2", snapshot_generation: pass,
      execution_generation: generation, state: "idle", assistant: "claude", backend: "tmux",
      source: { freshness: "current", observed_at: 1 },
    } }],
  ])
  const r = new RelayReader("mac-a", { classicStatus: true, now: () => 1000,
    fleetTarget: () => target,
    fleetPresentation: () => ({ title: "Opened on Linux" }),
  })
  r.attach(client)
  const snapshot = await r.snapshot()
  await settled()
  assert.deepEqual(snapshot.sessions.map((session) => [session.id, session.label, session.execution_generation]),
    [["s2", "Opened on Linux", generation]])
  assert.deepEqual(client.reads, [], "the already authorized list does not request a second rich list")

  const changed = new Map(client.statusSnapshots)
  changed.set(JSON.stringify(["mac-a", "s2"]), { payload: {
    machine_id: "mac-a", session_id: "s2", snapshot_generation: pass,
    execution_generation: "c".repeat(32), state: "idle", source: { freshness: "current", observed_at: 1 },
  } })
  client.statusSnapshots = changed
  assert.deepEqual((await r.snapshot()).sessions, [], "a later execution never inherits the opened row")
})

test("a single-machine page fills missing rich rows from one pinned list read", async () => {
  const client = new FakeClient()
  const generation = "a".repeat(32)
  const pass = "b".repeat(32)
  client.statusSnapshots = new Map([
    [JSON.stringify(["mac-a", "__clawdline_inventory_v1__"]), { payload: {
      complete: true, snapshot_generation: pass, inventory: { version: 1, sessions: ["s1", "s2"] },
    } }],
    ...["s1", "s2"].map((session) => [JSON.stringify(["mac-a", session]), { payload: {
      machine_id: "mac-a", session_id: session, snapshot_generation: pass,
      execution_generation: generation, state: "idle", assistant: "claude", backend: "tmux",
      source: { freshness: "current", observed_at: 1 },
    } }] as [string, unknown]),
  ])
  let reads = 0
  const r = new RelayReader("mac-a", { classicStatus: true, now: () => 1000,
    listPresentations: async () => {
      reads++
      return ["s1", "s2"].map((session) => ({
        destination: { machineID: "mac-a", sessionID: session, executionGeneration: generation },
        title: "Title " + session,
      }))
    },
  })
  r.attach(client)
  const first = await r.snapshot()
  const second = await r.snapshot()
  assert.deepEqual(first.sessions.map((session) => session.label), ["Title s1", "Title s2"])
  assert.equal(first.scan.complete, true)
  assert.deepEqual(second.sessions.map((session) => session.label), ["Title s1", "Title s2"])
  assert.equal(reads, 1)
})

test("a signed marker accepts missing rows from the same pinned direct-list generation", async () => {
  const client = new FakeClient()
  const execution = "a".repeat(32), pass = "b".repeat(32)
  client.statusSnapshots = markerOnly("mac-a", pass, 1, "s1", "s2", "s3")
  const r = new RelayReader("mac-a", { classicStatus: true, now: () => 1000,
    listPresentations: async () => ["s1", "s2", "s3"].map((session) => ({
      destination: { machineID: "mac-a", sessionID: session, executionGeneration: execution },
      title: "Title " + session, status: { state: "idle" },
      projection: { destination: { machineID: "mac-a", sessionID: session, executionGeneration: execution },
        state: "idle", freshness: "current", observedAt: 1000, snapshotGeneration: "direct:1000" },
    })),
  })
  r.attach(client)
  const result = await r.snapshot()
  assert.deepEqual(result.sessions.map((row) => row.id), ["s1", "s2", "s3"])
  assert.equal(result.scan.complete, true)
})

test("a waiting single-machine list asks again after a failed pinned read without a new marker event", async () => {
  const client = new FakeClient()
  client.statusSnapshots = new Map()
  let reads = 0
  const destination = { machineID: "mac-a", sessionID: "s1", executionGeneration: "a".repeat(32) }
  const r = new RelayReader("mac-a", { classicStatus: true,
    listPresentations: async () => ++reads === 1 ? null : [{ destination, title: "Recovered",
      status: { state: "idle" } }],
  })
  r.attach(client)
  const answer = await r.fetch("/v1/sessions", { signal: new AbortController().signal,
    headers: { "X-Clawdline-Initial-Read": "1" } })
  const snapshot = await answer.json() as SessionsSnapshot
  assert.deepEqual(snapshot.sessions.map((row) => row.label), ["Recovered"])
  assert.equal(reads, 2)
})

test("a single-machine list with no marker does not cache its first partial batch as complete", async () => {
  const client = new FakeClient()
  client.statusSnapshots = new Map()
  let now = 1000, reads = 0
  const counts: (number | null)[] = []
  const r = new RelayReader("mac-a", { classicStatus: true, now: () => now,
    listPresentations: async (_machine, expectedCount) => {
      counts.push(expectedCount)
      reads++
      return Array.from({ length: reads === 1 ? 3 : 11 }, (_, index) => ({
        destination: { machineID: "mac-a", sessionID: "s" + index, executionGeneration: "a".repeat(32) },
        title: "Session " + index, status: { state: "idle" },
      }))
    },
  })
  r.attach(client)
  const first = await r.snapshot()
  assert.equal(first.sessions.length, 3)
  assert.equal(first.scan.complete, false)
  assert.equal((await r.snapshot()).sessions.length, 3)
  assert.equal(reads, 1, "a second render shares the bounded read")
  now += 5_001
  const refreshed = await r.snapshot()
  assert.equal(refreshed.sessions.length, 11)
  assert.equal(refreshed.scan.complete, false, "eleven rows without a signed marker still prove no absence")
  assert.deepEqual(counts, [null, null])
})

test("a cross-machine conversation uses a verified list when its retained status marker is missing", async () => {
  const client = new FakeClient()
  const timings: { event: string; data: Record<string, unknown> }[] = []
  client.viewerEvents = { record: (event, data) => { timings.push({ event, data }) } }
  const generation = "a".repeat(32)
  const target = { machineID: "mac-a", sessionID: "s1", executionGeneration: generation }
  client.statusSnapshots = new Map()
  const r = new RelayReader("mac-a", { classicStatus: true, now: () => 1000,
    fleetTarget: () => target,
    listPresentations: async () => [{ destination: target, title: "Remote Session", status: { state: "idle" } }],
    resolveSession: async () => target,
  })
  r.attach(client)
  const snapshot = await r.snapshot()
  assert.deepEqual(snapshot.sessions.map((session) => session.label), ["Remote Session"])
  assert.equal(snapshot.scan.complete, false, "a display read is not a signed deletion barrier")
  const transcript = await r.fetch("/v1/transcript?session=s1&limit=200")
  assert.equal(transcript.status, 200)
  assert.deepEqual(client.pinnedReads, [target])
  assert.equal(timings.length, 1)
  assert.equal(timings[0].event, "cloud.transcript.first_read")
  assert.equal(timings[0].data.session, "s1")
  assert.equal(timings[0].data.outcome, "ready")
  for (const field of ["total_ms", "sessions_ms", "words_ms", "pin_ms", "wire_ms"]) {
    assert.equal(typeof timings[0].data[field], "number", field)
  }
  assert.equal(JSON.stringify(timings).includes("Remote Session"), false)
  await r.fetch("/v1/transcript?session=s1&limit=200")
  assert.equal(timings.length, 1, "cached rereads do not spend another diagnostic event")
})

test("a verified list cannot open a different execution under an old fleet target", async () => {
  const client = new FakeClient()
  const current = { machineID: "mac-a", sessionID: "s1", executionGeneration: "a".repeat(32) }
  client.statusSnapshots = new Map()
  const r = new RelayReader("mac-a", { classicStatus: true, now: () => 1000,
    fleetTarget: () => ({ ...current, executionGeneration: "b".repeat(32) }),
    resolveSession: async () => current,
  })
  r.attach(client)
  const transcript = await r.fetch("/v1/transcript?session=s1&limit=200")
  assert.equal(transcript.status, 409)
  assert.deepEqual(client.pinnedReads, [])
})

test("a selected verified fleet row reads without waiting for a cut word list or missing status replay", async () => {
  const client = new FakeClient()
  const target = { machineID: "mac-a", sessionID: "s1", executionGeneration: "a".repeat(32) }
  client.statusSnapshots = new Map()
  ;(client as FakeClient & { machineDescriptors: Map<string, unknown> }).machineDescriptors = new Map([
    ["mac-a", { machine: { commands: Array.from({ length: 64 }, (_, n) => `other.${n}`) } }],
  ])
  ;(client as FakeClient & { orchestratorSnapshots: Map<string, unknown> }).orchestratorSnapshots = new Map()
  ;(client as FakeClient & { machineLacks: Map<string, Set<string>> }).machineLacks = new Map()
  client.events = () => { throw new Error("a pinned read must not wait for a descriptor or retained marker") }
  let resolved = 0
  const r = new RelayReader("mac-a", { classicStatus: true,
    fleetTarget: () => target,
    fleetPresentation: () => ({ title: "Verified Session" }),
    resolveSession: async () => { resolved++; return null },
  })
  r.attach(client)
  assert.equal((await r.fetch("/v1/transcript?session=s1&limit=200")).status, 200)
  assert.deepEqual(client.pinnedReads, [target])
  assert.equal(resolved, 0, "the exact selected row already supplies the pinned target")

  const changed = new FakeClient()
  changed.statusSnapshots = new Map([[JSON.stringify(["mac-a", "s1"]),
    { payload: { execution_generation: "b".repeat(32) } }]])
  const refused = new RelayReader("mac-a", { classicStatus: true,
    fleetTarget: () => target,
    fleetPresentation: () => ({ title: "Old Session" }),
    resolveSession: async () => null,
  })
  refused.attach(changed)
  assert.equal((await refused.fetch("/v1/transcript?session=s1&limit=200")).status, 409)
  assert.deepEqual(changed.pinnedReads, [], "newer status cannot be bypassed by an old card")
})

/** One machine's pass with the marker only: every row is still on its way. */
function markerOnly(machine: string, pass: string, at: number, ...sessions: string[]) {
  return new Map<string, unknown>([
    [JSON.stringify([machine, "__clawdline_inventory_v1__"]), { payload: {
      complete: true, inventory: { version: 1, sessions }, at, snapshot_generation: pass } }],
  ])
}

/** The row the machine publishes for one Session of that pass. */
function statusRow(pass: string, generation: string, at: number) {
  return { payload: { snapshot_generation: pass, execution_generation: generation, projected_at: at,
    source: { freshness: "current", observed_at: at } } }
}

test("a verified pinned batch fills a single-machine list while one signed row is missing", async () => {
  const client = new FakeClient()
  const pass = "c".repeat(32)
  const generation = "a".repeat(32)
  const destination = { machineID: "mac-a", sessionID: "s1", executionGeneration: generation }
  client.statusSnapshots = markerOnly("mac-a", pass, 1, "s1")
  const r = new RelayReader("mac-a", { classicStatus: true, now: () => 1000,
    listPresentations: async () => [{ destination, title: "Verified Session",
      status: { state: "idle", work_state: "unknown" },
      projection: { destination, state: "idle", freshness: "current", observedAt: 1000,
        snapshotGeneration: "direct:1000" } }],
  })
  r.attach(client)
  const snapshot = await r.snapshot()
  assert.deepEqual(snapshot.sessions.map((row) => row.label), ["Verified Session"])
  assert.equal(snapshot.scan.complete, true)
})

test("a newer fleet batch prevents an older signed marker from declaring a shorter list complete", async () => {
  const client = new FakeClient()
  const pass = "c".repeat(32), execution = "a".repeat(32)
  client.statusSnapshots = markerOnly("mac-a", pass, 1, "s1")
  ;(client.statusSnapshots as Map<string, unknown>).set(JSON.stringify(["mac-a", "s1"]),
    statusRow(pass, execution, 1))
  client.rows = [row("mac-a", "s1", { execution_generation: execution })]
  const batch = ["s1", "s2", "s3"].map((session) => {
    const destination = { machineID: "mac-a", sessionID: session, executionGeneration: execution }
    return { destination, title: "Title " + session, status: { state: "idle" as const },
      projection: { destination, state: "idle", freshness: "current" as const,
        observedAt: 2000, snapshotGeneration: "direct:2000" } }
  })
  let reads = 0
  const r = new RelayReader("mac-a", { classicStatus: true, now: () => 2000,
    fleetPresentations: () => batch,
    listPresentations: async () => { reads++; return batch },
  })
  r.attach(client)
  const pending = await r.snapshot()
  assert.deepEqual(pending.sessions.map((session) => session.id), ["s1", "s2", "s3"])
  assert.equal(pending.scan.complete, false)
  assert.equal(reads, 1)

  client.statusSnapshots = markerOnly("mac-a", "d".repeat(32), 2, "s1", "s2", "s3")
  const settled = await r.snapshot()
  assert.deepEqual(settled.sessions.map((session) => session.id), ["s1", "s2", "s3"])
  assert.equal(settled.scan.complete, true)
})

test("an older retained direct row cannot satisfy a newer signed marker", async () => {
  const client = new FakeClient()
  const destination = { machineID: "mac-a", sessionID: "s1", executionGeneration: "a".repeat(32) }
  client.statusSnapshots = markerOnly("mac-a", "c".repeat(32), 3, "s1")
  const r = new RelayReader("mac-a", { classicStatus: true, now: () => 3000,
    listPresentations: async () => [{ destination, title: "Old title", status: { state: "idle" },
      projection: { destination, state: "idle", freshness: "current", observedAt: 2000,
        snapshotGeneration: "direct:2000" } }],
  })
  r.attach(client)
  const snapshot = await r.snapshot()
  assert.equal(snapshot.sessions.length, 0)
  assert.equal(snapshot.scan.complete, false)
})

test("a signed unverified Session stays visible with its honest source state", async () => {
  const client = new FakeClient()
  const pass = "c".repeat(32), execution = "a".repeat(32)
  const destination = { machineID: "mac-a", sessionID: "s1", executionGeneration: execution }
  client.statusSnapshots = markerOnly("mac-a", pass, 1, "s1")
  ;(client.statusSnapshots as Map<string, unknown>).set(JSON.stringify(["mac-a", "s1"]),
    { payload: { machine_id: "mac-a", session_id: "s1", execution_generation: execution,
      snapshot_generation: pass, state: "idle", source: { freshness: "unverified", observed_at: 1 } } })
  const r = new RelayReader("mac-a", { classicStatus: true, now: () => 1000,
    listPresentations: async () => [{ destination, title: "Retained Session", status: { state: "idle" },
      projection: { destination, state: "idle", freshness: "stale", observedAt: 1000,
        snapshotGeneration: pass } }],
  })
  r.attach(client)
  const snapshot = await r.snapshot()
  assert.deepEqual(snapshot.sessions.map((session) => session.label), ["Retained Session"])
  assert.equal(snapshot.sessions[0].source?.freshness, "unverified")
})

test("the single-machine page uses the fleet projection before a blocked rich snapshot", async () => {
  const client = new FakeClient()
  client.sessions = async () => { throw new Error("the rich snapshot must not gate the list") }
  const execution = "a".repeat(32)
  const rows = ["s1", "s2", "s3"].map((sessionID) => ({
    destination: { machineID: "mac-a", sessionID, executionGeneration: execution },
    state: "idle", freshness: "current" as const, observedAt: 1000,
    machineScope: sessionID === "s1", coordinatorBound: sessionID === "s1",
  }))
  let titles = 3, now = 1000
  let presentationGeneration = "a"
  const r = new RelayReader("mac-a", { classicStatus: true, now: () => now,
    machineProjection: async () => ({ kind: "ready", complete: true, observedAt: now,
      snapshotGeneration: "a".repeat(32), presentationGeneration, rows }),
    listPresentations: async () => rows.slice(0, titles).map((row) => ({
      destination: row.destination, title: "Title " + row.destination.sessionID,
      status: { state: "idle" }, projection: row,
    })),
  })
  r.attach(client)
  const first = await r.snapshot()
  assert.deepEqual(first.sessions.map((row) => row.label), ["Title s1", "Title s2", "Title s3"])
  assert.equal(first.sessions[0].machine_scope, true)
  assert.equal((first.sessions[0] as typeof first.sessions[0] & { coordinator_bound?: boolean }).coordinator_bound, true)
  assert.equal(first.sessions[1].machine_scope, false)
  assert.equal(first.scan.complete, true)
  titles = 1
  presentationGeneration = "b"
  const partial = await r.snapshot()
  assert.deepEqual(partial.sessions.map((row) => row.id), ["s1", "s2", "s3"])
  assert.equal(partial.scan.complete, false)
  titles = 3
  now += 5_001
  const restored = await r.snapshot()
  assert.deepEqual(restored.sessions.map((row) => row.label), ["Title s1", "Title s2", "Title s3"])
  assert.equal(restored.scan.complete, true)
})

test("the Cloud single-machine list keeps status ancestry through its displayed rows", async () => {
  const client = new FakeClient()
  client.sessions = async () => { throw new Error("the rich snapshot must not gate the list") }
  const execution = "a".repeat(32)
  const rows = ["root", "child", "other"].map((sessionID) => ({
    destination: { machineID: "mac-a", sessionID, executionGeneration: execution },
    state: sessionID === "child" ? "working" as const : "idle" as const, freshness: "current" as const,
    observedAt: 1000, parentSessionID: sessionID === "child" ? "root" : undefined,
  }))
  const reader = new RelayReader("mac-a", { classicStatus: true, now: () => 1000,
    machineProjection: async () => ({ kind: "ready", complete: true, observedAt: 1000,
      snapshotGeneration: "a".repeat(32), rows }),
    listPresentations: async () => rows.map((row) => ({
      destination: row.destination, title: row.destination.sessionID, status: { state: row.state }, projection: row,
    })),
  })
  reader.attach(client)
  const snapshot = await reader.snapshot()
  assert.equal(snapshot.sessions.find((row) => row.id === "child")?.parent_session_id, "root")
  assert.deepEqual(arrangeSessions({ sessions: snapshot.sessions, filter: "", tasks: [], shaping: () => true,
    hold: null }).map((row) => row.id), ["other", "root", "child"])
})

test("a projected state transition keeps the pinned conversation for Session work", async () => {
  const client = new FakeClient()
  const execution = "a".repeat(32)
  const destination = { machineID: "mac-a", sessionID: "s1", executionGeneration: execution }
  const row = { destination, state: "waiting", freshness: "current" as const, observedAt: 1000 }
  const reader = new RelayReader("mac-a", { classicStatus: true, now: () => 1000,
    machineProjection: async () => ({ kind: "ready", complete: true, observedAt: 1000,
      snapshotGeneration: execution, rows: [row] }),
    listPresentations: async () => [{ destination, title: "A Session",
      status: { state: "working", sessionId: "conversation-1", work_state: "working" }, projection: row }],
  })
  reader.attach(client)
  const snapshot = await reader.snapshot()
  assert.equal(snapshot.sessions[0].state, "waiting", "the current projection still decides activity")
  assert.equal(snapshot.sessions[0].sessionId, "conversation-1", "the Board read keeps its stable conversation")
})

// On 2026-10-12 a Session stopped on a menu was open on app.clawdline.com and
// its rich s/ row was arriving, yet the waiting card said Clawdline could not
// read the choices: the row drawn was put together from the status and the
// display list, and neither carries the question.
test("the opened waiting Session draws the question its current rich row carries", async () => {
  const client = new FakeClient()
  client.sessions = async () => { throw new Error("the rich snapshot must not gate the list") }
  const execution = "a".repeat(32)
  const now = 1_000_000
  const rows = ["s1", "s2"].map((sessionID) => ({
    destination: { machineID: "mac-a", sessionID, executionGeneration: execution },
    state: "waiting", freshness: "current" as const, observedAt: now,
  }))
  const menu = { question: "Tea or coffee?", options: [{ n: 1, label: "Tea", can: true }, { n: 2, label: "Coffee", can: true }] }
  const rich = (over: Record<string, unknown> = {}) => ({ machine: "mac-a", session: "s1", execution_generation: execution,
    state: "waiting", menu, screen_reading: "read", source: { freshness: "current", observed_at: now / 1000 }, ...over })
  const details = new Map<string, unknown>([["mac-a\u0000s1", rich()], ["mac-a\u0000s2", rich({ session: "s2" })]])
  ;(client as unknown as { detailSnapshots: Map<string, unknown> }).detailSnapshots = details
  let target = { machineID: "mac-a", sessionID: "s1", executionGeneration: execution }
  const r = new RelayReader("mac-a", { classicStatus: true, now: () => now, fleetTarget: () => target,
    machineProjection: async () => ({ kind: "ready", complete: true, observedAt: now,
      snapshotGeneration: "a".repeat(32), presentationGeneration: "a", rows }),
    listPresentations: async () => rows.map((row) => ({
      destination: row.destination, title: "Title " + row.destination.sessionID,
      status: { state: "waiting" }, projection: row,
    })),
  })
  r.attach(client)
  const opened = await r.snapshot()
  const row = (snapshot: SessionsSnapshot, id: string) => snapshot.sessions.find((s) => s.id === id) as
    { menu?: { question?: string }; screen_reading?: string } | undefined
  assert.equal(row(opened, "s1")?.menu?.question, "Tea or coffee?")
  assert.equal(row(opened, "s1")?.screen_reading, "read")
  assert.equal(row(opened, "s2")?.menu, undefined,
    "a detail's own copy is read only for the detail that is open")
  details.set("mac-a\u0000s1", rich({ execution_generation: "b".repeat(32) }))
  assert.equal(row(await r.snapshot(), "s1")?.menu, undefined, "a rich row of another execution is not this question")
  details.set("mac-a\u0000s1", rich({ source: { freshness: "current", observed_at: (now - 600_000) / 1000 } }))
  assert.equal(row(await r.snapshot(), "s1")?.menu, undefined, "a stale rich row is not drawn as the question now")
  target = { machineID: "mac-a", sessionID: "s2", executionGeneration: execution }
  assert.equal(row(await r.snapshot(), "s2")?.menu?.question, "Tea or coffee?")
  // Opening a detail empties its own copy, and a Session waiting on a question
  // sends no new row: the row the client already holds is the question.
  details.delete("mac-a\u0000s2")
  ;(client as unknown as { sessionSnapshots: Map<string, unknown> }).sessionSnapshots =
    new Map([["mac-a\u0000s2", rich({ session: "s2" })]])
  assert.equal(row(await r.snapshot(), "s2")?.menu?.question, "Tea or coffee?",
    "the held row stands in for a detail copy that was emptied on open")
})

// The machine sends a rich row only when the row changes, and a Session
// stopped on a question does not: on 2026-10-12 a page that opened it later
// waited twenty minutes for a row the machine never sent again.
test("an opened waiting Session with no question in hand asks the machine to restate, once per window", async () => {
  const client = new FakeClient()
  client.sessions = async () => { throw new Error("the rich snapshot must not gate the list") }
  const execution = "a".repeat(32)
  let now = 1_000_000
  const rows = [{ destination: { machineID: "mac-a", sessionID: "s1", executionGeneration: execution },
    state: "waiting", freshness: "current" as const, observedAt: now }]
  ;(client as unknown as { detailSnapshots: Map<string, unknown> }).detailSnapshots = new Map()
  const r = new RelayReader("mac-a", { classicStatus: true, now: () => now,
    fleetTarget: () => rows[0].destination,
    machineProjection: async () => ({ kind: "ready", complete: true, observedAt: now,
      snapshotGeneration: "a".repeat(32), presentationGeneration: "a", rows }),
    listPresentations: async () => rows.map((row) => ({
      destination: row.destination, title: "Title", status: { state: "waiting" }, projection: row })),
  })
  const recorded: { event: string; data: Record<string, unknown> }[] = []
  ;(client as unknown as { viewerEvents: unknown }).viewerEvents = {
    record: (event: string, data: Record<string, unknown>) => { recorded.push({ event, data }) } }
  r.attach(client)
  await r.snapshot()
  await r.snapshot()
  const asked = () => client.reads.filter((read) => read.word === "sessions.snapshot").length
  assert.equal(asked(), 1, "asked once, not on every redraw")
  assert.deepEqual(recorded.map((row) => [row.event, row.data.session, row.data.rich]),
    [["cloud.opened_question.missing", "s1", false]], "the machine's log hears what the page held, once per ask")
  now += 6_001
  await r.snapshot()
  assert.equal(asked(), 2, "asked again once the window passed")
  ;(client as unknown as { _machineImplements(machine: string, word: string): string })._machineImplements =
    (_machine, word) => word === "sessions.snapshot.initial" ? "yes" : "unknown"
  now += 6_001
  await r.snapshot()
  assert.equal(client.reads.at(-1)?.word, "sessions.snapshot.initial",
    "a machine that lists the immediate word is asked with it, not left to its next pass")
  ;(client as unknown as { _machineImplements(machine: string, word: string): string })._machineImplements =
    (_machine, word) => word === "sessions.row" || word === "sessions.snapshot.initial" ? "yes" : "unknown"
  now += 6_001
  const asking = await r.snapshot()
  assert.deepEqual(client.reads.at(-1), { machine: "mac-a", word: "sessions.row", body: { row: "s1" } },
    "a machine that can state one row is asked for just that row")
  assert.equal((asking.sessions[0] as { menu_arriving?: boolean }).menu_arriving, true,
    "the card says the question is on its way while it is asked for")
})

// The one-machine page names no fleet target. On 2026-10-12 a phone on it drew
// "could not read its choices" for a Session whose rich row, options and all,
// the page held.
test("a waiting row on the one-machine page draws the question its held rich row carries", async () => {
  const client = new FakeClient()
  client.sessions = async () => { throw new Error("the rich snapshot must not gate the list") }
  const execution = "a".repeat(32)
  const now = 1_000_000
  const rows = [{ destination: { machineID: "mac-a", sessionID: "s1", executionGeneration: execution },
    state: "waiting", freshness: "current" as const, observedAt: now }]
  const menu = { question: "Tea or coffee?", options: [{ n: 1, label: "Tea", can: true }] }
  ;(client as unknown as { sessionSnapshots: Map<string, unknown> }).sessionSnapshots = new Map([["mac-a\u0000s1",
    { machine: "mac-a", session: "s1", execution_generation: execution, state: "waiting", menu,
      screen_reading: "read", source: { freshness: "current", observed_at: now / 1000 } }]])
  const r = new RelayReader("mac-a", { classicStatus: true, now: () => now,
    machineProjection: async () => ({ kind: "ready", complete: true, observedAt: now,
      snapshotGeneration: "a".repeat(32), presentationGeneration: "a", rows }),
    listPresentations: async () => rows.map((row) => ({
      destination: row.destination, title: "Title", status: { state: "waiting" }, projection: row })),
  })
  r.attach(client)
  const snapshot = await r.snapshot()
  const drawn = snapshot.sessions[0] as { menu?: { question?: string }; menu_arriving?: boolean }
  assert.equal(drawn.menu?.question, "Tea or coffee?")
  assert.equal(client.reads.filter((read) => read.word.startsWith("sessions.")).length, 0,
    "a row nobody has open asks the machine for nothing")
})

// 2026-10-16: a Session answered one question and stopped on the next while
// the page missed the s/ row carrying it. The status row said it moved after
// the held rich row was read; the card drew the old question's options anyway.
test("a held question older than the Session's last movement is not drawn as its question", async () => {
  const draw = async (movedAt: number) => {
    const client = new FakeClient()
    const execution = "a".repeat(32)
    const now = 1_000_000
    const rows = [{ destination: { machineID: "mac-a", sessionID: "s1", executionGeneration: execution },
      state: "waiting", freshness: "current" as const, observedAt: now }]
    const menu = { question: "Proceed?", options: [{ n: 1, label: "Yes", can: true }] }
    ;(client as unknown as { sessionSnapshots: Map<string, unknown> }).sessionSnapshots = new Map([["mac-a\u0000s1",
      { machine: "mac-a", session: "s1", execution_generation: execution, state: "waiting", menu,
        activity: { at: 900, known: true }, screen_reading: "read",
        source: { freshness: "current", observed_at: now / 1000 } }]])
    client.statusSnapshots = new Map([[JSON.stringify(["mac-a", "s1"]),
      { payload: { execution_generation: execution, last_movement_at: movedAt } }]])
    const r = new RelayReader("mac-a", { classicStatus: true, now: () => now,
      machineProjection: async () => ({ kind: "ready", complete: true, observedAt: now,
        snapshotGeneration: "a".repeat(32), presentationGeneration: "a", rows }),
      listPresentations: async () => rows.map((row) => ({
        destination: row.destination, title: "Title", status: { state: "waiting" }, projection: row })),
    })
    r.attach(client)
    return ((await r.snapshot()).sessions[0] as { menu?: { question?: string } }).menu?.question
  }
  assert.equal(await draw(900), "Proceed?", "the question the Session last moved to is drawn")
  assert.equal(await draw(960), undefined, "a question from before the last movement is not")
})

test("a Session whose status row has not come is asked for, not called a changed execution", async () => {
  const client = new FakeClient()
  const pass = "c".repeat(32)
  const generation = "a".repeat(32)
  const now = 1_000_000
  client.statusSnapshots = markerOnly("mac-a", pass, now / 1000, "s1")
  const asked: string[] = []
  client.recoverStatusRow = async (machine, session, wanted) => {
    asked.push(machine + "/" + session + "/" + wanted)
    ;(client.statusSnapshots as Map<string, unknown>)
      .set(JSON.stringify([machine, session]), statusRow(wanted, generation, now / 1000))
    return true
  }
  const r = new RelayReader("mac-a", { classicStatus: true, now: () => now })
  r.attach(client)
  const answer = await r.fetch("/v1/transcript?session=s1&limit=200")
  assert.equal(answer.status, 200, "the read goes through once the row is here")
  assert.deepEqual(asked, ["mac-a/s1/" + pass], "the row of this pass is the one asked for")
  assert.deepEqual(client.pinnedReads, [{ machineID: "mac-a", sessionID: "s1", executionGeneration: generation }],
    "and it is pinned to the execution that row names")
})

test("a status row that still does not come is the refusal it always was", async () => {
  const client = new FakeClient()
  const now = 1_000_000
  client.statusSnapshots = markerOnly("mac-a", "c".repeat(32), now / 1000, "s1")
  let asks = 0
  client.recoverStatusRow = async () => { asks += 1; return false }
  const r = new RelayReader("mac-a", { classicStatus: true, now: () => now })
  r.attach(client)
  const answer = await r.fetch("/v1/transcript?session=s1&limit=200")
  assert.equal(answer.status, 409)
  assert.equal(((await answer.json()) as { error?: string }).error, "execution_generation_changed")
  assert.equal(asks, 1, "asked once; the page refuses rather than asking again inside one read")
})

test("a replaced execution is not asked for again: the page is told it changed", async () => {
  const client = new FakeClient()
  const running = "b".repeat(32)
  client.rows = [row("mac-a", "s1", { execution_generation: running })]
  client.statusSnapshots = statusPass("mac-a", running, "s1", "s2")
  const r = new RelayReader("mac-a", { classicStatus: true, now: () => 1000,
    // What a person opened before the assistant restarted.
    fleetTarget: () => ({ machineID: "mac-a", sessionID: "s2", executionGeneration: "c".repeat(32) }) })
  r.attach(client)
  await r.snapshot()
  await settled()
  assert.deepEqual(client.reads, [], "the machine says a different execution is running; asking cannot produce this one")
})

test("refresh asks the machine to restate quiet Sessions after the first list", async () => {
  const client = new FakeClient()
  client.rows = [row("mac-a", "s1")]
  client.sessionInventoryByMachine.set("mac-a", inventory("mac-a", "s1"))
  const r = reader(client, { t: 1000 })
  await r.fetch("/v1/sessions", { signal: new AbortController().signal })
  assert.equal(client.reads.length, 0, "connection recovery owns the first restatement")

  await r.fetch("/v1/sessions", { signal: new AbortController().signal })
  assert.deepEqual(client.reads, [{ machine: "mac-a", word: "sessions.snapshot", body: {} }])
})

test("refresh waits for the newly stated Session set before answering the page", async () => {
  const client = new FakeClient()
  const generation = "a".repeat(32)
  client.rows = [row("mac-a", "s1", { execution_generation: generation })]
  client.statusSnapshots = statusPass("mac-a", generation, "s1")
  client.readAnswer = async () => ({ sessions: ["s1", "s2"], complete: true })
  const r = new RelayReader("mac-a", { classicStatus: true, now: () => 1000 })
  r.attach(client)
  await r.fetch("/v1/sessions", { signal: new AbortController().signal })
  let answered = false
  const refreshed = r.fetch("/v1/sessions", { signal: new AbortController().signal })
    .then((response) => { answered = true; return body<SessionsSnapshot>(response) })
  await settled()
  assert.equal(answered, false, "the command reply is earlier than the new ss/ pass")
  client.rows = [row("mac-a", "s1", { execution_generation: "b".repeat(32) }),
    row("mac-a", "s2", { execution_generation: "b".repeat(32) })]
  client.statusSnapshots = statusPass("mac-a", "b".repeat(32), "s1", "s2")
  client.emit({ type: "sessions", identity: { machine: "mac-a" } })
  assert.deepEqual((await refreshed).sessions.map((session) => session.id).sort(), ["s1", "s2"])
})

test("refresh waits for a resumed Session even when its id has not changed", async () => {
  const client = new FakeClient()
  const previous = "a".repeat(32)
  const resumed = "b".repeat(32)
  client.rows = [row("mac-a", "s1", { execution_generation: previous })]
  client.statusSnapshots = statusPass("mac-a", previous, "s1")
  client.readAnswer = async () => ({ sessions: ["s1"], complete: true })
  const r = new RelayReader("mac-a", { classicStatus: true, now: () => 1000 })
  r.attach(client)
  await r.fetch("/v1/sessions", { signal: new AbortController().signal })
  let answered = false
  const refreshed = r.fetch("/v1/sessions", { signal: new AbortController().signal })
    .then((response) => { answered = true; return body<SessionsSnapshot>(response) })
  await settled()
  assert.equal(answered, false, "an unchanged id is not proof of a new execution")
  client.rows = [row("mac-a", "s1", { execution_generation: resumed })]
  client.statusSnapshots = statusPass("mac-a", resumed, "s1")
  client.emit({ type: "sessions", identity: { machine: "mac-a" } })
  assert.equal((await refreshed).sessions[0]?.execution_generation, resumed)
})

test("refreshing an unchanged Session set answers as soon as the machine names the pass it holds", async () => {
  // The pass id hashes the set, so an unchanged set restates the id the page
  // already holds. Before the machine named it, the refresh waited for a newer
  // id until the request deadline (15 s on the page).
  const client = new FakeClient()
  const generation = "a".repeat(32)
  client.rows = [row("mac-a", "s1", { execution_generation: generation })]
  client.statusSnapshots = statusPass("mac-a", generation, "s1")
  client.readAnswer = async () => ({ sessions: ["s1"], complete: true, snapshot_generation: generation })
  const r = new RelayReader("mac-a", { classicStatus: true, now: () => 1000 })
  r.attach(client)
  await r.fetch("/v1/sessions", { signal: new AbortController().signal })
  const deadline = new AbortController()
  let answered = false
  const refreshed = r.fetch("/v1/sessions", { signal: deadline.signal })
    .then((response) => { answered = true; return body<SessionsSnapshot>(response) })
  await settled()
  await settled()
  const early = answered
  deadline.abort()
  assert.deepEqual((await refreshed).sessions.map((session) => session.id), ["s1"])
  assert.equal(early, true, "the held marker names the confirmed pass; nothing newer is coming")
})

test("a named pass the page does not hold yet is still waited for", async () => {
  const client = new FakeClient()
  const previous = "a".repeat(32)
  const resumed = "b".repeat(32)
  client.rows = [row("mac-a", "s1", { execution_generation: previous })]
  client.statusSnapshots = statusPass("mac-a", previous, "s1")
  client.readAnswer = async () => ({ sessions: ["s1"], complete: true, snapshot_generation: resumed })
  const r = new RelayReader("mac-a", { classicStatus: true, now: () => 1000 })
  r.attach(client)
  await r.fetch("/v1/sessions", { signal: new AbortController().signal })
  let answered = false
  const refreshed = r.fetch("/v1/sessions", { signal: new AbortController().signal })
    .then((response) => { answered = true; return body<SessionsSnapshot>(response) })
  await settled()
  assert.equal(answered, false, "the same ids under an older pass are not the stated list")
  client.rows = [row("mac-a", "s1", { execution_generation: resumed })]
  client.statusSnapshots = statusPass("mac-a", resumed, "s1")
  client.emit({ type: "sessions", identity: { machine: "mac-a" } })
  assert.equal((await refreshed).sessions[0]?.execution_generation, resumed)
})

/**
 * The account has two Macs; the Board item's Project is on `mac-b`, the one
 * under the console. `mac-a` hears every read and never answers. The page's
 * own session list has already read `mac-b` once.
 */
async function boardOnSecondMac(sessions: string[], held: string[]) {
  const client = new FakeClient()
  const generation = "a".repeat(32)
  client.machineRows = [{ id: "mac-a", freshness: "current" }, { id: "mac-b", freshness: "current" }]
  client.rows = held.map((session) => row("mac-b", session, { execution_generation: generation }))
  client.statusSnapshots = statusPass("mac-b", generation, ...sessions)
  client._machineRequest = (machine, word, reqBody) => {
    client.reads.push({ machine, word, body: reqBody })
    return machine === "mac-b" ? Promise.resolve({ sessions, complete: true }) : new Promise(() => {})
  }
  const r = new RelayReader("mac-b", { classicStatus: true, now: () => 1000 })
  r.attach(client)
  const first = new AbortController()
  setTimeout(() => first.abort(), 20)
  await r.fetch("/v1/sessions", { signal: first.signal, headers: { "X-Clawdline-Initial-Read": "1" } })
  client.reads = []
  return { client, r }
}

/** The Board's Session list read, as `readSessionsForWorkV2` sends it, through `r`, with a short deadline. */
async function boardSessionRead(r: RelayReader, timeoutMs: number): Promise<SessionsSnapshot> {
  const saved = globalThis.fetch
  globalThis.fetch = ((input: RequestInfo | URL, init?: RequestInit) => r.fetch(input, init)) as typeof fetch
  try {
    const res = await fetchWithDeadline(SESSION_LIST_READ.path, { headers: SESSION_LIST_READ.headers }, timeoutMs, SESSION_LIST_READ.answerMs)
    return await body<SessionsSnapshot>(res)
  } finally { globalThis.fetch = saved }
}

test("the Board's Session list answers at once when the machine's set has not changed", async () => {
  const { client, r } = await boardOnSecondMac(["s1"], ["s1"])
  const started = Date.now()
  const snap = await boardSessionRead(r, 200)
  assert.deepEqual(snap.sessions.map((session) => session.id), ["s1"])
  assert.equal(snap.scan.complete, true)
  assert.ok(Date.now() - started < 200, "a held complete list is not a wait for the machine's next pass")
  assert.ok(client.reads.every((read) => read.machine === "mac-b"), "only the Project's machine is asked")
})

test("the Board's Session list is the held list, incomplete, when the machine's new pass does not come", async () => {
  const { client, r } = await boardOnSecondMac(["s1", "s2"], ["s1"])
  const snap = await boardSessionRead(r, 30)
  assert.deepEqual(snap.sessions.map((session) => session.id), ["s1"])
  assert.equal(snap.scan.complete, false, "a list missing a row says it is incomplete, not that it failed")
  assert.deepEqual(client.reads.map((read) => read.machine), ["mac-b"], "the silent machine is never asked")
})

test("the Board's Session list failure is recorded for the machine's log without anything of the person's", () => {
  const rows: { event: string; data: Record<string, unknown>; key?: string }[] = []
  const client = new FakeClient()
  client.viewerEvents = { record(event, data, key) { rows.push({ event, data, key }); return { n: rows.length } } }
  const r = new RelayReader("mac-b")
  assert.equal(r.reportSessionListFailure({ code: "timeout", elapsedMs: 15000.4 }), false)
  r.attach(client)
  assert.equal(r.reportSessionListFailure({ code: "timeout", elapsedMs: 15000.4 }), true)
  assert.deepEqual(rows, [{ event: "work.sessions.read_failed", key: "work.sessions.read_failed|mac-b|timeout",
    data: { word: "sessions.snapshot", machine: "mac-b", code: "timeout", elapsed_ms: 15000, connection: "ready" } }])
})

test("reopening a complete list reads the relay once without restating every Session", async () => {
  const client = new FakeClient()
  client.rows = [row("mac-a", "s1")]
  client.sessionInventoryByMachine.set("mac-a", inventory("mac-a", "s1"))
  const r = reader(client, { t: 1000 })
  const initial = { signal: new AbortController().signal, headers: { "X-Clawdline-Initial-Read": "1" } }
  await r.fetch("/v1/sessions", initial)
  await r.fetch("/v1/sessions", initial)
  assert.equal(client.reads.length, 0)
})

test("reopening an incomplete list still asks the machine for missing rows", async () => {
  const client = new FakeClient()
  client.rows = [row("mac-a", "s1")]
  client.sessionInventoryByMachine.set("mac-a", inventory("mac-a", "s1"))
  const r = reader(client, { t: 1000 })
  const initial = { signal: new AbortController().signal, headers: { "X-Clawdline-Initial-Read": "1" } }
  await r.fetch("/v1/sessions", initial)
  client.recovering = ["mac-a"]
  await r.fetch("/v1/sessions", initial)
  assert.deepEqual(client.reads, [{ machine: "mac-a", word: "sessions.snapshot", body: {} }])
})

test("an agent transcript is a session read of the chosen machine, never the generic machine request", async () => {
  const client = new FakeClient()
  const r = reader(client, { t: 1000 })

  const answer = await r.fetch("/v1/sessions/root%20pane/agents/agent%204?limit=200")
  assert.equal(answer.status, 200)
  assert.deepEqual(client.sessionReads, [{
    word: "agent", identity: { machine: "mac-a", session: "root pane" }, id: "agent 4",
  }])
  assert.deepEqual(client.reads, [])
  assert.deepEqual(await answer.json(), { read: "agent" })
  assert.equal(r.log.at(-1)?.word, "agent")
})

test("a callback output uses the pinned shell read on the selected machine", async () => {
  const client=new FakeClient(), r=reader(client,{t:1000})
  const answer=await r.fetch("/v1/sessions/root%20pane/shells/callback-c5100000-0000-4000-8000-000000000001?bytes=65536")
  assert.equal(answer.status,200)
  assert.deepEqual(client.sessionReads,[{word:"shell",identity:{machine:"mac-a",session:"root pane"},id:"callback-c5100000-0000-4000-8000-000000000001"}])
  assert.deepEqual(client.reads,[])
})

test("a shell's output is a session read of the chosen machine, never the generic machine request", async () => {
  const client = new FakeClient()
  const r = reader(client, { t: 1000 })

  const answer = await r.fetch("/v1/sessions/root%20pane/shells/b0aau3e6s?bytes=65536")
  assert.equal(answer.status, 200)
  const bare = await r.fetch("/v1/sessions/root%20pane/shells/b0aau3e6s")
  assert.equal(bare.status, 200)
  assert.deepEqual(client.sessionReads, [
    { word: "shell", identity: { machine: "mac-a", session: "root pane" }, id: "b0aau3e6s" },
    { word: "shell", identity: { machine: "mac-a", session: "root pane" }, id: "b0aau3e6s" },
  ])
  assert.deepEqual(client.reads, [])
  assert.equal(r.log.at(-1)?.word, "shell")
})

test("an agent or shell window other than the one the client reads is refused by name, and nothing is sent", async () => {
  const client = new FakeClient()
  const r = reader(client, { t: 1000 })

  const agent = await r.fetch("/v1/sessions/s1/agents/a1?limit=40")
  assert.equal(agent.status, 501)
  assert.equal((await body<{ error: string }>(agent)).error, "cloud_not_carried")
  const shell = await r.fetch("/v1/sessions/s1/shells/b1?bytes=1024")
  assert.equal(shell.status, 501)
  assert.deepEqual(client.sessionReads, [])
})

test("a copied client without the agent reader refuses the read by name instead of hanging", async () => {
  const client = new FakeClient()
  client.agent = undefined
  const r = reader(client, { t: 1000 })

  const answer = await r.fetch("/v1/sessions/s1/agents/a1")
  assert.equal(answer.status, 501)
  assert.equal((await body<{ error: string }>(answer)).error, "cloud_not_carried")
  assert.deepEqual(client.reads, [])
})

test("empty is believed only after the machine's inventory, and not while it is still sending rows", async () => {
  const client = new FakeClient()
  const r = reader(client, { t: 1000 })
  const before = await r.snapshot()
  assert.equal(before.scan.emptyAuthoritative, false)
  client.sessionInventoryByMachine.set("mac-a", inventory("mac-a"))
  assert.equal((await r.snapshot()).scan.emptyAuthoritative, true)
  client.recovering = ["mac-a"]
  const recovering = await r.snapshot()
  assert.equal(recovering.scan.emptyAuthoritative, false)
  assert.ok(recovering.scan.generation > before.scan.generation, "each snapshot is newer than the last")
  assert.equal(recovering.scan.epoch, before.scan.epoch)
})

test("the first list paints once after every row named by the inventory has arrived", async () => {
  const client = new FakeClient()
  client.rows = [row("mac-a", "s1")]
  const r = reader(client, { t: 1000 })
  const controller = new AbortController()
  let answered = false
  const pending = r.fetch("/v1/sessions", { signal: controller.signal }).then(async (response) => {
    answered = true
    return body<SessionsSnapshot>(response)
  })

  await new Promise((resolve) => setTimeout(resolve, 5))
  assert.equal(answered, false, "a partial retained row is not the first paint")

  client.sessionInventoryByMachine.set("mac-a", inventory("mac-a", "s1", "s2"))
  client.emit({ type: "sessions", identity: { machine: "mac-a", session: "__clawdline_inventory_v1__" } })
  await new Promise((resolve) => setTimeout(resolve, 5))
  assert.equal(answered, false, "the marker is a receipt for rows, not a substitute for them")

  client.rows = [row("mac-a", "s1"), row("mac-a", "s3")]
  client.emit({ type: "sessions", identity: { machine: "mac-a", session: "s3" } })
  await new Promise((resolve) => setTimeout(resolve, 5))
  assert.equal(answered, false, "the same number of rows is not the marker's exact set")

  client.rows = [row("mac-a", "s1"), row("mac-a", "s2")]
  client.emit({ type: "sessions", identity: { machine: "mac-a", session: "s2" } })
  const snapshot = await pending
  assert.deepEqual(snapshot.sessions.map((session) => session.id), ["s1", "s2"])
  assert.equal(snapshot.scan.complete, true)
})

test("the first list falls back to its best partial reading at the existing request deadline", async () => {
  const client = new FakeClient()
  client.rows = [row("mac-a", "s1")]
  const r = reader(client, { t: 1000 })
  const controller = new AbortController()
  const pending = r.fetch("/v1/sessions", { signal: controller.signal })

  await new Promise((resolve) => setTimeout(resolve, 5))
  controller.abort()
  const snapshot = await body<SessionsSnapshot>(await pending)
  assert.deepEqual(snapshot.sessions.map((session) => session.id), ["s1"])
  assert.equal(snapshot.scan.complete, false)
})

// A viewer that connected after the machine's last change holds no inventory
// marker for it. Two facts follow, and the page needs both: the list it can
// draw is whatever rows did arrive, and it must not be read as the whole set.
//
// This is what the empty state's wording rests on. A viewer with rows draws
// them — the "waiting" sentence is only ever for a genuinely empty list — and
// a viewer with neither rows nor marker is waiting on the machine, not on an
// app it has no line to.
test("rows that arrived without the machine's marker are drawn, and are still not the whole set", async () => {
  const client = new FakeClient()
  client.rows = [row("mac-a", "s1"), row("mac-a", "s3")]
  const r = reader(client, { t: 1000 })

  const waiting = await r.snapshot()
  assert.deepEqual(waiting.sessions.map((s) => s.id), ["s1", "s3"], "the rows it has are handed over")
  assert.equal(waiting.scan.complete, false, "without the marker no reading is the whole set")
  assert.equal(waiting.scan.emptyAuthoritative, false)

  // The marker arriving is what makes it whole; nothing else does.
  client.sessionInventoryByMachine.set("mac-a", inventory("mac-a", "s1", "s3"))
  const whole = await r.snapshot()
  assert.equal(whole.scan.complete, true)
  assert.deepEqual(whole.sessions.map((s) => s.id), ["s1", "s3"])
})

test("with no writer carried, a write is refused as not carried, as an uncarried read is, both typed", async () => {
  const r = reader(new FakeClient(), { t: 0 })
  const post = await r.fetch("/v1/sessions/s1/send", { method: "POST", body: "{}" })
  assert.equal(post.status, 501)
  assert.deepEqual(await post.json(), {
    error: "cloud_not_carried",
    detail: "POST /v1/sessions/s1/send is not carried over Clawdline Cloud: do it on the machine itself.",
    route: "/v1/sessions/s1/send",
  })
  // `/v1/board` stood here until this console began asking for it. The read
  // that stands for "most of this daemon's API" now is one that is no Cloud
  // word at all (`carry.ts`, `notCarriedDetail`'s second sentence).
  const uncarried = await r.fetch("/v1/devstacks")
  assert.equal(uncarried.status, 501)
  assert.equal((await body<{ error: string }>(uncarried)).error, "cloud_not_carried")
  assert.deepEqual(r.log.map((x: { answer: string; code?: string }) => [x.answer, x.code]), [
    ["refused", "cloud_not_carried"],
    ["refused", "cloud_not_carried"],
  ])
})

test("with the line down, reads reject as unanswered instead of refusing", async () => {
  const client = new FakeClient()
  client.ready = false
  const r = reader(client, { t: 0 })
  await assert.rejects(r.fetch("/v1/health"), TypeError)
  await assert.rejects(r.fetch("/v1/sessions"), TypeError)
})

test("health belongs to the chosen machine, not merely the relay socket", async () => {
  const client = new FakeClient()
  const r = reader(client, { t: 12_000 })

  const current = await r.fetch("/v1/health")
  assert.equal(current.status, 200)
  assert.equal((await body<{ ok: boolean }>(current)).ok, true)

  client.machineRows = [{ id: "mac-a", freshness: "stale" }]
  const stale = await r.fetch("/v1/health")
  assert.equal(stale.status, 503)
  assert.equal((await body<{ error: string }>(stale)).error, "machine_stale")

  client.machineRows = [{ id: "mac-a", freshness: "unknown" }]
  const unknown = await r.fetch("/v1/health")
  assert.equal(unknown.status, 503)
  assert.equal((await body<{ error: string }>(unknown)).error, "machine_freshness_unknown")

  client.machineRows = []
  const absent = await r.fetch("/v1/health")
  assert.equal(absent.status, 503)
  assert.equal((await body<{ error: string }>(absent)).error, "machine_not_reported")
})

test("a transcript is asked again only when its row could say it changed", async () => {
  const client = new FakeClient()
  const source = { freshness: "current", observed_at: 10, provenance: "tmux" }
  client.rows = [row("mac-a", "s1", { line: "Working (1s)", source })]
  const clock = { t: 10_000 }
  const r = reader(client, clock)
  const read = async () => body<TranscriptPage>(await r.fetch("/v1/transcript?session=s1&limit=200"))

  assert.equal((await read()).id, "s1")
  assert.equal(client.asks, 1)

  clock.t += 4_000 // the console's next poll, nothing moved
  await read()
  assert.equal(client.asks, 1, "an unchanged row reuses the answer")

  client.rows = [row("mac-a", "s1", {
    line: "Working (1s)",
    source: { ...source, observed_at: 14 },
  })]
  clock.t += 4_000
  await read()
  assert.equal(client.asks, 1, "a newer terminal observation does not make the transcript newer")

  client.rows = [row("mac-a", "s1", { line: "Working (5s)", source: { ...source, observed_at: 18 } })]
  clock.t += 4_000
  await read()
  assert.equal(client.asks, 1, "the status line alone is not worth a read inside the window")

  clock.t = 10_000 + TRANSCRIPT_LINE_REREAD_MS
  await read()
  assert.equal(client.asks, 2, "past the window the moving line costs one read")

  client.rows = [row("mac-a", "s1", {
    line: "Working (5s)", state: "waiting", work_state: "waiting_you", source: { ...source, observed_at: 19 },
  })]
  clock.t += 1_000
  await read()
  assert.equal(client.asks, 3, "any other change is read at once")

  clock.t += TRANSCRIPT_MAX_REUSE_MS
  await read()
  assert.equal(client.asks, 4, "and nothing is reused past the ceiling")
  assert.deepEqual(r.log.filter((x: { path: string }) => x.path === "/v1/transcript").map((x: { answer: string }) => x.answer), [
    "relay", "cache", "cache", "cache", "relay", "relay", "relay",
  ])
})

test("two polls while one read is in flight share it", async () => {
  const client = new FakeClient()
  client.rows = [row("mac-a", "s1")]
  let release!: () => void
  client.answer = () => new Promise((resolve) => (release = () => resolve({ entries: [], signature: "x" })))
  const r = reader(client, { t: 0 })
  const first = r.fetch("/v1/transcript?session=s1")
  await new Promise((resolve) => setImmediate(resolve))
  const second = r.fetch("/v1/transcript?session=s1")
  await new Promise((resolve) => setImmediate(resolve))
  release()
  const pages = await Promise.all([first, second].map(async (p) => body<TranscriptPage>(await p)))
  assert.equal(client.asks, 1)
  assert.deepEqual(pages.map((p) => [p.id, p.evidence]), [["s1", "transcript"], ["s1", "transcript"]])
})

test("the machine's refusal stays typed; a timeout is nobody answering", async () => {
  const client = new FakeClient()
  const r = reader(client, { t: 0 })
  client.answer = () => Promise.reject(Object.assign(new Error("no session named that"), { code: "not_found", status: 404 }))
  const refused = await r.fetch("/v1/transcript?session=gone")
  assert.equal(refused.status, 404)
  assert.equal((await body<{ error: string }>(refused)).error, "not_found")
  client.answer = () => Promise.reject(Object.assign(new Error("the machine did not answer this read"), { code: "cloud_read_timeout" }))
  await assert.rejects(r.fetch("/v1/transcript?session=slow"), /cloud_read_timeout/)
})

// The Mac's channel filled and the answer could not leave. What the page gets
// back is that sentence, at once, with the bound it met — not sixty seconds of
// "loading" and then `cloud_read_timeout`, which was the whole of it before:
// measured 2026-09-21, one long session's transcript channel was refused 34
// times and every one of them was a log line on the Mac and nothing here.
test("a full channel on the machine is a typed refusal here, not a read that times out", async () => {
  const client = new FakeClient()
  const r = reader(client, { t: 0 })
  client.answer = () =>
    Promise.reject(
      Object.assign(new Error("This machine answered, and the channel that answer goes on is full; try again shortly."), {
        code: "cloud_read_busy",
        status: 429,
        layer: "mac_transport",
        detail: { lane: "egress", limit: 4 << 20, retry_after: 5 },
      }),
    )
  const refused = await r.fetch("/v1/transcript?session=%19")
  assert.equal(refused.status, 429)
  const answered = await body<{ error: string; detail: string }>(refused)
  assert.equal(answered.error, "cloud_read_busy")
  assert.match(answered.detail, /full/)
  // It is a refusal the page can draw, not a transport failure it draws as
  // "away": the Mac is there and said something.
  assert.deepEqual(
    r.log.map((row) => [row.answer, row.code]),
    [["refused", "cloud_read_busy"]],
  )
})

test("the stream sends one frame per turn for this machine and none for another", async () => {
  const client = new FakeClient()
  client.rows = [row("mac-a", "s1")]
  const r = reader(client, { t: 0 })
  const frames: string[] = []
  const errors: unknown[] = []
  let opened = 0
  const handle = r.stream().open("/v1/events", {
    onFrame: (event: string, data: string) => frames.push(event + ":" + (JSON.parse(data) as SessionsSnapshot).sessions.length),
    onOpen: () => (opened += 1),
    onError: (cause: unknown) => errors.push(cause),
  })
  assert.equal(opened, 1)
  for (let i = 0; i < 5; i++) client.emit({ type: "sessions", identity: { machine: "mac-a", session: "s" + i } })
  client.emit({ type: "sessions", identity: { machine: "mac-b", session: "x" } })
  await new Promise((resolve) => setTimeout(resolve, 5))
  assert.deepEqual(frames, ["sessions:1"])
  client.emit({ type: "sessions", identity: { machine: "mac-b", session: "x" } })
  await new Promise((resolve) => setTimeout(resolve, 5))
  assert.deepEqual(frames, ["sessions:1"])
  client.emit({ type: "connection", state: "offline" })
  assert.equal(errors.length, 1)

  // A renewed client: the open stream follows it, is told the line is up, and gets the rows.
  const renewed = new FakeClient()
  renewed.rows = [row("mac-a", "s1"), row("mac-a", "s2")]
  r.attach(renewed)
  await new Promise((resolve) => setTimeout(resolve, 5))
  assert.equal(opened, 2)
  assert.deepEqual(frames, ["sessions:1", "sessions:2"])
  client.emit({ type: "sessions", identity: { machine: "mac-a", session: "s1" } })
  await new Promise((resolve) => setTimeout(resolve, 5))
  assert.deepEqual(frames, ["sessions:1", "sessions:2"], "the retired client is not listened to")
  handle.close()
})

// F2: "try again" reads the transcript first, and a read already on its way
// can be one asked before the attempt that failed. The copied client shares a
// read with any other for the same session (`readKey`), so asking again while
// one is in flight is handed that older answer. A `no-store` read waits for
// the one on its way to finish and then asks the machine anew.
test("a fresh read is never handed an answer asked before it", async () => {
  const client = new FakeClient()
  client.rows = [row("mac-a", "s1")]
  const clock = { t: 1000 }
  const r = reader(client, clock)
  // The copied client's sharing: while one ask is out, another gets its answer.
  let release: (v: unknown) => void = () => {}
  let out: Promise<unknown> | null = null
  const stale = { id: "s1", entries: [], signature: "old", evidence: "transcript" }
  const now = { id: "s1", entries: [{ role: "user", text: "yes" }], signature: "new", evidence: "transcript" }
  client.answer = () => {
    if (out) return out
    out = new Promise((resolve) => { release = resolve }).then((v) => { out = null; return v })
    return out
  }
  const polling = r.fetch("/v1/transcript?session=s1&limit=200")
  await new Promise((resolve) => setTimeout(resolve, 0))
  const fresh = r.fetch("/v1/transcript?session=s1&limit=200", { cache: "no-store" })
  await new Promise((resolve) => setTimeout(resolve, 0))
  release(stale)
  await polling
  await new Promise((resolve) => setTimeout(resolve, 0))
  release(now)
  const page = await body<TranscriptPage>(await fresh)
  assert.equal(page.signature, "new", "the fresh read was handed the answer asked before it")
})

// The first thing a phone does when somebody presses "notify me". It is a
// read — the machine mints the key and nothing else changes — so it is answered
// here rather than by the writer, and it is what the whole registration
// stopped on: before this route existed, `GET /v1/push/key` was refused
// `cloud_not_carried` in the browser and the request never left the phone.
test("the application server key is asked of the machine", async () => {
  const client = new FakeClient()
  const r = reader(client, { t: 1000 })
  const res = await r.fetch("/v1/push/key")
  assert.equal(res.status, 200)
  assert.deepEqual(await res.json(), { key: "BPk" })
  assert.equal(client.pushKeyAsks, 1)
  const last = r.log[r.log.length - 1]
  assert.equal(last.answer, "relay")
  assert.equal(last.word, "push-key")
})

// A copied client too old to know the word, and a machine that answered no.
// Both settle the request typed, in the flat spelling `push/api.ts` reads
// through `isRefusal` — never the static host's page as a body that is not
// JSON.
test("a key nobody can ask for is refused by name, not left to the network", async () => {
  const older = new FakeClient()
  older.pushKey = undefined
  const noWord = await reader(older, { t: 0 }).fetch("/v1/push/key")
  assert.equal(noWord.status, 501)
  assert.equal((await body<{ error: string }>(noWord)).error, "cloud_not_carried")

  const client = new FakeClient()
  client.pushKey = async () => {
    throw Object.assign(new Error("this machine does not carry notifications"), {
      code: "cloud_feature_unavailable",
      status: 501,
    })
  }
  const refused = await reader(client, { t: 0 }).fetch("/v1/push/key")
  assert.equal(refused.status, 501)
  const refusal = await body<{ error: string; detail: string }>(refused)
  assert.equal(refusal.error, "cloud_feature_unavailable")
  assert.equal(typeof refusal.detail, "string", "`isRefusal` needs a string detail")
})

// The list under the session list, which on a phone was not there at all.
// `carry.ts` had the word in `DEFERRED` saying schedules were not read over
// Cloud yet, and this machine had been answering `schedules` the whole time — so
// the seam refused the route to itself and the section stayed hidden, with
// nothing in the machine's log because nothing had been asked for.
test("the schedule list is every machine's rows, each with its machine", async () => {
  const client = new FakeClient()
  client.scheduleAnswer = async () => ({
    schedules: [schedule("mac-a", "s-1"), schedule("mac-b", "s-2"), schedule("mac-a", "s-3")],
    at: 1_700,
  })
  const r = reader(client, { t: 1000 })
  const res = await r.fetch("/v1/orchestrator/schedules")
  assert.equal(res.status, 200)
  const list = await body<{ schedules: { id: string; machine: string }[]; at: number; unanswered: unknown[] }>(res)
  // It used to be cut to this machine's rows, which is why a schedule on the
  // Linux machine could only be seen by switching the whole console to it.
  assert.deepEqual(list.schedules.map((s) => [s.id, s.machine]),
    [["s-1", "mac-a"], ["s-2", "mac-b"], ["s-3", "mac-a"]], "every machine's schedules are this list")
  assert.equal(list.at, 1_700)
  assert.deepEqual(list.unanswered, [])
  // `fresh`, and not for freshness: this daemon publishes no schedules on its
  // `orch/` descriptor, so the retained reading refuses forever.
  assert.deepEqual(client.scheduleAsks, [{ fresh: true }])
  const last = r.log[r.log.length - 1]
  assert.equal(last.answer, "relay")
  assert.equal(last.word, "schedules")
})

test("a machine-scoped schedule read can finish without another machine's answer", async () => {
  const client = new FakeClient()
  client.scheduleAnswer = async (options) => ({
    schedules: [schedule(options?.machine || "mac-a", "own"), schedule("mac-b", "other")], at: 1_700,
  })
  const res = await reader(client, { t: 1000 }).fetch("/v1/orchestrator/schedules?machine=mac-a")
  assert.equal(res.status, 200)
  assert.deepEqual((await body<{ schedules: { id: string }[] }>(res)).schedules.map((row) => row.id), ["own"])
  assert.deepEqual(client.scheduleAsks, [{ fresh: true, machine: "mac-a" }])
})

test("one schedule is asked of the chosen machine so its history and webhook panel can open", async () => {
  const client = new FakeClient()
  client.readAnswer = async (word) => word === "schedule"
    ? { schedule: { id: "schedule 9", webhook_binding_availability: "unbound" } }
    : { read: word }
  const r = reader(client, { t: 1000 })

  const res = await r.fetch("/v1/orchestrator/schedules/schedule%209")
  assert.equal(res.status, 200)
  assert.deepEqual(await res.json(), {
    schedule: { id: "schedule 9", webhook_binding_availability: "unbound" },
  })
  // A row the list said is on another machine is read there.
  const there = await r.fetch("/v1/orchestrator/schedules/schedule%209?machine=mac-b")
  assert.equal(there.status, 200)
  assert.deepEqual(client.reads, [
    { machine: "mac-a", word: "schedule", body: { id: "schedule 9" } },
    { machine: "mac-b", word: "schedule", body: { id: "schedule 9" } },
  ])
})

// Measured on 2026-09-27 against app.clawdline.com: a tab that had been hidden
// past the grace had its client retired by `keepConnected` (a hidden-page
// retirement removed on 2026-10-06; renewals and drops still leave a retired
// client), and every schedule — timed or trigger-only — then opened as "Could not reach
// Clawdline. Is it still running on the machine? (offline)" within a second,
// with the machine online: the seam threw `NotConnected` without asking the
// client to come back, and `jsonFetch` turned the throw into `offline`.
test("a machine read asked while the page's connection is paused waits for it to come back", async () => {
  const retired = new FakeClient()
  retired.ready = false
  const demands: string[] = []
  const paused = retired as FakeClient & { lifecycle?: (reason: string) => boolean }
  paused.lifecycle = (reason) => {
    demands.push(reason)
    return true
  }
  const r = reader(retired, { t: 1000 })
  const renewed = new FakeClient()
  renewed.readAnswer = async (word) => ({ schedule: { id: "t-1", when: { trigger_only: true }, word } })

  const asked = r.fetch("/v1/orchestrator/schedules/t-1")
  await new Promise((resolve) => setTimeout(resolve, 5))
  r.attach(renewed)
  const res = await asked
  assert.equal(res.status, 200)
  assert.deepEqual(await res.json(), { schedule: { id: "t-1", when: { trigger_only: true }, word: "schedule" } })
  assert.deepEqual(demands, ["demand"], "the paused client is asked to reconnect")
  assert.deepEqual(retired.reads, [], "nothing is sent through the retired client")
  assert.deepEqual(renewed.reads, [{ machine: "mac-a", word: "schedule", body: { id: "t-1" } }])
})

test("a machine read the connection does not come back for is refused as reconnecting, not offline", async () => {
  for (const coming of [false]) {
    const retired = new FakeClient() as FakeClient & { lifecycle?: (reason: string) => boolean }
    retired.ready = false
    retired.lifecycle = () => coming
    const r = new RelayReader("mac-a", { now: () => 1000, reconnectWaitMs: 20 })
    r.attach(retired)
    const res = await r.fetch("/v1/orchestrator/schedules/t-1")
    assert.equal(res.status, 503, `lifecycle answered ${String(coming)}`)
    const refusal = await body<{ error: string }>(res)
    assert.equal(refusal.error, "cloud_reconnecting")
    assert.deepEqual(retired.reads, [])
  }
})

// Seen on 2026-10-18 on app.clawdline.com: the phone came back to the
// Schedules page a moment before its relay line did, and the list read threw
// `NotConnected`, which `jsonFetch` spells `offline`. Every machine on the
// account — the online Mac among them — was drawn as offline or unreadable,
// and the lane asked again only a minute later. One schedule's read had waited
// for the line since 2026-09-27; the list did not.
test("the schedule list asked while the page's connection is paused waits for it to come back", async () => {
  const retired = new FakeClient() as FakeClient & { lifecycle?: (reason: string) => boolean }
  retired.ready = false
  const demands: string[] = []
  retired.lifecycle = (reason) => {
    demands.push(reason)
    return true
  }
  const r = reader(retired, { t: 1000 })
  const renewed = new FakeClient()
  renewed.scheduleAnswer = async (options) => ({ schedules: [schedule(options?.machine || "mac-a", "s-1")], at: 1_700 })

  const asked = r.fetch("/v1/orchestrator/schedules?machine=mac-a")
  await new Promise((resolve) => setTimeout(resolve, 5))
  r.attach(renewed)
  const res = await asked
  assert.equal(res.status, 200)
  assert.deepEqual((await body<{ schedules: { id: string }[] }>(res)).schedules.map((row) => row.id), ["s-1"])
  assert.deepEqual(demands, ["demand"], "the paused client is asked to reconnect")
  assert.deepEqual(retired.scheduleAsks, [], "nothing is sent through the retired client")
  assert.deepEqual(renewed.scheduleAsks, [{ fresh: true, machine: "mac-a" }])
})

test("a schedule list the connection does not come back for is refused as reconnecting, recorded, not offline", async () => {
  const retired = new FakeClient() as FakeClient & { lifecycle?: (reason: string) => boolean }
  retired.ready = false
  retired.lifecycle = () => true
  const rows: { event: string; data: Record<string, unknown> }[] = []
  retired.viewerEvents = { record(event, data) { rows.push({ event, data }); return { n: rows.length } } }
  const r = new RelayReader("mac-a", { now: () => 1000, reconnectWaitMs: 20 })
  r.attach(retired)
  const res = await r.fetch("/v1/orchestrator/schedules?machine=mac-b")
  assert.equal(res.status, 503)
  assert.equal((await body<{ error: string }>(res)).error, "cloud_reconnecting")
  assert.deepEqual(retired.scheduleAsks, [])
  // The machine's log is where a phone's failure is read (`diagnostics.events`):
  // a failed schedule read names its word and the machine it was for.
  const failed = rows.filter((row) => row.event === "cloud.read.failed")
  assert.equal(failed.length, 1)
  assert.equal(failed[0]?.data.word, "schedules")
  assert.equal(failed[0]?.data.code, "http_503")
  assert.equal(failed[0]?.data.machine, "mac-b")
  assert.equal(failed[0]?.data.connection, "not_ready")
})

// The distinction this whole read is drawn around: "this machine has none" and
// "nobody answered" are opposite facts, and `pages/schedules.tsx` draws the
// section for one and leaves it alone for the other. An empty answer is an
// answer; a refusal must never arrive as `{schedules: []}`.
test("an empty list is only ever what the machine said, never what a refusal became", async () => {
  const empty = new FakeClient()
  const said = await reader(empty, { t: 1000 }).fetch("/v1/orchestrator/schedules")
  assert.equal(said.status, 200)
  assert.deepEqual(await said.json(), { schedules: [], at: 1, unanswered: [] }, "an answer with no rows is an answer")

  // The copied client's own refusal for a machine running a build that publishes
  // no schedules, and for an account no snapshot has arrived from.
  for (const code of ["cloud_schedules_unpublished", "cloud_read_unavailable"]) {
    const client = new FakeClient()
    client.scheduleAnswer = async () => {
      throw Object.assign(new Error(code), { code })
    }
    const res = await reader(client, { t: 1000 }).fetch("/v1/orchestrator/schedules")
    assert.equal(res.status, 502, code + " must not resolve")
    assert.equal((await body<{ error: string }>(res)).error, code)
  }
})

// The fan-out's own trap: `schedules()` settles as soon as *one* machine
// answers, so an account with two machines and one silent one resolves with the
// silent one's rows simply absent. Read as a list that is the page asserting
// that machine has no schedules; instead the machine is named, with its code,
// and `pages/schedules.tsx` draws it as a machine it could not read.
test("a machine that did not answer is not a machine with no schedules", async () => {
  const silent = new FakeClient()
  silent.scheduleAnswer = async () => ({
    schedules: [schedule("mac-b", "s-2")],
    at: 1_700,
    unanswered: [{ machine: "mac-a", label: "this machine", error: Object.assign(new Error("timed out"), { code: "cloud_read_timeout" }) }],
  })
  const res = await reader(silent, { t: 1000 }).fetch("/v1/orchestrator/schedules")
  assert.equal(res.status, 200)
  assert.deepEqual(await res.json(), {
    schedules: [schedule("mac-b", "s-2")],
    at: 1_700,
    unanswered: [{ machine: "mac-a", label: "this machine", code: "cloud_read_timeout" }],
  })

  // A machine never asked, because its descriptor has not arrived, is named too.
  const never = new FakeClient()
  never.scheduleAnswer = async () => ({ schedules: [schedule("mac-b", "s-2")], at: 1_700, unconfirmed: ["mac-a"] })
  const list = await (await reader(never, { t: 1000 }).fetch("/v1/orchestrator/schedules")).json()
  assert.deepEqual(list.unanswered, [{ machine: "mac-a", label: "mac-a", code: "cloud_read_unavailable" }])
})

// A copied client older than the word: refused by name, in the spelling
// `schedules-bridge.ts`'s `jsonFetch` reads, rather than thrown inside the
// one-minute lane where nothing is catching by type.
test("a client that cannot ask for schedules is refused by name", async () => {
  const older = new FakeClient()
  older.schedules = undefined
  const res = await reader(older, { t: 0 }).fetch("/v1/orchestrator/schedules")
  assert.equal(res.status, 501)
  const refusal = await body<{ error: string; detail: string }>(res)
  assert.equal(refusal.error, "cloud_not_carried")
  assert.match(refusal.detail, /schedules/)
})

// 常用句 on a phone: the sheet that answered "這件事還不能經由 Clawdline Cloud
// 做，請直接在 Mac 上操作。" That sentence was true — this machine's catalog knew the
// word `snippets` and had no route behind it, so the seam refused the read to
// itself rather than asking. Both halves have changed, and this is the read.
test("the snippet list is asked of the machine the session is on, and is that machine's rows", async () => {
  const client = new FakeClient()
  client.snippetAnswer = async () => ({
    snippets: [snippet("mac-a", "sn-1"), snippet("mac-b", "sn-2"), snippet("mac-a", "sn-3")],
    at: 1_700,
  })
  const r = reader(client, { t: 1000 })
  const res = await r.fetch("/v1/snippets?session=s1")
  assert.equal(res.status, 200)
  const list = await body<{ snippets: { id: string }[]; project?: unknown }>(res)
  assert.deepEqual(list.snippets.map((row) => row.id), ["sn-1", "sn-3"], "another machine's snippets are not this list")
  // **No project.** The local route resolves one from the session and names
  // it; the wire carries no session, so an answer that named a project would
  // be this page guessing one. `snippetGroups` groups by the session's own
  // `cwd` instead.
  assert.equal("project" in list, false)
  assert.deepEqual(client.snippetAsks, [{ identity: { machine: "mac-a", session: "s1" }, options: { fresh: true } }])
  const last = r.log[r.log.length - 1]
  assert.equal(last.answer, "relay")
  assert.equal(last.word, "snippets")
})

test("a Cloud Session asks its machine to resolve the Project before offering Playbooks", async () => {
  const client = new FakeClient()
  client.readAnswer = async (word) => word === "session-snippets"
    ? { project: { key: "/repo/project", label: "Project" }, snippets: [] }
    : { read: word }
  const response = await reader(client, { t: 1000 }).fetch("/v1/playbook-project?session=s1")
  assert.equal(response.status, 200)
  assert.deepEqual(await response.json(), { project: { key: "/repo/project", label: "Project" }, snippets: [] })
  assert.deepEqual(client.reads.at(-1), { machine: "mac-a", word: "session-snippets", body: { row: "s1" } })
})

// The same distinction the schedule list is drawn around, and the sheet has
// two different things to draw for it: an empty group offers the starters,
// and a refusal says what could not be read. A refusal must never arrive as
// `{snippets: []}`.
test("a machine with no snippets and a machine nobody could ask are not the same answer", async () => {
  const empty = new FakeClient()
  const said = await reader(empty, { t: 1000 }).fetch("/v1/snippets?session=s1")
  assert.equal(said.status, 200)
  assert.deepEqual(await said.json(), { snippets: [] }, "an answer with no rows is an answer")

  // The copied client's own two, which it raises for this one machine rather
  // than for the account: a machine whose inventory carries no such field, and a
  // Mac nothing has arrived from at all.
  for (const code of ["cloud_snippets_unpublished", "cloud_read_unavailable"]) {
    const client = new FakeClient()
    client.snippetAnswer = async () => {
      throw Object.assign(new Error(code), { code })
    }
    const res = await reader(client, { t: 1000 }).fetch("/v1/snippets?session=s1")
    assert.equal(res.status, 502, code + " must not resolve")
    assert.equal((await body<{ error: string }>(res)).error, code)
  }
})

test("a snippet list with no session named is refused, and an older client by name", async () => {
  const client = new FakeClient()
  const unnamed = await reader(client, { t: 0 }).fetch("/v1/snippets")
  assert.equal(unnamed.status, 400)
  assert.equal((await body<{ error: string }>(unnamed)).error, "bad_request")
  assert.deepEqual(client.snippetAsks, [], "nothing is asked of the machine for a request that named no session")

  const older = new FakeClient()
  older.snippets = undefined
  const res = await reader(older, { t: 0 }).fetch("/v1/snippets?session=s1")
  assert.equal(res.status, 501)
  const refusal = await body<{ error: string; detail: string }>(res)
  assert.equal(refusal.error, "cloud_not_carried")
  assert.match(refusal.detail, /snippets/)
})

/* ---- the work system, the Projects page and what they carry ---------------
   Before this, every one of these was the `default:` case — 501
   `cloud_not_carried` — so a phone showed the Mac's work board, its Backlog,
   its proposals, its decisions, its digests, its Project catalog, its
   worktree lifecycle and its timeline as screens that could not be read. */

/** Every read carried by word, the URL the console asks it with, and the body that must reach the machine. */
const CARRIED_READS: [string, string, Record<string, unknown>][] = [
  ["/v1/work/proposals?project=%2Fp", "work.proposals", { project: "/p" }],
  ["/v1/work/decisions", "work.decisions", {}],
  ["/v1/work/decisions/10000000-0000-4000-8000-000000000001", "work.decision", { id: "10000000-0000-4000-8000-000000000001" }],
  ["/v1/work/digests?kind=daily", "work.digests", { kind: "daily" }],
  ["/v1/work/v2/items", "work.v2.items", { project: "" }],
  ["/v1/work/v2/items?status=open", "work.v2.search", { project: "", status: "open", query: "" }],
  ["/v1/work/v2/items?project=cloud-p1&cursor=page-2", "work.v2.items", { project: "p1", cursor: "page-2" }],
  ["/v1/work/v2/items?project=cloud-p1&status=done&q=needle&cursor=page-3", "work.v2.search", { project: "p1", status: "done", query: "needle", cursor: "page-3" }],
  ["/v1/work/v2/items/work-1", "work.v2.item", { id: "work-1" }],
  ["/v1/work/v2/items/work-1/gate-export", "work.v2.gate-export", { id: "work-1" }],
  ["/v1/work/v2/proposals?state=pending", "work.v2.proposals", { state: "pending" }],
  ["/v1/work/v2/session-todos/%251", "work.v2.session-todos", { terminal: "%1" }],
  ["/v1/work/v2/human-interventions/conversation%3A10000000-0000-4000-8000-000000000002", "work.v2.human-interventions", { terminal: "conversation:10000000-0000-4000-8000-000000000002" }],
  ["/v1/projects", "projects", {}],
  ["/v1/projects/cloud-p1/files", "project-file-list", { project: "p1" }],
  ["/v1/projects/cloud-p1/graphify", "project-graphify", { project: "p1" }],
  ["/v1/projects/cloud-p1/files/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "project-file-read", { project: "p1", file: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" }],
  ["/v1/projects/cloud-p1/antigravity-consent", "project-antigravity-consent-status", { project: "p1" }],
  ["/v1/projects/cloud-p1/unify", "project-unify-plan", { project: "p1" }],
  ["/v1/projects/cloud-p1/tree?directory=", "project-tree-list", { project: "p1", directory: "" }],
  ["/v1/projects/cloud-p1/tree?directory=src%2Fnested", "project-tree-list", { project: "p1", directory: "src/nested" }],
  ["/v1/projects/cloud-p1/tree/file?path=src%2Fpage.ts", "project-tree-read", { project: "p1", path: "src/page.ts" }],
  ["/v1/personas", "personas", {}],
  ["/v1/projects/%2Fp/worktrees", "project-worktree-lifecycle", { project: "/p" }],
  ["/v1/board?project=p1", "board", { project: "p1", item: "" }],
  ["/v1/board?project=p1&item=i1", "board", { project: "p1", item: "i1" }],
]

test("the work system and projects reach the machine as their own words", async () => {
  for (const [path, word, sent] of CARRIED_READS) {
    const client = new FakeClient()
    const r = reader(client, { t: 1000 })
    const res = await r.fetch(path)
    assert.equal(res.status, 200, path + " was not answered")
    assert.deepEqual(await body(res), { read: word }, path + " did not answer what the machine said")
    assert.equal(client.reads.length, 1, path + " asked the machine " + client.reads.length + " times")
    assert.equal(client.reads[0].machine, "mac-a", path + " asked a machine this page is not reading")
    assert.equal(client.reads[0].word, word, path + " asked for " + client.reads[0].word)
    assert.deepEqual(client.reads[0].body, sent, path + " carried the wrong body")
    const logged = r.log[r.log.length - 1]
    assert.equal(logged.answer, "relay", path + " was answered somewhere other than the machine")
    assert.equal(logged.word, word, path + " is logged as " + logged.word)
  }
})

test("Project file reads stay on the selected Project's machine and accept only inventoried ids", async () => {
  const client = new FakeClient()
  const r = reader(client, { t: 1000 })
  client._place = () => ({ machine: "mac-b", id: "p1", path: "/repo" })
  const other = await r.fetch("/v1/projects/cloud-p1/files")
  assert.equal(other.status, 409)
  assert.equal(client.reads.length, 0)
  const invalid = await r.fetch("/v1/projects/cloud-p1/files/%2Fetc%2Fpasswd")
  assert.equal(invalid.status, 501)
  assert.equal(client.reads.length, 0)
})

test("a carried read stops when its caller's signal fires, instead of waiting out the relay's minute", async () => {
  const client = new FakeClient()
  let settle: (value: unknown) => void = () => {}
  client.readAnswer = () => new Promise((resolve) => { settle = resolve })
  const r = reader(client, { t: 1000 })
  const controller = new AbortController()
  const asked = r.fetch("/v1/work/v2/session-todos/%251", { signal: controller.signal })
  controller.abort()
  // Raced, so a seam that ignores the signal is a red test and not a hung one.
  const outcome = await Promise.race([
    asked.then(() => "answered", (error: unknown) => error),
    new Promise((resolve) => setTimeout(() => resolve("still waiting"), 200)),
  ]) as Error & { code?: string }
  assert.ok(outcome instanceof Error, `a fired signal left the read ${String(outcome)}`)
  assert.equal(outcome.name, "AbortError", "a fired signal rejects the way fetch does")
  assert.equal(outcome.code, "cloud_read_abandoned")
  assert.match(outcome.message, /work\.v2\.session-todos/)
  const logged = r.log[r.log.length - 1]
  assert.equal(logged.answer, "unanswered")
  assert.equal(logged.code, "cloud_read_abandoned")
  assert.equal(logged.word, "work.v2.session-todos")
  // The machine's late answer settles nothing and throws nowhere.
  settle({ direct_todos: [] })
  await new Promise((resolve) => setTimeout(resolve, 0))

  // A signal that has already fired is not sent at all past the ask, and one
  // that never fires changes nothing.
  const before = new AbortController()
  before.abort()
  await assert.rejects(r.fetch("/v1/work/v2/session-todos/%251", { signal: before.signal }), { name: "AbortError" })
  client.readAnswer = async (word) => ({ read: word })
  const calm = await r.fetch("/v1/work/v2/session-todos/%251", { signal: new AbortController().signal })
  assert.equal(calm.status, 200)
  assert.deepEqual(await body(calm), { read: "work.v2.session-todos" })
})

test("a query field no word carries is refused by name, never quietly dropped", async () => {
  const client = new FakeClient()
  const r = reader(client, { t: 1000 })
  // A retired v1 board URL is never sent to the machine.
  const res = await r.fetch("/v1/work/board?project=%2Fp&section=active")
  assert.equal(res.status, 501)
  const refusal = await body<{ error: string; detail: string }>(res)
  assert.equal(refusal.error, "cloud_not_carried")
  assert.equal(client.reads.length, 0, "the machine was asked a question it could not have been told")
})

test("a single decision read refuses malformed paths before asking the machine", async () => {
  const client = new FakeClient()
  const r = reader(client, { t: 1000 })
  for (const path of [
    "/v1/work/decisions/not-a-uuid",
    "/v1/work/decisions/10000000-0000-4000-8000-000000000001/answer",
    "/v1/work/decisions/10000000-0000-4000-8000-000000000001?state=all",
  ]) {
    const res = await r.fetch(path)
    assert.ok(res.status >= 400, path)
  }
  assert.equal(client.reads.length, 0)
})

test("a client with no generic read refuses each word by name rather than throwing", async () => {
  const client = new FakeClient()
  client._machineRequest = undefined
  const r = reader(client, { t: 1000 })
  for (const [path, word] of CARRIED_READS) {
    const res = await r.fetch(path)
    assert.equal(res.status, 501, path)
    const refusal = await body<{ error: string; detail: string }>(res)
    assert.equal(refusal.error, "cloud_not_carried", path)
    assert.match(refusal.detail, new RegExp(word.replace(".", "\\.")), path + " did not name the word")
  }
})

test("squad reads carry exact scope, binding and cursor fields to the chosen machine", async () => {
  const client = new FakeClient()
  const r = reader(client, { t: 1000 })
  const cases: [string, string, Record<string, unknown>][] = [
    ["/v1/squad/catalog", "squad.catalog", {}],
    ["/v1/squad/scopes", "squad.scopes", {}],
    ["/v1/squad/skill-sources?provider=codex&place_id=p1", "squad.skill-sources", { provider: "codex", place_id: "p1" }],
    ["/v1/squad/skill-sources?provider=claude-code&id=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef&folder=false", "squad.skill-sources", { provider: "claude-code", id: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", folder: "false" }],
    ["/v1/squad/definitions/role%20one?version=2.0.0", "squad.definition", { definition_id: "role one", version: "2.0.0" }],
    ["/v1/squad/settings?place_id=p1", "squad.settings", { place_id: "p1" }],
    ["/v1/squad/settings?scope_id=project-a", "squad.settings", { scope_id: "project-a" }],
    ["/v1/squad/session-bindings", "squad-session-bindings", {}],
    ["/v1/squad/session-snapshots/conversation-a", "squad-session-snapshot", { conversation: "conversation-a" }],
    ["/v1/squad/events/head", "squad-event-head", {}],
    ["/v1/squad/events?after=12&limit=50", "squad-events", { after: 12, limit: 50 }],
  ]
  for (const [path, word, reqBody] of cases) {
    assert.equal((await r.fetch(path)).status, 200, path)
    assert.deepEqual(client.reads.at(-1), { machine: "mac-a", word, body: reqBody }, path)
  }
  const count = client.reads.length
  for (const path of ["/v1/squad/events?after=-1", "/v1/squad/events?after=1.5", "/v1/squad/events?limit=0", "/v1/squad/settings?place_id=p1&scope_id=p2"]) {
    assert.equal((await r.fetch(path)).status, 400, path)
  }
  assert.equal(client.reads.length, count, "invalid reads never reach the machine")
})

// "Read again" on a waiting card (2026-10-14): across Cloud it is the one-row
// read, so the machine captures that Session's screen and states its row now.
test("read again asks the machine for just that row", async () => {
  const client = new FakeClient()
  ;(client as unknown as { _machineImplements(machine: string, word: string): string })._machineImplements =
    (_machine, word) => word === "sessions.row" ? "yes" : "unknown"
  const r = reader(client, { t: 10_000 })
  await r.fetch("/v1/sessions?look=%2519")
  assert.deepEqual(client.reads.at(-1), { machine: "mac-a", word: "sessions.row", body: { row: "%19" } })
  ;(client as unknown as { _machineImplements(machine: string, word: string): string })._machineImplements = () => "unknown"
  await r.fetch("/v1/sessions?look=%2519")
  assert.equal(client.reads.at(-1)?.word, "sessions.snapshot", "an older machine is asked to restate everything")
})

// 2026-10-16: the machine read the menu whole after "read again" and put it on
// an s/ channel this page did not hold at that moment (`fanout=0`). The answer
// carries the row, and the page takes it — unless an s/ envelope for the row
// arrived while the ask was out, which is newer than the answer's copy.
test("read again takes the row its answer carries", async () => {
  const client = new FakeClient()
  ;(client as unknown as { _machineImplements(machine: string, word: string): string })._machineImplements =
    (_machine, word) => word === "sessions.row" ? "yes" : "unknown"
  const row = { session: { id: "%19", state: "waiting", menu: { options: [{ n: 1, label: "Yes" }] } }, at: 1, scan: {} }
  client.readAnswer = async () => ({ sessions: ["%19"], complete: true, row })
  const applied: { machine: string; session: string; payload: unknown; mark: number }[] = []
  let mark = 7
  const c = client as unknown as {
    _sessionRowMark(machine: string, session: string): number
    _applyAnsweredSessionRow(machine: string, session: string, payload: unknown, mark: number): boolean
  }
  c._sessionRowMark = () => mark
  c._applyAnsweredSessionRow = (machine, session, payload, at) => { applied.push({ machine, session, payload, mark: at }); return true }
  const r = reader(client, { t: 10_000 })
  const res = await r.fetch("/v1/sessions?look=%2519")
  assert.equal(res.status, 200)
  assert.deepEqual(applied, [{ machine: "mac-a", session: "%19", payload: row, mark: 7 }],
    "the mark is read before the ask, so a row delivered meanwhile wins")
  // An older machine's answer names the row and carries none: nothing to take.
  client.readAnswer = async () => ({ sessions: ["%19"], complete: true })
  mark = 8
  await r.fetch("/v1/sessions?look=%2519")
  assert.equal(applied.length, 1)
})

/**
 * The single-machine page after a Session ended on the machine: the fleet's
 * held pass, taken before, still lists it; the retained ss/ marker, taken
 * after, does not. On 2026-10-15 the list kept the closed Session and opening
 * it said "could not read the transcript" (`cond=not_listed`).
 */
function closedOnMachine() {
  const client = new FakeClient()
  const execution = "a".repeat(32)
  const rows = ["s1", "s2"].map((sessionID) => ({
    destination: { machineID: "mac-a", sessionID, executionGeneration: execution },
    state: "idle", freshness: "current" as const, observedAt: 100_000,
  }))
  const clock = { now: 100_000 }
  client.statusSnapshots = markerOnly("mac-a", "c".repeat(32), 100, "s1", "s2")
  const r = new RelayReader("mac-a", { classicStatus: true, now: () => clock.now,
    machineProjection: async () => ({ kind: "ready", complete: true, observedAt: 100_000,
      snapshotGeneration: "b".repeat(32), rows }),
    listPresentations: async () => rows.map((row) => ({
      destination: row.destination, title: row.destination.sessionID, status: { state: "idle" }, projection: row,
    })),
  })
  r.attach(client)
  const end = () => {
    clock.now = 130_000
    client.statusSnapshots = markerOnly("mac-a", "d".repeat(32), 130, "s1")
  }
  return { r, end, client }
}

test("a Session the machine's newer marker leaves out leaves the single-machine list though the held pass still has it", async () => {
  const { r, end } = closedOnMachine()
  let list = acceptSnapshot(null, await r.snapshot())!
  assert.deepEqual(list.sessions.map((row: { id: string }) => row.id), ["s1", "s2"])
  end()
  list = acceptSnapshot(list, await r.snapshot()) ?? list
  assert.deepEqual(list.sessions.map((row: { id: string }) => row.id), ["s1"])
})

test("opening a Session the marker no longer lists reads the list again", async () => {
  const { r, end, client } = closedOnMachine()
  const frames: string[][] = []
  const handle = r.stream().open("/v1/events", {
    onFrame: (_event: string, data: string) => frames.push((JSON.parse(data) as SessionsSnapshot).sessions.map((row) => row.id)),
    onOpen: () => {}, onError: () => {},
  })
  client.emit({ type: "sessions", identity: { machine: "mac-a", session: "s1" } })
  await new Promise((resolve) => setTimeout(resolve, 5))
  assert.deepEqual(frames.at(-1), ["s1", "s2"])
  const before = frames.length
  end()
  const read = r.fetch("/v1/transcript?session=s2")
  await new Promise((resolve) => setTimeout(resolve, 5))
  assert.equal(frames.length, before + 1, "the refusal asked for a new list frame")
  assert.deepEqual(frames.at(-1), ["s1"])
  await read.catch(() => {})
  handle.close()
})

test("a held title of a closed Session does not stand in for a new child's title", async () => {
  const client = new FakeClient()
  client.sessions = async () => { throw new Error("the rich snapshot must not gate the list") }
  const execution = "a".repeat(32)
  const projected = ["s1", "child"].map((sessionID) => ({
    destination: { machineID: "mac-a", sessionID, executionGeneration: execution },
    state: "working", freshness: "current" as const, observedAt: 1000,
  }))
  let now = 1000, reads = 0
  const r = new RelayReader("mac-a", { classicStatus: true, now: () => now,
    machineProjection: async () => ({ kind: "ready", complete: true, observedAt: now,
      snapshotGeneration: "b".repeat(32), presentationGeneration: "c".repeat(32), rows: projected }),
    // The first answer is two rows, as many as asked, but one is a Session
    // that closed; the child's title arrives on the next read.
    listPresentations: async () => {
      reads++
      return ["s1", reads === 1 ? "closed" : "child"].map((sessionID) => ({
        destination: { machineID: "mac-a", sessionID, executionGeneration: execution },
        title: "Title " + sessionID, status: { state: "working" },
      }))
    },
  })
  r.attach(client)
  const first = await r.snapshot()
  assert.equal(first.sessions.find((row) => row.id === "child")?.label, "child")
  now += 5_001
  const next = await r.snapshot()
  assert.equal(reads, 2, "the missing title was asked for again")
  assert.equal(next.sessions.find((row) => row.id === "child")?.label, "Title child")
})

// The version rides on the machine's own orch/ descriptor. Before that has
// arrived the version is pending, which a page says as "still reading"; after
// it, a machine that publishes no version is simply unknown.
test("health says a version is still coming until the machine's descriptor arrives", async () => {
  const client = new FakeClient() as FakeClient & {
    orchestratorSnapshots: Map<string, unknown>; orchestratorReceived: Set<string>
  }
  client.orchestratorSnapshots = new Map([["mac-a", { schedules: [] }]])
  client.orchestratorReceived = new Set()
  const r = reader(client, { t: 12_000 })
  const before = await body<{ version?: string; version_pending?: boolean }>(await r.fetch("/v1/health"))
  assert.equal(before.version, undefined)
  assert.equal(before.version_pending, true, "a schedules read's entry is not the descriptor")

  client.orchestratorSnapshots.set("mac-a", { app: { version: "v1.0.9", api_level: 10 } })
  client.orchestratorReceived.add("mac-a")
  const after = await body<{ version?: string; version_pending?: boolean }>(await r.fetch("/v1/health"))
  assert.equal(after.version, "v1.0.9")
  assert.equal(after.version_pending, undefined)

  client.orchestratorSnapshots.set("mac-a", { machine: { platform: "linux" } })
  const none = await body<{ version?: string; version_pending?: boolean }>(await r.fetch("/v1/health"))
  assert.equal(none.version_pending, undefined, "a descriptor without a version is not pending")
})


test("a transcript generation refusal requests recovery without needing a diagnostic recorder", async () => {
  const client = new FakeClient()
  const now = 1_000_000
  client.statusSnapshots = markerOnly("mac-a", "c".repeat(32), now / 1000, "s1")
  client.recoverStatusRow = async () => false
  const recoveries: string[] = []
  const r = new RelayReader("mac-a", { classicStatus: true, now: () => now,
    onExecutionChanged: (machine, session) => { recoveries.push(machine + "/" + session) } })
  r.attach(client)
  const response = await r.fetch("/v1/transcript?session=s1&limit=200")
  assert.equal(response.status, 409)
  assert.deepEqual(recoveries, ["mac-a/s1"])
  assert.deepEqual(client.pinnedReads, [], "the old execution is never read as a substitute")
})


test("a pinned native Cloud conversation retains its binding and admits the current execution", async () => {
  // @ts-expect-error -- Node tests resolve source extensions.
  const { assistantStateCurrent, assistantProtocolOwned, assistantComposeAdmits } = await import("../session/antigravity-ui.ts")
  const client = new FakeClient()
  const execution = "a".repeat(32), now = 1_000_000
  const destination = { machineID: "mac-a", sessionID: "owned:acp:synthetic", executionGeneration: execution }
  // Current machines omit owned/provider in ss/, but their authorized list names the native binding.
  const projected = { destination, state: "idle", freshness: "current" as const, observedAt: now - 30_000 }
  const reader = new RelayReader("mac-a", { classicStatus: true, now: () => now,
    machineProjection: async () => ({ kind: "ready", complete: true, observedAt: now,
      snapshotGeneration: execution, rows: [projected] }),
    listPresentations: async () => [{ destination, title: "Native conversation", assistant: "antigravity",
      status: { state: "idle", identity: "protocol", sessionId: "native-conversation", work_state: "ready" }, projection: projected }],
  })
  reader.attach(client)
  const row = (await reader.snapshot()).sessions[0]
  assert.equal(row.backend, "owned")
  assert.equal(assistantProtocolOwned(row), true)
  assert.equal(assistantComposeAdmits(row.assistant, undefined, row.state, false, assistantProtocolOwned(row)), true)
  assert.equal(assistantComposeAdmits(row.assistant, undefined, "working", false, assistantProtocolOwned(row)), true)
  assert.equal(assistantStateCurrent(row, now), false, "native text input does not require a terminal observation")
  assert.deepEqual(documentIdentityForSession([row], destination.sessionID, "cloud"),
    { machine: "mac-a", session: destination.sessionID })
})


test("a native-looking title or another execution cannot supply a Cloud protocol binding", async () => {
  // @ts-expect-error -- Node tests resolve source extensions.
  const { assistantProtocolOwned } = await import("../session/antigravity-ui.ts")
  for (const variant of ["missing_binding", "different_execution"] as const) {
    const client = new FakeClient(), execution = "a".repeat(32), now = 1_000_000
    const destination = { machineID: "mac-a", sessionID: "owned:acp:synthetic", executionGeneration: execution }
    const projected = { destination, state: "idle", freshness: "current" as const, observedAt: now }
    const reader = new RelayReader("mac-a", { classicStatus: true, now: () => now,
      machineProjection: async () => ({ kind: "ready", complete: true, observedAt: now,
        snapshotGeneration: execution, rows: [projected] }),
      listPresentations: async () => [{ destination: { ...destination,
        executionGeneration: variant === "different_execution" ? "b".repeat(32) : execution },
        title: "Antigravity conversation", assistant: "antigravity", status: { state: "idle",
          identity: variant === "missing_binding" ? undefined : "protocol", sessionId: "native-conversation" }, projection: projected }],
    })
    reader.attach(client)
    assert.equal(assistantProtocolOwned((await reader.snapshot()).sessions[0]), false, variant)
  }
})


test("a native reply is read after its user echo without waiting for terminal activity", async () => {
  const client = new FakeClient(), clock = { t: 1_000_000 }
  client.rows = [row("mac-a", "s1", { assistant: "antigravity", backend: "owned", sessionBinding: "protocol" })]
  let revision = 0, reads = 0
  client.transcript = async () => {
    reads++
    return { id: "s1", signature: "acp:synthetic:" + revision, evidence: "structured", entries:
      revision === 0 ? [] : revision === 1 ? [{ role: "user", text: "hello" }] :
        [{ role: "user", text: "hello" }, { role: "assistant", text: "reply" }] } as any
  }
  const r = reader(client, clock)
  await r.transcript("s1")
  r.wrote("s1", "done")
  revision = 1
  await r.transcript("s1")
  revision = 2
  clock.t += 4_001
  const next = await r.transcript("s1")
  assert.equal(next.page.entries.at(-1)?.text, "reply")
  assert.equal(reads, 3)
  await r.transcript("s1")
  assert.equal(reads, 3, "unchanged redraws share the existing short read window")
})


test("both Cloud list paths reuse provider counts without opening rich rows or issuing reads", async () => {
  for (const projected of [false, true]) {
    const client = new FakeClient()
    const now = 1_000_000
    const generation = "a".repeat(32)
    const destination = { machineID: "mac-a", sessionID: "s1", executionGeneration: generation }
    const projection = { destination, state: "working", freshness: "current" as const, observedAt: now, runningAgentCount: 6 }
    const payload = { machine_id: "mac-a", session_id: "s1", execution_generation: generation,
      snapshot_generation: generation, running_agent_count: 6 as number | undefined, agent_count_unknown: false,
      state: "working", source: { freshness: "current", observed_at: now / 1000 } }
    client.statusSnapshots = new Map([
      [JSON.stringify(["mac-a", SESSION_INVENTORY_ID]), { payload: { at: now / 1000, complete: true,
        snapshot_generation: generation, inventory: { version: 1, sessions: ["s1"] } } }],
      [JSON.stringify(["mac-a", "s1"]), { payload }],
    ])
    const reader = new RelayReader("mac-a", { classicStatus: true, now: () => now,
      ...(projected ? { machineProjection: async () => ({ kind: "ready" as const, complete: true,
        observedAt: now, snapshotGeneration: generation, rows: [projection] }) } : {}),
      listPresentations: async () => [{ destination, title: "Fixture Session", assistant: "claude", status: { state: "working" }, projection }],
    })
    reader.attach(client)
    const drawn = async () => listRunningAgents((await reader.snapshot()).sessions[0])
    assert.equal(await drawn(), 6)
    payload.running_agent_count = 0
    assert.equal(await drawn(), 0, "an ended agent clears the previous total")
    payload.running_agent_count = undefined
    payload.agent_count_unknown = true
    assert.equal(await drawn(), null)
    payload.agent_count_unknown = false
    assert.equal(await drawn(), undefined, "older machines do not invent zero")
    assert.equal(client.reads.length, 0, "the list never requests per-Session provider work")
  }
})

test("board summaries use the versioned word only when the machine advertises it", async () => {
  for (const capability of ["yes", "unknown", "no"]) {
    const client = new FakeClient()
    ;(client as unknown as { _machineImplements(machine: string, word: string): string })._machineImplements =
      (_machine, word) => word === "work.v2.cards" ? capability : "unknown"
    const r = reader(client, { t: 10_000 })
    const response = await r.fetch("/v1/work/v2/items?view=cards-v1&project=cloud-p1&status=all&q=needle&cursor=page-2")
    assert.equal(response.status, 200)
    assert.deepEqual(client.reads.at(-1), {
      machine: "mac-a", word: capability === "yes" ? "work.v2.cards" : "work.v2.search",
      body: { project: "p1", status: "all", query: "needle", cursor: "page-2" },
    })
  }
})

test("expanding a stored tool body carries its exact machine and execution pin", async () => {
  const client = new FakeClient()
  const target = { machineID: "mac-a", sessionID: "s1", executionGeneration: "a".repeat(32) }
  const signal = new AbortController().signal
  const view = "entry-v1z:0:" + "b".repeat(64)
  const requests: unknown[] = []
  Object.assign(client, { readForGeneration: async (...args: unknown[]) => {
    requests.push(args)
    return { reference: "0:" + "b".repeat(64), entry: { role: "tool", text: "complete stored output" } }
  } })
  const r = new RelayReader("mac-a", { classicStatus: true, now: () => 1000,
    resolveSession: async () => target })
  r.attach(client)
  const answer = await r.fetch("/v1/transcript?session=s1&view=" + view, { signal })
  assert.equal(answer.status, 200)
  assert.deepEqual(requests, [[target, "transcript", { limit: 200, view }, signal]])
  assert.equal(client.asks, 0, "expansion must not be replaced by a cached full-page read")
})


test("Graphify session reads preserve the exact session and reject extra fields", async () => {
 const client=new FakeClient();const r=reader(client,{t:1000})
 const res=await r.fetch("/v1/sessions/"+encodeURIComponent("%19")+"/graphify")
 assert.equal(res.status,200)
 assert.deepEqual(client.sessionPageReads,[{identity:{machine:"mac-a",session:"%19"},word:"session-graphify",body:{},answer:"session-graphify"}])
 const bad=await r.fetch("/v1/sessions/"+encodeURIComponent("%19")+"/graphify?path=/elsewhere")
 assert.equal(bad.status,501);assert.equal(client.sessionPageReads.length,1)
})
