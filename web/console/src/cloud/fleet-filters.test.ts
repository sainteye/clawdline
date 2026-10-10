import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- node's type-stripping runner resolves the source .ts file.
import { matchesFleetFilter } from "./fleet-filters.ts"
import type { ProjectedSession } from "./all-machine-sessions.js"

const row: ProjectedSession = {
  destination: { machineID: "machine", sessionID: "session", executionGeneration: "a".repeat(32) },
  state: "idle", freshness: "current", observedAt: 1,
}

test("attention shows replies and user notes, not other status problems", () => {
  assert.equal(matchesFleetFilter({ ...row, waitingForReply: true }, "attention"), true)
  assert.equal(matchesFleetFilter({ ...row, needsAttention: true }, "attention"), true)
  assert.equal(matchesFleetFilter({ ...row, completedUnconfirmed: true, closeBlocked: true,
    failedAgentCount: 1, noMovement: true }, "attention"), false)
  assert.equal(matchesFleetFilter(row, "attention"), false)
})

test("working shows only a current working state", () => {
  assert.equal(matchesFleetFilter({ ...row, state: "working" }, "working"), true)
  assert.equal(matchesFleetFilter({ ...row, state: "working", freshness: "stale" }, "working"), false)
  assert.equal(matchesFleetFilter({ ...row, state: "waiting" }, "working"), false)
  assert.equal(matchesFleetFilter(row, "all"), true)
})

test("idle shows only a current idle state", () => {
  assert.equal(matchesFleetFilter(row, "idle"), true)
  assert.equal(matchesFleetFilter({ ...row, freshness: "stale" }, "idle"), false)
  assert.equal(matchesFleetFilter({ ...row, state: "working" }, "idle"), false)
})
