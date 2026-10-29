import assert from "node:assert/strict"
import test from "node:test"
import type { SessionRow } from "@clawdline/contract"
import {
  afterEventGap, checkedProjection, destinationAvailable, destinationFragment, destinationFromFragment,
  displayedPresentationStatus, fleetRowTitle, mergeListPresentation, identifyOpenedFleetRow, sessionDetailID, withConfirmedFleetTitle,
  destinationKey, prependOlderPage, projectionRefreshAt, settleProjection, SessionDetailCache, shownDestinationKeys,
  SessionExecutionRecovery, supersedingDestination,
  type MachineSessionProjection, type ProjectedSession,
// @ts-expect-error -- node's type-stripping runner resolves the source .ts file.
} from "./all-machine-sessions.ts"

const row = (machineID: string, sessionID: string, executionGeneration: string): ProjectedSession => ({
  destination: { machineID, sessionID, executionGeneration: executionGeneration.padStart(32, "0") }, state: "working",
  freshness: "current", needsAttention: true, observedAt: 1000,
})
const ready = (machine: string, rows: ProjectedSession[]): MachineSessionProjection =>
  checkedProjection(machine, { kind: "ready", complete: true, observedAt: 1000, rows })

test("a newly opened Cloud Session selects its own detail before local selection catches up", () => {
  const target = row("machine/a", "new", "7").destination
  const old = { id: "old", execution_generation: target.executionGeneration } as SessionRow
  const next = { id: "new", execution_generation: target.executionGeneration } as SessionRow
  assert.equal(sessionDetailID("old", target, "machine/a", [old, next]), "new")
  assert.equal(sessionDetailID(null, target, "machine/a", [old, next]), "new")
})

test("a pending Cloud detail never falls back to the previously opened conversation", () => {
  const target = row("machine/a", "new", "7").destination
  const old = { id: "old", execution_generation: target.executionGeneration } as SessionRow
  const replaced = { id: "new", execution_generation: "8".padStart(32, "0") } as SessionRow
  const next = { ...replaced, execution_generation: target.executionGeneration }
  assert.equal(sessionDetailID("old", target, "machine/a", [old]), null)
  assert.equal(sessionDetailID("old", target, "machine/a", [old, replaced]), null)
  assert.equal(sessionDetailID("old", target, "machine/b", [old, next]), null)
})

test("a local detail keeps its selected Session without a Cloud destination", () => {
  assert.equal(sessionDetailID("old", null, null, []), "old")
  assert.equal(sessionDetailID(null, null, null, []), null)
})

test("fleet list names come from authorized presentations, never routing ids", () => {
  const ordinary = row("machine/a", "%14", "7")
  assert.equal(fleetRowTitle(ordinary, { title: "Named work" }, "Unknown"), "Named work")
  assert.equal(fleetRowTitle(ordinary, undefined, "Unknown"), "Unknown")
  assert.equal(fleetRowTitle(ordinary, { title: "Old" }, "Unknown", "Saved"), "Saved")
  assert.equal(fleetRowTitle({ ...ordinary, machineScope: true }, undefined, "Unknown"), "Clawdfather")
})

test("a saved title reaches only the selected machine and execution while its list is stale", () => {
  const target = row("machine/a", "%14", "7").destination
  const old = { id: target.sessionID, execution_generation: target.executionGeneration, label: "Old" } as SessionRow
  const nextRun = { ...old, execution_generation: "8".padStart(32, "0") }
  const otherMachine = { ...old, machine: "machine/b" }
  const shown = withConfirmedFleetTitle([old, nextRun], target, "machine/a", "New")
  assert.deepEqual(shown.map((item) => item.label), ["New", "Old"])
  assert.equal(withConfirmedFleetTitle([old], target, "machine/b", "New")[0].label, "Old")
  assert.equal(withConfirmedFleetTitle([otherMachine], target, "machine/b", "New")[0].label, "Old")
})

test("the opened Cloud detail uses the signed machine identity for only its exact execution", () => {
  const target = row("machine/a", "%14", "7").destination
  const rich = { id: target.sessionID, execution_generation: target.executionGeneration,
    label: "Ordinary title", machine_scope: false } as SessionRow
  const replaced = { ...rich, execution_generation: row("machine/a", "%14", "8").destination.executionGeneration }
  const identity = { machineScope: true, coordinatorBound: true }
  const identified = identifyOpenedFleetRow([rich, replaced], target, target.machineID, identity)
  assert.equal(identified[0].machine_scope, true)
  assert.equal((identified[0] as SessionRow & { coordinator_bound?: boolean }).coordinator_bound, true)
  assert.equal(identified[0].label, "Ordinary title")
  assert.equal(identified[1], replaced)
  assert.equal(rich.machine_scope, false)
  assert.equal(identifyOpenedFleetRow([rich], target, "machine/b", identity)[0], rich)
  assert.equal(identifyOpenedFleetRow([rich], target, target.machineID,
    { machineScope: false, coordinatorBound: true })[0], rich)
})

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

