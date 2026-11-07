import test from "node:test"
import assert from "node:assert/strict"
import type { SessionInfo, SessionRow } from "@clawdline/contract"
// @ts-expect-error -- Node strip-types runner loads source extensions.
import { rotationReason } from "./rotation.ts"

const now = 200000
const conversation = "10000000-0000-4000-8000-000000000001"
const row = { id: "rotation-fixture", assistant: "codex", backend: "tmux", sessionId: conversation,
  state: "idle", work_state: "milestone_complete", disposition: { scope: "session", receiptAt: now - 86400 },
  activity: { known: true, at: now - 86400 } } as SessionRow
const info = { session: { id: row.id, sessionId: conversation }, models: [], record: { bytes: 50 * 1024 * 1024 },
  rotation: { recordBytesLimit: 50 * 1024 * 1024, idleSecondsLimit: 86400 } } as SessionInfo

test("size includes the threshold and can warn while working without requiring delivery", () => {
  assert.equal(rotationReason({ ...row, state: "working", work_state: "working", disposition: undefined }, info, now), "size")
  assert.equal(rotationReason({ ...row, disposition: undefined }, { ...info, record: { bytes: info.record!.bytes - 1 } }, now), null)
  assert.equal(rotationReason(row, { ...info, record: {bytes:0} }, now), "idle")
})

test("idle requires a current delivery, a full quiet day and known activity", () => {
  const small = { ...info, record: undefined }
  assert.equal(rotationReason(row, small, now), "idle")
  for (const change of [
    { work_state: "ready" }, { state: "working" }, { disposition: undefined },
    { disposition: { scope: "task", receiptAt: now - 86400 } },
    { disposition: { scope: "session", receiptAt: now - 86399 } },
    { activity: { known: false, at: 0 } }, { activity: undefined },
    { activity: { known: true, at: now - 86399 } }, { activity: { known: true, at: now + 1 } },
  ]) assert.equal(rotationReason({ ...row, ...change } as SessionRow, small, now), null, JSON.stringify(change))
})

test("missing facts, replaced conversations and broker child or machine roles never get a handoff offer", () => {
  assert.equal(rotationReason(row, null, now), null)
  assert.equal(rotationReason(null, info, now), null)
  assert.equal(rotationReason(row, { ...info, rotation: undefined }, now), null)
  for (const change of [{ sessionId: "another" }, { sessionId: undefined }, { assistant: "antigravity" },
    { parent_session_id: "parent" }, { machine_scope: true }, { coordinator: { live: true } }]) {
    assert.equal(rotationReason({ ...row, ...change } as SessionRow, info, now), null)
  }
})
