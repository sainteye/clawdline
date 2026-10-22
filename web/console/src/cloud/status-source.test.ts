import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- node's type-stripping runner resolves the source .ts file.
import { pinnedQuestion, statusSource, STATUS_GAP_RETRY_MS } from "./status-source.ts"

const machineID = "one"
const sessionID = "same"
const executionGeneration = "0123456789abcdef0123456789abcdef"
const snapshotGeneration = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const inventory = "__clawdline_inventory_v1__"

function clientFixture() {
  const calls: string[] = []
  let emit = (_event: unknown) => {}
  const statusSnapshots = new Map<string, unknown>()
  const detailSnapshots = new Map<string, unknown>()
  const at = Math.floor(Date.now() / 1000)
  const row = { machine_id: machineID, session_id: sessionID, execution_generation: executionGeneration,
    snapshot_generation: snapshotGeneration, state: "working", source: { provenance: "session_watch", observed_at: at,
      freshness: "current" }, projected_at: at, inventory_complete: true }
  const setRow = () => statusSnapshots.set(JSON.stringify([machineID, sessionID]), {
    identity: { machine: machineID, session: sessionID }, payload: row, observedAt: at, sequence: 1,
  })
  setRow()
  statusSnapshots.set(JSON.stringify([machineID, inventory]), {
    identity: { machine: machineID, session: inventory },
    payload: { inventory: { version: 1, sessions: [sessionID] }, at, complete: true,
      snapshot_generation: snapshotGeneration }, observedAt: at, sequence: 2,
  })
  const client = {
    ready: true,
    statusSnapshots,
    orchestratorSnapshots: new Map<string, unknown>(),
    machineOffline: new Map<string, { until: number }>(),
    now: () => Date.now(),
    readContentCapabilities: new Map([[machineID, { at, supported: true }]]),
    detailSnapshots,
    openDetail() {},
    closeDetail() {},
    events(listener: (event: unknown) => void) { emit = listener; return () => {} },
    async machines() { return { machines: [{ id: machineID, freshness: "current" }], syncing: false, retryAfterMs: 0 } },
    async recoverStatusRow(machine: string, session: string, generation: string) {
      calls.push("recover:" + [machine, session, generation].join(":"))
      setRow()
      return true
    },
    subscribe(channels: string[]) { calls.push("subscribe:" + channels.join(",")) },
    unsubscribe(channels: string[]) { calls.push("unsubscribe:" + channels.join(",")) },
    async refreshSessionStatus(machine: string, session: string) {
      calls.push("sessions.row:" + machine + ":" + session)
    },
    async listPresentationsForMachine(machine: string) {
      calls.push("sessions.list:" + machine)
      return { at, complete: true, sessions: [{ id: sessionID, execution_generation: executionGeneration, title: "The real title",
        assistant: "claude",
        status: { state: "idle", work_state: "ready", work_note: "Queued heavy command finished", work_provenance: "self",
          persona: "product-manager" } }] }
    },
    async infoForGeneration(destination: { executionGeneration: string }) {
      calls.push("info:" + destination.executionGeneration)
      return { info: { session: { title: "Pinned" } } }
    },
    async transcriptForGeneration(destination: { executionGeneration: string }): Promise<unknown> {
      calls.push("transcript:" + destination.executionGeneration)
      return { id: sessionID, entries: [{ role: "assistant", text: "Done", artifacts: [{ id: "image-1",
        media_type: "image/png", byte_count: 3, width: 1, height: 1, expires_at: 4_000_000_000 }] }], nextBefore: 123 }
    },
    async transcriptPageForGeneration(destination: { executionGeneration: string }, before: number): Promise<unknown> {
      calls.push("older:" + destination.executionGeneration + ":" + before)
      return { id: sessionID, entries: [{ role: "user", text: "Earlier" }], nextBefore: 40 }
    },
    async skillsForGeneration(destination: { executionGeneration: string }) {
      calls.push("skills:" + destination.executionGeneration)
      return { skills: [{ name: "review", description: "Review work", source: "project" }] }
    },
    async imageForGeneration(destination: { executionGeneration: string }, id: string) {
      calls.push("image:" + destination.executionGeneration + ":" + id)
      return { id, media_type: "image/png", byte_count: 3, data: "cG5n" }
    },
  }
  return { client, calls, row, emit: (event: unknown) => emit(event) }
}

test("a status row event names the Session whose list title should refresh", () => {
  const { client, emit } = clientFixture()
  const events: unknown[] = []
  const stop = statusSource(() => client as never).subscribe((event) => events.push(event))
  emit({ type: "session_status", identity: { machine: machineID, session: sessionID } })
  assert.deepEqual(events, [{ machineID, sessionID, kind: "changed" }])
  stop()
})

test("a task descriptor arriving after status regroups the fleet without opening a Session", async () => {
  const { client, row, emit } = clientFixture()
  const marker = client.statusSnapshots.get(JSON.stringify([machineID, inventory])) as { payload: { inventory: { sessions: string[] } } }
  marker.payload.inventory.sessions.push("root")
  client.statusSnapshots.set(JSON.stringify([machineID, "root"]), {
    identity: { machine: machineID, session: "root" }, payload: { ...row, session_id: "root" },
    observedAt: row.projected_at, sequence: 3,
  })
  const source = statusSource(() => client as never)
  const before = await source.readMachine(machineID, new AbortController().signal)
  assert.equal(before.rows?.find((r) => r.destination.sessionID === sessionID)?.parentSessionID, undefined)
  const events: unknown[] = []
  const stop = source.subscribe((event) => events.push(event))
  client.orchestratorSnapshots.set(machineID, { at: row.projected_at, tasks: [
    { child: { terminalId: sessionID }, root: { terminalId: "root" } },
  ] })
  emit({ type: "orchestrator", machine: machineID })
  assert.deepEqual(events, [{ machineID, kind: "access_changed" }])
  for (let refresh = 0; refresh < 3; refresh++) {
    const after = await source.readMachine(machineID, new AbortController().signal)
    assert.equal(after.rows?.find((r) => r.destination.sessionID === sessionID)?.parentSessionID, "root")
  }
  stop()
})

test("a cold pinned list uses task roots when its real display status omits ancestry", async () => {
  const { client, row } = clientFixture()
  client.statusSnapshots.clear()
  client.orchestratorSnapshots.set(machineID, { at: row.projected_at, tasks: [
    { child: { terminalId: sessionID }, root: { terminalId: "root" } },
  ] })
  const original = client.listPresentationsForMachine
  client.listPresentationsForMachine = async (machine) => {
    const reply = await original(machine)
    return { ...reply, sessions: [...reply.sessions, { ...reply.sessions[0], id: "root", title: "Root" }] }
  }
  const reading = await statusSource(() => client as never).readMachine(machineID, new AbortController().signal)
  assert.equal(reading.rows?.find((r) => r.destination.sessionID === sessionID)?.parentSessionID, "root")
})

