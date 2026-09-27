// The swipe's rules, without a browser:
//
//   node --test web/console/src/session/swipe.test.ts
//
// What a finger does to a row is held here; what a browser does with the
// events is held in `list-gestures.e2e.ts`, because only a browser can answer
// whether a passive listener got the gesture at all.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see `order.test.ts`.
import { ACTION_WIDTH, actionWidth, archivable, BOTH_WIDTH, BUTTON_WIDTH, offsetFor, revealFor, settleOpen, Swipes, type RevealWords } from "./swipe.ts"

const WORDS: RevealWords = {
  end: "關閉 Session",
}

test("a row only moves the way there is something to uncover", () => {
  assert.equal(offsetFor(0, 0), 0)
  assert.equal(offsetFor(0, 40), 0, "pulling right on a closed row uncovers nothing")
  assert.equal(offsetFor(0, -40), 40)
  assert.equal(offsetFor(ACTION_WIDTH, 40), ACTION_WIDTH - 40, "an open row closes as it is pulled back")
  assert.equal(offsetFor(ACTION_WIDTH, 400), 0)
})

test("past the action's own width the row gives and then stops", () => {
  const far = offsetFor(0, -(ACTION_WIDTH + 200))
  assert.ok(far > ACTION_WIDTH, "it gives: " + far)
  assert.ok(far < ACTION_WIDTH + 40, "and it is a wall, not a hole: " + far)
})

test("letting go settles by how far it got, and a flick settles by which way it went", () => {
  assert.equal(settleOpen(ACTION_WIDTH * 0.2, 0), false)
  assert.equal(settleOpen(ACTION_WIDTH * 0.8, 0), true)
  assert.equal(settleOpen(6, -2), true, "a fast flick left opens it however short")
  assert.equal(settleOpen(ACTION_WIDTH - 4, 2), false, "a fast flick right closes it however far")
})

test("the first real movement decides whose gesture it is, and nothing revisits it", () => {
  const s = new Swipes()
  s.begin("a", 300, 400, 0)
  assert.equal(s.move(297, 403, 16), "undecided", "a finger that has barely moved has said nothing")
  assert.equal(s.move(300, 440, 32), "list", "downwards is the scroller's: pull to refresh")
  assert.equal(s.move(200, 440, 48), "list", "and it stays the scroller's for the rest of the gesture")
  assert.equal(s.offsetOf("a"), 0, "so the row never moved")
})

test("a drag to the left is the row's, and the scroller is told to keep out", () => {
  const s = new Swipes()
  s.begin("a", 300, 400, 0)
  assert.equal(s.move(280, 402, 16), "row")
  assert.equal(s.move(200, 404, 32), "row")
  assert.equal(s.offsetOf("a"), 100)
  assert.equal(s.stateOf("a"), "dragging")
  assert.deepEqual(s.end(), { id: "a", open: true })
  assert.equal(s.stateOf("a"), "open")
  assert.equal(s.offsetOf("a"), ACTION_WIDTH)
})

test("a drag to the right on a closed row is the scroller's, not a swipe backwards", () => {
  const s = new Swipes()
  s.begin("a", 100, 400, 0)
  assert.equal(s.move(160, 402, 16), "list")
  assert.equal(s.end(), null)
})

test("an open row is pulled shut by the same gesture the other way", () => {
  const s = new Swipes()
  s.begin("a", 300, 400, 0)
  s.move(200, 400, 16)
  s.end()
  s.begin("a", 200, 400, 100)
  assert.equal(s.move(300, 402, 116), "row", "an open row owns a rightward drag too")
  assert.equal(s.offsetOf("a"), 26)
  assert.deepEqual(s.end(), { id: "a", open: false })
  assert.equal(s.stateOf("a"), "")
})

test("one row is open at a time, and the press that closes one opens nothing", () => {
  const s = new Swipes()
  s.begin("a", 300, 400, 0)
  s.move(180, 400, 16)
  s.end()
  assert.equal(s.openId(), "a")
  assert.equal(s.begin("b", 300, 500, 200), true, "the press was spent closing the open row")
  assert.equal(s.openId(), null)
  assert.equal(s.tookThePress(), true)
  assert.equal(s.tookThePress(), false, "asked once, answered once")
})

test("a gesture that moved the row is not a press on the row", () => {
  const s = new Swipes()
  s.begin("a", 300, 400, 0)
  s.move(180, 400, 16)
  s.end()
  assert.equal(s.tookThePress(), true, "the swipe is not also a tap that opens the session")
})

