// Sending a card's words, and sending them again: `node --test web/console/src/session/*.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see `order.test.ts`.
import { PendingSends, type SeenTurn } from "./pending.ts"
// @ts-expect-error -- a `.ts` path, for node; see `order.test.ts`.
import { Sender, type Posted } from "./sender.ts"
// @ts-expect-error -- a `.ts` path, for node; see `order.test.ts`.
import { outcomeOf } from "./outcome.ts"

const T0 = 1_789_800_000_000
const user = (text: string, atMs: number): SeenTurn => ({ role: "user", text, at: Math.floor(atMs / 1000) })

/** A daemon that answers each post as it is told, and a transcript it reads back. */
function harness() {
  const clock = { t: T0 }
  const cards = new PendingSends()
  const posts: { session: string; text: string; request: string }[] = []
  const answers: (Posted | "throw")[] = []
  let turns: SeenTurn[] | null = []
  const sender = new Sender({
    cards,
    now: () => clock.t,
    post: async (card) => {
      posts.push({ session: card.session, text: card.text, request: card.request })
      const next = answers.shift() ?? { ok: true }
      if (next === "throw") throw new Error("the network went away")
      return next
    },
    readBack: async () => turns,
    outcomeOf,
  })
  return {
    clock, cards, posts, sender,
    answer: (a: Posted | "throw") => answers.push(a),
    transcript: (t: SeenTurn[] | null) => { turns = t },
  }
}

test("a refusal that proves nothing was typed fails the card; anything else leaves it unknown", async () => {
  const h = harness()
  h.answer({ ok: false, status: 404, code: "session_not_found" })
  const a = h.cards.add("s", "one", [], T0)
  assert.equal(await h.sender.deliver(a), "session_not_found")
  assert.equal(h.cards.card(a.token)?.state, "failed")

  h.answer({ ok: false, status: 504, code: "cloud_read_timeout", outcome: "unknown" })
  const b = h.cards.add("s", "two", [], T0)
  await h.sender.deliver(b)
  assert.equal(h.cards.card(b.token)?.state, "unknown", "the machine may have typed it")

  h.answer("throw")
  const c = h.cards.add("s", "three", [], T0)
  await h.sender.deliver(c)
  assert.equal(h.cards.card(c.token)?.state, "unknown", "no answer at all is not a no")

  h.answer({ ok: false, status: 502, code: "terminal_io_failed" })
  const d = h.cards.add("s", "four", [], T0)
  await h.sender.deliver(d)
  assert.equal(h.cards.card(d.token)?.state, "unknown", "a terminal that failed part-way may have taken the words")
})

// F2: every attempt of one card is one request, so the Mac can answer a
// second attempt with the first one's answer instead of typing it again.
test("every attempt of a card is sent under the card's one request", async () => {
  const h = harness()
  h.answer({ ok: false, status: 429, code: "busy" })
  const card = h.cards.add("s", "delete the build directory", [], T0)
  await h.sender.deliver(card)
  h.clock.t += 5_000
  await h.sender.resend(card.token)
  assert.equal(h.posts.length, 2)
  assert.ok(h.posts[0].request, "the first attempt names a request")
  assert.equal(h.posts[1].request, h.posts[0].request, "the second attempt is the same request")
  const other = h.cards.add("s", "delete the build directory", [], h.clock.t)
  await h.sender.deliver(other)
  assert.notEqual(h.posts[2].request, h.posts[0].request, "another card is another request")
})

// F2: "try again" reads the transcript first. A read that fails is not a
// reason to type the words again — it is the moment the first attempt is
// most likely to have landed — so the card says it does not know, and nothing
// is sent.
test("try again with a read-back that fails sends nothing and leaves the card unknown", async () => {
  const h = harness()
  h.answer({ ok: false, status: 429, code: "busy" })
  const card = h.cards.add("s", "yes", [], T0)
  await h.sender.deliver(card)
  h.transcript(null)
  await h.sender.resend(card.token)
  assert.equal(h.posts.length, 1, "the words were sent a second time on a failed read")
  assert.equal(h.cards.card(card.token)?.state, "unknown")
})

test("try again: a turn the read-back shows settles the card; an unknown card is not retried blind", async () => {
  const h = harness()
  h.answer({ ok: false, status: 429, code: "busy" })
  const card = h.cards.add("s", "yes", [], T0)
  await h.sender.deliver(card)
  h.transcript([user("yes", T0 + 1_000)])
  await h.sender.resend(card.token)
  assert.equal(h.cards.card(card.token), undefined, "it arrived after all: settled, not typed again")
  assert.equal(h.posts.length, 1)

  h.answer({ ok: false, status: 504, code: "cloud_read_timeout", outcome: "unknown" })
  const unsure = h.cards.add("s", "no", [], T0)
  await h.sender.deliver(unsure)
  await h.sender.resend(unsure.token)
  assert.equal(h.posts.length, 2, "an unknown card is looked at before anything is sent again")
  h.transcript([])
  await h.sender.look(unsure.token)
  await h.sender.resend(unsure.token)
  assert.equal(h.posts.length, 3, "once the transcript shows no turn, sending again is offered")
  assert.equal(h.posts[2].request, h.posts[1].request, "under the same request")
})

// F3: an unknown card is looked at, not retried. Looking reads the
// transcript: the turn there settles the card; no turn there says so, and
// only then is sending offered — under the same request.
test("look: the turn settles the card; its absence is said; a failed read changes nothing", async () => {
  const h = harness()
  h.answer({ ok: false, status: 504, code: "cloud_read_timeout", outcome: "unknown" })
  const card = h.cards.add("s", "ok", [], T0)
  await h.sender.deliver(card)

  h.transcript(null)
  await h.sender.look(card.token)
  assert.equal(h.cards.card(card.token)?.state, "unknown")
  assert.equal(h.cards.card(card.token)?.absent, false, "an unread transcript says nothing about the turn")

  h.transcript([user("something else", T0 + 1_000)])
  await h.sender.look(card.token)
  assert.equal(h.cards.card(card.token)?.state, "unknown")
  assert.equal(h.cards.card(card.token)?.absent, true)
  assert.equal(h.posts.length, 1, "looking never sends")

  h.transcript([user("something else", T0 + 1_000), user("ok", T0 + 2_000)])
  await h.sender.look(card.token)
  assert.equal(h.cards.card(card.token), undefined)
})
