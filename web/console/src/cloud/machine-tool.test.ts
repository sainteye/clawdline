import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- node's type-stripping runner resolves the source .ts file.
import { HOLD_MS, toolMovesConsole, toolPlan } from "./machine-tool.ts"

const pressed = { machineID: "linux", action: "start" as const, at: 1000 }

test("starting names where, so the console stays on every machine", () => {
  assert.equal(toolMovesConsole("start"), false)
  assert.deepEqual(toolPlan({ pressed, reading: "mac", now: 1100 }),
    { do: "elsewhere", action: "start", machine: "linux" })
})

test("a start on the machine already being read is the ordinary sheet", () => {
  assert.deepEqual(toolPlan({ pressed, reading: "linux", now: 1100 }),
    { do: "here", action: "start" })
})

test("the machine's own console features wait for the console to be on it", () => {
  for (const action of ["terminal", "voice", "work", "start_terminal"] as const) {
    assert.equal(toolMovesConsole(action), true, action)
    assert.deepEqual(toolPlan({ pressed: { ...pressed, action }, reading: "mac", now: 1100 }),
      { do: "wait" }, action)
    assert.deepEqual(toolPlan({ pressed: { ...pressed, action }, reading: "linux", now: 1100 }),
      { do: "here", action }, action)
  }
})

test("a press older than the hold is not opened over whatever is on screen now", () => {
  assert.deepEqual(toolPlan({ pressed, reading: "mac", now: 1000 + HOLD_MS + 1 }), { do: "forget" })
  assert.deepEqual(toolPlan({ pressed, reading: "linux", now: 1000 + HOLD_MS + 1 }), { do: "forget" })
  assert.deepEqual(toolPlan({ pressed, reading: "linux", now: 1000 + HOLD_MS }), { do: "here", action: "start" })
})
