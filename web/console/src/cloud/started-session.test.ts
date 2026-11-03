import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- Node runs the source extension.
import { observeStartedSession } from "./started-session.ts"
import type { SessionDestination, SessionProjectionSource, MachineSessionProjection } from "./all-machine-sessions.js"

const destination = { machineID: "machine-b", sessionID: "%42", executionGeneration: "a".repeat(32) }
const empty: MachineSessionProjection = { kind: "ready", complete: true, observedAt: 1, rows: [] }
const arrived = { kind: "ready", complete: true, observedAt: 2, rows: [{ destination, freshness: "current" }] } as MachineSessionProjection
const tick = () => new Promise<void>((resolve) => setImmediate(resolve))

function fixture(read: SessionProjectionSource["readMachine"]) {
  let listener: Parameters<SessionProjectionSource["subscribe"]>[0] = () => {}
  let subscribed = false
  const source = { readMachine: read, subscribe(next: typeof listener) {
    listener = next; subscribed = true
    return () => { subscribed = false }
  } } as SessionProjectionSource
  return { source, emit: () => listener({ machineID: "machine-b", kind: "changed" }), subscribed: () => subscribed }
}

test("a confirmed launch requests a fresh list even while the pre-launch list is cached", async () => {
  const calls: unknown[] = []
  const f = fixture(async (machine, _signal, fresh) => {
    calls.push([machine, fresh])
    return fresh ? arrived : empty
  })
  const opened: SessionDestination[] = []
  observeStartedSession(f.source, "%42", () => ["machine-b"], () => true, (row) => opened.push(row))
  await tick()
  assert.deepEqual(opened, [destination])
  assert.deepEqual(calls, [["machine-b", true]])
  assert.equal(f.subscribed(), false)
})

test("a failed machine read does not hide a launch on the next machine", async () => {
  const f = fixture(async (machine) => {
    if (machine === "machine-a") throw Object.assign(new Error("unavailable"), { code: "cloud_read_timeout" })
    return arrived
  })
  const opened: SessionDestination[] = []
  observeStartedSession(f.source, "%42", () => ["machine-a", "machine-b"], () => true, (row) => opened.push(row))
  await tick()
  assert.deepEqual(opened, [destination])
})

test("a late arrival settles once even when the person no longer wants navigation", async () => {
  let reading: MachineSessionProjection = empty
  let released = 0
  const opened: SessionDestination[] = []
  const f = fixture(async () => reading)
  observeStartedSession(f.source, "%42", () => ["machine-b"], () => { released++; return false }, (row) => opened.push(row))
  await tick()
  reading = arrived
  f.emit()
  await tick()
  f.emit()
  await tick()
  assert.equal(released, 1)
  assert.deepEqual(opened, [])
  assert.equal(f.subscribed(), false)
})

test("cancelling an in-flight observation prevents it from opening a replaced start", async () => {
  let finish!: (value: MachineSessionProjection) => void
  let signal!: AbortSignal
  const f = fixture((_machine, current) => { signal = current; return new Promise((resolve) => { finish = resolve }) })
  let accepted = 0
  const stop = observeStartedSession(f.source, "%42", () => ["machine-b"], () => { accepted++; return true }, () => assert.fail("cancelled launch opened"))
  stop()
  finish(arrived)
  await tick()
  assert.equal(signal.aborted, true)
  assert.equal(accepted, 0)
  assert.equal(f.subscribed(), false)
})

test("events during a read are coalesced and checked after that read settles", async () => {
  let finish!: (value: MachineSessionProjection) => void
  let reads = 0
  let accepted = 0
  const f = fixture(async () => ++reads === 1 ? new Promise((resolve) => { finish = resolve }) : arrived)
  observeStartedSession(f.source, "%42", () => ["machine-b"], () => { accepted++; return true }, () => {})
  f.emit(); f.emit(); f.emit()
  finish(empty)
  await tick()
  assert.equal(reads, 2)
  assert.equal(accepted, 1)
})