test("a renewed client keeps the Session list hearing new Sessions", () => {
  // A token renewal puts a new client in place. A Session a schedule opened
  // afterwards arrives only on the new one, and nothing on the page asks for it.
  const before = clientFixture()
  const after = clientFixture()
  let stopped = 0
  const events_ = before.client.events
  before.client.events = (listener) => { const stop = events_(listener); return () => { stopped += 1; stop() } }
  let current: unknown = before.client
  const changes = new Set<() => void>()
  const events: unknown[] = []
  const stop = statusSource(() => current as never, (listener: () => void) => {
    changes.add(listener)
    return () => { changes.delete(listener) }
  }).subscribe((event) => events.push(event))
  current = after.client
  for (const listener of changes) listener()
  assert.deepEqual(events, [{ machineID, kind: "changed" }])
  events.length = 0
  assert.equal(stopped, 1)
  after.emit({ type: "session_status", identity: { machine: machineID, session: sessionID } })
  assert.deepEqual(events, [{ machineID, sessionID, kind: "changed" }])
  stop()
  assert.equal(changes.size, 0)
})

test("a new browser replays a missing status marker once and recovers the Session list", async () => {
  const { client } = clientFixture()
  client.readContentCapabilities.delete(machineID)
  client.listPresentationsForMachine = async () => { throw new Error("older machine") }
  const key = JSON.stringify([machineID, inventory])
  const marker = client.statusSnapshots.get(key)
  client.statusSnapshots.delete(key)
  let reads = 0
  ;(client as typeof client & { recoverStatusMarker: (machine: string) => Promise<boolean> }).recoverStatusMarker = async () => {
    reads++
    client.statusSnapshots.set(key, marker)
    return true
  }
  const source = statusSource(() => client as never)
  const result = await source.readMachine(machineID, new AbortController().signal)
  assert.equal(result.kind, "ready")
  assert.equal(reads, 1)
  assert.equal((await source.readMachine(machineID, new AbortController().signal)).kind, "ready")
  assert.equal(reads, 1)
})

test("an unanswered marker is retried at most once per minute", async () => {
  const { client } = clientFixture()
  client.readContentCapabilities.delete(machineID)
  client.listPresentationsForMachine = async () => { throw new Error("older machine") }
  client.statusSnapshots.delete(JSON.stringify([machineID, inventory]))
  let reads = 0
  ;(client as typeof client & { recoverStatusMarker: (machine: string) => Promise<boolean> }).recoverStatusMarker = async () => {
    reads++
    return false
  }
  const source = statusSource(() => client as never)
  const first = await source.readMachine(machineID, new AbortController().signal)
  assert.equal(first.kind, "unavailable")
  if (first.kind === "unavailable") assert.ok(first.retryAt! > Date.now() + STATUS_GAP_RETRY_MS - 1000)
  await source.readMachine(machineID, new AbortController().signal)
  assert.equal(reads, 1)
})

test("a late content capability tells the fleet to retry its pending names", () => {
  const { client, emit } = clientFixture()
  const events: unknown[] = []
  const stop = statusSource(() => client as never).subscribe((event) => events.push(event))
  emit({ type: "orchestrator", machine: machineID })
  assert.deepEqual(events, [{ machineID, kind: "access_changed" }])
  stop()
})

test("a newer row before its marker does not announce a transient event gap", () => {
  const { client, emit, calls } = clientFixture()
  const events: unknown[] = []
  const stop = statusSource(() => client as never).subscribe((event) => events.push(event))
  const next = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
  const rowEntry = client.statusSnapshots.get(JSON.stringify([machineID, sessionID])) as {
    payload: { snapshot_generation: string }; sequence: number
  }
  rowEntry.payload.snapshot_generation = next
  rowEntry.sequence = 3
  emit({ type: "session_status", identity: { machine: machineID, session: sessionID } })
  assert.deepEqual(events, [])
  const marker = client.statusSnapshots.get(JSON.stringify([machineID, inventory])) as {
    payload: { snapshot_generation: string }; sequence: number
  }
  marker.payload.snapshot_generation = next
  marker.sequence = 4
  emit({ type: "session_status", identity: { machine: machineID, session: inventory } })
  assert.deepEqual(events, [{ machineID, sessionID: undefined, kind: "changed" }])
  assert.deepEqual(calls, [])
  stop()
})

test("status-only list never subscribes to or reads rich content", async () => {
  const { client, calls } = clientFixture()
  const source = statusSource(() => client as never)
  const read = await source.readMachine(machineID, new AbortController().signal)
  assert.equal(read.kind, "ready")
  assert.deepEqual(calls, [])
})

test("opening a Session requests only its fresh machine status", async () => {
  const { client, calls } = clientFixture()
  client.refreshSessionStatus = async (machine, session) => {
    calls.push("sessions.row:" + machine + ":" + session)
  }
  const source = statusSource(() => client as never)
  const before = await source.readMachine(machineID, new AbortController().signal)
  await source.refreshSessionStatus?.({ machineID, sessionID, executionGeneration }, new AbortController().signal)
  assert.deepEqual(calls, ["sessions.row:" + machineID + ":" + sessionID])
  client.refreshSessionStatus = async () => { throw new Error("machine unavailable") }
  await assert.rejects(async () => source.refreshSessionStatus?.(
    { machineID, sessionID, executionGeneration }, new AbortController().signal))
  assert.deepEqual(await source.readMachine(machineID, new AbortController().signal), before,
    "a failed fresh read leaves the published row unchanged")
})

test("a cold status gap uses one complete pinned machine list for rows and titles", async () => {
  const { client, calls } = clientFixture()
  const list = client.listPresentationsForMachine
  client.listPresentationsForMachine = async (machine) => {
    const reply = await list(machine)
    return { ...reply, sessions: reply.sessions.map((row) => ({ ...row,
      status: { ...row.status, sessionId: "conversation-41" } })) }
  }
  client.statusSnapshots.delete(JSON.stringify([machineID, sessionID]))
  const source = statusSource(() => client as never)
  const reading = await source.readMachine(machineID, new AbortController().signal)
  assert.equal(reading.kind, "ready")
  assert.equal(reading.rows?.[0]?.destination.executionGeneration, executionGeneration)
  assert.equal(reading.rows?.[0]?.snapshotGeneration, reading.snapshotGeneration,
    "a direct row carries the generation the single-machine reader verifies")
  const titles = await source.readMachinePresentations?.(machineID, new AbortController().signal)
  assert.deepEqual(titles?.map((row) => row.title), ["The real title"])
  assert.equal(titles?.[0]?.status?.sessionId, "conversation-41",
    "the pinned Cloud row retains the conversation that owns its attention notes")
  assert.deepEqual(calls, ["sessions.list:" + machineID])
})

