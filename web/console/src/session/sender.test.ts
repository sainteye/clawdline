// Sending a card's words, and sending them again: `node --test web/console/src/session/*.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see `order.test.ts`.
import { PendingSends, type SeenTurn } from "./pending.ts"
// @ts-expect-error -- a `.ts` path, for node; see `order.test.ts`.
import { postCard, Sender, type Posted } from "./sender.ts"
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
  const steps: string[] = []
  let turns: SeenTurn[] | null = []
  const sender = new Sender({
    cards,
    now: () => clock.t,
    wakeConnection: () => { steps.push("wake") },
    post: async (card) => {
      posts.push({ session: card.session, text: card.text, request: card.request })
      const next = answers.shift() ?? { ok: true }
      if (next === "throw") throw new Error("the network went away")
      return next
    },
    readBack: async () => { steps.push("read"); return turns },
    outcomeOf,
  })
  return {
    clock, cards, posts, sender, steps,
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

// F2: unanswered attempts keep their request, so a lost reply cannot type twice.
test("an unanswered card keeps its request after the transcript is inspected", async () => {
  const h = harness()
  h.answer({ ok: false, status: null, code: "offline" })
  const card = h.cards.add("s", "delete the build directory", [], T0)
  await h.sender.deliver(card)
  h.clock.t += 5_000
  await h.sender.look(card.token)
  await h.sender.resend(card.token)
  assert.equal(h.posts.length, 2)
  assert.ok(h.posts[0].request, "the first attempt names a request")
  assert.equal(h.posts[1].request, h.posts[0].request, "the second attempt is the same request")
  const other = h.cards.add("s", "delete the build directory", [], h.clock.t)
  await h.sender.deliver(other)
  assert.notEqual(h.posts[2].request, h.posts[0].request, "another card is another request")
})

// A refusal that proved no words were typed can be tried again even when the
// transcript read is unavailable, just as a new composer send can.
test("try again after a definite refusal sends despite a failed read-back", async () => {
  const h = harness()
  h.answer({ ok: false, status: 429, code: "busy" })
  const card = h.cards.add("s", "yes", [], T0)
  await h.sender.deliver(card)
  h.transcript(null)
  await h.sender.resend(card.token)
  assert.deepEqual(h.steps.slice(-2), ["wake", "read"], "retry wakes Cloud before checking the transcript")
  assert.equal(h.posts.length, 2, "a failed read cannot block a send known not to have arrived")
  assert.notEqual(h.posts[1].request, h.posts[0].request, "a definite refusal may be filed by Cloud")
  assert.equal(h.cards.card(card.token)?.state, "accepted")
})

test("retry after an execution change uses a fresh request", async () => {
  const h = harness()
  h.answer({ ok: false, status: 409, code: "execution_generation_changed" })
  const card = h.cards.add("s", "follow up", [], T0)
  await h.sender.deliver(card)
  assert.equal(h.cards.card(card.token)?.state, "failed")
  await h.sender.resend(card.token)
  assert.equal(h.posts.length, 2)
  assert.notEqual(h.posts[1].request, h.posts[0].request)
  assert.equal(h.posts[1].text, h.posts[0].text)
})

for (const [code, status] of [
  ["backend_unsupported", 501],
  ["busy", 429],
  ["session_asking", 409],
  ["forbidden", 403],
  ["session_not_found", 404],
  ["execution_source_unknown", 503],
  ["execution_check_unavailable", 503],
  ["execution_records_full", 503],
] as const) {
  test(`an explicit retry escapes a filed ${code} without replaying an uncertain send`, async () => {
    const cards = new PendingSends()
    const receipts = new Map<string, Posted>()
    const requests: string[] = []
    let admitted = false
    let effects = 0
    const sender = new Sender({
      cards, now: () => T0,
      post: async (card) => {
        requests.push(card.request)
        const replay = receipts.get(card.request)
        if (replay) return replay
        const answer: Posted = admitted ? { ok: true } : { ok: false, status, code }
        if (answer.ok) effects++
        receipts.set(card.request, answer)
        return answer
      },
      readBack: async () => null,
      outcomeOf,
    })
    const card = cards.add("s", "follow up", [], T0)
    await sender.deliver(card)
    assert.equal(cards.card(card.token)?.state, "failed", "admission refused before any terminal effect")
    admitted = true
    assert.equal(effects, 0, "recovery alone never sends the draft")
    await sender.resend(card.token)
    assert.equal(requests.length, 2)
    assert.notEqual(requests[1], requests[0], "the old receipt still contains the original refusal")
    assert.equal(effects, 1)
    assert.equal(cards.card(card.token)?.state, "accepted")
  })
}

for (const code of ["backend_unsupported", "busy", "execution_source_unknown"]) {
  test(`an unknown ${code} outcome keeps its request after transcript inspection`, async () => {
    const h = harness()
    h.answer({ ok: false, status: 503, code, outcome: "unknown" })
    const card = h.cards.add("s", "follow up", [], T0)
    await h.sender.deliver(card)
    await h.sender.look(card.token)
    await h.sender.resend(card.token)
    assert.equal(h.posts.length, 2)
    assert.equal(h.posts[1].request, h.posts[0].request, "an unknown outcome cannot authorize a new effect")
  })
}

test("try again after an uncertain send still stops when read-back fails", async () => {
  const h = harness()
  h.answer({ ok: false, status: 504, code: "cloud_read_timeout", outcome: "unknown" })
  const card = h.cards.add("s", "yes", [], T0)
  await h.sender.deliver(card)
  h.transcript([])
  await h.sender.look(card.token)
  h.transcript(null)
  await h.sender.resend(card.token)
  assert.equal(h.posts.length, 1)
  assert.equal(h.cards.card(card.token)?.state, "unknown")
})

test("a confirmed terminal failure uses a new request only after the person checks an absent turn", async () => {
  const h = harness()
  h.answer({ ok: false, status: 500, code: "send_failed" })
  const card = h.cards.add("s", "follow up", [], T0)
  await h.sender.deliver(card)
  await h.sender.resend(card.token)
  assert.equal(h.posts.length, 1, "an unchecked failure is never sent again")
  h.transcript([])
  await h.sender.look(card.token)
  h.clock.t += 2_000
  await h.sender.resend(card.token)
  assert.equal(h.posts.length, 2)
  assert.notEqual(h.posts[1].request, h.posts[0].request, "the filed 500 cannot answer a new attempt")
  assert.equal(h.cards.card(card.token)?.state, "accepted")
})

test("a terminal failure is not retried with a new request when the fresh read is unavailable", async () => {
  const h = harness()
  h.answer({ ok: false, status: 500, code: "send_failed" })
  const card = h.cards.add("s", "follow up", [], T0)
  await h.sender.deliver(card)
  await h.sender.look(card.token)
  h.transcript(null)
  await h.sender.resend(card.token)
  assert.equal(h.posts.length, 1)
  assert.equal(h.cards.card(card.token)?.failure, "send_failed")
  h.transcript([user("follow up", T0 + 1_000)])
  await h.sender.look(card.token)
  assert.equal(h.cards.card(card.token), undefined, "a late turn settles the card")
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

/** A card the machine typed and held Enter back on, and a daemon whose Enter answers as it is told. */
function unsubmitted(...enters: (Posted | "throw")[]) {
  const clock = { t: T0 }
  const cards = new PendingSends()
  const pressed: string[] = []
  const sender = new Sender({
    cards,
    now: () => clock.t,
    post: async () => ({ ok: false, status: 502, code: "send_unsubmitted" }),
    enter: async (card) => {
      pressed.push(card.token)
      const next = enters.shift() ?? { ok: true }
      if (next === "throw") throw new Error("the network went away")
      return next
    },
    readBack: async () => [],
    outcomeOf,
  })
  const card = cards.add("s", "please read CHILD.md", [], T0)
  return { clock, cards, pressed, sender, card, ready: () => sender.deliver(card) }
}

// The phone cannot reach the machine's keyboard: words typed and held back
// are submitted from the card, by the Enter the machine held back.
test("an Enter on words typed and never submitted takes the card to accepted, and sends nothing again", async () => {
  const h = unsubmitted({ ok: true })
  await h.ready()
  assert.equal(h.cards.card(h.card.token)?.failure, "send_unsubmitted")
  h.clock.t = T0 + 30 * 60_000
  await h.sender.enter(h.card.token)
  assert.deepEqual(h.pressed, [h.card.token])
  const card = h.cards.card(h.card.token)
  assert.equal(card?.state, "accepted", "Enter reached the terminal; the turn settles the card")
  assert.equal(card?.checking, false)
  assert.equal(card?.sentAt, T0 + 30 * 60_000, "the turn is looked for from the Enter, not from the typing")
  h.cards.reconcile("s", [user("please read CHILD.md", T0 + 30 * 60_000 + 2_000)], T0 + 30 * 60_000 + 3_000)
  assert.equal(h.cards.card(h.card.token), undefined, "an Enter pressed long after the typing still settles on its turn")
  await h.sender.enter(h.card.token)
  assert.equal(h.pressed.length, 1, "an accepted card offers no second Enter")
})

// The words may still be in the input line after any of these, where "send
// again" would type them twice; only the Enter is offered, and the machine
// presses it only while they are there.
test("an Enter refused, behind a question, or unanswered is offered again, never a second send", async () => {
  for (const answer of [
    { ok: false, status: 409, code: "input_unreadable" },
    { ok: false, status: 409, code: "input_behind_question" },
    { ok: false, status: 504, code: "cloud_read_timeout", outcome: "unknown" },
    "throw",
  ] as (Posted | "throw")[]) {
    const h = unsubmitted(answer, { ok: true })
    await h.ready()
    await h.sender.enter(h.card.token)
    const card = h.cards.card(h.card.token)
    const code = answer === "throw" ? "offline" : answer.ok ? "" : answer.code
    assert.equal(card?.state, "unknown", code)
    assert.equal(card?.failure, "send_unsubmitted", code + ": the Enter is offered again")
    assert.equal(card?.enterRefused, code)
    assert.equal(card?.checking, false)
    await h.sender.enter(h.card.token)
    assert.equal(h.pressed.length, 2, code + ": a second Enter can be pressed")
  }
})

test("words no longer in the input line are looked at rather than pressed again", async () => {
  const moved = unsubmitted({ ok: false, status: 409, code: "input_moved" })
  await moved.ready()
  await moved.sender.enter(moved.card.token)
  assert.equal(moved.cards.card(moved.card.token)?.failure, "input_moved")
  await moved.sender.enter(moved.card.token)
  assert.equal(moved.pressed.length, 1, "no Enter is offered once the words have moved")
})


test("a lost Playbook send answer retries the durable to-do under the same receipt", async () => {
  const cards = new PendingSends()
  const card = cards.add("%fixture", "Launch message with its to-do reminder", [], T0, false, undefined, "todo-fixture")
  const requests: { path: string; key: string; body: string }[] = []
  const receipts = new Set<string>()
  let deliveries = 0
  const fetcher: typeof fetch = async (input, init) => {
    const request = { path: String(input), key: String(new Headers(init?.headers).get("Idempotency-Key")), body: String(init?.body) }
    requests.push(request)
    if (!receipts.has(request.key)) { receipts.add(request.key); deliveries++ }
    if (requests.length === 1) throw new Error("answer lost after delivery")
    return Response.json({ ok: true, todo: { id: "todo-fixture" } })
  }
  const sender = new Sender({ cards, now: () => T0 + 1, post: c => postCard(fetcher, p => p, c),
    readBack: async () => [], outcomeOf })
  await sender.deliver(card)
  assert.equal(card.state, "unknown")
  await sender.look(card.token)
  await sender.resend(card.token)
  assert.equal(card.state, "accepted")
  assert.equal(requests.length, 2)
  assert.equal(deliveries, 1)
  assert.equal(requests[0].path, "/v1/work/v2/session-todos/%25fixture/todo-fixture/send")
  assert.equal(requests[0].body, "{}")
  assert.deepEqual(requests[1], requests[0], "retry uses the same route, payload and key")
})


test("ordinary cards keep the composer route and original message payload", async () => {
  const card = new PendingSends().add("%fixture", "Ordinary message", ["data:image/png;base64,fixture"], T0, false, "conversation-fixture")
  let received: { path: string; body: unknown } | undefined
  const fetcher: typeof fetch = async (input, init) => {
    received = { path: String(input), body: JSON.parse(String(init?.body)) }
    return Response.json({ ok: true })
  }
  assert.deepEqual(await postCard(fetcher, p => p, card), { ok: true })
  assert.deepEqual(received, { path: "/v1/sessions/%25fixture/send", body: {
    text: "Ordinary message", images: ["data:image/png;base64,fixture"], conversation: "conversation-fixture",
  } })
})
