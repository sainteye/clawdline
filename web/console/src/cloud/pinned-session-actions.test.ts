import assert from "node:assert/strict"
import { test } from "node:test"
// @ts-expect-error -- Node's type-stripping runner loads this source in the focused test.
import { ACTION_STATUS_FRESH_MS, PinnedSessionActions, receiptPath, receiptStages, watchActionAvailability } from "./pinned-session-actions.ts"
import type { ActionContext, ActionInput, ActionProjection, PinnedClient } from "./pinned-session-actions.js"

const generationA = "0123456789abcdef0123456789abcdef"
const generationB = "fedcba9876543210fedcba9876543210"
const target = { machineID: "machine-a", sessionID: "same-session", executionGeneration: generationA }
const context: ActionContext = { destination: target, machine: { id: "machine-a", name: "Office", freshness: "current" },
  row: { destination: target, freshness: "current" }, content: { kind: "ready" } }

function fixture() {
  const values = new Map<string, string>()
  const calls: { identity: { machine: string; session: string }; type: string; body: Record<string, unknown> }[] = []
  const projections = new Map<string, ActionProjection>([["machine-a", { kind: "ready", rows: [context.row] }],
    ["machine-b", { kind: "ready", rows: [{ destination: { ...target, machineID: "machine-b" }, freshness: "current" }] }]])
  let count = 0
  let receipt: object = { request: "request-1", action: "send", execution_generation: generationA,
    machine_execution: "completed", status: 200, code: "", relay_accepted: "unknown", relay_delivered: "unknown",
    viewer_observed: "unknown", viewer_acknowledged: "unknown" }
  let mutationError: string | null = null
  const client: PinnedClient = {
    deviceID: "viewer-1", allowWrites: true, viewerVerified: new Map([["machine-a", true], ["machine-b", true]]),
    async machines() { return { machines: [{ id: "machine-a", freshness: "current" as const, pairing: "paired" },
      { id: "machine-b", freshness: "current" as const, pairing: "paired" }] } },
    machineDescriptor() { return { machine: { commands: ["send", "answer", "interrupt", "end", "session-receipt"] } } },
    async _read(identity, type, body) {
      calls.push({ identity, type, body })
      if (type === "session-receipt") return receipt
      if (mutationError) throw Object.assign(new Error(mutationError), { code: mutationError })
      return { ok: true }
    },
  }
  const store = { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => { values.set(key, value) } }
  const service = new PinnedSessionActions({ async readMachine(machine) { return projections.get(machine) ?? { kind: "unavailable", reason: "unknown" } } },
    () => client, store, () => "request-" + ++count)
  return { client, service, calls, projections, values, setReceipt(value: object) { receipt = value },
    setMutationError(value: string | null) { mutationError = value } }
}

test("two machines with the same Session ID keep independent destinations and receipts", async () => {
  const f = fixture()
  const a = await f.service.perform(context, "send", { text: "hello" })
  const bContext: ActionContext = { ...context, destination: { ...target, machineID: "machine-b" },
    machine: { id: "machine-b", name: "Home", freshness: "current" },
    row: { destination: { ...target, machineID: "machine-b" }, freshness: "current" } }
  f.setReceipt({ ...a.receipt, request: "request-3" })
  const b = await f.service.perform(bContext, "send", { text: "hello" })
  assert.notEqual(a.request, b.request)
  assert.deepEqual(f.calls.filter((call) => call.type === "send").map((call) => call.identity.machine), ["machine-a", "machine-b"])
  assert.equal(f.service.load(context, "send")?.destination.machineID, "machine-a")
  assert.equal(f.service.load(bContext, "send")?.destination.machineID, "machine-b")
})

test("a new execution generation, stale, offline and unknown projections stop the original target", async () => {
  const f = fixture()
  f.projections.set("machine-a", { kind: "ready", rows: [{ destination: { ...target, executionGeneration: generationB }, freshness: "current" }] })
  assert.equal(await f.service.availability(context, "send"), "changed")
  f.projections.set("machine-a", { kind: "ready", rows: [{ destination: target, freshness: "stale" }] })
  assert.equal(await f.service.availability(context, "send"), "stale")
  f.projections.set("machine-a", { kind: "unavailable", reason: "offline" })
  assert.equal(await f.service.availability(context, "send"), "offline")
  f.projections.set("machine-a", { kind: "unavailable", reason: "event_gap" })
  assert.equal(await f.service.availability(context, "send"), "unknown")
  assert.equal(f.calls.length, 0)
})