test("a pinned list fallback keeps attention and reply filters aligned with its visible status", async () => {
  const { client } = clientFixture()
  client.statusSnapshots.delete(JSON.stringify([machineID, sessionID]))
  let now = Date.now()
  client.now = () => now
  let attention = 1
  let replyQuestion = true
  const list = client.listPresentationsForMachine
  client.listPresentationsForMachine = async (machine) => {
    const reply = await list(machine)
    return { ...reply, at: Math.floor(now / 1000), sessions: reply.sessions.map((row) => ({ ...row,
      status: { ...row.status, state: attention ? "waiting" : "idle",
        work_state: attention ? "waiting_you" : "ready", attention_count: attention,
        ...(replyQuestion ? { screen_reading: "read", menu: { options: [{ n: 1, label: "Continue" }] } } : {}) } })) }
  }
  const source = statusSource(() => client as never)
  const first = await source.readMachine(machineID, new AbortController().signal)
  assert.equal(first.rows?.[0]?.needsAttention, true)
  assert.equal(first.rows?.[0]?.waitingForReply, true)
  assert.equal((await source.readMachinePresentations?.(machineID, new AbortController().signal))?.[0]?.status?.attention_count, 1)

  replyQuestion = false
  now += STATUS_GAP_RETRY_MS
  const noteOnly = await source.readMachine(machineID, new AbortController().signal)
  assert.equal(noteOnly.rows?.[0]?.needsAttention, true)
  assert.equal(noteOnly.rows?.[0]?.waitingForReply, undefined)

  attention = 0
  now += STATUS_GAP_RETRY_MS
  const cleared = await source.readMachine(machineID, new AbortController().signal)
  assert.equal(cleared.rows?.[0]?.needsAttention, false)
  assert.equal(cleared.rows?.[0]?.waitingForReply, undefined)
})

test("a cold pinned list retains an Epic child's visible parent", async () => {
  const { client } = clientFixture()
  client.statusSnapshots.delete(JSON.stringify([machineID, sessionID]))
  const original = client.listPresentationsForMachine
  client.listPresentationsForMachine = async (machine) => {
    const reply = await original(machine)
    const base = reply.sessions[0]
    return { ...reply, sessions: [
      { ...base, id: "root", title: "Root", status: { ...base.status, state: "idle", sessionId: "root-conversation" } },
      { ...base, id: "child", title: "Child", status: { ...base.status, state: "idle",
        epic_parent: { owner_session_id: "root-conversation", epic_id: "epic" } } },
    ] }
  }
  const reading = await statusSource(() => client as never).readMachine(machineID, new AbortController().signal)
  assert.equal(reading.rows?.find((row) => row.destination.sessionID === "child")?.parentSessionID, "root")
})

test("a cold pinned list retains a task child's listed parent without ss status", async () => {
  const { client } = clientFixture()
  client.statusSnapshots.delete(JSON.stringify([machineID, sessionID]))
  const original = client.listPresentationsForMachine
  client.listPresentationsForMachine = async (machine) => {
    const reply = await original(machine)
    const base = reply.sessions[0]
    return { ...reply, sessions: [
      { ...base, id: "root", title: "Root", status: { ...base.status, state: "idle" } },
      { ...base, id: "child", title: "Child", status: { ...base.status, state: "idle", parent_session_id: "root" } },
    ] }
  }
  const reading = await statusSource(() => client as never).readMachine(machineID, new AbortController().signal)
  assert.equal(reading.rows?.find((row) => row.destination.sessionID === "child")?.parentSessionID, "root")
})

test("a pinned list during stale status replay retains the same execution's task parent", async () => {
  const { client, row } = clientFixture()
  Object.assign(row, { parent_session_id: "root" })
  row.projected_at -= 600
  const marker = client.statusSnapshots.get(JSON.stringify([machineID, inventory])) as {
    payload: { at: number }
  }
  marker.payload.at -= 600
  const original = client.listPresentationsForMachine
  client.listPresentationsForMachine = async (machine) => {
    const reply = await original(machine)
    const child = reply.sessions[0]
    return { ...reply, sessions: [
      { ...child, id: "root", title: "Root" },
      { ...child, id: sessionID, title: "Child" },
    ] }
  }
  const reading = await statusSource(() => client as never).readMachine(machineID, new AbortController().signal)
  assert.equal(reading.rows?.find((entry) => entry.destination.sessionID === sessionID)?.parentSessionID, "root")
})

test("a cold browser asks once before the machine descriptor arrives", async () => {
  const { client, calls } = clientFixture()
  client.readContentCapabilities.delete(machineID)
  client.statusSnapshots.delete(JSON.stringify([machineID, inventory]))
  const source = statusSource(() => client as never)
  assert.equal((await source.readMachine(machineID, new AbortController().signal)).kind, "ready")
  assert.equal((await source.readMachine(machineID, new AbortController().signal)).kind, "ready")
  assert.deepEqual(calls, ["sessions.list:" + machineID])
})

test("a failed cold list retries after the short bound and recovers without a marker", async () => {
  const { client, calls } = clientFixture()
  const diagnostics: { event: string; data: Record<string, unknown> }[] = []
  ;(client as typeof client & { viewerEvents: { record: (event: string, data: Record<string, unknown>) => void } })
    .viewerEvents = { record: (event, data) => { diagnostics.push({ event, data }) } }
  client.statusSnapshots.delete(JSON.stringify([machineID, inventory]))
  let now = Date.now()
  client.now = () => now
  let fail = true
  client.listPresentationsForMachine = async (machine: string) => {
    calls.push("sessions.list:" + machine)
    if (fail) throw new Error("carrier_closed")
    return { at: Math.floor(now / 1000), complete: true,
      sessions: [{ id: sessionID, execution_generation: executionGeneration, title: "Recovered", assistant: "claude",
        status: { state: "idle", work_state: "ready", work_note: "", work_provenance: "self", persona: "" } }] }
  }
  const source = statusSource(() => client as never)
  const first = await source.readMachine(machineID, new AbortController().signal)
  assert.equal(first.kind, "unavailable")
  if (first.kind === "unavailable") assert.equal(first.retryAt, now + STATUS_GAP_RETRY_MS)
  now += STATUS_GAP_RETRY_MS - 1
  assert.equal((await source.readMachine(machineID, new AbortController().signal)).kind, "unavailable")
  assert.equal(calls.length, 1)
  now += 1
  const second = await source.readMachine(machineID, new AbortController().signal)
  assert.equal(second.kind, "unavailable")
  if (second.kind === "unavailable") assert.equal(second.retryAt, now + STATUS_GAP_RETRY_MS * 2)
  assert.equal(calls.length, 2)
  fail = false
  now += STATUS_GAP_RETRY_MS * 2 - 1
  assert.equal((await source.readMachine(machineID, new AbortController().signal)).kind, "unavailable")
  assert.equal(calls.length, 2)
  now += 1
  const recovered = await source.readMachine(machineID, new AbortController().signal)
  assert.equal(recovered.kind, "ready")
  assert.equal(recovered.rows?.[0]?.destination.executionGeneration, executionGeneration)
  assert.equal(calls.length, 3)
  assert.deepEqual(diagnostics.map((row) => [row.event, row.data.code, row.data.failures]), [
    ["cloud.status_list.failed", "untyped", 1], ["cloud.status_list.failed", "untyped", 2],
  ])
  assert.equal(JSON.stringify(diagnostics).includes("carrier_closed"), false)
})

