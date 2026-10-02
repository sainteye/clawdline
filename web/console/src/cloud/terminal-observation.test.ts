import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- the focused runner bundles TypeScript before Node executes it.
import { TerminalObservation, terminalStageLine } from "./terminal-observation.ts"

function clock(...times: number[]): () => number {
  let i = 0
  return () => times[Math.min(i++, times.length - 1)]!
}

test("receipt stages correlate without exposing channel identifiers or payloads", () => {
  const rows: unknown[] = []
  const observation = new TerminalObservation((row) => rows.push(row), clock(1000, 1003, 1010.4, 1012, 1012))
  const connection = "abcdefghijklmnopqrstuvwxyz"
  const requestID = "22222222-2222-4222-8222-222222222222"
  observation.record("raw_received", { connection, channel: "termr" })
  observation.record("envelope_opened", { connection, channel: "termr", requestID, operation: "capture" })
  observation.record("pending_match", { connection, requestID, operation: "capture" })
  observation.record("session_settled", { connection, requestID, operation: "capture" })
  assert.deepEqual(rows, [
    { seq: 1, ms: 3, stage: "raw_received", connection: 1, channel: "termr" },
    { seq: 2, ms: 10, stage: "envelope_opened", connection: 1, request: 1, channel: "termr", operation: "capture" },
    { seq: 3, ms: 12, stage: "pending_match", connection: 1, request: 1, operation: "capture" },
    { seq: 4, ms: 12, stage: "session_settled", connection: 1, request: 1, operation: "capture" },
  ])
  assert.equal(JSON.stringify(rows).includes(connection), false)
  assert.equal(JSON.stringify(rows).includes(requestID), false)
  assert.equal(observation.stopped(), null)
})

test("the default console line is one string with every field in a fixed order", () => {
  const lines: unknown[][] = []
  const info = console.info
  console.info = (...args: unknown[]) => { lines.push(args) }
  try {
    const observation = new TerminalObservation(undefined, clock(0, 42))
    observation.record("envelope_rejected", { connection: "c", channel: "termr", code: "terminal_bad_key" })
  } finally { console.info = info }
  assert.deepEqual(lines, [["cloud terminal stage seq=1 t=+42ms stage=envelope_rejected phase=verify_decrypt conn=1 req=- ch=termr op=- code=terminal_bad_key"]])
  assert.equal(terminalStageLine({ seq: 2, ms: 0, stage: "receipt_timeout", request: 3, operation: "input" }),
    "cloud terminal stage seq=2 t=+0ms stage=receipt_timeout phase=receipt_timeout conn=- req=3 ch=- op=input code=-")
})

test("the copyable text names the first stage that failed", () => {
  const observation = new TerminalObservation(() => {}, clock(0))
  observation.record("request_pending", { connection: "c", requestID: "r", operation: "capture" })
  observation.record("raw_received", { connection: "c", channel: "termr" })
  observation.record("pending_miss", { connection: "c", requestID: "other", operation: "capture", code: "request_unknown" })
  observation.record("receipt_timeout", { connection: "c", requestID: "r", operation: "capture" })
  const text = observation.text().split("\n")
  assert.match(text[0]!, /^cloud terminal diagnostics started=\S+ rows=4\/128$/)
  assert.equal(text[1], "stopped phase=pending_match stage=pending_miss code=request_unknown seq=3")
  assert.equal(text.length, 6)
})

test("observation is bounded during a broken stream", () => {
  const rows: unknown[] = []
  const observation = new TerminalObservation((row) => rows.push(row))
  for (let i = 0; i < 200; i++) observation.record("envelope_rejected", { connection: "same", code: "terminal_bad_key" })
  assert.equal(rows.length, 128)
  assert.equal(observation.text().split("\n").length, 130)
})

test("a broken diagnostic sink or clock cannot interrupt terminal delivery", () => {
  const observation = new TerminalObservation(() => { throw new Error("logging failed") })
  assert.doesNotThrow(() => observation.record("raw_received", { connection: "same", channel: "termr" }))
  let calls = 0
  const broken = new TerminalObservation(() => {}, () => { if (calls++) throw new Error("clock failed"); return 0 })
  assert.doesNotThrow(() => broken.record("raw_received", { connection: "same", channel: "termr" }))
})

test("a turned-off timeline keeps and writes nothing", () => {
  const observation = new TerminalObservation(null)
  observation.record("envelope_rejected", { connection: "same", code: "terminal_bad_key" })
  assert.equal(observation.stopped(), null)
  assert.match(observation.text(), /rows=0\/128\nstopped phase=-$/)
})

test("unexpected error text and operation values are redacted", () => {
  const rows: unknown[] = []
  const observation = new TerminalObservation((row) => rows.push(row), clock(0))
  observation.record("envelope_rejected", { code: "secret: terminal contents", operation: "secret content" })
  assert.deepEqual(rows, [{ seq: 1, ms: 0, stage: "envelope_rejected", operation: "unrecognized", code: "terminal_receive_failed" }])
})
