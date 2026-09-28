import assert from "node:assert/strict"
import test from "node:test"
import type { WorkGateCompactRead, WorkGateRoundSummary } from "@clawdline/contract"
// @ts-expect-error -- a .ts path for Node's type-stripping test runner.
import { gateStatus, roundStatus } from "./gate-status.ts"

const round = (state: WorkGateRoundSummary["state"], verdict?: WorkGateRoundSummary["verdict"]): WorkGateRoundSummary => ({
  id: "r", state, verdict, candidate_commit: "c", criteria_digest: "d", created_at: 1,
})
const gate = (latest_round?: WorkGateRoundSummary): WorkGateCompactRead => ({
  gate_snapshot_cycle: 1, planning_gate: true, verify_gate: true,
  metrics: { rounds: 1, fails: 0, findings: 0, overrides: 0 }, latest_round,
})

test("unknown evidence, stale and technical failure never read as PASS", () => {
  assert.match(gateStatus(), /尚未擷取/)
  assert.match(gateStatus(gate()), /尚未開始/)
  for (const state of ["queued", "dispatching", "running", "stale", "technical_failure"] as const) {
    assert.doesNotMatch(roundStatus(round(state, "PASS")), /^(獨立驗證 PASS)$/)
  }
  assert.match(roundStatus(round("complete")), /未知/)
  assert.match(roundStatus(round("complete", "NEEDS_WORK")), /無法驗證/)
  assert.equal(roundStatus(round("complete", "PASS")), "獨立驗證 PASS")
  assert.match(gateStatus(gate(round("complete", "PASS"))), /尚無有效合併授權/)
  assert.match(gateStatus({ ...gate(round("complete", "PASS")), current_authorization: { kind: "technical_person_override", reason: "repair", created_at: 2 } }), /未取得 checker PASS/)
})
