// What an open Session's attention panel reads, and what its head draws
// between reads: `node --test web/console/src/session/attention-reads.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see `order.test.ts`.
import { attentionReads, countMoveNeedsRead, unresolvedNoteCount } from "./attention-reads.ts"

test("a Session the row says has no notes reads none of them", () => {
  // Zero is an answer, and it came with the row: there is no page to fetch
  // and no clock to fetch it on.
  assert.deepEqual(attentionReads({ counted: true, expanded: false, count: 0 }), { page: false, lane: "none" })
})

test("a Session that has notes reads them, so opening the panel shows words", () => {
  assert.deepEqual(attentionReads({ counted: true, expanded: false, count: 2 }), { page: true, lane: "none" })
})

test("an open panel reads the notes and keeps the safety lane", () => {
  assert.deepEqual(attentionReads({ counted: true, expanded: true, count: 0 }), { page: true, lane: "safety" })
  assert.deepEqual(attentionReads({ counted: true, expanded: true, count: 2 }), { page: true, lane: "safety" })
})

test("a row with no count keeps the pace it had, open or closed", () => {
  assert.deepEqual(attentionReads({ counted: false, expanded: false, count: null }), { page: true, lane: "refresh" })
  assert.deepEqual(attentionReads({ counted: false, expanded: true, count: null }), { page: true, lane: "refresh" })
})

test("a counted row whose number has not arrived yet is read by nobody", () => {
  // The row said it counts them and has not said how many: the count is a
  // moment away on the stream, and the head draws nothing until it lands.
  assert.deepEqual(attentionReads({ counted: true, expanded: false, count: null }), { page: false, lane: "none" })
})

test("a count arriving at a closed panel that read nothing is read by nobody", () => {
  // The page opens on a Session by link, the row arrives a moment later with
  // its count, and the head draws it: there is nothing left to ask for.
  assert.equal(countMoveNeedsRead({ expanded: false, hasPage: false }), false)
})

test("an open panel reads the move, because the notes themselves are on screen", () => {
  assert.equal(countMoveNeedsRead({ expanded: true, hasPage: false }), true)
  assert.equal(countMoveNeedsRead({ expanded: true, hasPage: true }), true)
})

test("a panel that read a page refreshes it when the count moves", () => {
  // Its head is drawing that page's number, which the stream has moved past.
  assert.equal(countMoveNeedsRead({ expanded: false, hasPage: true }), true)
})

test("the head draws the row's count until this panel has read a page", () => {
  assert.equal(unresolvedNoteCount(null, 2), 2)
  assert.equal(unresolvedNoteCount(null, 0), 0)
})

test("a page this panel read wins over the row's count", () => {
  // Resolving a note empties the page at once; the row's count is a scan behind.
  assert.equal(unresolvedNoteCount(0, 1), 0)
})

test("no page and no count is nobody having said yet", () => {
  assert.equal(unresolvedNoteCount(null, null), null)
})
