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

test("durable entry IDs preserve identical turns and replace streamed tool results", () => {
  const retained: TranscriptEntry[] = [
    { id: "acp2:g:1", role: "user", text: "yes", at: 123 },
    { id: "acp2:g:2", role: "tool", text: "Read file", activity: { kind: "called", actions: [], durationMs: -1, status: "inProgress" } },
  ]
  const latest: TranscriptEntry[] = [
    { ...retained[1], activity: { kind: "called", actions: [], durationMs: -1, status: "completed", result: "file content" } },
    { id: "acp2:g:3", role: "user", text: "yes", at: 123 },
  ]
  const joined = joinTranscript(retained, latest)
  assert.deepEqual(joined.map((row) => row.id), ["acp2:g:1", "acp2:g:2", "acp2:g:3"])
  assert.equal(joined[1].activity?.result, "file content")
  assert.equal(joined.filter((row) => row.text === "yes").length, 2)
})
