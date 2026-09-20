// Renaming a machine, against a fake control plane:
// `node --test web/console/src/cloud/rename.test.ts`.
//
// Nothing here touches a real account. The fake answers exactly what
// `api/src/routes/machines.ts` answers — `{ok: true}` for a PATCH it stored,
// and the refusals `requireSession` and the route's own `unknown_machine`
// produce — because the point of these tests is that the failures stay
// different words on the screen, and that "it is called that now" is only
// said over an answer that actually said so.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see forget.test.ts.
import { NAME_MAX, renameMachine } from "./rename.ts"

const API = "https://api.example.test"
const MACHINE = "mac-a"

interface Asked {
  url: string
  method?: string
  credentials?: string
  body?: unknown
}

/** A control plane that answers once, and records what it was asked. */
function plane(answer: { status: number; body?: unknown; throws?: boolean }) {
  const asked: Asked[] = []
  const get = ((url: string, init?: RequestInit) => {
    asked.push({ url, method: init?.method, credentials: init?.credentials, body: init?.body })
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

test("a machine renamed: one PATCH with this browser's own cookie, and the name it was given back", async () => {
  const { asked, get } = plane({ status: 200, body: { ok: true } })
  const outcome = await renameMachine(API, MACHINE, "  Frankfurt box  ", get)

  assert.equal(asked.length, 1)
  assert.equal(asked[0].url, API + "/v1/machines/" + MACHINE)
  assert.equal(asked[0].method, "PATCH")
  assert.equal(asked[0].credentials, "include", "the account cookie this browser already holds")
  assert.equal(asked[0].body, JSON.stringify({ name: "Frankfurt box" }), "trimmed as the route trims it")
  assert.equal(outcome.kind, "renamed")
  assert.ok(outcome.kind === "renamed")
  assert.equal(outcome.name, "Frankfurt box")
})

test("a refusal is the account saying no, and says nothing was changed", async () => {
  const { asked, get } = plane({ status: 401, body: { error: { code: "no_session" } } })
  const outcome = await renameMachine(API, MACHINE, "Frankfurt box", get)

  assert.equal(asked.length, 1)
  assert.equal(outcome.kind, "refused")
  assert.ok(outcome.kind === "refused")
  assert.equal(outcome.status, 401)
  assert.equal(outcome.code, "no_session")
})

test("a machine the account does not have is its own answer, not a refusal", async () => {
  const { get } = plane({ status: 404, body: { error: { code: "unknown_machine" } } })
  const outcome = await renameMachine(API, MACHINE, "Frankfurt box", get)

  assert.equal(outcome.kind, "absent")
  assert.ok(outcome.kind === "absent")
  assert.equal(outcome.code, "unknown_machine")
})

test("nothing answered: it is not known whether the name changed, and nothing is sent again", async () => {
  const { asked, get } = plane({ status: 0, throws: true })
  const outcome = await renameMachine(API, MACHINE, "Frankfurt box", get)

  assert.equal(asked.length, 1, "no retry: a PATCH that may have landed is not sent twice")
  assert.equal(outcome.kind, "unreadable")
  assert.ok(outcome.kind === "unreadable")
  assert.equal(outcome.status, null)
  assert.equal(outcome.code, "no_answer")
})

test("a 500 with no readable body is unreadable, and carries where it came from", async () => {
  const { get } = plane({ status: 500 })
  const outcome = await renameMachine(API, MACHINE, "Frankfurt box", get)

  assert.equal(outcome.kind, "unreadable")
  assert.ok(outcome.kind === "unreadable")
  assert.equal(outcome.status, 500)
  assert.equal(outcome.code, "http_500")
})

test("a 200 that does not say ok is not read as one", async () => {
  const { get } = plane({ status: 200, body: { queued: true } })
  const outcome = await renameMachine(API, MACHINE, "Frankfurt box", get)

  assert.equal(outcome.kind, "unreadable")
  assert.ok(outcome.kind === "unreadable")
  assert.equal(outcome.code, "unreadable_answer")
})

test("an empty name never leaves the browser: the route would answer ok and store nothing", async () => {
  const { asked, get } = plane({ status: 200, body: { ok: true } })
  const outcome = await renameMachine(API, MACHINE, "   ", get)

  assert.deepEqual(asked, [], "nothing was asked")
  assert.equal(outcome.kind, "blank")
})

test("a name past the account's bound is refused here, by the bound's own number", async () => {
  const { asked, get } = plane({ status: 200, body: { ok: true } })
  const outcome = await renameMachine(API, MACHINE, "n".repeat(NAME_MAX + 1), get)

  assert.deepEqual(asked, [])
  assert.equal(outcome.kind, "too_long")
  assert.ok(outcome.kind === "too_long")
  assert.equal(outcome.max, NAME_MAX)
})

test("an id is a path segment, not a path", async () => {
  const { asked, get } = plane({ status: 200, body: { ok: true } })
  await renameMachine(API, "../machines/other", "Frankfurt box", get)

  assert.equal(asked[0].url, API + "/v1/machines/" + encodeURIComponent("../machines/other"))
})
