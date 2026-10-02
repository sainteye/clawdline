import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { CloudProjectReader, PROJECT_RETRY_MS, decodeCloudPlaceID, projectReadFailure, projectRetryDelay, resolveCloudTerminalProject } from "./cloud-project.ts"

// The copied client's spelling (cloud-client.js `cloudPlaceID`), written out here
// so the test checks the decoder against the encoder, not against itself.
const wrap = (machine: string, local: string) =>
  "cloud." + btoa(String.fromCharCode(...new TextEncoder().encode(JSON.stringify([machine, local]))))
    .replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "")

const here = "machine-alpha"
const there = "machine-beta"
const places = [
  { id: wrap(here, "p-local-1"), path: "/work/one", label: "One" },
  { id: wrap(here, "p-local-2"), path: "/work/shared", label: "Shared A" },
  { id: wrap(here, "p-local-3"), path: "/work/shared", label: "Shared B" },
  { id: wrap(there, "p-local-9"), path: "/work/nine", label: "Nine" },
  { id: wrap(there, "p-local-1"), path: "/work/one", label: "One elsewhere" },
]

test("no project in the address asks for one at once", () => {
  assert.deepEqual(resolveCloudTerminalProject("", places, here), { kind: "choose" })
  assert.deepEqual(resolveCloudTerminalProject("  ", [], here), { kind: "choose" })
})

test("a wrapped id of this machine gives the channel the machine-local id and keeps the wrapped one for the page", () => {
  assert.deepEqual(resolveCloudTerminalProject(places[0].id, places, here),
    { kind: "found", page: places[0].id, local: "p-local-1", label: "One" })
})

test("a wrapped id of another machine is unknown here", () => {
  assert.deepEqual(resolveCloudTerminalProject(places[3].id, places, here), { kind: "unknown" })
})

test("a wrapped id that is not in the list is unknown, even when it decodes to this machine", () => {
  assert.deepEqual(resolveCloudTerminalProject(wrap(here, "p-never-listed"), places, here), { kind: "unknown" })
})

test("a bare machine-local id is unknown on the hosted page", () => {
  assert.deepEqual(resolveCloudTerminalProject("p-local-1", places, here), { kind: "unknown" })
  // Even if a row's id were the bare spelling, it does not decode to a machine.
  assert.deepEqual(resolveCloudTerminalProject("p-local-1", [{ id: "p-local-1", path: "/x" }], here), { kind: "unknown" })
})

test("a folder resolves only when exactly one row of this machine has it", () => {
  assert.deepEqual(resolveCloudTerminalProject("/work/one", places, here),
    { kind: "found", page: places[0].id, local: "p-local-1", label: "One" })
  assert.deepEqual(resolveCloudTerminalProject("/work/shared", places, here), { kind: "unknown" })
  assert.deepEqual(resolveCloudTerminalProject("/work/nine", places, here), { kind: "unknown" })
})

test("the decoder takes only the wrapped pair", () => {
  assert.deepEqual(decodeCloudPlaceID(wrap("m-ü", "p/1+2")), ["m-ü", "p/1+2"])
  for (const bad of ["", "cloud.", "cloud.!!", "p-local-1", "cloud." + btoa("[1,2]").replace(/=+$/, ""),
    "cloud." + btoa('["only"]').replace(/=+$/, ""), "cloud." + btoa("not json").replace(/=+$/, "")]) {
    assert.equal(decodeCloudPlaceID(bad), null, bad)
  }
})

const refusal = (code: string) => Object.assign(new Error(code), { name: "RefusalError", code })
const transport = (cause: unknown) => Object.assign(new Error("GET /v1/places did not complete"), { name: "TransportError", cause })

test("a failed read says whether it is this page's connection or names the code", () => {
  assert.deepEqual(projectReadFailure(refusal("cloud_reconnecting")), { temporary: true, code: "cloud_reconnecting" })
  assert.deepEqual(projectReadFailure(refusal("offline")), { temporary: true, code: "offline" })
  assert.deepEqual(projectReadFailure(new TypeError("Failed to fetch")), { temporary: true, code: "network" })
  assert.deepEqual(projectReadFailure(transport(new TypeError("Failed to fetch"))), { temporary: true, code: "network" })
  assert.deepEqual(projectReadFailure(transport(new SyntaxError("x"))), { temporary: false, code: "transport" })
  assert.deepEqual(projectReadFailure(refusal("cloud_read_only")), { temporary: false, code: "cloud_read_only" })
})