test("a stale retained status pass recovers from one pinned machine list without repeated reads", async () => {
  const { client, calls, row } = clientFixture()
  row.projected_at -= 600
  client.listPresentationsForMachine = async (machine: string) => {
    calls.push("sessions.list:" + machine)
    return { at: Math.floor(Date.now() / 1000), complete: false,
      sessions: [{ id: sessionID, execution_generation: executionGeneration, title: "The real title", assistant: "claude",
        status: { state: "idle", work_state: "ready", work_note: "", work_provenance: "self", persona: "" } }] }
  }
  const source = statusSource(() => client as never)
  const first = await source.readMachine(machineID, new AbortController().signal)
  const second = await source.readMachine(machineID, new AbortController().signal)
  assert.equal(first.kind, "ready")
  assert.equal(second.kind, "ready")
  assert.deepEqual(calls, ["sessions.list:" + machineID])
  assert.deepEqual((await source.readMachinePresentations?.(machineID, new AbortController().signal))?.map((row) => row.title),
    ["The real title"])
  assert.deepEqual(calls, ["sessions.list:" + machineID])
})

test("a machine list's project key travels beside its cwd, so the Board picks a worktree Session over Cloud", async () => {
  const { client } = clientFixture()
  client.listPresentationsForMachine = async () => ({ at: Math.floor(Date.now() / 1000), complete: true,
    sessions: [{ id: sessionID, execution_generation: executionGeneration, title: "The real title", assistant: "claude",
      cwd: "/repo/.worktrees/feature", project_key: "/repo",
      status: { state: "idle", work_state: "ready", work_note: "", work_provenance: "self", persona: "" } }] })
  const source = statusSource(() => client as never)
  await source.readMachine(machineID, new AbortController().signal)
  assert.deepEqual((await source.readMachinePresentations?.(machineID, new AbortController().signal))
    ?.map((row) => [row.cwd, row.projectKey]), [["/repo/.worktrees/feature", "/repo"]])
})

test("a direct fallback refreshes its presentation after the short gap window", async () => {
  const { client, calls } = clientFixture()
  client.statusSnapshots.delete(JSON.stringify([machineID, inventory]))
  let now = Date.now()
  client.now = () => now
  let workNote = "Waiting for another Session"
  client.listPresentationsForMachine = async (machine: string) => {
    calls.push("sessions.list:" + machine)
    return { at: Math.floor(now / 1000), complete: true,
      sessions: [{ id: sessionID, execution_generation: executionGeneration, title: "The real title", assistant: "claude",
        status: { state: "idle", work_state: "waiting_session", work_note: workNote,
          work_provenance: "broker", persona: "" } }] }
  }
  const source = statusSource(() => client as never)
  const first = await source.readMachine(machineID, new AbortController().signal)
  assert.equal((await source.readMachinePresentations?.(machineID, new AbortController().signal))?.[0]?.status?.work_note,
    workNote)
  workNote = "Queued heavy command finished"
  now += STATUS_GAP_RETRY_MS
  const refreshed = await source.readMachine(machineID, new AbortController().signal)
  assert.notEqual(refreshed.snapshotGeneration, first.snapshotGeneration)
  assert.equal((await source.readMachinePresentations?.(machineID, new AbortController().signal))?.[0]?.status?.work_note,
    workNote)
  assert.deepEqual(calls, ["sessions.list:" + machineID, "sessions.list:" + machineID])
})

test("a stale marker cannot turn an incomplete direct list into a complete one", async () => {
  const { client, row } = clientFixture()
  row.projected_at -= 600
  const marker = client.statusSnapshots.get(JSON.stringify([machineID, inventory])) as {
    payload: { at: number }
  }
  marker.payload.at -= 600
  client.listPresentationsForMachine = async () => ({ at: Math.floor(Date.now() / 1000), complete: false,
    sessions: [{ id: sessionID, execution_generation: executionGeneration, title: "The real title", assistant: "claude",
      status: { state: "idle", work_state: "ready", work_note: "", work_provenance: "self", persona: "" } }] })
  const read = await statusSource(() => client as never).readMachine(machineID, new AbortController().signal)
  assert.equal(read.kind, "ready")
  if (read.kind === "ready") {
    assert.equal(read.complete, false)
    assert.equal(read.rows.length, 1)
  }
})

test("an incomplete machine scan is complete only with the signed inventory's exact Session set", async () => {
  const { client, calls } = clientFixture()
  client.statusSnapshots.delete(JSON.stringify([machineID, sessionID]))
  client.listPresentationsForMachine = async (machine: string) => {
    calls.push("sessions.list:" + machine)
    return { at: Math.floor(Date.now() / 1000), complete: false,
      sessions: [{ id: sessionID, execution_generation: executionGeneration, title: "The real title", assistant: "claude",
        status: { state: "idle", work_state: "ready", work_note: "", work_provenance: "self", persona: "" } }] }
  }
  const source = statusSource(() => client as never)
  const certified = await source.readMachine(machineID, new AbortController().signal)
  assert.equal(certified.kind, "ready")
  if (certified.kind === "ready") assert.equal(certified.complete, true)
  assert.deepEqual(calls, ["sessions.list:" + machineID])

  const other = clientFixture()
  other.client.statusSnapshots.delete(JSON.stringify([machineID, sessionID]))
  other.client.listPresentationsForMachine = async () => ({ at: Math.floor(Date.now() / 1000), complete: false,
    sessions: [{ id: "different", execution_generation: executionGeneration, title: "Wrong set", assistant: "claude",
      status: { state: "idle", work_state: "ready", work_note: "", work_provenance: "self", persona: "" } }] })
  other.client.recoverStatusRow = async () => false
  const partial = await statusSource(() => other.client as never).readMachine(machineID, new AbortController().signal)
  assert.equal(partial.kind, "ready")
  if (partial.kind === "ready") {
    assert.equal(partial.complete, false)
    assert.equal(partial.rows[0].destination.sessionID, "different")
  }
})

test("a refused pinned list falls back to exact retained-row recovery", async () => {
  const { client, calls } = clientFixture()
  client.statusSnapshots.delete(JSON.stringify([machineID, sessionID]))
  client.listPresentationsForMachine = async () => {
    calls.push("sessions.list-refused")
    throw Object.assign(new Error("refused"), { code: "cloud_read_busy" })
  }
  const source = statusSource(() => client as never)
  assert.equal((await source.readMachine(machineID, new AbortController().signal)).kind, "ready")
  assert.equal((await source.readMachine(machineID, new AbortController().signal)).kind, "ready")
  assert.deepEqual(calls, ["sessions.list-refused", "recover:" + [machineID, sessionID, snapshotGeneration].join(":")])
})

test("one machine list read joins only the current Session execution", async () => {
  const { client, calls, row } = clientFixture()
  const source = statusSource(() => client as never)
  const rows = await source.readMachinePresentations?.(machineID, new AbortController().signal)
  assert.deepEqual(rows?.map((item) => item.title), ["The real title"])
  assert.equal(rows?.[0]?.assistant, "claude", "the verified list keeps the provider when status omits it")
  assert.equal(rows?.[0]?.status?.work_note, "Queued heavy command finished")
  assert.equal(rows?.[0]?.status?.persona, "product-manager")
  assert.deepEqual(calls, ["sessions.list:" + machineID])
  row.execution_generation = "ffffffffffffffffffffffffffffffff"
  assert.deepEqual(await source.readMachinePresentations?.(machineID, new AbortController().signal), [])
  assert.deepEqual(calls, ["sessions.list:" + machineID, "sessions.list:" + machineID])
})

