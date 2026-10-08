import assert from "node:assert/strict"
import test from "node:test"
import {
  afterEventGap, checkedProjection, destinationAvailable, destinationFragment, destinationFromFragment,
  destinationKey, matchesSession, SessionDetailCache, type MachineSessionProjection, type ProjectedSession,
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
  assert.equal(destinationAvailable(row("one", "s", "1").destination, gap), "waiting")
  assert.equal(destinationAvailable(row("two", "s", "1").destination, ready("two", [row("two", "s", "1")])), "ready")
})

test("malformed projection and URLs are refused without content reads", () => {
  assert.equal(checkedProjection("one", { kind: "ready", complete: true, observedAt: 1, rows: [row("two", "s", "1")] }).kind, "unavailable")
  assert.equal(checkedProjection("one", { kind: "ready", complete: true, observedAt: 1, rows: [row("one", "s", "1"), row("one", "s", "1")] }).kind, "unavailable")
  assert.equal(destinationFromFragment("#machine=one&session=s"), null)
  assert.equal(destinationFromFragment("#machine=one&session=s&generation=%ZZ"), null)
  assert.equal(destinationAvailable(row("one", "s", "1").destination, undefined), "waiting")
})

test("the five filters operate on status only", () => {
  const item = row("one", "s", "1")
  const filters = { machine: "one", platform: "macOS", state: "working", freshness: "current" as const, attention: "needed" as const }
  assert.equal(matchesSession(item, "macOS", filters), true)
  assert.equal(matchesSession(item, "Linux", filters), false)
  assert.equal(matchesSession({ ...item, needsAttention: false }, "macOS", filters), false)
  assert.equal(matchesSession({ ...item, freshness: "stale" }, "macOS", filters), false)
  assert.equal(matchesSession({ ...item, state: "waiting" }, "macOS", filters), false)
})
