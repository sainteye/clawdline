// Agent investigations on the operator incident detail, and their start, against the Cloud's
// investigation contract v1 (§1 start, §2 read, §6 state rule).
// `node --experimental-strip-types --test web/console/src/cloud/admin/health/investigations.test.ts`.
// Bodies come from health/fixture.ts or are built here as the Cloud's investigationView builds them.
import { test } from "node:test"
import assert from "node:assert/strict"
import type { Investigation, Investigations } from "./investigations.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { INVESTIGATION_REASONS, liveInvestigation, parseInvestigations, reasonWords, startGate } from "./investigations.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { fetchDestinations, parseDestinations, startInvestigation, startRequestFor } from "./investigation-actions.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { parseIncidentDetail } from "./stored.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { FIXTURE_INCIDENTS, fixtureAnswer } from "./fixture.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { healthWords } from "./words.ts"

const NOW = Date.parse("2026-10-20T12:00:00.000Z")
const API = "https://api.example.invalid"
const INC = FIXTURE_INCIDENTS as Record<"active" | "loss" | "recovering" | "review" | "closed" | "expired", string>
const en = healthWords("en")
const zh = healthWords("zh-TW")

const view = (over: Record<string, unknown> = {}) => ({
  investigation_id: "hiv_0123456789abcdef0123456789abcdef", incident_id: "hi_0123456789abcdef0123456789abcdef",
  incident_revision: 3, incident_content_hash: "a".repeat(64), hook_id: "swh_abc", machine_id: "mac_abc", delivery_id: "swd_abc",
  started_at: "2026-10-20T11:00:00.000Z", deadline_at: "2026-10-20T15:02:00.000Z", closed_at: null,
  state: "investigating", reason: "queued_unclaimed", delivery_state: "queued", task_terminal_state: null, report: null, recovery: null,
  ...over,
})
type Inv = Investigation
type Invs = Investigations
const itemsOf = (r: Invs): Inv[] => (r.status === "ok" ? r.items : assert.fail(`expected ok, got ${r.status}`))
const val = (k: { known: boolean; value?: string } | null | undefined): string | null => (k && k.known ? k.value ?? null : null)
const ok = (...items: unknown[]) => ({ status: "ok", state_rule: "investigation-state/1", items })
const detail = async (scenario: string, id: string) =>
  parseIncidentDetail(await (await fixtureAnswer(scenario as never, `${API}/su/v1/health/incidents/${id}`, NOW)).json())

/* ---- reading ------------------------------------------------------------------ */

test("an older Cloud without the field is not supported, never an empty list, and cannot start", async () => {
  const d = await detail("investigations_old", INC.loss)
  assert.deepEqual(d.investigations, { status: "unsupported" })
  assert.equal(startGate(d.investigations, true, false), "unsupported")
})

test("unreadable investigation storage is unavailable and blocks a start", async () => {
  const d = await detail("investigations_unavailable", INC.loss)
  assert.deepEqual(d.investigations, { status: "unavailable" })
  assert.equal(startGate(d.investigations, true, false), "unavailable")
  // A malformed field is unavailable too, not an empty ok list.
  assert.deepEqual(parseInvestigations({ status: "ok", items: null }), { status: "unavailable" })
  assert.deepEqual(parseInvestigations({ status: "weird" }), { status: "unavailable" })
})

test("an ok list with no rows allows a start; a live one does not", async () => {
  const empty = (await detail("normal", INC.loss)).investigations
  assert.deepEqual(empty, { status: "ok", items: [], unreadable: 0 })
  assert.equal(startGate(empty, true, false), "ok")
  assert.equal(startGate(empty, false, false), "older_revision")
  assert.equal(startGate(empty, true, true), "expired")
  const live = (await detail("normal", INC.active)).investigations
  assert.equal(val(liveInvestigation(live)?.reason), "running")
  assert.equal(startGate(live, true, false), "live")
})

