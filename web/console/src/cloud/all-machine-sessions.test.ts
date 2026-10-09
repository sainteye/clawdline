import assert from "node:assert/strict"
import test from "node:test"
import {
  afterEventGap, checkedProjection, destinationAvailable, destinationFragment, destinationFromFragment,
  displayedPresentationStatus,
  destinationKey, prependOlderPage, projectionRefreshAt, settleProjection, SessionDetailCache,
  type MachineSessionProjection, type ProjectedSession,
// @ts-expect-error -- node's type-stripping runner resolves the source .ts file.
} from "./all-machine-sessions.ts"

const row = (machineID: string, sessionID: string, executionGeneration: string): ProjectedSession => ({
  destination: { machineID, sessionID, executionGeneration: executionGeneration.padStart(32, "0") }, title: "Example", state: "working",
  freshness: "current", needsAttention: true, observedAt: 1000,
})
const ready = (machine: string, rows: ProjectedSession[]): MachineSessionProjection =>
  checkedProjection(machine, { kind: "ready", complete: true, observedAt: 1000, rows })

test("same Session id on two machines has distinct URLs, keys and destinations after reload", () => {
  const a = row("machine/a", "%14", "7")
  const b = row("machine/b", "%14", "7")
  assert.notEqual(destinationKey(a.destination), destinationKey(b.destination))
  assert.notEqual(destinationFragment(a.destination), destinationFragment(b.destination))
  assert.deepEqual(destinationFromFragment(destinationFragment(a.destination)), a.destination)
  assert.deepEqual(destinationFromFragment(destinationFragment(b.destination)), b.destination)
  assert.equal(destinationAvailable(a.destination, ready("machine/b", [b])), "changed")
  assert.equal(destinationAvailable(a.destination, ready("machine/a", [a])), "ready")
})

test("a reused Session id with a new execution generation cannot open or reuse old content", () => {
  const old = row("machine/a", "same", "7")
  const current = row("machine/a", "same", "8")
  const cache = new SessionDetailCache()
  assert.equal(cache.put({ kind: "ready", destination: old.destination, observedAt: 1000, info: {}, entries: [], question: null }), true)
  assert.equal(cache.get(current.destination), undefined)
  assert.equal(destinationAvailable(old.destination, ready("machine/a", [current])), "changed")
  assert.equal(destinationAvailable(current.destination, ready("machine/a", [current])), "ready")
  assert.equal(destinationAvailable(current.destination, ready("machine/a", [{ ...current, freshness: "stale" }])), "stale")
  cache.invalidateMachine("machine/a")
  assert.equal(cache.get(old.destination), undefined)
})

test("slow or offline machines do not make the fast machine's projection empty", () => {
  const fast = ready("fast", [row("fast", "s1", "1")])
  const slow: MachineSessionProjection = { kind: "unavailable", reason: "offline" }
  assert.equal(fast.kind, "ready")
  assert.equal(fast.kind === "ready" && fast.rows.length, 1)
  assert.equal(destinationAvailable(row("slow", "s2", "1").destination, slow), "waiting")
  assert.equal(ready("fast", []).kind, "ready") // only an explicit complete reply may be empty
})

test("an event gap marks only its machine unknown until an authoritative reread", () => {
  const before = ready("one", [row("one", "s", "1")])
  const gap = afterEventGap(before)
  assert.equal(gap.kind, "unavailable")
  assert.equal(gap.kind === "unavailable" && gap.reason, "event_gap")
  assert.equal(gap.rows?.[0].freshness, "stale")
  assert.equal(gap.observedAt, 1000)
  assert.equal(destinationAvailable(row("one", "s", "1").destination, gap), "waiting")
  assert.equal(destinationAvailable(row("two", "s", "1").destination, ready("two", [row("two", "s", "1")])), "ready")
})

test("a partial next status pass keeps the prior rows visible but disables their actions", () => {
  const previous = ready("one", [row("one", "s", "1")])
  const gap = settleProjection("one", previous, { kind: "unavailable", reason: "event_gap", observedAt: 1200 })
  assert.deepEqual(gap.rows?.map((item) => item.destination.sessionID), ["s"])
  assert.equal(gap.rows?.[0].freshness, "stale")
  assert.equal(destinationAvailable(row("one", "s", "1").destination, gap), "waiting")
  const next = settleProjection("one", gap, ready("one", [row("one", "s", "2")]))
  assert.deepEqual(next.rows?.map((item) => item.destination.sessionID), ["s"])
  assert.equal(destinationAvailable(row("one", "s", "2").destination, next), "ready")
})

