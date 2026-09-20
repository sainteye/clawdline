// Cards that outlive the page they were sent from (F4):
// `node --test web/console/src/session/persist.test.ts`.
// The `.ts` imports are for node, as in `order.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see `order.test.ts`.
import { PendingSends, turnWords, type PendingSend, type SeenTurn } from "./pending.ts"
// @ts-expect-error -- a `.ts` path, for node; see `order.test.ts`.
import { Cards, INTERRUPTED, KEPT_CARDS, KEPT_CHARS, KEPT_MS, type CardStore } from "./persist.ts"

const T0 = 1_789_800_000_000 // ms
const sec = (ms: number) => Math.floor(ms / 1000)

function user(text: string, atMs: number, imageCount?: number): SeenTurn {
  return { role: "user", text, at: sec(atMs), ...(imageCount === undefined ? {} : { imageCount }) }
}

/** A store in memory, and one that will not have any of it. */
function memory(initial: string | null = null): CardStore & { value: string | null; writes: number } {
  return {
    value: initial,
    writes: 0,
    read() {
      return this.value
    },
    write(value: string) {
      this.value = value
      this.writes += 1
    },
  }
}

/** One page: its cards, and the keeper that writes them, wired as `send.ts` wires them. */
function page(store: CardStore | null, now = T0, daemon = "") {
  const cards = new PendingSends()
  const keeper = new Cards({ store, turnWords })
  cards.restore(keeper.restore(daemon, now))
  cards.subscribe(() => keeper.keep(cards.all(), now))
  return { cards, keeper }
}

test("a card still sending comes back after a reload, under the same request", () => {
  const store = memory()
  const first = page(store)
  const sent = first.cards.add("s1", "ship it", [], T0)

  const second = page(store, T0 + 30_000)
  const back = second.cards.of("s1")
  assert.equal(back.length, 1, "the card is on the page again")
  assert.equal(back[0].text, "ship it", "the words are the words")
  assert.equal(back[0].request, sent.request, "and the request is the same one, so a second attempt is not a second message")
  // Nothing answered it and nothing ever will: the request went with the page.
  assert.equal(back[0].state, "unknown")
  assert.equal(back[0].failure, INTERRUPTED)
  assert.equal(back[0].absent, false, "an earlier page's read is not this page's read")
  assert.equal(back[0].checking, false)
})

test("the turn it stands for takes a restored card away, and nothing was sent to find that out", () => {
  const store = memory()
  page(store).cards.add("s1", "ship it", [], T0)
  const second = page(store, T0 + 30_000)
  assert.equal(second.cards.of("s1").length, 1)
  // What `Transcript.tsx` does with every read it already makes.
  second.cards.reconcile("s1", [user("ship it", T0 + 2_000)], T0 + 30_000)
  assert.equal(second.cards.of("s1").length, 0, "the message is in the conversation, so the card is not")
})

test("a read that shows no turn leaves a restored card saying it does not know", () => {
  const store = memory()
  page(store).cards.add("s1", "ship it", [], T0)
  const second = page(store, T0 + 30_000)
  second.cards.reconcile("s1", [user("something else", T0 + 2_000)], T0 + 30_000)
  const back = second.cards.of("s1")
  assert.equal(back.length, 1)
  assert.equal(back[0].state, "unknown", "still unknown; nothing here sends it again on its own")
  assert.equal(back[0].absent, false, "only a person's look says that")
})

test("a turn that was already there before the send does not settle a restored card", () => {
  const store = memory()
  const first = page(store)
  // Somebody said the same thing a minute ago, and the page had read it.
  const earlier = [user("ok", T0 - 60_000)]
  first.cards.reconcile("s1", earlier, T0)
  first.cards.add("s1", "ok", [], T0)

  const second = page(store, T0 + 30_000)
  second.cards.reconcile("s1", earlier, T0 + 30_000)
  assert.equal(second.cards.of("s1").length, 1, "the earlier turn is not this card's turn, across a reload as well as within one page")
  second.cards.reconcile("s1", [...earlier, user("ok", T0 + 2_000)], T0 + 31_000)
  assert.equal(second.cards.of("s1").length, 0, "its own turn does settle it")
})