test("a tap that moves nothing is left alone, so a row still opens on a press", () => {
  const s = new Swipes()
  assert.equal(s.begin("a", 300, 400, 0), false)
  assert.equal(s.move(301, 401, 16), "undecided")
  assert.equal(s.end(), null)
  assert.equal(s.tookThePress(), false)
})

test("a session that is provably safe to close is offered the close", () => {
  const reveal = revealFor({ state: "safe", reasons: [] }, WORDS)
  assert.deepEqual(reveal, { state: "safe", word: WORDS.end })
})

test("a session with an obligation still uncovers the close action", () => {
  const reveal = revealFor(
    { state: "blocked", reasons: [{ kind: "obligation", code: "terminal_working" }] },
    WORDS,
  )
  assert.equal(reveal.state, "blocked")
  assert.equal(reveal.word, WORDS.end)
})

test("a session nobody could read still uncovers the close action", () => {
  const reveal = revealFor(
    { state: "unknown", reasons: [{ kind: "evidence", code: "session_identity_ambiguous" }] },
    WORDS,
  )
  assert.equal(reveal.state, "unknown")
  assert.equal(reveal.word, WORDS.end)
})

test("a reading that has not been attested still uncovers the close action", () => {
  const reveal = revealFor(
    { state: "needs_attestation", reasons: [{ kind: "attestation", code: "not_attested" }] },
    WORDS,
  )
  assert.equal(reveal.state, "needs_attestation")
  assert.equal(reveal.word, WORDS.end)
})

test("an unrecognised state keeps an unknown status behind the same close action", () => {
  const reveal = revealFor({ state: "", reasons: [] }, WORDS)
  assert.equal(reveal.state, "unknown")
  assert.equal(reveal.word, WORDS.end)
})

test("a row that goes away takes its open action with it", () => {
  const s = new Swipes()
  s.begin("a", 300, 400, 0)
  s.move(180, 400, 16)
  s.end()
  let told = 0
  const stop = s.subscribe(() => told++)
  s.closeOpen()
  assert.equal(s.openId(), null)
  assert.equal(told, 1, "and the list is told once, so it redraws")
  stop()
})

// Archive (docs/session-archive.md): a row whose conversation can be brought
// back uncovers two buttons, 封存 beside 關閉; a row with nothing to resume
// keeps the one close it had.

test("a row with a conversation uncovers two thumb-sized buttons side by side", () => {
  assert.equal(archivable("conv-1"), true)
  assert.equal(actionWidth("conv-1"), BOTH_WIDTH)
  assert.equal(BOTH_WIDTH, 2 * BUTTON_WIDTH, "two buttons, the same width each")
  assert.ok(BUTTON_WIDTH >= 72, "each one still takes a thumb: " + BUTTON_WIDTH)
  assert.ok(BOTH_WIDTH > ACTION_WIDTH, "wider than the close alone")
})

test("a row with no conversation id offers no archive, and keeps the close's own width", () => {
  for (const none of ["", "   ", undefined, null]) {
    assert.equal(archivable(none), false, JSON.stringify(none))
    assert.equal(actionWidth(none), ACTION_WIDTH, JSON.stringify(none))
  }
})

test("the wider action stretches past its own edge, not the close's", () => {
  assert.equal(offsetFor(0, -150, BOTH_WIDTH), 150, "150 of 176 is still inside the two buttons")
  const far = offsetFor(0, -(BOTH_WIDTH + 200), BOTH_WIDTH)
  assert.ok(far > BOTH_WIDTH && far < BOTH_WIDTH + 40, "it gives, then stops: " + far)
  assert.equal(offsetFor(BOTH_WIDTH, 400, BOTH_WIDTH), 0)
})

test("letting go measures how far it got against the row's own width", () => {
  // 70px is past the 45% line of the close alone and short of it for two.
  assert.equal(settleOpen(70, 0), true)
  assert.equal(settleOpen(70, 0, BOTH_WIDTH), false)
  assert.equal(settleOpen(100, 0, BOTH_WIDTH), true)
})

test("a row dragged open with two buttons rests with both uncovered", () => {
  const s = new Swipes()
  s.begin("a", 300, 400, 0, BOTH_WIDTH)
  s.move(280, 400, 16)
  s.move(150, 400, 32)
  assert.equal(s.offsetOf("a"), 150)
  assert.deepEqual(s.end(), { id: "a", open: true })
  assert.equal(s.offsetOf("a"), BOTH_WIDTH, "open means both buttons, not the close's 126")
  s.begin("a", 150, 400, 100, BOTH_WIDTH)
  s.move(170, 400, 116)
  assert.equal(s.offsetOf("a"), BOTH_WIDTH - 20, "a drag on an open row starts from its own width")
})
