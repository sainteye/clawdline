import assert from "node:assert/strict"
import test from "node:test"
import type { WorkV2Item } from "./api.js"
// @ts-expect-error -- a `.ts` path is required by Node's native type stripping.
import { WORK_V2_EDIT_BASE_API_LEVEL, editWorkV2Body } from "./edit-request.ts"

const opened = { id: "w1", version: 4, title: "Old title", description: "Old text" } as WorkV2Item
const live = { ...opened, version: 7 } as WorkV2Item

// The edit route of a machine before API 117: it decodes strictly, so any
// field outside this set is refused with invalid_request, and it compares
// expected_version with the stored item.
const OLDER_FIELDS = new Set(["expected_version", "title", "description", "acceptance_criteria"])
function olderMachineEdit(stored: WorkV2Item, body: Record<string, unknown>): { ok: true } | { ok: false; code: string } {
  const unknown = Object.keys(body).find(field => !OLDER_FIELDS.has(field))
  if (unknown) return { ok: false, code: "invalid_request" }
  if (body.expected_version !== stored.version) return { ok: false, code: "version_conflict" }
  return { ok: true }
}

for (const apiLevel of [WORK_V2_EDIT_BASE_API_LEVEL - 1, undefined]) {
  test(`a Board edit succeeds on a machine at API ${apiLevel ?? "unknown"}, which does not know base`, () => {
    const body = editWorkV2Body(live, "New title", "New text", undefined, { base: opened, overwrite: true, apiLevel })
    assert.deepEqual(olderMachineEdit(live, body), { ok: true })
    assert.deepEqual(body, { expected_version: 7, title: "New title", description: "New text" })
  })
}

test("a Board edit names its base from API 117", () => {
  const body = editWorkV2Body(live, "New title", "New text", undefined, { base: opened, apiLevel: WORK_V2_EDIT_BASE_API_LEVEL })
  assert.deepEqual(body, {
    expected_version: 4, title: "New title", description: "New text",
    base: { title: "Old title", description: "Old text" },
  })
  const kept = editWorkV2Body(live, "New title", "New text", "Done when", { base: opened, overwrite: true, apiLevel: 120 })
  assert.deepEqual(kept.base, { title: "Old title", description: "Old text", acceptance_criteria: "" })
  assert.equal(kept.overwrite_conflicts, true)
})
