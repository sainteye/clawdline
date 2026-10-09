import test from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { appendedReadFrom, FOLLOW_MS, FOLLOW_WINDOW_MS, joinAppended, MERGED_ROWS_LIMIT, SAFETY_MS, BLIND_MS, transcriptPace } from "./transcript-follow.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { SharedTranscripts } from "./transcript-share.ts"

import type { TranscriptPage } from "@clawdline/contract"

const page = (entries: string[], nextAfter?: number, signature = "1-1"): TranscriptPage => ({
  id: "s",
  evidence: "transcript",
  signature,
  entries: entries.map((text) => ({ role: "assistant", text })),
  ...(nextAfter === undefined ? {} : { nextAfter }),
})

test("the pace: fast for 90 s after the newest send, then the safety read; the old pace for a row with no time", () => {
  const sent = 1_000_000
  assert.equal(transcriptPace({ following: true, newestSendAt: sent, now: sent + 1_000, rowTimed: true }), FOLLOW_MS)
  assert.equal(transcriptPace({ following: true, newestSendAt: sent, now: sent + FOLLOW_WINDOW_MS, rowTimed: true }), SAFETY_MS)
  assert.equal(transcriptPace({ following: false, newestSendAt: 0, now: sent, rowTimed: true }), SAFETY_MS)
  assert.equal(transcriptPace({ following: false, newestSendAt: 0, now: sent, rowTimed: false }), BLIND_MS)
  assert.equal(SAFETY_MS, 30_000)
  assert.equal(FOLLOW_MS, 2_000)
})

test("an appended read needs a held page that said where it ended, and is never the safety read", () => {
  const held = page(["a"], 120)
  assert.equal(appendedReadFrom(null, "poke", false), null)
  assert.equal(appendedReadFrom(page(["a"]), "poke", false), null, "an old daemon's page is read whole")
  assert.equal(appendedReadFrom({ ...held, evidence: "none" as const }, "poke", false), null)
  assert.equal(appendedReadFrom(held, "poke", false), 120)
  assert.equal(appendedReadFrom(held, "visible", false), 120)
  assert.equal(appendedReadFrom(held, "timer", false), null, "the safety read is whole")
  assert.equal(appendedReadFrom(held, "timer", true), 120, "the fast reads behind a card are appended")
  assert.equal(appendedReadFrom(held, "retry", false), null, "a person's retry reads whole")
})

test("an appended read joins at the newest end and keeps the held page's older cursor", () => {
  const held = { ...page(["a", "b"], 120), nextBefore: 40 }
  const joined = joinAppended(held, page(["c"], 180, "2-2"))!
  assert.deepEqual(joined.entries.map((e) => e.text), ["a", "b", "c"])
  assert.equal(joined.nextAfter, 180)
  assert.equal(joined.signature, "2-2")
  assert.equal(joined.nextBefore, 40)
})

test("an appended read with nothing new is the held page itself, so nothing is drawn again", () => {
  const held = page(["a"], 120)
  assert.equal(joinAppended(held, page([], 120)), held)
})

test("a page that would grow past its bound, or an answer without an end, is read whole", () => {
  const big = page(Array.from({ length: MERGED_ROWS_LIMIT }, (_, i) => String(i)), 9)
  assert.equal(joinAppended(big, page(["x"], 10)), null)
  assert.equal(joinAppended(page(["a"], 1), page(["b"])), null)
  assert.equal(joinAppended(page(["a"], 1), { ...page(["b"], 2), evidence: "none" as const }), null)
})

test("the shared page is the pane's, and is gone when the pane stops reading", () => {
  const shared = new SharedTranscripts()
  let heard = 0
  const off = shared.subscribe(() => heard++)
  const p = page(["a"], 1)
  shared.publish("s", p)
  shared.publish("s", p)
  assert.equal(heard, 1, "the same page twice is one change")
  assert.equal(shared.get("s"), p)
  assert.equal(shared.get("t"), null)
  shared.forget("s")
  assert.equal(shared.get("s"), null)
  assert.equal(heard, 2)
  off()
  shared.publish("s", p)
  assert.equal(heard, 2)
})

test("a row a whole read already showed is not shown twice when the appended read returns it again", () => {
  const held = page(["a", "b"], 100)
  const joined = joinAppended(held, page(["b", "c"], 160))!
  assert.deepEqual(joined.entries.map((e) => e.text), ["a", "b", "c"])
})
