import { test } from "node:test"
import assert from "node:assert/strict"
import type { SessionRow, TaskRow } from "@clawdline/contract"
// @ts-expect-error -- Node runs the source directly with type stripping.
import { activeCallbacksForSession } from "./callback-owner.ts"

const owner = { id: "terminal-a", sessionId: "conversation-a" } as SessionRow
const task = (state: TaskRow["state"], overrides: Partial<TaskRow> = {}): TaskRow => ({
  kind: "callback",
  state,
  root: { terminalId: owner.id, sessionId: owner.sessionId },
  ...overrides,
} as TaskRow)

test("the initiating Session owns live callbacks, including one waiting for the heavy slot", () => {
  const running = [task("queued"), task("spawning"), task("briefed")]
  assert.equal(activeCallbacksForSession(owner, running).length, 3)
  assert.equal(activeCallbacksForSession({ id: "terminal-b", sessionId: "conversation-b" }, running).length, 0)
  assert.equal(activeCallbacksForSession({ id: owner.id }, running).length, 0)
})

test("child tasks, reused terminals and settled callbacks do not mark the owner", () => {
  const mixed = [
    task("briefed", { kind: "child" }),
    task("briefed", { root: { terminalId: owner.id, sessionId: "older-conversation" } }),
    task("success"), task("failure"), task("timeout"), task("cancelled"), task("spawn_failed"),
  ]
  assert.deepEqual(activeCallbacksForSession(owner, mixed), [])
})
