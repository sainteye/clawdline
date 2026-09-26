// The "waiting for you" block reads the queue the person answers on the Board:
// `node --test --experimental-strip-types "src/pages/now/waiting.test.ts"`
//
// Work system v2 files an Agent's proposal in `/v1/work/v2/proposals`, and the
// v1 list stays mounted and empty. A block reading the v1 list therefore said
// "nothing is waiting for you" in the present tense while the Board held a
// proposal — a zero for something it had never read.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { readDecisions, readProposals } from "./api.ts"
// @ts-expect-error -- as above.
import { joinWaiting } from "./waiting.ts"

const NOW = Math.floor(Date.now() / 1000)
const current = { observed_at: NOW, provenance: "work", freshness: "current" }

/** A pending v2 proposal, as `proposalV2Of` (internal/transport/http/work_v2.go) writes it. */
const pending = {
  id: "0f1e2d3c4b5a69788796a5b4c3d2e1f0", project_id: "p1", project_path: "/src/p1", kind: "issue",
  title: "The retry loop never gives up", description: "", reason: "seen twice", suggested_acceptance: "",
  session_id: "s1", source_work_id: "", source_todo_id: "", state: "pending", accepted_work_id: "",
  created_at: NOW - 600, resolved_at: null, version: 1,
}

/**
 * A daemon after the v2 cutover: the v2 queue holds the proposal, the v1 list
 * answers `current` with nothing in it, as its routes still do.
 */
function daemon(v2: { rows: unknown[]; truncated: boolean }) {
  const asked: string[] = []
  const answers: Record<string, unknown> = {
    "/v1/work/v2/proposals?state=pending": { ok: true, ...v2 },
    "/v1/work/proposals": { ok: true, state: "pending", counts: { pending: 0 }, rows: [], source: current },
    "/v1/work/decisions": { ok: true, state: "open", counts: { open: 0 }, rows: [], source: current },
  }
  globalThis.fetch = (async (url: string) => {
    asked.push(url)
    const body = answers[url]
    if (body === undefined) return new Response(JSON.stringify({ ok: false, code: "not_found", message: "no" }), { status: 404 })
    return new Response(JSON.stringify(body), { status: 200 })
  }) as typeof fetch
  return asked
}

async function block() {
  const settle = <T>(p: Promise<T>) =>
    p.then((page) => ({ ok: true as const, page }), (e: unknown) => ({ ok: false as const, why: String(e) }))
  return joinWaiting(await settle(readProposals()), await settle(readDecisions()))
}

test("a pending v2 proposal on the Board is a row in the block", async () => {
  const asked = daemon({ rows: [pending], truncated: false })
  const got = await block()
  assert.ok(asked.includes("/v1/work/v2/proposals?state=pending"), `asked ${asked.join(", ")}`)
  assert.equal(got.reading.read, true)
  assert.equal(got.reading.rows, 1)
  assert.deepEqual(got.rows.map((r: { kind: string; id: string; title: string }) => [r.kind, r.id, r.title]),
    [["proposal", pending.id, pending.title]])
})

test("a v2 queue longer than one page is read and short, never settled", async () => {
  daemon({ rows: [pending], truncated: true })
  const got = await block()
  assert.equal(got.reading.read, true)
  assert.equal(got.reading.source?.freshness, "stale")
})

test("an empty v2 queue is a reading that found nothing, not a missing one", async () => {
  daemon({ rows: [], truncated: false })
  const got = await block()
  assert.equal(got.reading.read, true)
  assert.equal(got.reading.rows, 0)
  assert.equal(got.reading.source?.freshness, "current")
})