test("queued, running, failed, success without a report, needs evidence and verified each read in words", () => {
  const cases: [Record<string, unknown>, string, string][] = [
    [{ reason: "queued_unclaimed" }, "investigating", "Waiting for your machine to pick up the delivery."],
    [{ reason: "running", delivery_state: "schedule_dispatch_accepted" }, "investigating", "The agent is investigating on your machine."],
    [{ state: "needs_you", reason: "task_not_successful", task_terminal_state: "failure", closed_at: "2026-10-20T11:30:00.000Z" }, "needs_you", "The agent's task ended without succeeding."],
    [{ state: "needs_you", reason: "report_missing", task_terminal_state: "success" }, "needs_you", "The task ended, but the agent sent no report, so nothing can be verified."],
    [{ state: "needs_you", reason: "report_needs_evidence", task_terminal_state: "success", report: { conclusion: "needs_evidence", fix_commit: null, evidence_refs: [] } }, "needs_you", "The agent needs more evidence than this incident holds."],
    [{ state: "verified_recovered", reason: null, task_terminal_state: "success", report: { conclusion: "fix_prepared", fix_commit: "0".repeat(40), evidence_refs: ["he_1"] }, recovery: { event_key: "he_1", producer_seq: 42, received_at: "2026-10-20T11:40:00.000Z", basis: "evidence_ref" } }, "verified_recovered", "A recovery record in the same operation, received after the start, matches the agent's fix."],
  ]
  for (const [over, state, why] of cases) {
    const inv = itemsOf(parseInvestigations(ok(view(over))))[0]
    assert.deepEqual(inv.state, { known: true, value: state })
    assert.equal(reasonWords(inv, en).why, why)
  }
  // Task success alone is never recovery: success with no report reads as needing the person.
  const noReport = itemsOf(parseInvestigations(ok(view({ state: "needs_you", reason: "report_missing", task_terminal_state: "success" }))))[0]
  assert.notEqual(val(noReport.state), "verified_recovered")
})

test("every reason has its own explanation and next step in English and Chinese", () => {
  for (const say of [en, zh]) {
    const whys = new Set<string>()
    for (const reason of INVESTIGATION_REASONS) {
      const inv = itemsOf(parseInvestigations(ok(view({ state: "needs_you", reason }))))[0]
      const { why, next } = reasonWords(inv, say)
      assert.ok(!why.startsWith("iv_") && !next.startsWith("iv_"), `${reason} has no words`)
      whys.add(why)
    }
    assert.equal(whys.size, INVESTIGATION_REASONS.length, "two reasons share one explanation")
  }
})

test("an unknown state, reason, conclusion or basis is kept as unknown and does not refuse the incident", async () => {
  const r: Invs = parseInvestigations(ok(view({ state: "escalated", reason: "new_reason", report: { conclusion: "rewritten", fix_commit: "not-a-commit", evidence_refs: ["he_1", "../etc"] }, recovery: { event_key: "he_1", basis: "telepathy" } })))
  const inv = itemsOf(r)[0]
  assert.deepEqual(inv.state, { known: false, raw: "escalated" })
  assert.equal(reasonWords(inv, en).why, "Cloud gave a reason this page does not recognise (new_reason).")
  assert.deepEqual(inv.report?.conclusion, { known: false, raw: "rewritten" })
  assert.equal(inv.report?.fix_commit, null)
  assert.deepEqual(inv.report?.evidence_refs, ["he_1"])
  assert.deepEqual(inv.recovery?.basis, { known: false, raw: "telepathy" })
  // An open row this page cannot place may be the live one, so no start is offered.
  assert.equal(startGate(r, true, false), "unknown_state")
  // A row without an id is counted, not dropped silently, and the detail still parses.
  const body = await (await fixtureAnswer("normal", `${API}/su/v1/health/incidents/${INC.loss}`, NOW)).json() as Record<string, unknown>
  const d = parseIncidentDetail({ ...body, investigations: ok({ state: "needs_you" }, view({ state: "verified_recovered", reason: null, closed_at: "2026-10-20T11:30:00.000Z" })) })
  assert.equal(d.investigations.status === "ok" && d.investigations.unreadable, 1)
  assert.equal(startGate(d.investigations, true, false), "unknown_state")
})

test("the fixture's incidents carry verified, verification-failed and earlier investigations, newest first", async () => {
  const review = (await detail("normal", INC.review)).investigations
  assert.deepEqual(itemsOf(review).map((i) => val(i.state)), ["verified_recovered", "needs_you", "needs_you"])
  const closed = (await detail("normal", INC.closed)).investigations
  assert.equal(val(itemsOf(closed)[0].reason), "report_verification_failed")
})