test("buttons become stale when signed status expires without any source event", async () => {
  const f = fixture()
  const observedAt = Date.now() - ACTION_STATUS_FRESH_MS + 500
  const seen: unknown[] = []
  let reads = 0
  let events = 0
  const source = {
    async readMachine() {
      ++reads
      return Date.now() - observedAt > ACTION_STATUS_FRESH_MS
        ? { kind: "unavailable" as const, reason: "stale" }
        : { kind: "ready" as const, observedAt,
          rows: [{ destination: target, freshness: "current" as const, observedAt }] }
    },
    subscribe() { ++events; return () => {} },
  }
  const storage = { getItem: () => null, setItem: () => {} }
  const service = new PinnedSessionActions(source, () => f.client, storage, () => "request")
  let stop = () => {}
  const stale = new Promise<void>((resolve) => {
    stop = watchActionAvailability(context, source, service, (value) => {
      if (value.send !== undefined) seen.push(value.send)
      if (value.send === "stale") resolve()
    })
  })
  let timeout: ReturnType<typeof setTimeout> | undefined
  try {
    await Promise.race([stale, new Promise((_, reject) => { timeout = setTimeout(() => reject(new Error("freshness timer did not fire")), 2_000) })])
    assert.deepEqual(seen, [null, "stale"])
    assert.equal(events, 1, "the transition needs no source event")
    assert.ok(reads >= 2, "the timer reads fresh status again")
  } finally { stop(); if (timeout !== undefined) clearTimeout(timeout) }
})

test("machine inventory expiry disables an otherwise fresh pinned action without an event", async () => {
  const f = fixture()
  const observedAt = Date.now() - ACTION_STATUS_FRESH_MS + 500
  f.client.machines = async () => ({ machines: [{ id: "machine-a", observedAt,
    freshness: Date.now() - observedAt > ACTION_STATUS_FRESH_MS ? "stale" as const : "current" as const,
    pairing: "paired" }] })
  const source = { async readMachine() { const now = Date.now(); return { kind: "ready" as const, observedAt: now,
    rows: [{ destination: target, freshness: "current" as const, observedAt: now }] } } }
  const storage = { getItem: () => null, setItem: () => {} }
  const service = new PinnedSessionActions(source, () => f.client, storage, () => "request")
  const seen: unknown[] = []
  let stop = () => {}
  const stale = new Promise<void>((resolve) => {
    stop = watchActionAvailability(context, source, service, (value) => {
      if (value.send !== undefined) seen.push(value.send)
      if (value.send === "stale") resolve()
    })
  })
  let timeout: ReturnType<typeof setTimeout> | undefined
  try {
    await Promise.race([stale, new Promise((_, reject) => { timeout = setTimeout(() => reject(new Error("machine timer did not fire")), 2_000) })])
    assert.deepEqual(seen, [null, "stale"])
  } finally { stop(); if (timeout !== undefined) clearTimeout(timeout) }
})

test("capability, content and pairing revocation refuse a write before its envelope", async () => {
  const f = fixture()
  f.client.machineDescriptor = () => ({ machine: { commands: ["send"] } })
  assert.equal(await f.service.availability(context, "send"), "unsupported")
  f.client.machineDescriptor = () => ({ machine: { commands: ["send", "session-receipt"] } })
  assert.equal(await f.service.availability({ ...context, content: { kind: "unavailable", reason: "no_permission" } }, "send"), null)
  f.client.machineDescriptor = () => ({ machine: { commands: ["send", "answer", "interrupt", "end", "session-receipt"] } })
  assert.equal(await f.service.availability({ ...context, content: { kind: "unavailable", reason: "no_permission" } }, "answer"), "content_permission")
  f.client.viewerVerified = new Map()
  assert.equal(await f.service.availability(context, "send"), "revoked")
  assert.equal(f.calls.length, 0)
})

test("missing transcript permission only disables answers; close blockers only disable close", async () => {
  const f = fixture()
  const noTranscript: ActionContext = { ...context, content: { kind: "unavailable", reason: "no_permission" } }
  for (const action of ["send", "interrupt", "end"] as const) assert.equal(await f.service.availability(noTranscript, action), null)
  assert.equal(await f.service.availability(noTranscript, "answer"), "content_permission")
  f.projections.set("machine-a", { kind: "ready", rows: [{ destination: target, freshness: "current", closeBlocked: true }] })
  assert.equal(await f.service.availability(noTranscript, "send"), null)
  assert.equal(await f.service.availability(noTranscript, "interrupt"), null)
  assert.equal(await f.service.availability(noTranscript, "end"), "closeability_blocked")
})

test("one intent keeps its key; changed parameters conflict and uncertainty only queries the original receipt", async () => {
  const f = fixture()
  f.setMutationError("receipt_outcome_unknown")
  f.setReceipt({ request: "request-1", action: "send", execution_generation: generationA,
    machine_execution: "unknown", status: 0, code: "", relay_accepted: "unknown", relay_delivered: "unknown",
    viewer_observed: "unknown", viewer_acknowledged: "unknown" })
  const first = await f.service.perform(context, "send", { text: "hello" })
  assert.equal(first.request, "request-1")
  assert.equal(first.receipt?.machine_execution, "unknown")
  assert.deepEqual(receiptStages(first), ["unknown", "unknown", "unknown", "observed", "unknown"])
  const next = await f.service.perform(context, "send", { text: "hello" })
  assert.equal(next.request, first.request)
  assert.equal(f.calls.filter((call) => call.type === "send").length, 1)
  await assert.rejects(f.service.perform(context, "send", { text: "different" } as ActionInput), { code: "idempotency_key_reused" })
  assert.equal(f.calls.filter((call) => call.type === "send").length, 1)
  assert.equal(f.calls.filter((call) => call.type === "session-receipt")[0].body.target_request, first.request)
  assert.equal(receiptPath(first), `/v1/sessions/same-session/cloud-receipts/request-1?action=send&execution_generation=${generationA}`)
})

