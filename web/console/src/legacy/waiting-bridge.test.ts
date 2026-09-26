// The waiting card's markup: `node --test web/console/src/legacy/waiting-bridge.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see `session/order.test.ts`.
import { waitingHTML, type WaitingModel } from "./waiting-bridge.ts"

const unread: WaitingModel = {
  folded: false,
  steps: null,
  question: "",
  rows: null,
  submit: null,
  sent: false,
  pressing: null,
  write: true,
  refresh: { busy: false, status: "", off: true },
  focusOff: true,
  screenOff: false,
  cancel: true,
}

// On 2026-09-26 a picker whose choices could not be read left the card saying
// "answer this one on the Mac", on a machine that has none, with every button
// on it switched off. A card with no rows must offer a way out that needs no
// reading: an Esc, and the live screen.
test("a menu that could not be read offers its Esc and the live screen", () => {
  const html = waitingHTML(unread)
  assert.match(html, /<button type="button" class="go" data-cancel="1">/)
  assert.match(html, /<button type="button" class="go" data-screen="1">/)
  assert.doesNotMatch(html, /\bMac\b(?![^<]*<\/button>)/, "the card's sentences name no Mac")
})

test("the way out is shut where this device may not type", () => {
  const html = waitingHTML({ ...unread, cancel: false, screenOff: true })
  assert.match(html, /data-cancel="1" disabled>/)
  assert.match(html, /data-screen="1" disabled>/)
})

test("a menu that was read draws its rows and no Esc", () => {
  const html = waitingHTML({
    ...unread,
    question: "Tea or coffee?",
    rows: [
      { n: 1, label: "Tea", selected: true },
      { n: 2, label: "Coffee" },
    ] as WaitingModel["rows"],
  })
  assert.match(html, /data-key="1"/)
  assert.doesNotMatch(html, /data-cancel/)
})
