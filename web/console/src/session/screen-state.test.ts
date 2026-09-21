import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for Node's type-strip runner.
import { screenFailureWord } from "./screen-state.ts"

test("a gone session, read-only page, uncarried screen, and lost transport give different next steps", () => {
  const refused = (code: string) => ({ phase: "refused" as const, error: { code } })
  const words = [
    screenFailureWord(refused("session_not_found")),
    screenFailureWord(refused("forbidden")),
    screenFailureWord(refused("cloud_not_carried")),
    screenFailureWord({ phase: "unanswered", error: new TypeError("offline") }),
  ]
  assert.deepEqual(words, ["screenSessionGone", "screenForbidden", "screenCloudNotCarried", "screenUnanswered"])
  assert.equal(new Set(words).size, 4)
})
