import { test } from "node:test"
import assert from "node:assert/strict"
import type { UpdateState, UpdateStatus } from "@clawdline/contract"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { settleUpdateRead, shortStamp, updateNotice, UPDATE_READ_EVERY_MS } from "./update-model.ts"
// @ts-expect-error -- a `.ts` path for node's type-stripping test runner.
import { nextWord } from "../next-strings.ts"

function status(state: UpdateState): UpdateStatus {
  return {
    state,
    running: { stamp: "1a2b3c4d5e6f7a8b9c0d", committed_at: "2026-10-01T00:00:00Z" },
    latest: { stamp: "4b7c3f8d9e0f1a2b3c4d", committed_at: "2026-10-04T02:31:05Z" },
    checked_at: "2026-10-04T12:40:00Z",
    source_url: "https://example.invalid/BUILD.json",
  }
}

test("only a machine that trails or differs says anything", () => {
  for (const state of ["update_available", "differs"] as const) {
    const line = updateNotice(status(state), nextWord)
    assert.ok(line, state + " said nothing")
    assert.ok(line!.includes("1a2b3c4d") && !line!.includes("1a2b3c4d5"), line!)
    assert.ok(line!.includes("4b7c3f8d") && !line!.includes("4b7c3f8d9"), line!)
    assert.ok(line!.includes("clawdline update --apply"), line!)
  }
  for (const state of ["current", "ahead", "unknown"] as const) {
    assert.equal(updateNotice(status(state), nextWord), null, state + " spoke")
  }
})

test("a refused or failed read is silent, a daemon predating the route above all", () => {
  // 404 from a daemon that has no /v1/update.
  assert.equal(settleUpdateRead(false, { error: "not_found", detail: "no route" }), null)
  // The relay's answer for a daemon that predates the `update` word.
  assert.equal(settleUpdateRead(false, { error: { code: "unknown_command" } }), null)
  assert.equal(settleUpdateRead(false, null), null)
  assert.equal(settleUpdateRead(true, null), null)
  assert.equal(settleUpdateRead(true, { state: "update_available" }), null)
  assert.equal(updateNotice(null, nextWord), null)
  const read = settleUpdateRead(true, status("update_available"))
  assert.equal(read?.state, "update_available")
  assert.ok(updateNotice(read, nextWord))
})

test("the notice reads no more often than every ten minutes, and prints eight characters", () => {
  assert.ok(UPDATE_READ_EVERY_MS >= 10 * 60 * 1000)
  assert.equal(shortStamp("4b7c3f8d9e0f"), "4b7c3f8d")
  assert.equal(shortStamp(undefined), "")
})