test("pending and outcome unknown retain a lookup pointer after reload; only the initiating viewer can query", async () => {
  const f = fixture()
  f.setMutationError("receipt_pending")
  f.setReceipt({ request: "request-1", action: "interrupt", execution_generation: generationA,
    machine_execution: "pending", status: 0, code: "", relay_accepted: "unknown", relay_delivered: "unknown",
    viewer_observed: "unknown", viewer_acknowledged: "unknown" })
  const record = await f.service.perform(context, "interrupt", {})
  assert.equal(record.receipt?.machine_execution, "pending")
  assert.equal(f.service.load(context, "interrupt")?.request, record.request)
  assert.equal(f.service.load(context, "interrupt")?.receipt, null, "reloaded local data is only a lookup pointer")
  f.client.deviceID = "viewer-2"
  const blocked = await f.service.lookup(record)
  assert.equal(blocked.problem, "revoked")
  assert.equal(f.calls.filter((call) => call.type === "session-receipt").length, 1)
  f.client.deviceID = "viewer-1"
  f.client.machines = async () => ({ machines: [{ id: "machine-a", freshness: "current", pairing: "not_paired" }] })
  assert.equal((await f.service.lookup(record)).problem, "revoked")
  assert.equal(f.calls.filter((call) => call.type === "session-receipt").length, 1)
})

test("a missing receipt remains unresolved and cannot release the original request key", async () => {
  const f = fixture()
  f.setMutationError("receipt_outcome_unknown")
  f.setReceipt({ request: "request-1", action: "send", execution_generation: generationA,
    machine_execution: "missing", status: 0, code: "", relay_accepted: "unknown", relay_delivered: "unknown",
    viewer_observed: "unknown", viewer_acknowledged: "unknown" })
  const record = await f.service.perform(context, "send", { text: "hello" })
  assert.equal(record.receipt?.machine_execution, "missing")
  assert.throws(() => f.service.acknowledge(record), { code: "receipt_outcome_unknown" })
  const retried = await f.service.perform(context, "send", { text: "hello" })
  assert.equal(retried.request, record.request)
  assert.equal(f.calls.filter((call) => call.type === "send").length, 1)
})

test("high-risk actions carry pinned target and their result can be acknowledged", async () => {
  const f = fixture()
  f.setReceipt({ request: "request-1", action: "end", execution_generation: generationA,
    machine_execution: "completed", status: 200, code: "", relay_accepted: "unknown", relay_delivered: "unknown",
    viewer_observed: "unknown", viewer_acknowledged: "unknown" })
  const result = await f.service.perform(context, "end", {})
  assert.deepEqual(f.calls[0].identity, { machine: "machine-a", session: "same-session" })
  assert.equal(f.calls[0].body.execution_generation, generationA)
  assert.equal(f.service.acknowledge(result).acknowledged, true)
  assert.equal(f.service.load(context, "end")?.acknowledged, true)
})

test("answers use a verified displayed option and its authoritative question fingerprint", async () => {
  const f = fixture()
  assert.equal(await f.service.availability(context, "answer"), "menu_unverified")
  const question = { fingerprint: "a".repeat(64), options: [{ key: "2", label: "Allow this request" }], observedAt: Date.now() }
  const withQuestion: ActionContext = { ...context, content: { kind: "ready", question } }
  f.projections.set("machine-a", { kind: "ready", rows: [{ destination: target, freshness: "current", observedAt: question.observedAt - 100 }] })
  assert.equal(await f.service.availability(withQuestion, "answer"), null)
  await assert.rejects(f.service.perform(withQuestion, "answer", { answer: "1", expect: question.fingerprint }), { code: "menu_unverified" })
  await assert.rejects(f.service.perform(withQuestion, "answer", { answer: "2", expect: "b".repeat(64) }), { code: "menu_unverified" })
  f.setReceipt({ request: "request-1", action: "answer", execution_generation: generationA,
    machine_execution: "completed", status: 200, code: "", relay_accepted: "unknown", relay_delivered: "unknown",
    viewer_observed: "unknown", viewer_acknowledged: "unknown" })
  await f.service.perform(withQuestion, "answer", { answer: "2", expect: question.fingerprint })
  assert.deepEqual(f.calls[0].body, { request: "request-1", execution_generation: generationA,
    answer: "2", expect: question.fingerprint })
  f.projections.set("machine-a", { kind: "ready", rows: [{ destination: target, freshness: "current", observedAt: question.observedAt + 100 }] })
  assert.equal(await f.service.availability(withQuestion, "answer"), "menu_unverified")
})
