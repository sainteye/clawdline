// The cards for messages on their way: `node --test web/console/src/session/*.test.ts`.
// The `.ts` import is for node, as in `order.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see `order.test.ts`.
import { PENDING_LIFETIME_MS, PendingSends, turnWords, type SeenTurn } from "./pending.ts"

const T0 = 1_789_800_000_000 // ms
const sec = (ms: number) => Math.floor(ms / 1000)

function user(text: string, atMs: number, imageCount?: number): SeenTurn {
  return { role: "user", text, at: sec(atMs), ...(imageCount === undefined ? {} : { imageCount }) }
}
const reply = (text: string, atMs: number): SeenTurn => ({ role: "assistant", text, at: sec(atMs) })

function states(p: PendingSends, session = "s"): string[] {
  return p.of(session).map((c) => c.state + ":" + c.text)
}

test("a card goes when its turn is read back, and not before", () => {
  const p = new PendingSends()
  p.reconcile("s", [user("earlier", T0 - 60_000)], T0 - 1)
  const card = p.add("s", "hello", [], T0)
  assert.deepEqual(states(p), ["sending:hello"])
  p.accepted(card.token, T0 + 200)
  p.reconcile("s", [user("earlier", T0 - 60_000)], T0 + 1_000)
  assert.deepEqual(states(p), ["accepted:hello"])
  p.reconcile("s", [user("earlier", T0 - 60_000), user("hello", T0 + 1_500), reply("hi", T0 + 3_000)], T0 + 4_000)
  assert.deepEqual(states(p), [])
})

test("the same words already in the transcript when it was sent are not its turn", () => {
  const p = new PendingSends()
  const before = [user("ok", T0 - 20_000)]
  p.reconcile("s", before, T0 - 1)
  p.add("s", "ok", [], T0)
  p.reconcile("s", before, T0 + 1_000)
  assert.equal(p.of("s").length, 1)
  p.reconcile("s", [...before, user("ok", T0 + 1_000)], T0 + 2_000)
  assert.equal(p.of("s").length, 0)
})

test("two cards with the same words take one turn each, across reads", () => {
  const p = new PendingSends()
  p.reconcile("s", [], T0 - 1)
  const a = p.add("s", "again", [], T0)
  const b = p.add("s", "again", [], T0 + 100)
  p.accepted(a.token, T0 + 200)
  p.accepted(b.token, T0 + 300)
  const first = [user("again", T0 + 1_000)]
  p.reconcile("s", first, T0 + 1_500)
  assert.deepEqual(p.of("s").map((c) => c.token), [b.token])
  // Read again with nothing new: the turn that settled the first card is
  // still there, and it is not the second card's.
  p.reconcile("s", first, T0 + 2_500)
  assert.deepEqual(p.of("s").map((c) => c.token), [b.token])
  p.reconcile("s", [...first, user("again", T0 + 3_000)], T0 + 3_500)
  assert.equal(p.of("s").length, 0)
})

test("a failed send keeps its words and says why; a later send of the same words gets the turn", () => {
  const p = new PendingSends()
  p.reconcile("s", [], T0 - 1)
  const lost = p.add("s", "retry me", [], T0)
  p.failed(lost.token, "unexpected_error")
  assert.deepEqual(states(p), ["failed:retry me"])
  assert.equal(p.of("s")[0].failure, "unexpected_error")
  const next = p.add("s", "retry me", [], T0 + 5_000)
  p.accepted(next.token, T0 + 5_100)
  p.reconcile("s", [user("retry me", T0 + 6_000)], T0 + 7_000)
  assert.deepEqual(states(p), ["failed:retry me"])
})

test("a send reported failed that arrived after all goes like any other", () => {
  const p = new PendingSends()
  p.reconcile("s", [], T0 - 1)
  const card = p.add("s", "timed out but typed", [], T0)
  p.failed(card.token, "unexpected_error")
  p.reconcile("s", [user("timed out but typed", T0 + 800)], T0 + 9_000)
  assert.equal(p.of("s").length, 0)
})

test("trying again is a new attempt, and waits for a turn of its own", () => {
  const q = new PendingSends()
  q.reconcile("s", [], T0 - 1)
  const c2 = q.add("s", "once more", [], T0)
  q.failed(c2.token, "network")
  q.reconcile("s", [], T0 + 1_000)
  assert.equal(q.resend(c2.token, T0 + 20_000)?.state, "sending")
  assert.equal(q.resend(c2.token, T0 + 20_000), null, "only a failed card is sent again")
  q.reconcile("s", [user("once more", T0 + 21_000)], T0 + 22_000)
  assert.equal(q.of("s").length, 0)
})

