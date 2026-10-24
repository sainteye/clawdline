// The waiting card's markup: `node --test web/console/src/legacy/waiting-bridge.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see `session/order.test.ts`.
import { menuKey, waitingHTML, type WaitingModel } from "./waiting-bridge.ts"
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
  refresh: { busy: false, status: "" },
  screenOff: false,
  cancel: true,
}

test("a repeated native offer releases the previous menu hold", () => {
  const menu = { question: "Run synthetic command?", options: [{ n: 1, label: "Allow", can: true, selected: false }] }
  assert.notEqual(menuKey({ ...menu, revision: "offer-1" }), menuKey({ ...menu, revision: "offer-2" }))
  assert.equal(menuKey(menu), menuKey({ ...menu, revision: "" }))
})

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

// On 2026-10-12 a Session open through Cloud sat on a question its machine had
// read whole, while the page held only the machine list's row for it. The card
// said Clawdline "could not read its choices from the screen".
test("a waiting row that has not brought its menu yet says the choices are on their way", () => {
  const said = (html: string) =>
    [...html.matchAll(/<div class="say[^"]*"[^>]*>(?:<canvas class="spin"><\/canvas><span>)?(.*?)(?:<\/span>)?<\/div>/g)].map((m) => m[1]).join(" ")
  const arriving = waitingHTML({ ...unread, arriving: true })
  assert.ok(said(arriving).includes(nextWord("menuArrivingSay")))
  assert.ok(!said(arriving).includes(nextWord("menuUnreadSay")))
  assert.match(arriving, /data-cancel="1">/)
  assert.match(arriving, /data-screen="1">/)
})

// On 2026-10-14 a question came to a phone late and the card had no way to say
// whether anything was being read, nor to ask again. Every waiting card, read
// or not, offers "read again"; it says so while it reads; the bring-forward button (never
// live here) and the close button (which left a question unanswerable from
// the page) are gone.
test("every waiting card offers read again, and neither the bring-forward button nor a close button", () => {
  const rows = [{ n: 1, label: "Tea" }] as WaitingModel["rows"]
  for (const html of [waitingHTML(unread), waitingHTML({ ...unread, rows, question: "Tea?" })]) {
    assert.match(html, /<button type="button" class="go" data-refresh="1">/)
    assert.ok(html.includes(nextWord("menuReread")))
    assert.doesNotMatch(html, /data-focus|data-dismiss/)
  }
  const busy = waitingHTML({ ...unread, refresh: { busy: true, status: "" } })
  assert.match(busy, /data-refresh="1" disabled aria-busy="true">/)
  assert.ok(busy.includes(nextWord("menuRereading")))
  const done = waitingHTML({ ...unread, refresh: { busy: false, status: "read" } })
  assert.match(done, /data-refresh-status="1"[^>]*>read</)
})

test("choices on their way are drawn with the pending spinner", () => {
  assert.match(waitingHTML({ ...unread, arriving: true }), /class="say pending-state"[^>]*><canvas class="spin">/)
  assert.doesNotMatch(waitingHTML(unread), /canvas class="spin"/)
})
