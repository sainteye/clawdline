import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for Node's type-strip runner.
import { readAnswer, readAnswered, readFailure, readReady, readValue } from "./read-state.ts"

test("loading, authoritative empty, refusal, and no answer remain different states", () => {
  const row = { id: "one" }
  assert.deepEqual(readAnswer([], true), { phase: "empty_authoritative", value: [] })
  assert.deepEqual(readAnswer([row], false), { phase: "ready", value: [row] })
  assert.deepEqual(readValue(readAnswer([row], false)), [row])

  const refused = Object.assign(new Error("no"), { name: "RefusalError", code: "forbidden" })
  const unanswered = Object.assign(new Error("no answer"), { name: "TransportError" })
  assert.equal(readFailure(refused).phase, "refused")
  assert.equal(readFailure(unanswered).phase, "unanswered")
  assert.equal(readFailure(Object.assign(new Error("offline"), { code: "offline" })).phase, "unanswered")
  assert.equal(readFailure(Object.assign(new Error("server"), { code: "http_500" })).phase, "refused")
  assert.equal(readValue(readFailure(unanswered)), null)
  assert.equal(readAnswered(readAnswer([], true)), true)
  assert.equal(readAnswered(readFailure(refused)), false)
  assert.equal(readReady(readAnswer([], true)), false)
  assert.equal(readReady(readAnswer([row], false)), true)
})