test("the retry schedule is 1, 2, 4, 8, 16, 30 seconds and then stops", () => {
  assert.deepEqual([0, 1, 2, 3, 4, 5, 6].map(projectRetryDelay), [1_000, 2_000, 4_000, 8_000, 16_000, 30_000, null])
  assert.equal(PROJECT_RETRY_MS.length, 6)
})

class Clock {
  waits: { ms: number; run: () => void; cancelled: boolean }[] = []
  after(ms: number, run: () => void): () => void {
    const wait = { ms, run, cancelled: false }
    this.waits.push(wait)
    return () => { wait.cancelled = true }
  }
  pending() { return this.waits.filter((w) => !w.cancelled) }
  fire() { const w = this.pending().at(-1)!; w.cancelled = true; w.run() }
}
const settle = () => new Promise((resolve) => setImmediate(resolve))

test("the reader retries on the schedule, stops after the bound, and Try again starts over", async () => {
  const clock = new Clock()
  let reads = 0
  const states: unknown[] = []
  const reader = new CloudProjectReader(async () => { reads++; throw refusal("cloud_reconnecting") }, (s: unknown) => states.push(s), clock)
  reader.start()
  await settle()
  for (let i = 0; i < PROJECT_RETRY_MS.length; i++) {
    assert.equal(clock.pending().length, 1)
    assert.equal(clock.pending()[0].ms, PROJECT_RETRY_MS[i])
    clock.fire()
    await settle()
  }
  assert.equal(reads, 1 + PROJECT_RETRY_MS.length)
  assert.equal(clock.pending().length, 0, "nothing is scheduled after the bound")
  assert.deepEqual(reader.state, { state: "failed", temporary: true, code: "cloud_reconnecting", retryInMs: null })
  reader.retry()
  await settle()
  assert.equal(reads, 2 + PROJECT_RETRY_MS.length)
  assert.equal(clock.pending()[0].ms, 1_000, "Try again starts a fresh schedule")
  reader.dispose()
  assert.equal(clock.pending().length, 0, "dispose cancels the waiting retry")
})

test("the terminal host coming back re-reads at once, without waiting for the retry", async () => {
  const clock = new Clock()
  let fail = true
  let reads = 0
  const reader = new CloudProjectReader(async () => { reads++; if (fail) throw new TypeError("Failed to fetch"); return { places: [] } }, () => {}, clock)
  const first = {}
  reader.host(first)
  reader.start()
  await settle()
  assert.equal(reader.state.state, "failed")
  reader.host(null)
  assert.equal(reads, 1, "losing the host does not read")
  fail = false
  reader.host({})
  await settle()
  assert.equal(reads, 2, "a returning host reads at once")
  assert.equal(reader.state.state, "ready")
  assert.equal(clock.pending().length, 0, "the waiting retry was cancelled")
  // A re-attached client that replaces the host directly also counts, but not once the list is in hand.
  reader.host({})
  await settle()
  assert.equal(reads, 2)
  reader.dispose()
})

test("a re-attached client replacing the host re-reads a failed list", async () => {
  const clock = new Clock()
  let reads = 0
  const reader = new CloudProjectReader(async () => { reads++; throw refusal("cloud_reconnecting") }, () => {}, clock)
  reader.host({})
  reader.start()
  await settle()
  reader.host({})
  await settle()
  assert.equal(reads, 2)
  reader.dispose()
})

test("an answer that arrives after dispose changes nothing", async () => {
  let answer: (v: unknown) => void = () => {}
  const states: unknown[] = []
  const reader = new CloudProjectReader(() => new Promise((resolve) => { answer = resolve }), (s: unknown) => states.push(s), new Clock())
  reader.start()
  reader.dispose()
  answer({ places: [] })
  await settle()
  assert.deepEqual(states, [{ state: "loading" }])
})