test("a cold browser reads current Session names before the content descriptor arrives", async () => {
  const { client, calls } = clientFixture()
  client.readContentCapabilities.delete(machineID)
  const rows = await statusSource(() => client as never).readMachinePresentations?.(
    machineID, new AbortController().signal)
  assert.deepEqual(rows?.map((row) => row.title), ["The real title"])
  assert.deepEqual(calls, ["sessions.list:" + machineID])
})

test("Relay-confirmed offline applies to one machine only and expires to unknown", async () => {
  const { client } = clientFixture()
  let now = Date.now()
  client.now = () => now
  client.machineOffline.set(machineID, { until: now + 100 })
  const source = statusSource(() => client as never)
  client.ready = false
  assert.deepEqual(await source.readMachine(machineID, new AbortController().signal),
    { kind: "unavailable", reason: "unknown" })
  client.ready = true
  const offline = await source.readMachine(machineID, new AbortController().signal)
  assert.equal(offline.kind === "unavailable" && offline.reason, "offline")
  assert.equal(offline.kind === "unavailable" && offline.retryAt, now + 101)
  assert.equal(offline.rows?.[0].freshness, "stale")
  now += 101
  const expired = await source.readMachine(machineID, new AbortController().signal)
  assert.equal(expired.kind === "unavailable" && expired.reason, "unknown")
  const marker = client.statusSnapshots.get(JSON.stringify([machineID, inventory])) as { payload: { at: number } }
  marker.payload.at = Math.floor((Date.now() - 301_000) / 1000)
  const stale = await source.readMachine(machineID, new AbortController().signal)
  assert.equal(stale.kind === "unavailable" && stale.reason, "stale")
  assert.equal(stale.rows?.[0].freshness, "stale")
  client.machineOffline.delete(machineID)
  marker.payload.at = Math.floor(Date.now() / 1000)
  assert.equal((await source.readMachine(machineID, new AbortController().signal)).kind, "ready")
})

test("opening and leaving an exact detail subscribes, reads pinned content, and releases both channels", async () => {
  const { client, calls } = clientFixture()
  const timings: { event: string; data: Record<string, unknown> }[] = []
  ;(client as typeof client & { viewerEvents: { record: (event: string, data: Record<string, unknown>) => void } })
    .viewerEvents = { record: (event, data) => { timings.push({ event, data }) } }
  const source = statusSource(() => client as never)
  const destination = { machineID, sessionID, executionGeneration }
  const detail = await source.readDetail(destination, new AbortController().signal)
  assert.equal(detail.kind, "ready")
  assert.equal(detail.kind === "ready" && detail.info.title, "Pinned")
  assert.equal(detail.kind === "ready" && detail.nextBefore, 123)
  assert.equal(timings.length, 1)
  assert.equal(timings[0].event, "cloud.detail.first_read")
  assert.equal(timings[0].data.session, sessionID)
  assert.equal(timings[0].data.outcome, "ready")
  assert.equal(typeof timings[0].data.info_ms, "number")
  assert.equal(typeof timings[0].data.transcript_ms, "number")
  assert.equal(JSON.stringify(timings).includes("Pinned"), false)
  await source.readDetail(destination, new AbortController().signal)
  assert.equal(timings.length, 1, "an open detail's routine reread does not create another diagnostic")
  source.closeDetail?.(destination)
  assert.deepEqual(calls, ["subscribe:s/one/same,t/one/same", "info:" + executionGeneration,
    "transcript:" + executionGeneration, "info:" + executionGeneration,
    "transcript:" + executionGeneration, "unsubscribe:s/one/same,t/one/same"])
})

test("a transient roster refusal keeps its code and the same detail can recover", async () => {
  const { client } = clientFixture()
  const readInfo = client.infoForGeneration
  let attempts = 0
  client.infoForGeneration = async (destination) => {
    if (attempts++ === 0) throw Object.assign(new Error("roster unreadable"), { code: "command_roster_unreadable" })
    return readInfo(destination)
  }
  const source = statusSource(() => client as never)
  const destination = { machineID, sessionID, executionGeneration }
  assert.deepEqual(await source.readDetail(destination, new AbortController().signal),
    { kind: "unavailable", reason: "unknown", code: "command_roster_unreadable" })
  assert.equal((await source.readDetail(destination, new AbortController().signal)).kind, "ready")
  source.closeDetail?.(destination)
})

test("an abandoned first detail read still reports which Session was waiting", async () => {
  const { client } = clientFixture()
  const timings: { event: string; data: Record<string, unknown> }[] = []
  ;(client as typeof client & { viewerEvents: { record: (event: string, data: Record<string, unknown>) => void } })
    .viewerEvents = { record: (event, data) => { timings.push({ event, data }) } }
  let finish!: (value: { info: { session: { title: string } } }) => void
  let began!: () => void
  const started = new Promise<void>((resolve) => { began = resolve })
  client.infoForGeneration = async () => new Promise<{ info: { session: { title: string } } }>((resolve) => { finish = resolve; began() })
  const source = statusSource(() => client as never)
  const destination = { machineID, sessionID, executionGeneration }
  const abort = new AbortController()
  const read = source.readDetail(destination, abort.signal)
  await started
  abort.abort()
  finish({ info: { session: { title: "Pinned" } } })
  assert.deepEqual(await read, { kind: "unavailable", reason: "unknown" })
  assert.equal(timings[0].data.session, sessionID)
  assert.equal(timings[0].data.outcome, "aborted")
  assert.equal(typeof timings[0].data.total_ms, "number")
})

test("an image tile reads only an artifact visible in the opened exact execution", async () => {
  const { client, calls } = clientFixture()
  const source = statusSource(() => client as never)
  const destination = { machineID, sessionID, executionGeneration }
  const artifact = { id: "image-1", media_type: "image/png", byte_count: 3,
    width: 1, height: 1, expires_at: 4_000_000_000 }
  await assert.rejects(() => source.readImage!(destination, artifact), { code: "execution_generation_changed" })
  assert.equal((await source.readDetail(destination, new AbortController().signal)).kind, "ready")
  await assert.rejects(() => source.readImage!(destination, { ...artifact, id: "another" }),
    { code: "execution_generation_changed" })
  const image = await source.readImage!(destination, artifact)
  assert.ok(image.url.startsWith("blob:"))
  image.release()
  assert.deepEqual(calls.filter((call) => call.startsWith("image:")), ["image:" + executionGeneration + ":image-1"])
  source.closeDetail?.(destination)
  await assert.rejects(() => source.readImage!(destination, artifact), { code: "execution_generation_changed" })
})

