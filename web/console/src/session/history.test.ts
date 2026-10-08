import { test } from "node:test"
import assert from "node:assert/strict"
import type { TranscriptEntry } from "@clawdline/contract"
// @ts-expect-error -- Node executes the source test with type stripping.
import { joinTranscript } from "./history.ts"

const entry = (text: string): TranscriptEntry => ({ role: "user", text })

test("older messages stay visible when the polled newest window moves", () => {
  const retained = ["one", "two", "three", "four"].map(entry)
  const latest = ["three", "four", "five", "six"].map(entry)
  assert.deepEqual(joinTranscript(retained, latest).map((row) => row.text),
    ["one", "two", "three", "four", "five", "six"])
})

test("a repeated sentence remains when it is a distinct turn", () => {
  const retained = ["yes", "done", "yes"].map(entry)
  const latest = ["done", "yes", "new"].map(entry)
  assert.deepEqual(joinTranscript(retained, latest).map((row) => row.text),
    ["yes", "done", "yes", "new"])
})
