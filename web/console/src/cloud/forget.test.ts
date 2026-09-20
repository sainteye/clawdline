// Forgetting a machine, against a fake control plane:
// `node --test web/console/src/cloud/forget.test.ts`.
//
// Nothing here touches a real account. The fake answers exactly what
// `api/src/routes/machines.ts` answers — a revoke's `routing`,
// `content_key_rotation` and note, and the refusals `requireSession` and the
// route's own `unknown_machine` produce — because the point of these tests is
// that the three failures stay three different words on the screen, and that
// the sentence about what a revoke does not undo comes out of the response
// rather than out of this build's memory of it.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { ROTATION_LAZY, ROUTING_STOPPED, afterForget, forgetMachine, honestyIsOurs } from "./forget.ts"

const API = "https://api.example.test"
const MACHINE = "mac-a"

/** What the route answers a revoke with (`routes/machines.ts`). */
const REVOKED = {
  revoked_at: "2026-09-20T09:00:00.000Z",
  routing: ROUTING_STOPPED,
  content_key_rotation: ROTATION_LAZY,
  note: "Routing stops now. A device that already held the master secret can still open ciphertext it recorded earlier; key rotation happens lazily.",
}

interface Asked {
  url: string
  method?: string
  credentials?: string
}

/** A control plane that answers once, and records what it was asked. */
function plane(answer: { status: number; body?: unknown; throws?: boolean }) {
  const asked: Asked[] = []
  const get = ((url: string, init?: RequestInit) => {
    asked.push({ url, method: init?.method, credentials: init?.credentials })
    if (answer.throws) return Promise.reject(new TypeError("Failed to fetch"))
    return Promise.resolve({
      status: answer.status,
      json: () =>
        answer.body === undefined
          ? Promise.reject(new SyntaxError("Unexpected end of JSON input"))
          : Promise.resolve(answer.body),
    } as Response)
  }) as unknown as typeof fetch
  return { asked, get }
}

test("a machine forgotten: one DELETE with this browser's own cookie, and the honest sentence comes back with it", async () => {
  const { asked, get } = plane({ status: 200, body: REVOKED })
  const outcome = await forgetMachine(API, MACHINE, get)

  assert.deepEqual(asked, [
    { url: API + "/v1/machines/" + MACHINE, method: "DELETE", credentials: "include" },
  ], "the route is asked once, by DELETE, with the account cookie this browser already holds")
  assert.equal(outcome.kind, "forgotten")
  assert.ok(outcome.kind === "forgotten")
  assert.equal(outcome.revokedAt, REVOKED.revoked_at)
  assert.equal(outcome.routing, ROUTING_STOPPED)
  assert.equal(outcome.rotation, ROTATION_LAZY)
  assert.match(outcome.note, /key rotation happens lazily/)
  assert.equal(honestyIsOurs(outcome), true, "the page may say the sentence in the person's own language")
})

test("a revoke that says something else about keys is quoted, not translated", async () => {
  const { get } = plane({
    status: 200,
    body: { ...REVOKED, content_key_rotation: "immediate", note: "Keys were rotated." },
  })
  const outcome = await forgetMachine(API, MACHINE, get)

  assert.ok(outcome.kind === "forgotten")
  assert.equal(honestyIsOurs(outcome), false,
    "this build's sentence describes `lazy`; it must not be shown for a route that answered something else")
  assert.equal(outcome.note, "Keys were rotated.")
})

test("a revoke whose answer cannot be read still happened, and the page has no sentence to offer for it", async () => {
  const { get } = plane({ status: 200 })
  const outcome = await forgetMachine(API, MACHINE, get)

  assert.equal(outcome.kind, "forgotten", "a 200 is the revoke having happened, whatever came back with it")
  assert.ok(outcome.kind === "forgotten")
  assert.equal(outcome.note, "", "and nothing was said about the keys, so nothing may be said about them")
  assert.equal(honestyIsOurs(outcome), false)
})

test("refused: the account said no, nothing changed, and it is not asked twice", async () => {
  const { asked, get } = plane({
    status: 401,
    body: { error: { code: "no_session", message: "Sign in first" } },
  })
  const outcome = await forgetMachine(API, MACHINE, get)

  assert.equal(outcome.kind, "refused")
  assert.ok(outcome.kind === "refused")
  assert.equal(outcome.status, 401)
  assert.equal(outcome.code, "no_session")
  assert.equal(asked.length, 1, "a refusal is an answer; asking again is not what to do with one")
})

test("no such machine: a 404 is the account having none, which is not a refusal and not a failure to read", async () => {
  const { get } = plane({
    status: 404,
    body: { error: { code: "unknown_machine", message: "No such machine" } },
  })
  const outcome = await forgetMachine(API, MACHINE, get)

  assert.equal(outcome.kind, "absent")
  assert.ok(outcome.kind === "absent")
  assert.equal(outcome.code, "unknown_machine")
})

test("could not be read: nothing answered, so it is not known whether the machine was forgotten", async () => {
  const { asked, get } = plane({ status: 0, throws: true })
  const outcome = await forgetMachine(API, MACHINE, get)

  assert.equal(outcome.kind, "unreadable")
  assert.ok(outcome.kind === "unreadable")
  assert.equal(outcome.status, null, "no status at all: the request may still have arrived")
  assert.equal(outcome.code, "no_answer")
  assert.equal(asked.length, 1, "and it is not sent again, because a second DELETE would be a second irreversible act on a guess")
})

test("could not be read: a 500 is the far end failing, named by its status when it names no code", async () => {
  const { get } = plane({ status: 500, body: {} })
  const outcome = await forgetMachine(API, MACHINE, get)

  assert.equal(outcome.kind, "unreadable")
  assert.ok(outcome.kind === "unreadable")
  assert.equal(outcome.status, 500)
  assert.equal(outcome.code, "http_500")
})

test("an id with something in it that a path would read is escaped before it becomes one", async () => {
  const { asked, get } = plane({ status: 200, body: REVOKED })
  await forgetMachine(API, "../machines/other", get)

  assert.equal(asked[0].url, API + "/v1/machines/" + encodeURIComponent("../machines/other"))
})

test("forgetting the machine this tab is looking at takes the tab off it, and out of its memory", () => {
  assert.deepEqual(
    afterForget({ forgotten: MACHINE, remembered: MACHINE, reading: MACHINE }),
    { clearRemembered: true, backToList: true },
    "the console is reading a machine the account no longer routes to, and a reload would open it again",
  )
})

test("forgetting one this tab only remembers clears the memory and leaves the screen alone", () => {
  assert.deepEqual(afterForget({ forgotten: MACHINE, remembered: MACHINE, reading: null }), {
    clearRemembered: true,
    backToList: false,
  })
})

test("forgetting another machine touches neither the memory nor the console on screen", () => {
  assert.deepEqual(afterForget({ forgotten: "mac-b", remembered: MACHINE, reading: MACHINE }), {
    clearRemembered: false,
    backToList: false,
  })
})

test("a tab that remembers nothing and reads nothing is not sent anywhere by a forget", () => {
  assert.deepEqual(afterForget({ forgotten: MACHINE, remembered: null, reading: null }), {
    clearRemembered: false,
    backToList: false,
  })
})
