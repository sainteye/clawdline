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

test("a confirmed launch rechecks after an early empty read even when no event arrives", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout", "Date"] })
  let reading: MachineSessionProjection = empty
  const calls: unknown[] = []
  const f = fixture(async (_machine, _signal, fresh) => { calls.push(fresh); return reading })
  const opened: SessionDestination[] = []
  const stop = observeStartedSession(f.source, "%42", () => ["machine-b"], () => true, (row) => opened.push(row))
  await tick()
  reading = arrived
  t.mock.timers.tick(5000)
  await tick()
  assert.deepEqual(opened, [destination])
  assert.deepEqual(calls, [true, true])
  stop()
  t.mock.timers.tick(60000)
  await tick()
  assert.equal(calls.length, 2)
})

test("one silent machine cannot hold a confirmed launch on another machine", async () => {
  const f = fixture(async (machine, signal) => machine === "machine-a"
    ? new Promise<MachineSessionProjection>((resolve) => signal.addEventListener("abort", () => resolve(empty), { once: true }))
    : arrived)
  const opened: SessionDestination[] = []
  const stop = observeStartedSession(f.source, "%42", () => ["machine-a", "machine-b"], () => true, (row) => opened.push(row))
  await tick()
  assert.deepEqual(opened, [destination])
  stop()
})

test("all machines share a finite read budget and a deadline cancels a silent peer", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout", "Date"] })
  let reads = 0
  let reading: MachineSessionProjection = empty
  const f = fixture(async (machine, signal) => {
    if (machine === "machine-a") return new Promise<MachineSessionProjection>((resolve) =>
      signal.addEventListener("abort", () => resolve(empty), { once: true }))
    reads++
    return reading
  })
  let accepted = 0
  const stopped: string[] = []
  const stop = observeStartedSession(f.source, "%42", () => ["machine-a", "machine-b"], () => { accepted++; return true }, () => {}, (reason) => stopped.push(reason))
  await tick()
  for (let i = 0; i < 12; i++) { t.mock.timers.tick(5000); await tick() }
  assert.equal(reads, 11, "the silent peer consumes one of the twelve total reads")
  assert.deepEqual(stopped, ["time_limit"])
  assert.equal(f.subscribed(), false)
  t.mock.timers.tick(60000)
  await tick()
  assert.equal(reads, 11)
  reading = arrived
  f.emit()
  await tick()
  assert.equal(accepted, 0, "events cannot restart an expired observation")
  stop()
  t.mock.timers.tick(60000)
  await tick()
  assert.equal(reads, 11)
})

test("a rapid event storm exhausts at most twelve reads and manual retry gets a new finite observation", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout", "Date"] })
  let reads = 0
  let reading: MachineSessionProjection = empty
  const stopped: string[] = []
  const f = fixture(async () => { reads++; return reading })
  let accepted = 0
  const stop = observeStartedSession(f.source, "%42", () => ["machine-b"], () => true, () => accepted++, (reason) => stopped.push(reason))
  await tick()
  for (let i = 0; i < 100; i++) { f.emit(); await tick() }
  assert.equal(reads, 12)
  assert.deepEqual(stopped, ["read_limit"])
  assert.equal(f.subscribed(), false)
  t.mock.timers.tick(600000)
  await tick()
  assert.equal(reads, 12)
  reading = arrived
  const retry = observeStartedSession(f.source, "%42", () => ["machine-b"], () => true, () => accepted++)
  await tick()
  assert.equal(reads, 13)
  assert.equal(accepted, 1)
  stop(); retry()
})
