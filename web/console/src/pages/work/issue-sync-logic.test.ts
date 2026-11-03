import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- a .ts path for Node's type-stripping test runner.
import { failureKey, readyChoice } from "./issue-sync-logic.ts"

test("a Jira choice waits for its project and issue type", () => {
  assert.deepEqual(readyChoice({ provider: "github" }), { provider: "github" })
  assert.equal(readyChoice({ provider: "jira" }), null)
  assert.equal(readyChoice({ provider: "jira", jira_project: "OPS" }), null)
  assert.deepEqual(readyChoice({ provider: "jira", jira_project: "OPS", jira_issue_type: "Task" }),
    { provider: "jira", jira_project: "OPS", jira_issue_type: "Task" })
})

test("each failed link says what the person can do about it", () => {
  assert.equal(failureKey("github_reauthorization_required"), "issueSyncErrAuth")
  assert.equal(failureKey("issue_sync_authorization_required"), "issueSyncErrAuth")
  assert.equal(failureKey("jira_rate_limited"), "issueSyncErrRate")
  assert.equal(failureKey("issue_target_not_found"), "issueSyncErrNotFound")
  assert.equal(failureKey("issue_target_refused"), "issueSyncErrRefused")
  assert.equal(failureKey("jira_transition_missing"), "issueSyncErrTransition")
  // A code this console does not know is still a sentence, never blank.
  assert.equal(failureKey("something_new"), "issueSyncErrUnreachable")
  assert.equal(failureKey(undefined), "issueSyncErrUnreachable")
})
