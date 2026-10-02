// The waiting card's markup: `node --test web/console/src/legacy/waiting-bridge.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see `session/order.test.ts`.
import { waitingHTML, type WaitingModel } from "./waiting-bridge.ts"
// @ts-expect-error -- a `.ts` path, for node; see `session/order.test.ts`.
import { nextWord } from "../next-strings.ts"

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
  const said = [...html.matchAll(/<div class="say">(.*?)<\/div>/g)].map((m) => m[1]).join(" ")
  assert.ok(said.length > 0)
  assert.doesNotMatch(said, /\bMac\b/, "the card's sentences name the machine as a machine")
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

// On 2026-10-02 a terminal that answered nothing for two minutes left the card
// saying Clawdline "could not read the choices from the screen", as though a
// screen had been read and not understood. No screen at all is said as that.
test("a waiting row with no screen says the terminal is not answering, not that the menu was unreadable", () => {
  const blind = waitingHTML({ ...unread, screen: "unavailable" })
  const read = waitingHTML({ ...unread, screen: "read" })
  const said = (html: string) => [...html.matchAll(/<div class="say">(.*?)<\/div>/g)].map((m) => m[1]).join(" ")
  assert.notEqual(said(blind), said(read))
  assert.ok(!said(blind).includes(nextWord("menuUnreadSay")))
  assert.ok(said(read).includes(nextWord("menuUnreadSay")))
  // The way out stays the same either way.
  assert.match(blind, /data-cancel="1">/)
})

test("choices taken from the conversation record say so above their buttons", () => {
  const rows = [
    { n: 1, label: "Tea", can: true, selected: false },
    { n: 2, label: "Coffee", can: true, selected: false },
  ] as WaitingModel["rows"]
  const inferred = waitingHTML({ ...unread, rows, question: "Tea or coffee?", screen: "unavailable", inferred: true })
  const seen = waitingHTML({ ...unread, rows, question: "Tea or coffee?", screen: "read" })
  assert.match(inferred, /data-key="1"/)
  assert.match(inferred, /data-source="transcript"/)
  assert.doesNotMatch(seen, /data-source/)
})
