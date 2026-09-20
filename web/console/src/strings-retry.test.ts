// The string catalog's later attempts: `node --test web/console/src/strings-retry.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see `session/order.test.ts`.
import { RETRY_DELAYS_MS, retrying } from "./strings-retry.ts"

/** A clock that never waits, and records what it was asked to wait for. */
function clock() {
  const waited: number[] = []
  return { waited, sleep: async (ms: number) => void waited.push(ms) }
}

test("the words arrive on a later attempt, and the caller is handed them", async () => {
  const time = clock()
  let tries = 0
  const got: string[] = []
  const done = await retrying(
    async () => {
      tries += 1
      if (tries < 3) throw new Error("offline")
      return "words"
    },
    (value: string) => got.push(value),
    { delays: [1, 2, 3, 4], sleep: time.sleep },
  )
  assert.equal(done, true)
  assert.deepEqual(got, ["words"])
  assert.equal(tries, 3)
  // It waited before each attempt and stopped the moment one worked.
  assert.deepEqual(time.waited, [1, 2, 3])
})

test("every attempt failing is an answer, not a throw", async () => {
  const time = clock()
  let tries = 0
  const done = await retrying(
    async () => {
      tries += 1
      throw new Error("offline")
    },
    () => assert.fail("nothing was taken"),
    { delays: [1, 2], sleep: time.sleep },
  )
  assert.equal(done, false)
  assert.equal(tries, 2)
})

test("the delays climb, and the last one is far enough out to still be there", () => {
  assert.ok(RETRY_DELAYS_MS.length >= 4)
  for (let i = 1; i < RETRY_DELAYS_MS.length; i++) {
    assert.ok(RETRY_DELAYS_MS[i] > RETRY_DELAYS_MS[i - 1], "delay " + i)
  }
  assert.ok(RETRY_DELAYS_MS[0] <= 5_000, "a blip is over in seconds")
  assert.ok(RETRY_DELAYS_MS[RETRY_DELAYS_MS.length - 1] >= 600_000, "a home-screen page outlives a tunnel")
})