/* ---- where an investigation can run --------------------------------------------- */

const SCHEMA = "clawdline.health_investigation.destinations.v1"
const MAC_HOOK = "swh_mac_investigation"
const row = (machine_id: string, machine_name: string, status: unknown, hook_id: string | null = null, reason = "r") => ({ machine_id, machine_name, status, hook_id, reason })
const listOf = (machines: unknown, over: Record<string, unknown> = {}) => ({ schema: SCHEMA, status: "ok", complete: true, machines, ...over })
const okList = (body: unknown) => {
  const d = parseDestinations(body)
  assert.ok(d !== null && d.status === "ok")
  return d
}

test("a machine with three webhooks and a Linux machine with one are two machines, each listed once", () => {
  // Cloud resolves the first machine's three hooks to its one investigation hook; the Linux machine has none bound.
  const d = okList(listOf([row("mac_1", "Studio", "ready", MAC_HOOK), row("linux_1", "Build server", "setup_required")]))
  assert.equal(d.machines.length, 2)
  assert.deepEqual(d.machines.map((m) => [m.machine_name, m.state]), [["Studio", "ready"], ["Build server", "setup_required"]])
  // The name is shown on its own; the webhook stays a separate field and never joins it.
  assert.equal(d.machines[0].machine_name, "Studio")
  assert.equal(d.machines[1].hook_id, null)
  // A list repeating one machine per hook, as the webhook list did, cannot be trusted at all.
  assert.equal(parseDestinations(listOf([row("mac_1", "Studio", "ready", MAC_HOOK), row("mac_1", "Studio", "ready", "swh_other")])), null)
  // Nor can two machines claiming one hook.
  assert.equal(parseDestinations(listOf([row("mac_1", "A", "ready", MAC_HOOK), row("mac_2", "B", "ready", MAC_HOOK)])), null)
})

test("two machines with the same name and different IDs stay two rows", () => {
  const d = okList(listOf([row("mac_1", "MacBook Pro", "ready", "swh_one"), row("mac_2", "MacBook Pro", "ready", "swh_two")]))
  assert.deepEqual(d.machines.map((m) => m.machine_id), ["mac_1", "mac_2"])
  assert.deepEqual(startRequestFor(d, "mac_2", 4), { hook_id: "swh_two", expected_incident_revision: 4 })
})

test("only a ready machine maps to a start; ambiguous, unconfigured, offline and nothing chosen do not", () => {
  const d = okList(listOf([
    row("mac_1", "One", "ambiguous", "swh_first_of_many"), row("mac_2", "Two", "setup_required"), row("mac_3", "Three", "offline", "swh_offline"), row("mac_4", "Four", "ready", "swh_four"),
  ]))
  // A non-ready row never carries a hook onward, even when Cloud sent one.
  assert.deepEqual(d.machines.map((m) => m.hook_id), [null, null, null, "swh_four"])
  for (const id of ["mac_1", "mac_2", "mac_3", "", "mac_missing"]) assert.equal(startRequestFor(d, id, 2), null)
  assert.deepEqual(startRequestFor(d, "mac_4", 2), { hook_id: "swh_four", expected_incident_revision: 2 })
})

test("an unknown state or a ready row without a hook is said as unknown and the list stops claiming to be complete", () => {
  const d = okList(listOf([row("mac_1", "One", "sleeping", "swh_x"), row("mac_2", "Two", "ready", null), row("mac_3", "Three", "ready", "swh_three")]))
  assert.deepEqual(d.machines.map((m) => [m.state, m.raw, m.hook_id]), [["unknown", "sleeping", null], ["unknown", "ready", null], ["ready", null, "swh_three"]])
  assert.equal(d.complete, false)
  assert.equal(startRequestFor(d, "mac_1", 1), null)
  // Cloud's own partial or paged answer stays partial.
  assert.equal(okList(listOf([row("mac_3", "Three", "ready", "swh_three")], { complete: false })).complete, false)
  // Unreadable shapes fail closed rather than look like an empty or complete list.
  for (const bad of [listOf(null), listOf([row("mac_1", "", "ready", "swh_a")]), listOf([row("mac 1", "One", "ready", "swh_a")]), listOf([row("mac_1", "One", "ready", "bad hook")]),
    { ...listOf([]), schema: "clawdline.schedule_webhook.list.v1" }, { ...listOf([]), status: "degraded" }, { ...listOf([]), complete: "yes" }]) {
    assert.equal(parseDestinations(bad), null, JSON.stringify(bad))
  }
  assert.deepEqual(okList(listOf([])).machines, [])
  assert.deepEqual(parseDestinations({ schema: SCHEMA, status: "unavailable", complete: false, machines: null }), { status: "unavailable" })
})

