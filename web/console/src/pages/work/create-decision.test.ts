import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { workV2CreateDecision } from "./create-decision.ts"

const body = {
  project_id: "project-a",
  kind: "issue" as const,
  title: "One user decision",
  description: "Do not turn an uncertain reply into a second card",
  deployment_policy: "agent_decides" as const,
}

test("an uncertain create keeps one idempotency key until its content changes", () => {
  const first = workV2CreateDecision(body)
  const retry = workV2CreateDecision({ ...body }, first)
  assert.equal(retry.key, first.key)

  const changed = workV2CreateDecision({ ...body, description: "A different decision" }, retry)
  assert.notEqual(changed.key, first.key)
})
