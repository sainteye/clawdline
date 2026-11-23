import { test } from "node:test"
import assert from "node:assert/strict"
import { StatusCloudClient } from "./status-client.js"
import { ViewerEventLog } from "../legacy/js/net/cloud-viewer-events.js"

// An account with two Macs. On 2026-10-18 every viewer-event batch went to the
// one whose id sorts first, while the phone was making direct attempts to the
// other, so the machine those attempts were about logged none of the phone's half.
const FIRST = "mac_c58e0000"
const SECOND = "mac_c6c20000"

function twoMacs({ verified = [FIRST, SECOND], page = null, refuse = null, storage = null } = {}) {
  const clock = { t: 1_700_000_000_000 }
  const client = Object.create(StatusCloudClient.prototype)
  client.viewerVerified = new Map(verified.map((machine) => [machine, { sender: "agent-" + machine }]))
  client._machineImplements = () => "yes"
  client._evidentMac = () => true
  client.classicSessionMachine = page
  client.viewerEvents = new ViewerEventLog({ now: () => clock.t, storage })
  client.ready = true
  client.tabID = "tab-1"
  client.deviceID = "web_device"
  client.webBuild = "web-1"
  client.orchestratorSnapshots = new Map()
  client.machineDescriptors = new Map()
  const sent = []
  client._machineRequestAs = async (request, machine, word, body) => {
    sent.push({ machine, word, batch: body.batch })
    if (refuse) throw refuse
    return { batch_id: body.batch.batch_id, rows: body.batch.rows.length }
  }
  // Each send must wait for the log's spacing; move the clock past it.
  const deliver = async () => {
    const outcome = await client._deliverViewerEvents()
    clock.t += 61_000
    return outcome
  }
  return { client, sent, deliver, clock }
}

const attempt = (log, machine) => log.record("cloud.direct.attempt", { machine, kind: "carrier", stage: "open" },
  "cloud.direct.attempt|" + machine + "|" + Math.random())
const readFailed = (log, machine) => log.record("cloud.read.failed", { machine, word: "sessions.list", code: "cloud_read_timeout" },
  "cloud.read.failed|" + machine + "|" + Math.random())

function consistent(batch) {
  const c = batch.completeness
  return batch.rows.length + c.dropped_overflow + c.dropped_unflushed === c.n_to - c.n_from + 1
}

test("a direct attempt's row goes to the Mac of that attempt, not the Mac whose id sorts first", async () => {
  const { client, sent, deliver } = twoMacs()
  attempt(client.viewerEvents, SECOND)
  attempt(client.viewerEvents, SECOND)
  const outcome = await deliver()
  assert.equal(outcome.state, "delivered")
  assert.deepEqual(sent.map((s) => s.machine), [SECOND])
  assert.equal(sent[0].batch.rows.length, 2)
})

test("rows about different Macs go out as one batch per Mac, in order, each batch whole", async () => {
  const { client, sent, deliver } = twoMacs({ page: SECOND })
  const log = client.viewerEvents
  attempt(log, SECOND)
  readFailed(log, SECOND)
  readFailed(log, FIRST)
  // A page-level row names no machine: it goes to the machine the page is showing.
  log.record("cloud.frame.failed", { frame_type: "error", code: "relay_closed" })
  for (let i = 0; i < 3; i += 1) assert.equal((await deliver()).state, "delivered")
  assert.equal((await deliver()).state, "empty")
  assert.deepEqual(sent.map((s) => [s.machine, s.batch.rows.map((row) => row.event)]), [
    [SECOND, ["cloud.direct.attempt", "cloud.read.failed"]],
    [FIRST, ["cloud.read.failed"]],
    [SECOND, ["cloud.frame.failed"]],
  ])
  const ranges = sent.map((s) => [s.batch.completeness.n_from, s.batch.completeness.n_to])
  assert.deepEqual(ranges, [[1, 2], [3, 3], [4, 4]])
  assert.ok(sent.every((s) => consistent(s.batch)))
  const routes = log.snapshot().deliveries.map((entry) => [entry.state, entry.machine, entry.route, entry.rows])
  assert.deepEqual(routes, [
    ["delivered", SECOND, "row_machine", 2],
    ["delivered", FIRST, "row_machine", 1],
    ["delivered", SECOND, "page_machine", 1],
  ])
})