test("slash-menu skills belong to the opened execution and stop when that execution changes", async () => {
  const { client, calls, row } = clientFixture()
  const source = statusSource(() => client as never)
  const destination = { machineID, sessionID, executionGeneration }
  await assert.rejects(() => source.readSkills!(destination, new AbortController().signal), { code: "old_version" })
  await source.readDetail(destination, new AbortController().signal)
  assert.deepEqual((await source.readSkills!(destination, new AbortController().signal)).map((skill) => skill.name), ["review"])
  row.execution_generation = "ffffffffffffffffffffffffffffffff"
  await assert.rejects(() => source.readSkills!(destination, new AbortController().signal),
    { code: "execution_generation_changed" })
  assert.deepEqual(calls.filter((call) => call.startsWith("skills:")), ["skills:" + executionGeneration])
  source.closeDetail?.(destination)
})

test("an opened rich-row change rereads pinned content without replaying its subscription", async () => {
  const { client, calls } = clientFixture()
  const source = statusSource(() => client as never)
  const destination = { machineID, sessionID, executionGeneration }
  await source.readDetail(destination, new AbortController().signal)
  await source.readDetail(destination, new AbortController().signal)
  assert.equal(calls.filter((call) => call.startsWith("subscribe:")).length, 1)
  assert.equal(calls.filter((call) => call.startsWith("transcript:")).length, 2)
  source.closeDetail?.(destination)
  assert.equal(calls.filter((call) => call.startsWith("unsubscribe:")).length, 1)
})

test("a pinned read keeps the local transcript's structured cards and Unix-second times", async () => {
  const { client } = clientFixture()
  const entry = { role: "tool", text: "Edited a file", tool: "Edit", at: 123,
    fileChanges: [{ kind: "edit", path: "sample.ts", unifiedDiff: "+line" }],
    plan: [{ step: "Review", status: "completed" }],
    activity: { kind: "explored", title: "Files" } }
  client.transcriptForGeneration = async () => ({ id: sessionID, entries: [entry], nextBefore: 123 })
  const detail = await statusSource(() => client as never).readDetail(
    { machineID, sessionID, executionGeneration }, new AbortController().signal)
  assert.equal(detail.kind, "ready")
  if (detail.kind === "ready") assert.deepEqual(detail.entries, [entry])
})

test("an opened detail reads an older page with the same execution and a decreasing cursor", async () => {
  const { client, calls } = clientFixture()
  const source = statusSource(() => client as never)
  const destination = { machineID, sessionID, executionGeneration }
  await source.readDetail(destination, new AbortController().signal)
  const older = await source.readOlder?.(destination, 123, new AbortController().signal)
  assert.deepEqual(older, { kind: "ready", destination, before: 123,
    entries: [{ role: "user", text: "Earlier" }], nextBefore: 40 })
  assert.deepEqual(calls.filter((call) => call.startsWith("older:")), ["older:" + executionGeneration + ":123"])
  source.closeDetail?.(destination)
  assert.deepEqual(await source.readOlder?.(destination, 40, new AbortController().signal),
    { kind: "unavailable", reason: "unknown" })
})

test("native detail signatures survive the source and pin the exact older request", async () => {
  const { client } = clientFixture()
  const source = statusSource(() => client as never)
  const destination = { machineID, sessionID, executionGeneration }
  const signature = "agy1:synthetic:123:abc"
  client.transcriptForGeneration = async () => ({ id: sessionID, entries: [], nextBefore: 123, signature })
  const detail = await source.readDetail(destination, new AbortController().signal)
  assert.equal(detail.kind === "ready" && detail.signature, signature)
  let sent: unknown
  Object.assign(client, { transcriptPageForGeneration: async (_destination: unknown, _before: number, _signal: AbortSignal, revision: string) => {
    sent = revision
    return { id: sessionID, entries: [], signature, nextBefore: 40 }
  } })
  const older = await source.readOlder?.(destination, 123, new AbortController().signal, signature)
  assert.equal(sent, signature)
  assert.equal(older?.kind === "ready" && older.requestedRevision, signature)
  Object.assign(client, { transcriptPageForGeneration: async () => { throw Object.assign(new Error("changed"), { code: "transcript_cursor_stale" }) } })
  const stale = await source.readOlder?.(destination, 40, new AbortController().signal, signature)
  assert.equal(stale?.kind === "unavailable" && stale.code, "transcript_cursor_stale")
})

test("an older page is refused if status changes during its machine read", async () => {
  const { client, calls, row } = clientFixture()
  const source = statusSource(() => client as never)
  const destination = { machineID, sessionID, executionGeneration }
  await source.readDetail(destination, new AbortController().signal)
  client.transcriptPageForGeneration = async () => {
    calls.push("older")
    row.execution_generation = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    return { id: sessionID, entries: [{ role: "user", text: "wrong generation" }] }
  }
  assert.deepEqual(await source.readOlder?.(destination, 123, new AbortController().signal),
    { kind: "unavailable", reason: "changed" })
  assert.equal(calls.filter((call) => call === "older").length, 1)
})

test("an older page rejects a nondecreasing cursor and stops when no cursor remains", async () => {
  const { client } = clientFixture()
  const source = statusSource(() => client as never)
  const destination = { machineID, sessionID, executionGeneration }
  await source.readDetail(destination, new AbortController().signal)
  client.transcriptPageForGeneration = async () => ({ id: sessionID, entries: [], nextBefore: 123 })
  assert.deepEqual(await source.readOlder?.(destination, 123, new AbortController().signal),
    { kind: "unavailable", reason: "unknown" })
  client.transcriptPageForGeneration = async () => ({ id: sessionID, entries: [] })
  assert.deepEqual(await source.readOlder?.(destination, 123, new AbortController().signal),
    { kind: "ready", destination, before: 123, entries: [], nextBefore: undefined })
})

test("an older machine rejects only pagination while its already read first page remains usable", async () => {
  const { client } = clientFixture()
  const source = statusSource(() => client as never)
  const destination = { machineID, sessionID, executionGeneration }
  const first = await source.readDetail(destination, new AbortController().signal)
  assert.equal(first.kind, "ready")
  client.transcriptPageForGeneration = async () => { throw Object.assign(new Error("Unsupported before"), { code: "malformed_read" }) }
  assert.deepEqual(await source.readOlder?.(destination, 123, new AbortController().signal),
    { kind: "unavailable", reason: "old_version" })
  assert.equal(first.kind === "ready" && first.entries[0]?.text, "Done")
  assert.equal((await source.readMachine(machineID, new AbortController().signal)).kind, "ready")
})

test("leaving a detail prevents an in-flight older page from reaching the view", async () => {
  const { client } = clientFixture()
  const source = statusSource(() => client as never)
  const destination = { machineID, sessionID, executionGeneration }
  await source.readDetail(destination, new AbortController().signal)
  let finish!: (value: unknown) => void
  let began!: () => void
  const started = new Promise<void>((resolve) => { began = resolve })
  client.transcriptPageForGeneration = () => new Promise((resolve) => { finish = resolve; began() })
  const abort = new AbortController()
  const older = source.readOlder?.(destination, 123, abort.signal)
  await started
  abort.abort()
  source.closeDetail?.(destination)
  finish({ id: sessionID, entries: [{ role: "user", text: "closed" }] })
  assert.deepEqual(await older, { kind: "unavailable", reason: "unknown" })
})