test("the destinations read: the real route, an older Cloud's 404 as not supported, and no fallback to the webhook list", async () => {
  const seen: string[] = []
  const get = (async (u: RequestInfo | URL) => { seen.push(String(u)); return fixtureAnswer("normal", String(u), NOW) }) as typeof fetch
  const r = await fetchDestinations(get, API, new AbortController().signal)
  assert.ok("ok" in r && r.ok && r.destinations.status === "ok")
  assert.deepEqual(seen, [`${API}/su/v1/health/investigation-destinations`])
  const answer = (status: number, body: unknown = {}) => (async () => new Response(JSON.stringify(body), { status })) as typeof fetch
  const signal = new AbortController().signal
  assert.deepEqual(await fetchDestinations(answer(404, { error: { code: "not_found" } }), API, signal), { ok: false, code: "not_supported" })
  assert.deepEqual(await fetchDestinations(answer(401), API, signal), { refused: "signed_out" })
  assert.deepEqual(await fetchDestinations(answer(503), API, signal), { ok: false, code: "unavailable" })
  assert.deepEqual(await fetchDestinations(answer(200, { hooks: [] }), API, signal), { ok: false, code: "contract_shape" })
  assert.deepEqual(await fetchDestinations((async () => { throw new TypeError("offline") }) as typeof fetch, API, signal), { ok: false, code: "network" })
})

test("each machine state has its own Traditional Chinese words and next step", () => {
  for (const s of ["ready", "setup_required", "ambiguous", "offline", "unknown"]) {
    const word = zh(`iv_dest_${s}` as never)
    const next = zh(`iv_destNext_${s}` as never, { raw: "sleeping" })
    assert.notEqual(word, en(`iv_dest_${s}` as never))
    assert.match(word + next, /[\u4e00-\u9fff]/)
    assert.doesNotMatch(next, /\{\w+\}/)
  }
  assert.match(zh("iv_destNext_unknown", { raw: "sleeping" }), /sleeping/)
})

/* ---- starting ---------------------------------------------------------------- */

type Call = { url: string; init: RequestInit }
function recorder(answer: (call: Call, n: number) => Response | "throw" | Promise<Response | "throw">) {
  const calls: Call[] = []
  const get = (async (u: RequestInfo | URL, init?: RequestInit) => {
    const call = { url: String(u), init: init ?? {} }
    calls.push(call)
    const r = await answer(call, calls.length)
    if (r === "throw") throw new TypeError("network")
    return r
  }) as typeof fetch
  return { get, calls }
}
const fixtureCall = (scenario: string) => (c: Call) =>
  fixtureAnswer(scenario as never, c.url, NOW, c.init.method ?? "GET", { key: new Headers(c.init.headers).get("idempotency-key"), body: String(c.init.body ?? "") })

