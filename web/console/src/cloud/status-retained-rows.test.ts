import assert from "node:assert/strict"
import test from "node:test"
import type { SessionStatus, SessionStatusInventory } from "@clawdline/contract"
// @ts-expect-error -- node's type-stripping runner resolves the source .ts file.
import { statusGapTarget, statusPassTransition, statusProjection } from "./status-projection.ts"
// @ts-expect-error -- the same runner, for the key the pinned read is asked again on.
import { presentationPass } from "./all-machine-sessions.ts"

/**
 * The page against a machine that states only the status rows that changed.
 *
 * `internal/transport/cloud/session_status.go` used to restate every `ss/` row
 * of a machine whenever any part of the set moved, each with a fresh random
 * pass id. It now gives each row its own window and derives the pass id from
 * the set — the listed ids and each row's execution — so an unchanged row is
 * left to the one the relay retains. These rows are *the page's* side of that:
 * the cache below only ever holds what the machine published, with the relay
 * retaining the rest, and the acceptance rules it is read by are unchanged.
 *
 * The payload types come from the generated contract, so a field renamed in
 * `api/v1/session-status.schema.json` fails here rather than at runtime.
 */
const MACHINE = "mac_a"
const INVENTORY = "__clawdline_inventory_v1__"
const setPass = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const nextSetPass = "cccccccccccccccccccccccccccccccc"
const firstDisplay = "dddddddddddddddddddddddddddddddd"
const nextDisplay = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
const executionOf = (id: string) => id.charCodeAt(1).toString(16).repeat(32).slice(0, 32)
const NOW = 1_000_000

/** The relay: one retained value per channel, and the sequence it arrived at. */
function relay() {
  const statusSnapshots = new Map<string, unknown>()
  let sequence = 0
  const publish = (session: string, payload: SessionStatus | SessionStatusInventory) => {
    sequence += 1
    statusSnapshots.set(JSON.stringify([MACHINE, session]),
      { identity: { machine: MACHINE, session }, payload, observedAt: NOW, sequence })
  }
  return { client: { statusSnapshots }, publish }
}

function row(id: string, pass: string, at: number): SessionStatus {
  return {
    machine_id: MACHINE, session_id: id, execution_generation: executionOf(id),
    snapshot_generation: pass, assistant: "claude", backend: "tmux", state: "idle",
    source: { provenance: "tmux", observed_at: at, freshness: "current" },
    inventory_complete: true, projected_at: at, last_movement_at: at - 60,
    no_progress_after_ms: 1_800_000, no_movement: false,
  }
}

function marker(ids: string[], pass: string, display: string, at: number): SessionStatusInventory {
  return { inventory: { version: 1, sessions: ids }, at, complete: true,
    snapshot_generation: pass, presentation_generation: display }
}

const seconds = NOW / 1000
const ids = ["%10", "%11", "%12"]

test("a machine that restates one row of a pass still has a complete list", () => {
  const { client, publish } = relay()
  for (const id of ids) publish(id, row(id, setPass, seconds - 10))
  publish(INVENTORY, marker(ids, setPass, firstDisplay, seconds - 10))
  const first = statusProjection(client, MACHINE, NOW)
  assert.equal(first.kind, "ready")
  if (first.kind !== "ready") return
  assert.equal(first.rows.length, 3)
  assert.equal(first.presentationGeneration, firstDisplay)

  // One row says something new. The other two are not restated, and the
  // marker is not either: the set is the same one it already named.
  publish("%11", row("%11", setPass, seconds))
  const after = statusProjection(client, MACHINE, NOW)
  assert.equal(after.kind, "ready", "the rows the machine left alone stopped completing its marker")
  if (after.kind !== "ready") return
  assert.equal(after.rows.length, 3)
  assert.equal(statusGapTarget(client, MACHINE), null)
  assert.equal(statusPassTransition(client, MACHINE), false)

  // A work line moved. Only the marker goes out, with a new presentation id
  // and the same pass id, and the list re-reads its pinned titles.
  publish(INVENTORY, marker(ids, setPass, nextDisplay, seconds))
  const told = statusProjection(client, MACHINE, NOW)
  assert.equal(told.kind, "ready")
  if (told.kind !== "ready") return
  assert.equal(told.presentationGeneration, nextDisplay)
  assert.equal(told.snapshotGeneration, setPass)
  assert.equal(told.rows.length, 3)
})