test("a changed status generation stops detail before transcript", async () => {
  const { client, calls, row } = clientFixture();
  (client as { infoForGeneration: (destination: { executionGeneration: string }) => Promise<unknown> }).infoForGeneration = async () => {
    calls.push("info")
    row.execution_generation = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    return { info: { session: {} } }
  }
  const source = statusSource(() => client as never)
  const detail = await source.readDetail({ machineID, sessionID, executionGeneration }, new AbortController().signal)
  assert.deepEqual(detail, { kind: "unavailable", reason: "changed" })
  assert.deepEqual(calls, ["subscribe:s/one/same,t/one/same", "info"])
})

test("a stale status row refuses content without subscribing", async () => {
  const { client, calls, row } = clientFixture()
  row.source.freshness = "unverified"
  const source = statusSource(() => client as never)
  const detail = await source.readDetail({ machineID, sessionID, executionGeneration }, new AbortController().signal)
  assert.deepEqual(detail, { kind: "unavailable", reason: "stale" })
  assert.deepEqual(calls, [])
})

test("a missing ss row settles before it can shake the visible list", async () => {
  const { client, calls } = clientFixture()
  client.readContentCapabilities.delete(machineID)
  client.listPresentationsForMachine = async () => { throw new Error("older machine") }
  client.statusSnapshots.delete(JSON.stringify([machineID, sessionID]))
  const source = statusSource(() => client as never)
  const recovered = await source.readMachine(machineID, new AbortController().signal)
  assert.equal(recovered.kind, "ready")
  assert.deepEqual(calls, ["recover:" + [machineID, sessionID, snapshotGeneration].join(":")])
  const reading = await source.readMachine(machineID, new AbortController().signal)
  assert.equal(reading.kind, "ready")
})

test("a failed retained-row request keeps the machine in event_gap", async () => {
  const { client, calls } = clientFixture()
  client.readContentCapabilities.delete(machineID)
  client.listPresentationsForMachine = async () => { throw new Error("older machine") }
  let now = Date.now()
  client.now = () => now
  client.statusSnapshots.delete(JSON.stringify([machineID, sessionID]));
  (client as { recoverStatusRow: (machine: string, session: string, generation: string) => Promise<boolean> }).recoverStatusRow = async () => {
    calls.push("recover-refused")
    return false
  }
  const source = statusSource(() => client as never)
  const reading = await source.readMachine(machineID, new AbortController().signal)
  assert.equal(reading.kind === "unavailable" && reading.reason, "event_gap")
  const repeated = await source.readMachine(machineID, new AbortController().signal)
  assert.equal(repeated.kind === "unavailable" && repeated.reason, "event_gap")
  assert.equal(repeated.kind === "unavailable" ? repeated.retryAt : undefined, now + STATUS_GAP_RETRY_MS)
  assert.deepEqual(calls, ["recover-refused"])
  now += STATUS_GAP_RETRY_MS
  await source.readMachine(machineID, new AbortController().signal)
  assert.deepEqual(calls, ["recover-refused", "recover-refused"])
  const marker = client.statusSnapshots.get(JSON.stringify([machineID, inventory])) as { payload: { snapshot_generation: string } }
  marker.payload.snapshot_generation = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
  await source.readMachine(machineID, new AbortController().signal)
  assert.deepEqual(calls, ["recover-refused", "recover-refused", "recover-refused"])
})

test("a machine that stopped reporting is stale, not a gap that is being read again", async () => {
  // A closed MacBook: its retained marker names a row the relay no longer has,
  // and nothing from it has arrived inside the roster's window. Asking it to
  // restate that row cannot succeed, and saying "reading again" never ends.
  const { client, calls } = clientFixture()
  client.readContentCapabilities.delete(machineID)
  client.listPresentationsForMachine = async () => { throw new Error("older machine") }
  client.statusSnapshots.delete(JSON.stringify([machineID, sessionID]));
  (client as { recoverStatusRow: (machine: string, session: string, generation: string) => Promise<boolean> }).recoverStatusRow = async () => {
    calls.push("recover")
    return false
  }
  client.machines = async () => ({ machines: [{ id: machineID, freshness: "stale" }], syncing: false, retryAfterMs: 0 })
  const reading = await statusSource(() => client as never).readMachine(machineID, new AbortController().signal)
  assert.equal(reading.kind === "unavailable" && reading.reason, "stale")
  assert.deepEqual(calls, [])
})

test("a stale machine refuses detail before subscribing to content", async () => {
  const { client, calls } = clientFixture()
  client.machines = async () => ({ machines: [{ id: machineID, freshness: "stale" }], syncing: false, retryAfterMs: 0 })
  const source = statusSource(() => client as never)
  const reading = await source.readMachine(machineID, new AbortController().signal)
  assert.deepEqual(reading.kind === "unavailable" && reading.reason, "stale")
  const detail = await source.readDetail({ machineID, sessionID, executionGeneration }, new AbortController().signal)
  assert.deepEqual(detail, { kind: "unavailable", reason: "stale" })
  assert.deepEqual(calls, [])
})

test("an explicit r/ refusal blocks detail while an unread descriptor permits an exact attempt", async () => {
  const { client, calls } = clientFixture()
  const source = statusSource(() => client as never)
  const destination = { machineID, sessionID, executionGeneration }
  const list = await source.readMachine(machineID, new AbortController().signal)
  assert.equal(list.kind, "ready")
  const descriptor = client.readContentCapabilities.get(machineID) as { at: number; supported?: boolean }
  // Published, and it says this machine has no r/ reader: it really is too old.
  delete descriptor.supported
  assert.deepEqual(await source.readDetail(destination, new AbortController().signal),
    { kind: "unavailable", reason: "old_version" })
  // A signed, current execution can ask its own machine before orch/ arrives.
  // The machine still checks the exact execution and read permission.
  descriptor.supported = true
  descriptor.at -= 301
  assert.equal((await source.readDetail(destination, new AbortController().signal)).kind, "ready")
  client.readContentCapabilities.delete(machineID)
  assert.equal((await source.readDetail(destination, new AbortController().signal)).kind, "ready")
  assert.equal((await source.readOlder?.(destination, 123, new AbortController().signal))?.kind, "ready")
  assert.deepEqual(calls.filter((call) => call.startsWith("info:")), ["info:" + executionGeneration,
    "info:" + executionGeneration])
})

