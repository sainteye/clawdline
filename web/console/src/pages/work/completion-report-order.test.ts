import assert from "node:assert/strict"
import test from "node:test"
import type { WorkV2Document } from "./api.js"
import { completionReportsNewestFirst } from "./completion-report-order.js"

function document(id: string, role: WorkV2Document["role"], created_at: number): WorkV2Document {
  return { id, role, created_at, title: id, body: id, reference: "", position: 0, version: 1 }
}

test("completion reports put the most recently written report first without changing the item", () => {
  const documents = [
    document("older", "completion_report", 100),
    document("design", "design", 300),
    document("newest", "completion_report", 300),
    document("middle", "completion_report", 200),
  ]

  assert.deepEqual(completionReportsNewestFirst(documents).map((entry) => entry.id), ["newest", "middle", "older"])
  assert.deepEqual(documents.map((entry) => entry.id), ["older", "design", "newest", "middle"])
})