test("a batch cut short by another Mac's row still counts the rows the bound dropped below it", async () => {
  const { client, sent, deliver } = twoMacs()
  client.viewerEvents.limits.rows = 4
  // n1..n6 alternate; the row bound drops n3 and n4 from the middle.
  for (const machine of [FIRST, SECOND, FIRST, SECOND, FIRST, SECOND]) readFailed(client.viewerEvents, machine)
  while ((await deliver()).state === "delivered") { /* drain */ }
  assert.deepEqual(sent.map((s) => [s.machine, s.batch.completeness.n_from, s.batch.completeness.n_to,
    s.batch.completeness.dropped_overflow]), [
    [FIRST, 1, 1, 0], [SECOND, 2, 4, 2], [FIRST, 5, 5, 0], [SECOND, 6, 6, 0],
  ])
  assert.ok(sent.every((s) => consistent(s.batch)))
})

test("a batch with nowhere to go says why, once per change, in the page's own record", async () => {
  const { client, sent, deliver } = twoMacs({ verified: [] })
  attempt(client.viewerEvents, SECOND)
  assert.equal((await deliver()).why, "cloud_machine_unavailable")
  assert.equal((await deliver()).why, "cloud_machine_unavailable")
  assert.deepEqual(sent, [])
  const [entry, ...rest] = client.viewerEvents.snapshot().deliveries
  assert.deepEqual(rest, [])
  assert.equal(entry.state, "deferred")
  assert.equal(entry.why, "cloud_machine_unavailable")
  assert.equal(entry.times, 2)
})

test("a refused Mac's hold names that Mac, and a sealed batch is resent to the Mac it was sealed for", async () => {
  const refusal = Object.assign(new Error("old"), { code: "unknown_command", layer: "mac", status: 400 })
  const { client, sent, deliver } = twoMacs({ refuse: refusal })
  attempt(client.viewerEvents, SECOND)
  const first = await deliver()
  assert.equal(first.state, "blocked")
  assert.equal(first.machine, SECOND)
  // A row about the other Mac arrives meanwhile; the sealed batch still goes nowhere else.
  readFailed(client.viewerEvents, FIRST)
  const second = await deliver()
  assert.equal(second.state, "blocked")
  assert.equal(second.machine, SECOND)
  assert.equal(second.route, "sealed")
  assert.deepEqual(sent.map((s) => s.machine), [SECOND])
  assert.deepEqual(client.viewerEvents.snapshot().deliveries.map((entry) => [entry.state, entry.machine, entry.code]), [
    ["blocked", SECOND, "unknown_command"],
    ["blocked", SECOND, "unknown_command"],
  ])
})

// A store holding what an earlier page of this device left: its stored state and its journal.
function storeWith(state, journal) {
  const items = new Map([["clawdline.cloud.viewer-events.v1", JSON.stringify(state)],
    ["clawdline.cloud.viewer-events.v1:journal", JSON.stringify(journal)]])
  return {
    getItem: (name) => (items.has(name) ? items.get(name) : null),
    setItem: (name, value) => { items.set(name, String(value)) },
    removeItem: (name) => { items.delete(name) },
  }
}

const storedRow = (n, machine) => ({ n, at_ms: 1_699_999_000_000, event: "cloud.read.failed", tab: "tab-0",
  data: { machine, word: "sessions.list", code: "cloud_read_timeout" } })

function storedState({ rows, nextN, overflow }) {
  return {
    v: 1, next_n: nextN, cursor: 1, counting_since_ms: 1_699_999_000_000, rows, rate_limited_total: 0,
    counts: { rate_limited: 0, overflow, storage_errors: 0, refused: 0, unflushed: 0 },
    rate: {}, outbox: null, sends: [], blocked: null, target: null, acknowledged: null,
    refusals: 0, deliveries: [],
  }
}

// Each batch says which of its missing numbers the row bound cut (overflow) and which a killed
// page never wrote (unflushed). A batch that ends at a row bound for another Mac must count the
// gaps below that row by what they are, whichever comes first, not just in the right total.
for (const tc of [
  {
    name: "an overflow gap below the cut and the unflushed gap above it",
    // n2 was cut by the bound before the page was killed; n4 and n5 were never written.
    stored: storedState({ rows: [storedRow(1, FIRST), storedRow(3, SECOND)], nextN: 4, overflow: 1 }),
    journal: { n: 6, rl: 0 },
    then: [FIRST],
    want: [[FIRST, 1, 2, 1, 1, 0], [SECOND, 3, 5, 1, 0, 2], [FIRST, 6, 6, 1, 0, 0]],
  },
  {
    name: "the unflushed gap below the cut and an overflow gap above it",
    // n2 and n3 were never written; then the bound of four rows cuts n5 from the middle.
    rows: 4,
    stored: storedState({ rows: [storedRow(1, FIRST)], nextN: 2, overflow: 0 }),
    journal: { n: 4, rl: 0 },
    then: [SECOND, SECOND, SECOND, FIRST],
    want: [[FIRST, 1, 3, 1, 0, 2], [SECOND, 4, 6, 2, 1, 0], [FIRST, 7, 7, 1, 0, 0]],
  },
]) {
  test(`a batch cut at another Mac's row counts each gap below it as what it is: ${tc.name}`, async () => {
    const { client, sent, deliver } = twoMacs({ storage: storeWith(tc.stored, tc.journal) })
    if (tc.rows) client.viewerEvents.limits.rows = tc.rows
    for (const machine of tc.then) readFailed(client.viewerEvents, machine)
    while ((await deliver()).state === "delivered") { /* drain */ }
    // [machine, n_from, n_to, rows, dropped_overflow, dropped_unflushed]
    assert.deepEqual(sent.map(({ machine, batch: { completeness: c } }) =>
      [machine, c.n_from, c.n_to, c.rows, c.dropped_overflow, c.dropped_unflushed]), tc.want)
  })
}

