import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- a `.ts` path for node's type-stripping test runner.
import { runScheduleWithForce } from "./schedule-force-run.ts"

test("schedule_active can be confirmed into exactly one forced retry", async () => {
  const calls: boolean[] = []
  const answer = await runScheduleWithForce(async (force) => {
    calls.push(force)
    if (!force) throw Object.assign(new Error("active"), { code: "schedule_active" })
    return "started"
  }, () => true)

  assert.equal(answer, "started")
  assert.deepEqual(calls, [false, true])
})

test("declining force and unrelated refusals preserve the original answer", async () => {
  for (const [code, confirm, confirmations] of [
    ["schedule_active", false, 1],
    ["over_capacity", true, 0],
  ] as const) {
    const refusal = Object.assign(new Error(code), { code })
    let asked = 0
    await assert.rejects(
      runScheduleWithForce(async () => { throw refusal }, () => { asked++; return confirm }),
      (error) => error === refusal,
    )
    assert.equal(asked, confirmations)
  }
})

test("an asynchronous force decision sends nothing until explicitly accepted", async () => {
  for (const answer of [false, true]) {
    const calls: boolean[] = []
    const refusal = Object.assign(new Error("active"), { code: "schedule_active" })
    let decide!: (value: boolean) => void
    const decision = new Promise<boolean>(resolve => { decide = resolve })
    const result = runScheduleWithForce(async force => {
      calls.push(force)
      if (!force) throw refusal
      return "started"
    }, () => decision)
    await Promise.resolve()
    assert.deepEqual(calls, [false])
    decide(answer)
    if (answer) assert.equal(await result, "started")
    else await assert.rejects(result, error => error === refusal)
    assert.deepEqual(calls, answer ? [false, true] : [false])
  }
})