test("a card that failed comes back failed, and may still be sent again under its request", () => {
  const store = memory()
  const first = page(store)
  const sent = first.cards.add("s1", "ship it", [], T0)
  first.cards.failed(sent.token, "busy")

  const second = page(store, T0 + 30_000)
  const back = second.cards.of("s1")[0]
  assert.equal(back.state, "failed")
  assert.equal(back.failure, "busy")
  const again = second.cards.retrying(back.token)
  assert.ok(again, "a refusal proved nothing was typed, so try again is still offered")
  assert.equal(again.request, sent.request)
})

test("a card the daemon accepted comes back accepted, and is not asked about again", () => {
  const store = memory()
  const first = page(store)
  const sent = first.cards.add("s1", "ship it", [], T0)
  first.cards.accepted(sent.token, T0 + 500)

  const back = page(store, T0 + 30_000).cards.of("s1")[0]
  assert.equal(back.state, "accepted", "the daemon's answer said the bytes reached the terminal; that is still true")
  assert.equal(back.failure, "")
  assert.equal(back.acceptedAt, T0 + 500)
})

test("storage that throws leaves the page exactly as it is today", () => {
  const broken: CardStore = {
    read() {
      throw new Error("a private window")
    },
    write() {
      throw new Error("no room")
    },
  }
  const first = page(broken)
  const sent = first.cards.add("s1", "ship it", [], T0)
  assert.equal(first.cards.of("s1").length, 1, "the card is on the page, as it always was")
  first.cards.accepted(sent.token, T0 + 500)
  first.cards.dismiss(sent.token)
  assert.equal(page(broken, T0 + 30_000).cards.of("s1").length, 0, "nothing was kept, and nothing threw")
})

for (const junk of ["", "{", "null", "[]", '{"v":9,"cards":[{"t":"p1"}]}', '{"v":1,"cards":[1,2,3],"gone":"no"}']) {
  test(`a stored value this page did not write is no cards, not a broken page (${junk || "empty"})`, () => {
    const store = memory(junk)
    assert.deepEqual(page(store, T0).cards.all(), [])
  })
}

test("only so many cards are kept, and only for so long", () => {
  const store = memory()
  const first = page(store)
  for (let i = 0; i < KEPT_CARDS + 6; i++) first.cards.add("s1", "message " + i, [], T0 + i)
  const back = page(store, T0 + 1_000).cards.of("s1")
  assert.equal(back.length, KEPT_CARDS, "the oldest go first")
  assert.equal(back[0].text, "message 6")
  assert.equal(back[back.length - 1].text, "message " + (KEPT_CARDS + 5))

  // Past the Mac's receipt window there is no receipt to send it under.
  assert.equal(page(store, T0 + KEPT_MS + 60_000).cards.of("s1").length, 0)
})

test("pictures go before words do, and a card that lost them is never sent again", () => {
  const store = memory()
  const first = page(store)
  const big = "data:image/jpeg;base64," + "A".repeat(KEPT_CHARS)
  const sent = first.cards.add("s1", "look at this", [big], T0)
  first.cards.failed(sent.token, "busy")

  const second = page(store, T0 + 30_000)
  const back = second.cards.of("s1")[0]
  assert.ok(back, "the words are kept even when the picture cannot be")
  assert.equal(back.text, "look at this")
  assert.equal(back.request, sent.request)
  assert.equal(back.partial, true)
  assert.equal(back.pictures.length, 1, "it is still a message with a picture, which is how its turn is recognised")
  assert.equal(back.pictures[0], "")
  assert.equal(second.cards.retrying(back.token), null, "a second attempt with a different body is refused by the Mac, so it is not offered")
  assert.equal(second.cards.resend(back.token, T0 + 31_000), null)
  // And it is still settled by its own turn, pictures and all.
  second.cards.reconcile("s1", [user("look at this [Image #1]", T0 + 2_000, 1)], T0 + 30_000)
  assert.equal(second.cards.of("s1").length, 0)
})