test("a push address selects the exact Cloud execution while retaining the local session field", () => {
  const destination = row("machine/a", "%14", "7").destination
  const address = `#session=%2514&machine=machine%2Fa&generation=${destination.executionGeneration}`
  assert.deepEqual(destinationFromFragment(address), destination)
  assert.equal(destinationFromFragment("#session=%2514"), null)
  assert.equal(destinationFromFragment(address.replace(destination.executionGeneration, "bad")), null)
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

test("a partial authenticated list opens named executions but proves no Session absent", () => {
  const known = row("machine/a", "known", "7")
  const missing = row("machine/a", "unscanned", "8")
  const partial = checkedProjection("machine/a", { kind: "ready", complete: false,
    observedAt: 1000, rows: [known] })
  assert.equal(partial.kind, "ready")
  assert.equal(destinationAvailable(known.destination, partial), "ready")
  assert.equal(destinationAvailable(missing.destination, partial), "waiting")
  assert.equal(supersedingDestination(missing.destination, partial), null)
  assert.equal(destinationAvailable(missing.destination, checkedProjection("machine/a", {
    kind: "ready", complete: false, observedAt: 1000, rows: [],
  })), "waiting")
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

test("a list that went stale keeps its rows' titles, and only rows nothing shows lose them", () => {
  const previous = ready("one", [row("one", "s", "1")])
  const stale = settleProjection("one", previous, { kind: "unavailable", reason: "unknown" })
  const offline = settleProjection("two", undefined, { kind: "unavailable", reason: "offline" })
  assert.deepEqual([...shownDestinationKeys([stale, offline, undefined])],
    [destinationKey(row("one", "s", "1").destination)])
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

test("a gap keeps the listed Sessions but draws each from its newest row", () => {
  // The set is uncertain during a gap; what a listed Session says is not. An
  // answered Session must not keep saying "waiting" because another row is late.
  const previous = ready("one", [{ ...row("one", "s", "1"), state: "waiting" }, row("one", "t", "1")])
  const answered = { ...row("one", "s", "1"), state: "idle", needsAttention: false, observedAt: 1500, freshness: "stale" as const }
  const older = { ...row("one", "t", "1"), state: "idle", observedAt: 900, freshness: "stale" as const }
  const newcomer = { ...row("one", "u", "1"), freshness: "stale" as const }
  const gap = settleProjection("one", previous, { kind: "unavailable", reason: "event_gap", observedAt: 1200,
    latest: [answered, older, newcomer] })
  assert.deepEqual(gap.rows?.map((item) => item.destination.sessionID), ["s", "t"], "a gap never adds or drops a Session")
  assert.equal(gap.rows?.[0].state, "idle")
  assert.equal(gap.rows?.[0].needsAttention, false)
  assert.equal(gap.rows?.[1].state, "working", "an older row never replaces a newer one")
  assert.ok(gap.rows?.every((item) => item.freshness === "stale"), "nothing in a gap authorizes an action")
  const restarted = settleProjection("one", previous, { kind: "unavailable", reason: "event_gap", observedAt: 1200,
    latest: [{ ...answered, destination: row("one", "s", "2").destination }] })
  assert.equal(restarted.rows?.[0].state, "waiting", "another execution's row never speaks for this one")
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
  const retry = settleProjection("one", previous, { kind: "unavailable", reason: "unknown" }, 5_000)
  assert.ok(retry.kind === "unavailable")
  assert.ok(retry.retryAt !== undefined && retry.retryAt >= Date.now() + 4_000 && retry.retryAt <= Date.now() + 5_000)
  assert.equal(projectionRefreshAt(retry, 300_000), retry.retryAt)
  assert.equal(settleProjection("one", previous, { kind: "unavailable", reason: "no_permission" }).rows, undefined)
})

test("a new pass retains the same execution's work line until fresh status contradicts it", () => {
  const current = row("one", "s", "1")
  const presentation = { title: "Example", status: { state: "working" as const, work_state: "working" as const } }
  assert.equal(displayedPresentationStatus(current, presentation), presentation.status)
  assert.equal(displayedPresentationStatus({ ...current, freshness: "stale", state: "idle" }, presentation), presentation.status)
  assert.equal(displayedPresentationStatus({ ...current, state: "waiting" }, presentation), undefined)
  assert.equal(displayedPresentationStatus({ ...current, state: "waiting", observedAt: 1000 },
    { ...presentation, observedAt: 1001 }), presentation.status)
  assert.equal(displayedPresentationStatus({ ...current, state: "waiting", observedAt: 1002 },
    { ...presentation, observedAt: 1001 }), undefined)
})

test("a newer capacity reading invalidates old work copy even when terminal activity is unchanged", () => {
  const current = { ...row("one", "s", "1"), state: "idle", observedAt: 1002 }
  const old = { title: "Example", observedAt: 1001,
    status: { state: "idle" as const, provider_block: "model_capacity" as const } }
  assert.equal(displayedPresentationStatus(current, old), undefined)
  assert.equal(displayedPresentationStatus({ ...current, providerBlock: "model_capacity" }, old), old.status)
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

test("an old transcript revision cannot fill an equal cursor from a newer held page", () => {
  const destination = row("one", "same", "1").destination
  const current = { kind: "ready" as const, destination, observedAt: 1000, info: {}, signature: "agy1:new",
    entries: [{ role: "assistant", text: "newest" }], nextBefore: 123, question: null }
  const older = { kind: "ready" as const, destination, before: 123, signature: "agy1:old", requestedRevision: "agy1:old",
    entries: [{ role: "user", text: "older" }], nextBefore: 40 }
  assert.equal(prependOlderPage(current, older), null)
  assert.equal(prependOlderPage(current, { ...older, requestedRevision: "agy1:new", signature: "agy1:new" })?.signature, "agy1:new")
})

test("current status is reread when its earliest status time expires", () => {
  const one = ready("one", [{ ...row("one", "s", "1"), observedAt: 900 }])
  const two = ready("two", [{ ...row("two", "s", "1"), observedAt: 950 }])
  assert.equal(projectionRefreshAt(one, 300_000), 300_901)
  assert.equal(projectionRefreshAt(two, 300_000), 300_951)
  assert.equal(projectionRefreshAt(ready("one", [{ ...row("one", "s", "1"), freshness: "stale", observedAt: 1 }]),
    300_000), 301_001)
  assert.equal(projectionRefreshAt({ kind: "unavailable", reason: "stale" }, 300_000), null)
  assert.equal(projectionRefreshAt({ kind: "unavailable", reason: "unknown", retryAt: 1200 }, 300_000), 1200)
  assert.equal(projectionRefreshAt({ kind: "unavailable", reason: "offline", retryAt: 1200 }, 300_000), 1200)
  assert.equal(projectionRefreshAt({ kind: "unavailable", reason: "event_gap", retryAt: 1200 }, 300_000), 1200)
  assert.equal(projectionRefreshAt({ ...one, observedAt: 900, snapshotGeneration: "direct:900" },
    300_000, 60_000), 60_901)
})

test("a restarted assistant names the execution that replaced the one a person opened", () => {
  const old = row("machine/a", "%14", "7")
  const current = row("machine/a", "%14", "8")
  const projection = ready("machine/a", [current])
  assert.equal(destinationAvailable(old.destination, projection), "changed")
  assert.deepEqual(supersedingDestination(old.destination, projection), current.destination)
  // The row that is still readable is not superseded by itself.
  assert.equal(supersedingDestination(current.destination, projection), null)
})

test("nothing is followed when the Session is gone, unproven, or ambiguous", () => {
  const old = row("machine/a", "%14", "7")
  // Closed on that machine: no row to follow, so the person is told it is gone.
  assert.equal(supersedingDestination(old.destination, ready("machine/a", [row("machine/a", "%15", "8")])), null)
  // A stale reading is not proof that the old execution was replaced.
  assert.equal(supersedingDestination(old.destination,
    ready("machine/a", [{ ...row("machine/a", "%14", "8"), freshness: "stale" }])), null)
  // Never from a projection that is not a complete, current reading.
  assert.equal(supersedingDestination(old.destination, { kind: "unavailable", reason: "offline" }), null)
  assert.equal(supersedingDestination(old.destination, undefined), null)
  // Never across machines, even with the same Session id.
  assert.equal(supersedingDestination(old.destination, ready("machine/b", [row("machine/b", "%14", "8")])), null)
})


test("a push to an old execution follows the current row once, sharing concurrent failures", async () => {
  const recovery = new SessionExecutionRecovery()
  let selected = row("machine/a", "%14", "7").destination
  const original = selected
  const next = row("machine/a", "%14", "8")
  let finish!: (projection: MachineSessionProjection) => void
  let reads = 0
  const read = () => { reads++; return new Promise<MachineSessionProjection>((resolve) => { finish = resolve }) }
  const adopt = (target: typeof selected) => { selected = target }
  const first = recovery.recover(original, read, () => selected, adopt)
  assert.equal(await recovery.recover(original, read, () => selected, adopt), false)
  finish(ready("machine/a", [next]))
  assert.equal(await first, true)
  assert.deepEqual(selected, next.destination)
  assert.equal(reads, 1)
  assert.equal(await recovery.recover(selected, read, () => selected, adopt), false,
    "a second replacement cannot turn one opening into a recovery loop")
  recovery.reset()
  assert.equal(await recovery.recover(selected, async () => ready("machine/a", [row("machine/a", "%14", "9")]),
    () => selected, adopt), true, "a deliberate opening can recover again")
})

test("automatic recovery stops on missing, stale, ambiguous, offline and other-machine rows", async () => {
  const original = row("machine/a", "%14", "7").destination
  const next = row("machine/a", "%14", "8")
  for (const projection of [ready("machine/a", []),
    ready("machine/a", [{ ...next, freshness: "stale" }]),
    ready("machine/a", [next, row("machine/a", "%14", "9")]),
    ready("machine/b", [row("machine/b", "%14", "8")]),
    { kind: "unavailable", reason: "offline" } as MachineSessionProjection]) {
    const recovery = new SessionExecutionRecovery()
    let adopted = false
    const read = async () => projection
    assert.equal(await recovery.recover(original, read, () => original, () => { adopted = true }), false)
    assert.equal(adopted, false)
    assert.equal(await recovery.recover(original, read, () => original, () => { adopted = true }), false)
  }
})

test("late recovery cannot replace another selection, a closed page or a new opening", async () => {
  const original = row("machine/a", "%14", "7").destination
  for (const action of ["navigate", "close", "reopen", "disconnect"]) {
    const recovery = new SessionExecutionRecovery()
    let selected: typeof original | null = original
    let finish!: (projection: MachineSessionProjection) => void
    let adopted = false
    const result = recovery.recover(original, () => new Promise((resolve) => { finish = resolve }),
      () => selected, () => { adopted = true })
    if (action === "navigate") selected = row("machine/b", "%14", "7").destination
    else if (action === "reopen") recovery.reset()
    else selected = null
    finish(ready("machine/a", [row("machine/a", "%14", "8")]))
    assert.equal(await result, false, action)
    assert.equal(adopted, false, action)
  }
})

test("a failed recovery query settles without retrying or adopting", async () => {
  const recovery = new SessionExecutionRecovery()
  const original = row("machine/a", "%14", "7").destination
  let asks = 0
  const read = async (): Promise<MachineSessionProjection> => { asks++; throw new Error("offline") }
  const adopt = () => { throw new Error("must not adopt") }
  assert.equal(await recovery.recover(original, read, () => original, adopt), false)
  assert.equal(await recovery.recover(original, read, () => original, adopt), false)
  assert.equal(asks, 1)
})


test("a refreshed list can restore the same execution after a stale status refusal", async () => {
  const recovery = new SessionExecutionRecovery()
  const target = row("machine/a", "%14", "7")
  let adopted = 0
  assert.equal(await recovery.recover(target.destination, async () => ready("machine/a", [target]),
    () => target.destination, () => { adopted++ }), true)
  assert.equal(adopted, 1)
})

for (const assistant of ["codex", "claude"] as const) {
  test(`the fleet presentation cache keeps ${assistant} through partial display updates`, () => {
    const first = mergeListPresentation({ title: "Named work", assistant })
    assert.equal(first.assistant, assistant)
    const partial = mergeListPresentation({ title: "Renamed work" }, first)
    assert.equal(partial.assistant, assistant)
    assert.equal(partial.title, "Renamed work")
    assert.equal(mergeListPresentation({ title: "Known work", assistant },
      { title: "Earlier", assistant: assistant === "codex" ? "claude" : "codex" }).assistant, assistant)
    assert.equal(mergeListPresentation({ title: "Unknown work" }).assistant, undefined)
  })
}

test("output links keep the exact machine, Session and execution generation", () => {
  const target = { machineID: "machine-a", sessionID: "%19", executionGeneration: "a".repeat(32) }
  assert.deepEqual(destinationFromFragment(destinationFragment(target) + "&output=" + "b".repeat(64)), target)
  assert.equal(destinationFromFragment(destinationFragment(target) + "&output=../../private"), null)
  assert.equal(destinationFromFragment(destinationFragment(target) + "&output=" + "b".repeat(64) + "&machine=another"), null)
})