test("an opened rich row names only the same execution and current question", async () => {
  const { client } = clientFixture()
  const at = Math.floor(Date.now() / 1000)
  const menu = { question: "Choose", options: [
    { n: 1, label: "Allow", can: true, selected: false },
    { n: 2, label: "Deny", can: true, selected: false },
  ] }
  const rich = { identity: { machine: machineID, session: sessionID }, machine: machineID, session: sessionID,
    execution_generation: executionGeneration, source: { freshness: "current", observed_at: at }, menu }
  client.detailSnapshots.set(machineID + "\u0000" + sessionID, rich)
  const destination = { machineID, sessionID, executionGeneration }
  const first = pinnedQuestion(client, destination)
  assert.deepEqual(first?.options, [{ key: "1", label: "Allow" }, { key: "2", label: "Deny" }])
  assert.match(first?.fingerprint ?? "", /^[0-9a-f]{64}$/u)
  const detail = await statusSource(() => client as never).readDetail(destination, new AbortController().signal)
  assert.equal(detail.kind === "ready" && detail.question?.fingerprint, first?.fingerprint)
  menu.options[0].label = "Allow once"
  const second = await statusSource(() => client as never).readQuestion?.(destination, new AbortController().signal)
  assert.notEqual(second?.fingerprint, first?.fingerprint)
  rich.execution_generation = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
  assert.equal(pinnedQuestion(client, destination), null)
  rich.execution_generation = executionGeneration
  rich.source.observed_at = at - 301
  assert.equal(pinnedQuestion(client, destination), null)
  rich.source.observed_at = at
  rich.source.freshness = "unverified"
  assert.equal(pinnedQuestion(client, destination), null)
  rich.source.freshness = "current"
  rich.identity.machine = "other"
  assert.equal(pinnedQuestion(client, destination), null)
})

test("a status gap keeps the machine's Clawdfather while the direct list stands in", async () => {
  const { client, row } = clientFixture()
  Object.assign(row, { machine_scope: true, coordinator_bound: true })
  // The marker names a Session whose row has not arrived: an event gap.
  const marker = client.statusSnapshots.get(JSON.stringify([machineID, inventory])) as {
    payload: { inventory: { sessions: string[] } } }
  marker.payload.inventory.sessions.push("late")
  const reading = await statusSource(() => client as never).readMachine(machineID, new AbortController().signal)
  assert.ok(reading.snapshotGeneration?.startsWith("direct:"), "the direct list answered")
  assert.equal(reading.rows?.[0]?.machineScope, true, "an older machine's list leaves the flag out")
  assert.equal(reading.rows?.[0]?.coordinatorBound, true)
})

test("a direct list names the Clawdfather without any held status row", async () => {
  const { client } = clientFixture()
  client.statusSnapshots.delete(JSON.stringify([machineID, sessionID]))
  const list = client.listPresentationsForMachine
  client.listPresentationsForMachine = async (machine) => {
    const reply = await list(machine)
    return { ...reply, sessions: reply.sessions.map((row) => ({ ...row,
      status: { ...row.status, machine_scope: true, coordinator_bound: true } })) }
  }
  const reading = await statusSource(() => client as never).readMachine(machineID, new AbortController().signal)
  assert.ok(reading.snapshotGeneration?.startsWith("direct:"))
  assert.equal(reading.rows?.[0]?.machineScope, true)
  assert.equal(reading.rows?.[0]?.coordinatorBound, true)
})


test("execution recovery bypasses a fresh retained old generation and subsequent reads use the verified replacement", async () => {
  const { client, calls } = clientFixture()
  const newer = "b".repeat(32)
  client.listPresentationsForMachine = async (machine: string) => {
    calls.push("sessions.list:" + machine)
    return { at: Math.floor(Date.now() / 1000), complete: true, sessions: [{ id: sessionID,
      execution_generation: newer, title: "Current", assistant: "claude",
      status: { state: "idle", work_state: "ready", work_note: "", work_provenance: "self", persona: "product-manager" } }] }
  }
  const source = statusSource(() => client as never)
  const before = await source.readMachine(machineID, new AbortController().signal)
  assert.equal(before.rows?.[0].destination.executionGeneration, executionGeneration)
  const recovered = await source.readMachine(machineID, new AbortController().signal, true)
  assert.equal(recovered.kind, "ready")
  assert.equal(recovered.rows?.[0].destination.executionGeneration, newer)
  const next = await source.readMachine(machineID, new AbortController().signal)
  assert.equal(next.rows?.[0].destination.executionGeneration, newer)
  assert.equal(calls.filter((call) => call.startsWith("sessions.list:")).length, 1)
})

for (const assistant of ["codex", "claude", undefined, "unsupported"]) {
  test(`a direct list preserves only a recognized assistant (${assistant}) without extra reads`, async () => {
    const { client, calls } = clientFixture()
    client.statusSnapshots.delete(JSON.stringify([machineID, inventory]))
    client.listPresentationsForMachine = async (machine: string) => {
      calls.push("sessions.list:" + machine)
      return { at: Math.floor(Date.now() / 1000), complete: true,
        sessions: [{ id: sessionID, execution_generation: executionGeneration, title: "Named work", assistant,
          status: { state: "idle", work_state: "ready" } }] } as never
    }
    const source = statusSource(() => client as never)
    const signal = new AbortController().signal
    const first = await source.readMachine(machineID, signal)
    const second = await source.readMachine(machineID, signal)
    const presentations = await source.readMachinePresentations?.(machineID, signal)
    const expected = assistant === "claude" || assistant === "codex" ? assistant : undefined
    assert.equal(first.kind, "ready")
    assert.equal(first.rows?.[0]?.assistant, expected)
    assert.equal(second.rows?.[0]?.assistant, expected)
    assert.equal(presentations?.[0]?.assistant, expected)
    assert.deepEqual(calls, ["sessions.list:" + machineID])
  })
}

test("a direct list never carries the previous execution's assistant into a new one", async () => {
  const { client, calls } = clientFixture()
  client.statusSnapshots.delete(JSON.stringify([machineID, inventory]))
  let now = Date.now()
  client.now = () => now
  let generation = executionGeneration
  let assistant: string | undefined = "codex"
  client.listPresentationsForMachine = async (machine: string) => {
    calls.push("sessions.list:" + machine)
    return { at: Math.floor(now / 1000), complete: true,
      sessions: [{ id: sessionID, execution_generation: generation, title: "Named work", assistant,
        status: { state: "idle", work_state: "ready" } }] } as never
  }
  const source = statusSource(() => client as never)
  const signal = new AbortController().signal
  assert.equal((await source.readMachine(machineID, signal)).rows?.[0]?.assistant, "codex")
  generation = "f".repeat(32)
  assistant = undefined
  now += 60_001
  const restarted = await source.readMachine(machineID, signal)
  assert.equal(restarted.rows?.[0]?.destination.executionGeneration, generation)
  assert.equal(restarted.rows?.[0]?.assistant, undefined)
  assert.equal(calls.length, 2)
})

test("a Cloud presentation preserves a project's SVG and independent accent", async () => {
  const { client } = clientFixture()
  client.statusSnapshots.delete(JSON.stringify([machineID, inventory]))
  const icon = { accent: "#123456", cells: [], svg: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/></svg>' }
  client.listPresentationsForMachine = async () => ({
    at: Math.floor(Date.now() / 1000), complete: true,
    sessions: [{ id: sessionID, execution_generation: executionGeneration, title: "Vector project", icon,
      status: { state: "idle", work_state: "ready" } }],
  }) as never
  const source = statusSource(() => client as never)
  const signal = new AbortController().signal
  assert.equal((await source.readMachine(machineID, signal)).kind, "ready")
  const presentations = await source.readMachinePresentations?.(machineID, signal)
  assert.deepEqual(presentations?.[0]?.icon, icon)
})
