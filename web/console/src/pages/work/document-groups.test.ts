// Every Board document has one visible home: `node --test --experimental-strip-types web/console/src/pages/work/document-groups.test.ts`.
import assert from "node:assert/strict"
import test from "node:test"
import type { WorkV2Document } from "./api.js"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { groupDocumentsByRole } from "./document-groups.ts"

function document(id: string, role: string, created_at: number): WorkV2Document {
  return { id, role, created_at, title: id, body: id, reference: "", position: 0, version: 1 } as WorkV2Document
}

test("every document role has one group, including an unknown future role, newest first", () => {
  const documents = [
    document("spec-old", "spec", 100),
    document("plan", "plan", 200),
    document("spec-new", "spec", 300),
    document("review", "plan_review", 210),
    document("design", "design", 220),
    document("test", "test", 230),
    document("deploy", "deploy", 240),
    document("report", "completion_report", 250),
    document("other", "other", 260),
    document("future", "decision_record", 270),
  ]

  const groups = groupDocumentsByRole(documents)
  assert.deepEqual(groups.map((group) => group.role), [
    "plan", "plan_review", "spec", "design", "test", "deploy", "completion_report", "other", "decision_record",
  ])
  assert.deepEqual(groups.flatMap((group) => group.documents.map((entry) => entry.id)), [
    "plan", "review", "spec-new", "spec-old", "design", "test", "deploy", "report", "other", "future",
  ])
  assert.equal(new Set(groups.flatMap((group) => group.documents.map((entry) => entry.id))).size, documents.length)
  assert.deepEqual(groupDocumentsByRole(undefined), [])
})
