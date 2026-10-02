import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- the focused runner bundles TypeScript before Node executes it.
import { TerminalObservation } from "./terminal-observation.ts"

test("receipt stages correlate without exposing channel identifiers or payloads", () => {
  const rows: unknown[] = []
  const observation = new TerminalObservation((row) => rows.push(row))
  const connection = "abcdefghijklmnopqrstuvwxyz"
  const requestID = "22222222-2222-4222-8222-222222222222"
  observation.record("raw_received", { connection, channel: "termr" })
  observation.record("envelope_opened", { connection, channel: "termr", requestID, operation: "capture" })
  observation.record("pending_match", { connection, requestID, operation: "capture" })
  observation.record("session_settled", { connection, requestID, operation: "capture" })
  assert.deepEqual(rows, [
    { stage: "raw_received", connection: 1, channel: "termr" },
    { stage: "envelope_opened", connection: 1, request: 1, channel: "termr", operation: "capture" },
    { stage: "pending_match", connection: 1, request: 1, operation: "capture" },
    { stage: "session_settled", connection: 1, request: 1, operation: "capture" },
  ])
  assert.equal(JSON.stringify(rows).includes(connection), false)
  assert.equal(JSON.stringify(rows).includes(requestID), false)
})

test("observation is bounded during a broken stream", () => {
  const rows: unknown[] = []
  const observation = new TerminalObservation((row) => rows.push(row))
  for (let i = 0; i < 200; i++) observation.record("envelope_rejected", { connection: "same", code: "terminal_bad_key" })
  assert.equal(rows.length, 128)
})

test("a broken diagnostic sink cannot interrupt terminal delivery", () => {
  const observation = new TerminalObservation(() => { throw new Error("logging failed") })
  assert.doesNotThrow(() => observation.record("raw_received", { connection: "same", channel: "termr" }))
})

test("unexpected error text and operation values are redacted", () => {
  const rows: unknown[] = []
  const observation = new TerminalObservation((row) => rows.push(row))
  observation.record("envelope_rejected", { code: "secret: terminal contents", operation: "secret content" })
  assert.deepEqual(rows, [{ stage: "envelope_rejected", operation: "unrecognized", code: "terminal_receive_failed" }])
})