test("a start sends the exact body and key; a lost answer is retried with the same key and replays one investigation", async () => {
  const req = { hook_id: "swh_fixture00000000000000000000a1", expected_incident_revision: 0 }
  const rev = (await detail("normal", INC.recovering)).incident.revision
  req.expected_incident_revision = rev
  const real = fixtureCall("normal")
  // The first answer is lost after Cloud committed it.
  const { get, calls } = recorder(async (c, n) => { const r = await real(c); return n === 1 ? "throw" : r })
  const signal = new AbortController().signal
  const first = await startInvestigation(get, API, INC.recovering, null, req, "inv-key-1", signal)
  assert.deepEqual(first, { ok: false, definite: false, code: "network" })
  const again = await startInvestigation(get, API, INC.recovering, null, req, "inv-key-1", signal)
  assert.ok("ok" in again && again.ok)
  assert.equal(again.replayed, true)
  assert.equal(val(again.investigation?.reason), "queued_unclaimed")
  for (const c of calls) {
    assert.equal(c.url, `${API}/su/v1/health/incidents/${INC.recovering}/investigations`)
    assert.equal(c.init.method, "POST")
    assert.equal(c.init.credentials, "include")
    assert.equal(new Headers(c.init.headers).get("idempotency-key"), "inv-key-1")
    assert.deepEqual(JSON.parse(String(c.init.body)), req)
    assert.deepEqual(Object.keys(JSON.parse(String(c.init.body))), ["hook_id", "expected_incident_revision"])
  }
  // The incident now reads back with that one live investigation, and a second start is refused.
  const d = await detail("normal", INC.recovering)
  assert.equal(startGate(d.investigations, true, false), "live")
  const second = await startInvestigation(get, API, INC.recovering, null, req, "inv-key-2", signal)
  assert.deepEqual(second, { ok: false, definite: true, code: "investigation_open" })
})

test("Cloud's typed refusals are definite; an untyped 5xx keeps the key; a signed-out session is a refusal", async () => {
  const signal = new AbortController().signal
  const req = { hook_id: "swh_x", expected_incident_revision: 1 }
  const answer = (status: number, code?: string) => recorder(() => new Response(JSON.stringify(code ? { error: { code, message: code } } : {}), { status })).get
  for (const [status, code] of [[409, "incident_revision_moved"], [409, "hook_unavailable"], [403, "pro_required"], [429, "webhook_queue_full"], [409, "idempotency_conflict"], [503, "health_store_unavailable"], [403, "bad_origin"]] as const) {
    assert.deepEqual(await startInvestigation(answer(status, code), API, INC.loss, null, req, "k", signal), { ok: false, definite: true, code })
  }
  assert.deepEqual(await startInvestigation(answer(502), API, INC.loss, null, req, "k", signal), { ok: false, definite: false, code: "unavailable" })
  assert.deepEqual(await startInvestigation(answer(401), API, INC.loss, null, req, "k", signal), { refused: "signed_out" })
  assert.deepEqual(await startInvestigation(answer(404), API, INC.loss, null, req, "k", signal), { refused: "forbidden" })
  // A 202 whose body is not the started schema leaves the outcome unknown, so the key is kept.
  const odd = recorder(() => new Response("{}", { status: 202 })).get
  assert.deepEqual(await startInvestigation(odd, API, INC.loss, null, req, "k", signal), { ok: false, definite: false, code: "contract_shape" })
  // The operator's account scope travels as the read's own query.
  const { get, calls } = recorder(() => new Response("{}", { status: 503 }))
  await startInvestigation(get, API, INC.loss, "acct_1", req, "k", signal)
  assert.equal(calls[0].url, `${API}/su/v1/health/incidents/${INC.loss}/investigations?account=acct_1`)
})

test("the chosen machine's hook is what the start sends, and a lost answer is retried with the same key and body", async () => {
  const r = await fetchDestinations((async (u: RequestInfo | URL) => fixtureAnswer("normal", String(u), NOW)) as typeof fetch, API, new AbortController().signal)
  assert.ok("ok" in r && r.ok)
  const rev = (await detail("normal", INC.loss)).incident.revision
  const req = startRequestFor(r.destinations, "fixture_mac_b", rev)
  assert.deepEqual(req, { hook_id: "swh_fixture00000000000000000000b2", expected_incident_revision: rev })
  const real = fixtureCall("normal")
  const { get, calls } = recorder(async (c, n) => { const a = await real(c); return n === 1 ? "throw" : a })
  const signal = new AbortController().signal
  assert.deepEqual(await startInvestigation(get, API, INC.loss, null, req!, "inv-dest-1", signal), { ok: false, definite: false, code: "network" })
  const again = await startInvestigation(get, API, INC.loss, null, req!, "inv-dest-1", signal)
  assert.ok("ok" in again && again.ok)
  assert.equal(calls.length, 2)
  for (const c of calls) {
    assert.equal(new Headers(c.init.headers).get("idempotency-key"), "inv-dest-1")
    assert.deepEqual(JSON.parse(String(c.init.body)), req)
  }
})