test("a turn dated well before the send is not it", () => {
  const p = new PendingSends()
  // The transcript had not been read when this was sent, so nothing is known.
  p.add("s", "fresh", [], T0)
  p.reconcile("s", [user("fresh", T0 - 10 * 60_000)], T0 + 1_000)
  assert.equal(p.of("s").length, 1)
  p.reconcile("s", [user("fresh", T0 - 10 * 60_000), user("fresh", T0 + 1_000)], T0 + 2_000)
  assert.equal(p.of("s").length, 0)
})

test("pictures: the daemon's markers are not words, and a picture-only send matches a picture-only turn", () => {
  assert.equal(turnWords("look  at\nthis [Image #1]", 1), turnWords("look at this", 2))
  assert.notEqual(turnWords("look at this", 0), turnWords("look at this", 1))
  const p = new PendingSends()
  p.reconcile("s", [], T0 - 1)
  p.add("s", "", ["data:image/png;base64,AAAA"], T0)
  p.reconcile("s", [user("[Image #1]", T0 + 1_000, 1)], T0 + 2_000)
  assert.equal(p.of("s").length, 0)
})

test("an accepted card the transcript never confirms goes after its lifetime; a failed one stays", () => {
  const p = new PendingSends()
  p.reconcile("s", [], T0 - 1)
  const a = p.add("s", "never read back", [], T0)
  const f = p.add("s", "never sent", [], T0)
  p.accepted(a.token, T0 + 100)
  p.failed(f.token, "session_gone")
  p.reconcile("s", [], T0 + PENDING_LIFETIME_MS)
  assert.equal(p.of("s").length, 2)
  p.reconcile("s", [], T0 + 100 + PENDING_LIFETIME_MS + 1)
  assert.deepEqual(states(p), ["failed:never sent"])
  p.dismiss(f.token)
  assert.equal(p.of("s").length, 0)
})

test("cards belong to their session, and every change is announced", () => {
  const p = new PendingSends()
  let heard = 0
  const stop = p.subscribe(() => heard++)
  const v0 = p.getVersion()
  p.add("a", "for a", [], T0)
  p.add("b", "for b", [], T0)
  p.reconcile("b", [user("for a", T0 + 500)], T0 + 1_000)
  assert.equal(p.of("a").length, 1)
  assert.equal(p.of("b").length, 1)
  assert.equal(heard, 2)
  assert.ok(p.getVersion() > v0)
  stop()
  p.add("a", "unheard", [], T0)
  assert.equal(heard, 2)
})

test("try again reads first: a failed attempt that was typed after all settles, and is not typed twice", () => {
  // Across Clawdline Cloud the Mac can type the words and its answer be lost.
  const p = new PendingSends()
  p.reconcile("s", [user("older", T0 - 5_000)], T0 - 1)
  const card = p.add("s", "typed but unanswered", [], T0)
  p.failed(card.token, "cloud_read_timeout")
  assert.equal(p.retrying(card.token)?.state, "sending", "the card says it is on its way while the page looks")
  assert.equal(p.retrying(card.token), null, "one look per press")
  // What the look found: the failed attempt's turn.
  p.reconcile("s", [user("older", T0 - 5_000), user("typed but unanswered", T0 + 700)], T0 + 70_000)
  assert.equal(p.card(card.token), undefined, "settled by the read")
  assert.equal(p.resend(card.token, T0 + 70_000), null, "so nothing is sent again")
})

test("try again reads first: when the attempt did not arrive, it goes again as a new attempt", () => {
  const p = new PendingSends()
  p.reconcile("s", [], T0 - 1)
  const card = p.add("s", "refused", [], T0)
  p.failed(card.token, "cloud_commands_disabled")
  p.retrying(card.token)
  p.reconcile("s", [user("unrelated", T0 + 500)], T0 + 30_000)
  const again = p.resend(card.token, T0 + 30_000)
  assert.equal(again?.state, "sending")
  assert.equal(again?.checking, false)
  assert.equal(again?.sentAt, T0 + 30_000)
  p.reconcile("s", [user("unrelated", T0 + 500), user("refused", T0 + 31_000)], T0 + 32_000)
  assert.equal(p.of("s").length, 0)
})

test("a `!` command settles whether or not a space followed the `!`", () => {
  for (const recorded of ["!ls -la", "! ls -la"]) {
    const p = new PendingSends()
    p.reconcile("s", [], T0 - 1)
    const card = p.add("s", "! ls -la", [], T0)
    p.accepted(card.token, T0 + 200)
    p.reconcile("s", [user(recorded, T0 + 1_000)], T0 + 2_000)
    assert.deepEqual(states(p), [], recorded)
  }
  const q = new PendingSends()
  q.reconcile("s", [], T0 - 1)
  const plain = q.add("s", "hello", [], T0)
  q.accepted(plain.token, T0 + 200)
  q.reconcile("s", [user("!hello", T0 + 1_000)], T0 + 2_000)
  assert.deepEqual(states(q), ["accepted:hello"])
})
