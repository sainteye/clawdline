import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- Node's type-stripping runner loads the source directly.
import { arrangeFleetRows, fleetWaitingKey } from "./fleet-order.ts"
import type { ProjectedSession } from "./all-machine-sessions.js"

function row(id: string, state: string, parentSessionID?: string): ProjectedSession {
  return { destination: { machineID: "machine-a", sessionID: id,
    executionGeneration: "0123456789abcdef0123456789abcdef" },
  title: id, state, freshness: "current", observedAt: 1000, parentSessionID }
}

test("the fleet adapter uses the original state and title order while keeping children under their root", () => {
  const rows = [row("idle", "idle"), row("child", "working", "root"), row("root", "working"),
    row("wait", "waiting"), row("other", "working")]
  const titles = new Map([["root", "A root"], ["child", "A child"], ["other", "B work"]])
  const arranged = arrangeFleetRows(rows, titles)
  assert.deepEqual(arranged.map(({ row }) => row.destination.sessionID), ["wait", "root", "child", "other", "idle"])
  assert.deepEqual(arranged.map(({ depth }) => depth), [0, 0, 1, 0, 0])
})

test("an absent parent leaves its child visible without an invented branch", () => {
  const arranged = arrangeFleetRows([row("child", "working", "on-another-machine")], new Map())
  assert.equal(arranged[0].depth, 0)
  assert.equal(arranged[0].row.destination.sessionID, "child")
})

test("fleet rows keep the original pointer hold until a waiting Session changes", () => {
  const before = [row("one", "working"), row("two", "idle")]
  const titles = new Map([["one", "A work"], ["two", "B work"]])
  const hold = { order: arrangeFleetRows(before, titles).map(({ row }) => row.destination.sessionID),
    waiting: fleetWaitingKey(before) }
  const changed = [row("one", "idle"), row("two", "working")]
  assert.deepEqual(arrangeFleetRows(changed, titles, hold).map(({ row }) => row.destination.sessionID), ["one", "two"])
  assert.deepEqual(arrangeFleetRows(changed, titles).map(({ row }) => row.destination.sessionID), ["two", "one"])
  assert.notEqual(fleetWaitingKey([row("one", "waiting"), changed[1]]), hold.waiting)
})