test("the log keeps the newest sixteen delivery outcomes and lets the older ones go", () => {
  const log = new ViewerEventLog({ now: () => 1_700_000_000_000 })
  for (let i = 1; i <= 20; i += 1) {
    log.noteDelivery({ state: "delivered", machine: FIRST, route: "row_machine", batch_id: "batch-" + i, rows: 1 })
  }
  const kept = log.snapshot().deliveries.map((entry) => entry.batch_id)
  assert.deepEqual(kept, Array.from({ length: 16 }, (_, i) => "batch-" + (i + 5)))
})


test("a deliberate diagnostic reaches its named machine despite an exhausted background budget and another tab's writer lock", async () => {
  const { client, sent } = twoMacs()
  const log = client.viewerEvents
  for (let i = 0; i < log.limits.dailySends; i++) log.noteAttempt()
  log.writer = false
  await client.sendDiagnostic(SECOND, { code: "unexpected_error", connection: "ready" })
  assert.equal(sent.length, 1)
  assert.equal(sent[0].machine, SECOND)
  assert.equal(sent[0].word, "diagnostics.events")
  assert.equal(sent[0].batch.rows[0].event, "cloud.send.diagnostic_requested")
  assert.equal(sent[0].batch.rows[0].data.code, "unexpected_error")
  assert.ok(consistent(sent[0].batch))
})

test("a deliberate diagnostic does not call an unready or unsupported machine and never accepts the wrong receipt", async () => {
  const { client, sent } = twoMacs()
  client.ready = false
  await assert.rejects(client.sendDiagnostic(SECOND, { code: "unexpected_error" }), { code: "cloud_reconnecting" })
  client.ready = true
  client._machineImplements = () => "no"
  await assert.rejects(client.sendDiagnostic(SECOND, {}), { code: "cloud_feature_unavailable" })
  assert.equal(sent.length, 0)
  client._machineImplements = () => "yes"
  client._machineRequestAs = async () => ({ batch_id: "wrong", rows: 1 })
  await assert.rejects(client.sendDiagnostic(SECOND, {}), { code: "bad_payload" })
  client._machineRequestAs = async () => { throw Object.assign(new Error("lost"), { code: "cloud_read_timeout" }) }
  await assert.rejects(client.sendDiagnostic(SECOND, {}), { code: "cloud_read_timeout" })
})


test("a displayed outage is persisted through an exhausted background window and sent after reconnect", async () => {
  const items = new Map()
  const storage = {
    getItem: key => items.get(key) ?? null,
    setItem: (key, value) => { items.set(key, String(value)) },
    removeItem: key => { items.delete(key) },
  }
  const {client} = twoMacs({storage})
  client.ready = false
  const log = client.viewerEvents
  for (let index = 0; index < log.limits.allInWindow; index++) readFailed(log, FIRST)
  const event = "cloud.bots.roster.unavailable"
  const data = {observed_at_ms: log.now(), source_count: 2, sources: [FIRST + "|pa_offline", SECOND + "|pa_unsupported"], web_build: "shown-build"}
  assert.ok(log.recordImmediately(event, data).n)
  assert.equal((await client._deliverViewerEvents()).why, "not_ready")

  // Do not close or flush the old log. The new page must see the row already in storage.
  const reopened = twoMacs({storage})
  assert.equal(reopened.client.viewerEvents.rows().length, log.limits.allInWindow)
  assert.equal((await reopened.deliver()).state, "delivered")
  assert.equal(reopened.sent.length, 1)
  const displayed = reopened.sent[0].batch.rows.filter(row => row.event === event)
  assert.equal(displayed.length, 1)
  assert.deepEqual(displayed[0].data, data)
  assert.equal(reopened.client.viewerEvents.pending(), false)
})
