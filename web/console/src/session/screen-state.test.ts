import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for Node's type-strip runner.
import { screenFailureWord } from "./screen-state.ts"

test("a gone session, read-only page, and lost transport give different next steps", () => {
  const refused = (code: string) => ({ phase: "refused" as const, error: { code } })
  const words = [
    screenFailureWord(refused("session_not_found")),
    screenFailureWord(refused("forbidden")),
    screenFailureWord({ phase: "unanswered", error: new TypeError("offline") }),
  ]
  assert.deepEqual(words, ["screenSessionGone", "screenForbidden", "screenUnanswered"])
  assert.equal(new Set(words).size, 3)
  // Clawdline Cloud carries the screen now, so its old refusal is no longer a
  // screen of its own: were one to arrive, the shared failure sentence says it.
  assert.equal(screenFailureWord(refused("cloud_not_carried")), null)
})