test("a temporary status read failure retains the last list without making its rows actionable", () => {
  const previous = ready("one", [row("one", "s", "1")])
  for (const reason of ["unknown", "unresponsive", "stale", "offline"] as const) {
    const held = settleProjection("one", previous, { kind: "unavailable", reason })
    assert.equal(held.kind, "unavailable")
    assert.deepEqual(held.rows?.map((item) => item.destination.sessionID), ["s"])
    assert.equal(held.rows?.[0].freshness, "stale")
    assert.equal(destinationAvailable(row("one", "s", "1").destination, held), "waiting")
    assert.equal(checkedProjection("one", held), held)
  }
  assert.equal(settleProjection("one", previous, { kind: "unavailable", reason: "no_permission" }).rows, undefined)
})

test("a new pass retains the same execution's work line until fresh status contradicts it", () => {
  const current = row("one", "s", "1")
  const presentation = { title: "Example", status: { state: "working" as const, work_state: "working" as const } }
  assert.equal(displayedPresentationStatus(current, presentation), presentation.status)
  assert.equal(displayedPresentationStatus({ ...current, freshness: "stale", state: "idle" }, presentation), presentation.status)
  assert.equal(displayedPresentationStatus({ ...current, state: "waiting" }, presentation), undefined)
})

test("retained observations remain readable but cannot authorize content", () => {
  const old = row("one", "same", "7")
  const retained: MachineSessionProjection = { kind: "unavailable", reason: "stale", complete: true,
    observedAt: 1000, rows: [{ ...old, freshness: "stale" }] }
  assert.equal(checkedProjection("one", retained), retained)
  assert.equal(destinationAvailable(old.destination, retained), "waiting")
  assert.equal(checkedProjection("one", { ...retained, rows: [old] }).kind, "unavailable")
  assert.equal((checkedProjection("one", { ...retained, rows: [old] }) as { reason: string }).reason, "bad_projection")
  assert.equal((checkedProjection("one", { ...retained, rows: [{ ...old, destination: { ...old.destination, machineID: "two" } }] }) as { reason: string }).reason,
    "bad_projection")
})

test("malformed projection and URLs are refused without content reads", () => {
  assert.equal(checkedProjection("one", { kind: "ready", complete: true, observedAt: 1, rows: [row("two", "s", "1")] }).kind, "unavailable")
  assert.equal(checkedProjection("one", { kind: "ready", complete: true, observedAt: 1, rows: [row("one", "s", "1"), row("one", "s", "1")] }).kind, "unavailable")
  assert.equal(destinationFromFragment("#machine=one&session=s"), null)
  assert.equal(destinationFromFragment("#machine=one&session=s&generation=%ZZ"), null)
  assert.equal(destinationAvailable(row("one", "s", "1").destination, undefined), "waiting")
})

test("older pages prepend only to the exact execution and requested cursor", () => {
  const destination = row("one", "same", "1").destination
  const current = { kind: "ready" as const, destination, observedAt: 1000, info: {},
    entries: [{ role: "assistant", text: "newest" }], nextBefore: 123, question: null }
  const older = { kind: "ready" as const, destination, before: 123,
    entries: [{ role: "user", text: "older" }], nextBefore: 40 }
  assert.deepEqual(prependOlderPage(current, older)?.entries.map((entry) => entry.text), ["older", "newest"])
  assert.equal(prependOlderPage(current, older)?.nextBefore, 40)
  assert.equal(prependOlderPage(current, { ...older, before: 122 }), null)
  assert.equal(prependOlderPage(current, { ...older, nextBefore: 123 }), null)
  assert.equal(prependOlderPage(current, { ...older, destination: row("two", "same", "1").destination }), null)
  assert.equal(prependOlderPage(current, { ...older, destination: row("one", "same", "2").destination }), null)
  assert.equal(prependOlderPage(current, { ...older, nextBefore: undefined })?.nextBefore, undefined)
  assert.equal(prependOlderPage(current, { kind: "unavailable", reason: "old_version" }), null)
  assert.deepEqual(current.entries, [{ role: "assistant", text: "newest" }])
})

test("current status is reread when its earliest status time expires", () => {
  const one = ready("one", [{ ...row("one", "s", "1"), observedAt: 900 }])
  const two = ready("two", [{ ...row("two", "s", "1"), observedAt: 950 }])
  assert.equal(projectionRefreshAt(one, 300_000), 300_901)
  assert.equal(projectionRefreshAt(two, 300_000), 300_951)
  assert.equal(projectionRefreshAt(ready("one", [{ ...row("one", "s", "1"), freshness: "stale", observedAt: 1 }]),
    300_000), 301_001)
  assert.equal(projectionRefreshAt({ kind: "unavailable", reason: "stale" }, 300_000), null)
  assert.equal(projectionRefreshAt({ kind: "unavailable", reason: "offline", retryAt: 1200 }, 300_000), 1200)
})
