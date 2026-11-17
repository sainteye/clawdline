import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node loads the TypeScript source directly.
import { guardCodeNavigation, mayLeaveCode } from "./navigation-guard.ts"

test("leaving a one-time code waits for the actual answer", async () => {
  for (const answer of [true, false]) {
    let decide!: (answer: boolean) => void
    const release = guardCodeNavigation(() => new Promise<boolean>(resolve => { decide = resolve }))
    try {
      let finished = false
      const leaving = mayLeaveCode().then((result: boolean) => { finished = true; return result })
      await Promise.resolve()
      assert.equal(finished, false)
      decide(answer)
      assert.equal(await leaving, answer)
    } finally { release() }
  }
  assert.equal(await mayLeaveCode(), true)
})

test("an unmounted or replaced code cannot approve stale navigation", async () => {
  let decide!: (answer: boolean) => void
  const release = guardCodeNavigation(() => new Promise<boolean>(resolve => { decide = resolve }))
  const leaving = mayLeaveCode()
  release()
  const next = guardCodeNavigation(() => false)
  try {
    decide(true)
    assert.equal(await leaving, false)
    release()
    assert.equal(await mayLeaveCode(), false)
  } finally { next() }
})
