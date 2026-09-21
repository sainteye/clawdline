import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for Node's strip-types test runner.
import { isRefusal, RefusalError } from "../../../core/src/refusal.ts"
import { failureSentence } from "../legacy/js/core/failure-text.js"

test("a nested named refusal stays named instead of becoming unexpected_error", () => {
  const body: unknown = { error: { code: "forbidden", message: "producer prose", request_id: "req-fixture" } }
  assert.equal(isRefusal(body), true)
  if (!isRefusal(body)) return
  const error = new RefusalError(403, body, "/v1/sessions")
  assert.equal(error.code, "forbidden")
  assert.equal(error.detail, "producer prose")
  assert.equal(error.route, "/v1/sessions")
  const said = failureSentence(error, "這一塊讀不到。")
  assert.match(said, /forbidden/)
  assert.doesNotMatch(said, /unexpected_error|producer prose/)
})

test("both refusal spellings keep their code even when prose is absent", () => {
  const flat = { error: "store_busy" }
  const nested = { error: { code: "rate_limited" } }
  assert.equal(isRefusal(flat), true)
  assert.equal(isRefusal(nested), true)
  if (!isRefusal(flat) || !isRefusal(nested)) return
  const first = new RefusalError(409, flat)
  const second = new RefusalError(429, nested)
  assert.deepEqual([first.code, first.detail], ["store_busy", "store_busy"])
  assert.deepEqual([second.code, second.detail], ["rate_limited", "rate_limited"])
})
