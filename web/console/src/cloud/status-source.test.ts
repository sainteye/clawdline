import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- node's type-stripping runner resolves the source .ts file.
import { pinnedQuestion, statusSource, STATUS_GAP_RETRY_MS, STATUS_MARKER_RETRY_MS } from "./status-source.ts"

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
    async listPresentationsForMachine(machine: string) {
      calls.push("sessions.list:" + machine)
      return { at, complete: true, sessions: [{ id: sessionID, execution_generation: executionGeneration, title: "The real title",
        status: { state: "idle", work_state: "ready", work_note: "Queued heavy command finished", work_provenance: "self" } }] }
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

test("a new browser replays a missing status marker once and recovers the Session list", async () => {
  const { client } = clientFixture()
  client.readContentCapabilities.delete(machineID)
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
  client.statusSnapshots.delete(JSON.stringify([machineID, inventory]))
  let reads = 0
  ;(client as typeof client & { recoverStatusMarker: (machine: string) => Promise<boolean> }).recoverStatusMarker = async () => {
    reads++
    return false
  }
  const source = statusSource(() => client as never)
  const first = await source.readMachine(machineID, new AbortController().signal)
  assert.equal(first.kind, "unavailable")
  if (first.kind === "unavailable") assert.ok(first.retryAt! > Date.now() + STATUS_MARKER_RETRY_MS - 1000)
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

test("a cold status gap uses one complete pinned machine list for rows and titles", async () => {
  const { client, calls } = clientFixture()
  client.statusSnapshots.delete(JSON.stringify([machineID, sessionID]))
  const source = statusSource(() => client as never)
  const reading = await source.readMachine(machineID, new AbortController().signal)
  assert.equal(reading.kind, "ready")
  assert.equal(reading.rows?.[0]?.destination.executionGeneration, executionGeneration)
  const titles = await source.readMachinePresentations?.(machineID, new AbortController().signal)
  assert.deepEqual(titles?.map((row) => row.title), ["The real title"])
  assert.deepEqual(calls, ["sessions.list:" + machineID])
})

test("an incomplete machine scan fills a gap only with the signed inventory's exact Session set", async () => {
  const { client, calls } = clientFixture()
  client.statusSnapshots.delete(JSON.stringify([machineID, sessionID]))
  client.listPresentationsForMachine = async (machine: string) => {
    calls.push("sessions.list:" + machine)
    return { at: Math.floor(Date.now() / 1000), complete: false,
      sessions: [{ id: sessionID, execution_generation: executionGeneration, title: "The real title",
        status: { state: "idle", work_state: "ready", work_note: "", work_provenance: "self" } }] }
  }
  const source = statusSource(() => client as never)
  assert.equal((await source.readMachine(machineID, new AbortController().signal)).kind, "ready")
  assert.deepEqual(calls, ["sessions.list:" + machineID])

  const other = clientFixture()
  other.client.statusSnapshots.delete(JSON.stringify([machineID, sessionID]))
  other.client.listPresentationsForMachine = async () => ({ at: Math.floor(Date.now() / 1000), complete: false,
    sessions: [{ id: "different", execution_generation: executionGeneration, title: "Wrong set",
      status: { state: "idle", work_state: "ready", work_note: "", work_provenance: "self" } }] })
  other.client.recoverStatusRow = async () => false
  assert.equal((await statusSource(() => other.client as never).readMachine(machineID, new AbortController().signal)).kind,
    "unavailable")
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
  assert.equal(rows?.[0]?.status?.work_note, "Queued heavy command finished")
  assert.deepEqual(calls, ["sessions.list:" + machineID])
  row.execution_generation = "ffffffffffffffffffffffffffffffff"
  assert.deepEqual(await source.readMachinePresentations?.(machineID, new AbortController().signal), [])
  assert.deepEqual(calls, ["sessions.list:" + machineID, "sessions.list:" + machineID])
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
  const source = statusSource(() => client as never)
  const destination = { machineID, sessionID, executionGeneration }
  const detail = await source.readDetail(destination, new AbortController().signal)
  assert.equal(detail.kind, "ready")
  assert.equal(detail.kind === "ready" && detail.info.title, "Pinned")
  assert.equal(detail.kind === "ready" && detail.nextBefore, 123)
  source.closeDetail?.(destination)
  assert.deepEqual(calls, ["subscribe:s/one/same,t/one/same", "info:" + executionGeneration,
    "transcript:" + executionGeneration, "unsubscribe:s/one/same,t/one/same"])
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

test("an r/ capability blocks only detail, and an unread one is not called an old machine", async () => {
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
  // Published but no longer fresh, and never published at all, are the same
  // thing: this browser does not know yet. Calling either one an old machine
  // sent a person to update a machine that was current, which is what they
  // could neither understand nor act on.
  descriptor.supported = true
  descriptor.at -= 301
  assert.deepEqual(await source.readDetail(destination, new AbortController().signal),
    { kind: "unavailable", reason: "unconfirmed" })
  client.readContentCapabilities.delete(machineID)
  assert.deepEqual(await source.readDetail(destination, new AbortController().signal),
    { kind: "unavailable", reason: "unconfirmed" })
  // Neither answer may reach for content on the wire.
  assert.deepEqual(calls, [])
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
