import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node executes the source test with type stripping.
import { HistoryFill, HISTORY_FILL_PAGES, historyAdvanced } from "./history-fill.ts"

const empty = { cursor: 100, contentHeight: 600, viewportHeight: 600, loading: false, failed: false }

test("an empty or compact page fills without a gesture, including an exact viewport fit", () => {
  const fill = new HistoryFill()
  assert.equal(fill.needed(empty), true)
  assert.equal(fill.needed({ ...empty, contentHeight: 100 }), true)
  assert.equal(fill.needed({ ...empty, contentHeight: 601 }), false)
})

test("sparse windows advance automatically until the viewport fills", () => {
  const fill = new HistoryFill()
  const requested: number[] = []
  let state = { ...empty, cursor: 500 }
  const pages = [400, 300, 200, 100]
  while (fill.needed(state)) {
    requested.push(state.cursor)
    fill.begin(true)
    const next = pages.shift()!
    assert.equal(historyAdvanced(state.cursor, next), true)
    state = { ...state, cursor: next, contentHeight: requested.length === 3 ? 900 : 600 }
  }
  assert.deepEqual(requested, [500, 400, 300])
})

test("polling and resizes cannot restart an exhausted fill; a manual request can", () => {
  const fill = new HistoryFill()
  for (let i = 0; i < HISTORY_FILL_PAGES; i++) {
    assert.equal(fill.needed(empty), true)
    fill.begin(true)
  }
  assert.equal(fill.needed(empty), false)
  assert.equal(fill.needed({ ...empty, viewportHeight: 900 }), false)
  fill.begin(false)
  assert.equal(fill.reads, 1)
  assert.equal(fill.needed(empty), true)
})

test("in-flight reads, failures, exhausted history and hidden viewports stop automatic reads", () => {
  const fill = new HistoryFill()
  for (const change of [{ loading: true }, { failed: true }, { cursor: 0 }, { cursor: undefined }, { viewportHeight: 0 }]) {
    assert.equal(fill.needed({ ...empty, ...change }), false)
  }
  assert.equal(fill.reads, 0)
})

test("repeated, forward and invalid cursors stop; an exhausted cursor is allowed", () => {
  assert.equal(historyAdvanced(100, 100), false)
  assert.equal(historyAdvanced(100, 101), false)
  assert.equal(historyAdvanced(100, -1), false)
  assert.equal(historyAdvanced(100, 90), true)
  assert.equal(historyAdvanced(100, 0), true)
  assert.equal(historyAdvanced(100, undefined), true)
})

test("opening another Session starts a fresh fill budget", () => {
  const previous = new HistoryFill()
  for (let i = 0; i < HISTORY_FILL_PAGES; i++) previous.begin(true)
  assert.equal(previous.needed(empty), false)
  assert.equal(new HistoryFill().needed(empty), true)
})


test("sixteen empty windows do not expose an incomplete conversation", () => {
  const fill = new HistoryFill()
  let state = { ...empty, cursor: 10000 }
  let reads = 0
  while (fill.needed(state)) {
    fill.begin(true)
    reads++
    state = { ...state, cursor: state.cursor - 100, contentHeight: reads === 17 ? 900 : 600 }
  }
  assert.equal(reads, 17)
  assert.equal(state.contentHeight, 900)
})
