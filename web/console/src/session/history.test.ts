import { test } from "node:test"
import assert from "node:assert/strict"
import type { TranscriptEntry, TranscriptPage } from "@clawdline/contract"
// @ts-expect-error -- Node executes the source test with type stripping.
import { joinTranscript, pendingHistoryPages } from "./history.ts"

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


test("an unfinished tool outside the newest window is reread by its exact page cursor", () => {
  const tool: TranscriptEntry = { id: "acp2:g:1", role: "tool", text: "Read file", activity: { kind: "called", actions: [], durationMs: -1, status: "inProgress" } }
  const old: TranscriptPage = { id: "owned", evidence: "structured", signature: "acp2:g:201", readBefore: 201, entries: [tool] }
  const newest: TranscriptPage = { ...old, readBefore: 202, signature: "acp2:g:202", entries: [{ id: "acp2:g:201", role: "assistant", text: "new" }] }
  assert.deepEqual(pendingHistoryPages({ base: newest, pages: [old] }, newest).map((page) => page.readBefore), [201])
  const completed: TranscriptPage = { ...old, signature: "acp2:g:203", entries: [{ ...tool, activity: { ...tool.activity!, status: "completed", result: "complete result" } }] }
  const retained = joinTranscript(completed.entries, newest.entries)
  assert.equal(retained[0].activity?.result, "complete result")
  assert.deepEqual(pendingHistoryPages({ base: newest, pages: [completed] }, newest), [])
  // A revision arriving during a reread still selects the unfinished page again.
  const stillRunning = { ...old, signature: "acp2:g:202" }
  assert.deepEqual(pendingHistoryPages({ base: newest, pages: [stillRunning] }, { ...newest, signature: "acp2:g:203" }), [stillRunning])
  assert.deepEqual(pendingHistoryPages({ base: newest, pages: [old] }, { ...newest, signature: "acp2:replacement:204" }), [])
})


test("a larger completed tool preserves the already-read middle-page coverage", () => {
  const held: TranscriptEntry[] = Array.from({ length: 200 }, (_, i) => ({ id: "acp2:g:" + (201 + i), role: "user", text: "same" }))
  held[199] = { id: "acp2:g:400", role: "tool", text: "Read file", activity: { kind: "called", actions: [], durationMs: -1, status: "inProgress" } }
  const shorter = held.slice(61)
  shorter[138] = { ...held[199], activity: { ...held[199].activity!, status: "completed", result: "x".repeat(900000) } }
  const merged = joinTranscript(held, shorter)
  assert.equal(merged.length, 200)
  assert.equal(merged[0].id, "acp2:g:201")
  assert.equal(merged[199].activity?.result?.length, 900000)
})
