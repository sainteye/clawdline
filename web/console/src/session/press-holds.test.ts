// A waiting card's press, from the tap until its question has gone: `node --test web/console/src/session/*.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see `order.test.ts`.
import { PressHolds, PRESS_SETTLED_MS } from "./press-holds.ts"

const T0 = 1_789_800_000_000

// F1(a), F3: the press went and nothing came back — the relay dropped, the
// answer was lost. It may have answered the question; if it did, the next
// question is up on the Mac and this card is still drawing the old one.
// Opening the options again is how the next question gets answered by a
// second tap. So they stay shut, saying it is not known, until the row moves.
test("a press that may have landed keeps the options shut while the same question is up", () => {
  const holds = new PressHolds()
  const press = holds.start("s1", "menu-A", T0)
  holds.failed(press, "unknown")
  const held = holds.current("s1", "menu-A", true, T0 + 60_000)
  assert.equal(held?.state, "unknown", "the options were opened again under a press that may have answered")
  // The row moved on: that is the answer to what happened.
  assert.equal(holds.current("s1", "menu-B", true, T0 + 61_000), null)
})

test("a press refused before anything was typed opens the options again at once", () => {
  const holds = new PressHolds()
  const press = holds.start("s1", "menu-A", T0)
  holds.failed(press, "not_done")
  assert.equal(holds.current("s1", "menu-A", true, T0 + 1), null)
})

// F1(b): switching to another session and back must not open the options
// under a press still on its way.
test("a press is held for its own session while another one is being read", () => {
  const holds = new PressHolds()
  holds.start("s1", "menu-A", T0)
  assert.equal(holds.current("s2", "menu-X", true, T0 + 100), null, "the other session has no press of its own")
  assert.equal(holds.current("s1", "menu-A", true, T0 + 200)?.state, "sending", "the press was dropped on the way back")
})

test("an answered press holds its question for a while, then the options are the row's again", () => {
  const holds = new PressHolds()
  const press = holds.start("s1", "menu-A", T0)
  holds.settled(press, T0 + 500)
  assert.equal(holds.current("s1", "menu-A", true, T0 + 1_000)?.state, "sent")
  assert.equal(holds.current("s1", "menu-A", true, T0 + 500 + PRESS_SETTLED_MS + 1), null)
})

test("choosing again is a person's decision, and releases an uncertain press", () => {
  const holds = new PressHolds()
  const press = holds.start("s1", "menu-A", T0)
  holds.failed(press, "unknown")
  holds.release("s1")
  assert.equal(holds.current("s1", "menu-A", true, T0 + 1), null)
})

test("every press is its own request, and a late answer does not settle a newer press", () => {
  const holds = new PressHolds()
  const first = holds.start("s1", "menu-A", T0)
  holds.failed(first, "not_done")
  const second = holds.start("s1", "menu-A", T0 + 10)
  assert.notEqual(second.request, first.request)
  holds.settled(first, T0 + 20)
  assert.equal(holds.current("s1", "menu-A", true, T0 + 30)?.state, "sending")
})

// A multi-select's rows toggle: nothing is sent until Submit, so the question
// does not move and the next tap is the next box. `menuKey` does not read a
// row's tick, so nothing else would ever release this hold — and for ten
// seconds every row and Submit with them were dead, which made "tick all three"
// impossible from a phone (2026-09-20).
test("a tick gives the card back the moment it lands, so the next box can be ticked", () => {
  const holds = new PressHolds()
  const press = holds.start("s1", "menu-A", T0, true)
  // On its way, it still shuts the rows: a second tap in flight is a second
  // digit at a question that has not answered the first one yet.
  assert.equal(holds.current("s1", "menu-A", true, T0 + 100)?.state, "sending")
  holds.settled(press, T0 + 500)
  assert.equal(holds.current("s1", "menu-A", true, T0 + 501), null, "the card was still locked after a tick landed")
})

// A tick that may have landed is not opened again either: it may have ticked,
// and a second tap would tick it back off.
test("a tick whose answer was lost keeps the rows shut, like any press", () => {
  const holds = new PressHolds()
  const press = holds.start("s1", "menu-A", T0, true)
  holds.failed(press, "unknown")
  assert.equal(holds.current("s1", "menu-A", true, T0 + 60_000)?.state, "unknown")
})

// The control: an answering press is not a tick and keeps its whole allowance.
test("an answer still holds for its ten seconds", () => {
  const holds = new PressHolds()
  const press = holds.start("s1", "menu-A", T0)
  holds.settled(press, T0 + 500)
  assert.equal(holds.current("s1", "menu-A", true, T0 + 501)?.state, "sent")
  assert.equal(holds.current("s1", "menu-A", true, T0 + 500 + PRESS_SETTLED_MS + 1), null)
})
