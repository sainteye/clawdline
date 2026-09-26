// The offer of the sessions a reboot took away: `node --test web/console/src/session/*.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
import type { RestorableSession, RestorableSessions } from "@clawdline/contract"
// @ts-expect-error -- a `.ts` path, for node; see `order.test.ts`.
import { allOpened, allTicked, offerShape, outcomeLine, outcomes, readOffer, restoreBatches, restoreBody, rowName, RESTORE_BATCH, RestoreRefusal, sendDismissAll, sendRestore } from "./restore-offer.ts"

function session(id: string, extra: Partial<RestorableSession> = {}): RestorableSession {
  return {
    conversation_id: id, assistant: "claude", place: "p1", place_label: "api", cwd: "/work/api",
    title: "", last_seen: 1_000, ...extra,
  }
}

function offer(sessions: RestorableSession[], available = true): RestorableSessions {
  return { available, sessions, at: 2_000 }
}

test("the card stands in the empty list's place, above rows, or nowhere", () => {
  const two = offer([session("a"), session("b")])
  assert.equal(offerShape(two, "home"), "hero")
  assert.equal(offerShape(two, "rows"), "compact")
  // Nothing to offer, or a machine that cannot tell a reboot from a restart:
  // nothing new is drawn and the empty list keeps its own words.
  assert.equal(offerShape(offer([]), "home"), null)
  assert.equal(offerShape(offer([]), "rows"), null)
  assert.equal(offerShape({ available: false, reason: "boot_unknown", sessions: [], at: 1 }, "home"), null)
  assert.equal(offerShape(offer([session("a")], false), "home"), null, "unavailable wins over rows it should not carry")
  // Not read yet is not "none", and is not drawn as anything.
  assert.equal(offerShape(null, "home"), null)
  // Nothing over the skeleton.
  assert.equal(offerShape(two, "loading"), null)
})

test("a press sends the ticked conversations in the sheet's order, each once", () => {
  const sessions = [session("a"), session("b"), session("c")]
  assert.deepEqual(restoreBody(sessions, allTicked(sessions)), { conversations: ["a", "b", "c"] })
  assert.deepEqual(restoreBody(sessions, new Set(["c", "a"])), { conversations: ["a", "c"] })
  assert.deepEqual(restoreBody([...sessions, session("a")], new Set(["a"])), { conversations: ["a"] })
  assert.equal(restoreBody(sessions, new Set()), null, "an empty selection sends nothing")
  assert.equal(restoreBody(sessions, new Set(["gone"])), null, "a tick for a row not listed is not sent")
})

test("a press over more than the daemon's batch goes as several requests the press owns", () => {
  const ids = Array.from({ length: RESTORE_BATCH * 2 + 1 }, (_, i) => "c" + i)
  const batches = restoreBatches({ conversations: ids }, "k")
  assert.deepEqual(batches.map((b) => b.key), ["k", "k.2", "k.3"])
  assert.deepEqual(batches.map((b) => b.conversations.length), [RESTORE_BATCH, RESTORE_BATCH, 1])
  assert.deepEqual(batches.flatMap((b) => b.conversations), ids)
  assert.deepEqual(restoreBatches({ conversations: ["a"] }, "k"), [{ conversations: ["a"], key: "k" }])
})

const WORDS: Record<string, string> = {
  restoreOpened: "opened",
  restoreOpening: "opening",
  restoreFailedNotRestorable: "not on offer",
  restoreFailedPlaceUnavailable: "no folder",
  restoreFailedNotFound: "no conversation",
  restoreFailedOpen: "terminal failed.",
  restoreFailedBusy: "busy",
  restoreFailedUnknown: "did not open",
  restoreFailedSaid: "did not open: {why}",
}
const word = (key: string, holes: Record<string, string> = {}) =>
  WORDS[key].replace(/\{(\w+)\}/g, (_, name: string) => holes[name] ?? "")

