import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- node's type-stripping runner resolves the source .ts file.
import { pinnedQuestion, statusSource } from "./status-source.ts"

const machineID = "one"
const sessionID = "same"
const executionGeneration = "0123456789abcdef0123456789abcdef"
const snapshotGeneration = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const inventory = "__clawdline_inventory_v1__"

function clientFixture() {
  const calls: string[] = []
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
    statusSnapshots,
    detailSnapshots,
    openDetail() {},
    closeDetail() {},
    events() { return () => {} },
    async machines() { return { machines: [{ id: machineID, freshness: "current" }], syncing: false, retryAfterMs: 0 } },
    async recoverStatusRow(machine: string, session: string, generation: string) {
      calls.push("recover:" + [machine, session, generation].join(":"))
      setRow()
      return true
    },
    subscribe(channels: string[]) { calls.push("subscribe:" + channels.join(",")) },
    unsubscribe(channels: string[]) { calls.push("unsubscribe:" + channels.join(",")) },
    async infoForGeneration(destination: { executionGeneration: string }) {
      calls.push("info:" + destination.executionGeneration)
      return { info: { session: { title: "Pinned" } } }
    },
    async transcriptForGeneration(destination: { executionGeneration: string }) {
      calls.push("transcript:" + destination.executionGeneration)
      return { entries: [{ role: "assistant", text: "Done" }] }
    },
  }
  return { client, calls, row }
}

test("status-only list never subscribes to or reads rich content", async () => {
  const { client, calls } = clientFixture()
  const source = statusSource(() => client as never)
  const read = await source.readMachine(machineID, new AbortController().signal)
  assert.equal(read.kind, "ready")
  assert.deepEqual(calls, [])
})

test("opening and leaving an exact detail subscribes, reads pinned content, and releases both channels", async () => {
  const { client, calls } = clientFixture()
  const source = statusSource(() => client as never)
  const destination = { machineID, sessionID, executionGeneration }
  const detail = await source.readDetail(destination, new AbortController().signal)
  assert.equal(detail.kind, "ready")
  assert.equal(detail.kind === "ready" && detail.info.title, "Pinned")
  source.closeDetail?.(destination)
  assert.deepEqual(calls, ["subscribe:s/one/same,t/one/same", "info:" + executionGeneration,
    "transcript:" + executionGeneration, "unsubscribe:s/one/same,t/one/same"])
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

test("a missing ss row recovers only that retained status channel", async () => {
  const { client, calls } = clientFixture()
  client.statusSnapshots.delete(JSON.stringify([machineID, sessionID]))
  const source = statusSource(() => client as never)
  const gap = await source.readMachine(machineID, new AbortController().signal)
  assert.equal(gap.kind === "unavailable" && gap.reason, "event_gap")
  assert.deepEqual(calls, ["recover:" + [machineID, sessionID, snapshotGeneration].join(":")])
  const reading = await source.readMachine(machineID, new AbortController().signal)
  assert.equal(reading.kind, "ready")
})

test("a failed retained-row request keeps the machine in event_gap", async () => {
  const { client, calls } = clientFixture()
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
  assert.deepEqual(calls, ["recover-refused"])
  const marker = client.statusSnapshots.get(JSON.stringify([machineID, inventory])) as { payload: { snapshot_generation: string } }
  marker.payload.snapshot_generation = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
  await source.readMachine(machineID, new AbortController().signal)
  assert.deepEqual(calls, ["recover-refused", "recover-refused"])
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