test("a machine with no presentation id keys the pinned read on its pass id", () => {
  const { client, publish } = relay()
  for (const id of ids) publish(id, row(id, setPass, seconds))
  const older = marker(ids, setPass, "", seconds) as unknown as Record<string, unknown>
  delete older.presentation_generation
  publish(INVENTORY, older as unknown as SessionStatusInventory)
  const reading = statusProjection(client, MACHINE, NOW)
  assert.equal(reading.kind, "ready")
  if (reading.kind !== "ready") return
  assert.equal(reading.presentationGeneration, undefined)
  assert.equal(reading.snapshotGeneration, setPass)
})

test("a retained row of a Session that has closed is not counted as one", () => {
  const { client, publish } = relay()
  for (const id of ids) publish(id, row(id, setPass, seconds))
  publish(INVENTORY, marker(ids, setPass, firstDisplay, seconds))
  // %12 closes. Its row stays on the relay; the set is smaller, so the pass
  // id moves and the Sessions that remain state the new one first.
  const left = ids.slice(0, 2)
  for (const id of left) publish(id, row(id, nextSetPass, seconds))
  publish(INVENTORY, marker(left, nextSetPass, nextDisplay, seconds))
  const reading = statusProjection(client, MACHINE, NOW)
  assert.equal(reading.kind, "ready")
  if (reading.kind !== "ready") return
  assert.deepEqual(reading.rows.map((entry) => entry.destination.sessionID), left)
})

test("a retained row of an older pass cannot complete the newest marker", () => {
  const { client, publish } = relay()
  for (const id of ids) publish(id, row(id, setPass, seconds))
  publish(INVENTORY, marker(ids, setPass, firstDisplay, seconds))
  // A Session restarts: same ids, one new execution, so the pass id moves.
  // The row of %10 is lost on the way, and what the relay retains for it is
  // the row of the pass before. The marker must not settle on it.
  for (const id of ids.slice(1)) publish(id, row(id, nextSetPass, seconds))
  publish(INVENTORY, marker(ids, nextSetPass, nextDisplay, seconds))
  const reading = statusProjection(client, MACHINE, NOW)
  assert.equal(reading.kind === "unavailable" && reading.reason, "event_gap")
  assert.deepEqual(statusGapTarget(client, MACHINE), { sessionID: "%10", snapshotGeneration: nextSetPass })
  publish("%10", row("%10", nextSetPass, seconds))
  assert.equal(statusProjection(client, MACHINE, NOW).kind, "ready")
})

test("rows of a new pass that overtake its marker are a transition, not a gap", () => {
  const { client, publish } = relay()
  for (const id of ids) publish(id, row(id, setPass, seconds))
  publish(INVENTORY, marker(ids, setPass, firstDisplay, seconds))
  for (const id of ids) publish(id, row(id, nextSetPass, seconds))
  const reading = statusProjection(client, MACHINE, NOW)
  assert.equal(reading.kind === "unavailable" && reading.reason, "event_gap")
  assert.equal(statusPassTransition(client, MACHINE), true, "the page would have shown a gap instead of waiting")
  publish(INVENTORY, marker(ids, nextSetPass, nextDisplay, seconds))
  assert.equal(statusProjection(client, MACHINE, NOW).kind, "ready")
  assert.equal(statusPassTransition(client, MACHINE), false)
})

test("a row left alone for longer than the freshness window is stale, not accepted", () => {
  const { client, publish } = relay()
  for (const id of ids) publish(id, row(id, setPass, seconds))
  // The machine restates every row and the marker on its heartbeat, inside
  // this window. Past it the list is retained but stale, as it always was.
  publish("%10", row("%10", setPass, seconds - 400))
  publish(INVENTORY, marker(ids, setPass, firstDisplay, seconds))
  const reading = statusProjection(client, MACHINE, NOW)
  assert.equal(reading.kind === "unavailable" && reading.reason, "stale")
  assert.equal(reading.rows?.length, 3)
  assert.ok(reading.rows?.every((entry) => entry.freshness === "stale"))
})

test("the pinned list is re-read when the display id moves, and when only it can", () => {
  const ready = (over: Record<string, unknown>) =>
    ({ kind: "ready" as const, complete: true as const, observedAt: NOW, rows: [], ...over })
  // A machine that states the pass id of a set keeps it still while a work
  // line moves, so the display id is what the read follows.
  assert.equal(presentationPass(ready({ snapshotGeneration: setPass, presentationGeneration: firstDisplay })), firstDisplay)
  assert.notEqual(
    presentationPass(ready({ snapshotGeneration: setPass, presentationGeneration: nextDisplay })),
    presentationPass(ready({ snapshotGeneration: setPass, presentationGeneration: firstDisplay })))
  // Against a machine that does not send one, the pass id moves with every
  // display change, which is what the read followed before.
  assert.equal(presentationPass(ready({ snapshotGeneration: setPass })), setPass)
  assert.equal(presentationPass(ready({})), String(NOW))
})