test("each row says what happened to it, in the words for its code", () => {
  const named = ["a", "b", "c", "d", "e", "f"]
  const rows = outcomes(named, {
    results: [
      { conversation_id: "a", ok: true, id: "%9", backend: "tmux" },
      { conversation_id: "b", ok: false, code: "conversation_not_found", message: "English, never shown" },
      { conversation_id: "c", ok: false, code: "open_failed", message: "iTerm2 is not running" },
      { conversation_id: "d", ok: false, code: "over_capacity" },
      { conversation_id: "e", ok: false, message: "a code this bundle does not know" },
    ],
  })
  assert.equal(outcomeLine(rows.get("a"), word), "opened")
  assert.equal(outcomeLine(rows.get("b"), word), "no conversation", "the code's sentence, not the machine's English")
  assert.equal(outcomeLine(rows.get("c"), word), "terminal failed. iTerm2 is not running", "the terminal's own reason is said")
  assert.equal(outcomeLine(rows.get("d"), word), "busy")
  assert.equal(outcomeLine(rows.get("e"), word), "did not open: a code this bundle does not know")
  // Named, and the answer says nothing about it: that is not an opening.
  assert.deepEqual(rows.get("f"), { kind: "failed", code: "", message: "" })
  assert.equal(outcomeLine(rows.get("f"), word), "did not open")
  assert.equal(outcomeLine(undefined, word), "")
  assert.equal(outcomeLine({ kind: "opening" }, word), "opening")
})

test("the sheet closes on its own only when every row it named opened", () => {
  const good = outcomes(["a", "b"], { results: [{ conversation_id: "a", ok: true }, { conversation_id: "b", ok: true }] })
  assert.equal(allOpened(["a", "b"], good), true)
  const mixed = outcomes(["a", "b"], { results: [{ conversation_id: "a", ok: true }, { conversation_id: "b", ok: false, code: "open_failed" }] })
  assert.equal(allOpened(["a", "b"], mixed), false)
  assert.equal(allOpened([], new Map()), false)
})

test("a row goes by its title, then its folder's label, then its folder", () => {
  assert.equal(rowName(session("a", { title: "Fix the bar" })), "Fix the bar")
  assert.equal(rowName(session("a", { title: "  " })), "api")
  assert.equal(rowName(session("a", { title: "", place_label: "" })), "/work/api")
})

function recorder(status: number, body: unknown) {
  const calls: { input: string; init?: RequestInit }[] = []
  const fetch = async (input: string, init?: RequestInit) => {
    calls.push({ input, init })
    return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } })
  }
  return { calls, fetch }
}

test("the three requests carry the press's key and the body the route reads", async () => {
  const list = recorder(200, offer([session("a")]))
  assert.equal((await readOffer(list.fetch)).sessions.length, 1)
  assert.equal(list.calls[0].input, "/v1/sessions/restorable")
  assert.equal(list.calls[0].init?.method, "GET")

  const restore = recorder(200, { results: [{ conversation_id: "a", ok: true }], at: 1 })
  await sendRestore(restore.fetch, { conversations: ["a"] }, "press-1")
  assert.equal(restore.calls[0].input, "/v1/sessions/restorable/restore")
  assert.equal(new Headers(restore.calls[0].init?.headers).get("idempotency-key"), "press-1")
  assert.deepEqual(JSON.parse(String(restore.calls[0].init?.body)), { conversations: ["a"] })

  // "Skip all" names no list: absent means every conversation on offer.
  const dismiss = recorder(200, { ok: true, dismissed: 2, at: 1 })
  await sendDismissAll(dismiss.fetch, "press-2")
  assert.equal(dismiss.calls[0].input, "/v1/sessions/restorable/dismiss")
  assert.equal(new Headers(dismiss.calls[0].init?.headers).get("idempotency-key"), "press-2")
  assert.deepEqual(JSON.parse(String(dismiss.calls[0].init?.body)), {})
})

test("a refusal keeps its code in either spelling, and a fetch that threw is offline", async () => {
  const flat = recorder(400, { error: "restore_batch_too_large", message: "At most 20." })
  await assert.rejects(sendRestore(flat.fetch, { conversations: ["a"] }, "k"), (e: unknown) =>
    e instanceof RestoreRefusal && e.code === "restore_batch_too_large" && e.message === "At most 20." && e.status === 400)
  const nested = recorder(403, { error: { code: "cloud_read_only", message: "This device may only read." } })
  await assert.rejects(sendRestore(nested.fetch, { conversations: ["a"] }, "k"), (e: unknown) =>
    e instanceof RestoreRefusal && e.code === "cloud_read_only")
  const thrown = async () => {
    throw new TypeError("Failed to fetch")
  }
  await assert.rejects(readOffer(thrown), (e: unknown) => e instanceof RestoreRefusal && e.code === "offline")
})