test("what the page holds is kept after every change, not only at the end", () => {
  const store = memory()
  const first = page(store)
  const sent = first.cards.add("s1", "ship it", [], T0)
  const afterAdd = store.value
  first.cards.uncertain(sent.token, "offline")
  assert.notEqual(store.value, afterAdd, "the state it reached is kept too")
  assert.equal(page(store, T0 + 1_000).cards.of("s1")[0].failure, "offline")
})

test("two tabs: each keeps its own cards, and neither writes the other's away", () => {
  const store = memory()
  const one = page(store)
  one.cards.add("s1", "from the first tab", [], T0)
  const two = page(store, T0 + 1_000)
  assert.equal(two.cards.of("s1").length, 1, "the second tab opens on the first tab's card")
  two.cards.add("s1", "from the second tab", [], T0 + 2_000)

  const third = page(store, T0 + 3_000)
  assert.deepEqual(
    third.cards.of("s1").map((card: PendingSend) => card.text),
    ["from the first tab", "from the second tab"],
  )
})

test('two tabs: "no thanks" in one is gone in both, and stays gone', () => {
  const store = memory()
  const one = page(store)
  const sent = one.cards.add("s1", "ship it", [], T0)
  const two = page(store, T0 + 1_000)
  assert.equal(two.cards.of("s1").length, 1)

  one.cards.dismiss(sent.token)
  // What `send.ts` does when the other tab's write reaches this one.
  assert.deepEqual(two.keeper.letGo(store.value, T0 + 2_000), [sent.token])
  two.cards.dismiss(sent.token)

  assert.equal(page(store, T0 + 3_000).cards.of("s1").length, 0, "and the tab that still had it did not write it back")
})

test("a card settled by its turn does not come back from the other tab's copy", () => {
  const store = memory()
  const one = page(store)
  one.cards.add("s1", "ship it", [], T0)
  const two = page(store, T0 + 1_000)

  one.cards.reconcile("s1", [user("ship it", T0 + 2_000)], T0 + 2_000)
  assert.equal(one.cards.of("s1").length, 0)
  assert.equal(page(store, T0 + 3_000).cards.of("s1").length, 0, "the message is in the conversation; the card is finished with")
  assert.equal(two.keeper.letGo(store.value, T0 + 3_000).length, 1, "and the tab still showing it is told")
})

test("a card this page already has is not doubled by one restored under the same token", () => {
  const store = memory()
  const first = page(store)
  const sent = first.cards.add("s1", "ship it", [], T0)
  const keeper = new Cards({ store, turnWords })
  first.cards.restore(keeper.restore("", T0 + 1_000))
  assert.equal(first.cards.of("s1").length, 1)
  assert.equal(first.cards.of("s1")[0].token, sent.token)
})

test("a card is put back only for the machine it was written for", () => {
  const store = memory()
  // A hosted console on one Mac. `%1` is a tmux pane, and every Mac has one.
  const alpha = page(store, T0, "machine-alpha")
  const sent = alpha.cards.add("%1", "restart the server", [], T0)

  const bravo = page(store, T0 + 1_000, "machine-bravo")
  assert.deepEqual(bravo.cards.all(), [], "another machine's pane of the same name is not this card's session")
  bravo.cards.add("%1", "something else entirely", [], T0 + 2_000)

  const alphaAgain = page(store, T0 + 3_000, "machine-alpha")
  assert.equal(alphaAgain.cards.of("%1").length, 1, "and the first machine's card is still kept, not written away")
  assert.equal(alphaAgain.cards.of("%1")[0].request, sent.request)
  assert.equal(alphaAgain.cards.of("%1")[0].text, "restart the server")
})

test("nothing is kept before the page knows which machine it is talking to", () => {
  const store = memory()
  const cards = new PendingSends()
  const keeper = new Cards({ store, turnWords })
  cards.subscribe(() => keeper.keep(cards.all(), T0))
  cards.add("%1", "too early", [], T0)
  assert.equal(store.writes, 0, "a card written under the wrong machine is worse than one not kept")
  assert.equal(store.value, null)
})
