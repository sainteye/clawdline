import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node executes source extensions.
import { approvalChoices, withApproval } from "./approvals.ts"
test("a new Bot starts in the agent's Auto mode, with Approve automatically last",()=>{
  const { choices, selected } = approvalChoices("claude", "ask", "", "Approve automatically", "Reads")
  assert.deepEqual(choices.map(c => c.id), ["mode:auto","mode:default","mode:acceptEdits","mode:plan","approve"])
  assert.equal(selected, "mode:auto")
  assert.equal(approvalChoices("codex", "ask", undefined, "A", "R").choices[0].label, "Auto review")
})
test("a choice maps to the daemon's policy and mode, and back",()=>{
  assert.deepEqual(withApproval("mode:auto"), { permission_policy: "ask", mode: "" })
  assert.deepEqual(withApproval("mode:plan"), { permission_policy: "ask", mode: "plan" })
  assert.deepEqual(withApproval("approve"), { permission_policy: "approve", mode: "" })
  assert.equal(approvalChoices("claude", "approve", "", "A", "R").selected, "approve")
  assert.equal(approvalChoices("claude", "ask", "plan", "A", "R").selected, "mode:plan")
})
test("an older read-only setting stays its own choice, never shown as Auto",()=>{
  const { choices, selected } = approvalChoices("claude", "auto_read", "", "A", "Reads")
  assert.equal(selected, "auto_read")
  assert.ok(choices.some(c => c.id === "auto_read"))
})
