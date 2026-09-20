// The relay seam's rules: `node --test web/console/src/cloud/*.test.ts`.
//
// A fake CloudClient stands in for the copied one, holding rows the way it
// does (`_sessionResponse`) and counting transcript asks, which is the number
// that costs an envelope each way across the relay.
import { test } from "node:test"
import assert from "node:assert/strict"
import type { SessionsSnapshot, TranscriptPage } from "@clawdline/contract"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { RelayReader, TRANSCRIPT_LINE_REREAD_MS, TRANSCRIPT_MAX_REUSE_MS, type CloudEvent, type CloudReadClient, type CloudRow, type CloudSchedules } from "./relay-reader.ts"

class FakeClient implements CloudReadClient {
  ready = true
  sessionInventoryByMachine = new Map<string, unknown>()
  rows: CloudRow[] = []
  recovering: string[] = []
  asks = 0
  answer: () => Promise<unknown> = async () => ({ id: "s1", entries: [{ role: "user", text: "hi" }], signature: "1-1", evidence: "transcript" })
  private listeners = new Set<(event: CloudEvent) => void>()
  events(listener: (event: CloudEvent) => void) {
    this.listeners.add(listener)
    return () => this.listeners.delete(listener)
  }
  emit(event: CloudEvent) {
    for (const fn of this.listeners) fn(event)
  }
  async sessions() {
    return { sessions: this.rows, at: 100, scan: { emptyAuthoritative: this.recovering.length === 0, recovering: this.recovering, failures: [] } }
  }
  transcript() {
    this.asks += 1
    return this.answer()
  }
  pushKeyAsks = 0
  pushKey?: () => Promise<unknown> = async () => {
    this.pushKeyAsks += 1
    return { key: "BPk" }
  }
  /** Every reading of the schedule list, with the options it was asked under. */
  scheduleAsks: ({ fresh?: boolean } | undefined)[] = []
  scheduleAnswer: () => Promise<CloudSchedules> = async () => ({ schedules: [], at: 0 })
  schedules?: (options?: { fresh?: boolean }) => Promise<CloudSchedules> = (options) => {
    this.scheduleAsks.push(options)
    return this.scheduleAnswer()
  }
}

/** A schedule row as the copied client tags it: the Mac's own row, plus the machine. */
function schedule(machine: string, id: string, extra: Record<string, unknown> = {}) {
  return { id, machine, title: "a schedule", enabled: true, project_dir: "/tmp/p", ...extra }
}

function row(machine: string, session: string, extra: Record<string, unknown> = {}): CloudRow {
  return { id: session, machine, session, identity: { machine, session }, state: "idle", work_state: "ready", ...extra }
}

function reader(client: FakeClient, clock: { t: number }) {
  const r = new RelayReader("mac-a", { now: () => clock.t })
  r.attach(client)
  return r
}

async function body<T>(res: Response): Promise<T> {
  return (await res.json()) as T
}

test("the list is the chosen machine's rows, with the machine kept and the relay's key dropped", async () => {
  const client = new FakeClient()
  client.rows = [row("mac-a", "s1"), row("mac-b", "s2"), row("mac-a", "s3")]
  const r = reader(client, { t: 1000 })
  const snap = await body<SessionsSnapshot>(await r.fetch("/v1/sessions"))
  assert.deepEqual(snap.sessions.map((s) => s.id), ["s1", "s3"])
  assert.equal((snap.sessions[0] as unknown as { machine: string }).machine, "mac-a")
  assert.equal("identity" in snap.sessions[0], false)
})

test("empty is believed only after the machine's inventory, and not while it is still sending rows", async () => {
  const client = new FakeClient()
  const r = reader(client, { t: 1000 })
  const before = await r.snapshot()
  assert.equal(before.scan.emptyAuthoritative, false)
  client.sessionInventoryByMachine.set("mac-a", { ids: new Set() })
  assert.equal((await r.snapshot()).scan.emptyAuthoritative, true)
  client.recovering = ["mac-a"]
  const recovering = await r.snapshot()
  assert.equal(recovering.scan.emptyAuthoritative, false)
  assert.ok(recovering.scan.generation > before.scan.generation, "each snapshot is newer than the last")
  assert.equal(recovering.scan.epoch, before.scan.epoch)
})

test("with no writer carried, a write is refused as not carried, as an uncarried read is, both typed", async () => {
  const r = reader(new FakeClient(), { t: 0 })
  const post = await r.fetch("/v1/sessions/s1/send", { method: "POST", body: "{}" })
  assert.equal(post.status, 501)
  assert.deepEqual(await post.json(), {
    error: "cloud_not_carried",
    detail: "POST /v1/sessions/s1/send is not carried over Clawdline Cloud: do it on the Mac itself.",
    route: "/v1/sessions/s1/send",
  })
  const board = await r.fetch("/v1/board")
  assert.equal(board.status, 501)
  assert.equal((await body<{ error: string }>(board)).error, "cloud_not_carried")
  assert.deepEqual(r.log.map((x: { answer: string; code?: string }) => [x.answer, x.code]), [
    ["refused", "cloud_not_carried"],
    ["refused", "cloud_not_carried"],
  ])
})

test("with the line down, reads reject as unanswered instead of refusing", async () => {
  const client = new FakeClient()
  client.ready = false
  const r = reader(client, { t: 0 })
  await assert.rejects(r.fetch("/v1/health"), TypeError)
  await assert.rejects(r.fetch("/v1/sessions"), TypeError)
})

test("a transcript is asked again only when its row could say it changed", async () => {
  const client = new FakeClient()
  client.rows = [row("mac-a", "s1", { line: "Working (1s)" })]
  const clock = { t: 10_000 }
  const r = reader(client, clock)
  const read = async () => body<TranscriptPage>(await r.fetch("/v1/transcript?session=s1&limit=200"))

  assert.equal((await read()).id, "s1")
  assert.equal(client.asks, 1)

  clock.t += 4_000 // the console's next poll, nothing moved
  await read()
  assert.equal(client.asks, 1, "an unchanged row reuses the answer")

  client.rows = [row("mac-a", "s1", { line: "Working (5s)" })]
  clock.t += 4_000
  await read()
  assert.equal(client.asks, 1, "the status line alone is not worth a read inside the window")

  clock.t = 10_000 + TRANSCRIPT_LINE_REREAD_MS
  await read()
  assert.equal(client.asks, 2, "past the window the moving line costs one read")

  client.rows = [row("mac-a", "s1", { line: "Working (5s)", state: "waiting", work_state: "waiting_you" })]
  clock.t += 1_000
  await read()
  assert.equal(client.asks, 3, "any other change is read at once")

  clock.t += TRANSCRIPT_MAX_REUSE_MS
  await read()
  assert.equal(client.asks, 4, "and nothing is reused past the ceiling")
  assert.deepEqual(r.log.filter((x: { path: string }) => x.path === "/v1/transcript").map((x: { answer: string }) => x.answer), [
    "relay", "cache", "cache", "relay", "relay", "relay",
  ])
})

test("two polls while one read is in flight share it", async () => {
  const client = new FakeClient()
  client.rows = [row("mac-a", "s1")]
  let release!: () => void
  client.answer = () => new Promise((resolve) => (release = () => resolve({ entries: [], signature: "x" })))
  const r = reader(client, { t: 0 })
  const first = r.fetch("/v1/transcript?session=s1")
  await new Promise((resolve) => setImmediate(resolve))
  const second = r.fetch("/v1/transcript?session=s1")
  await new Promise((resolve) => setImmediate(resolve))
  release()
  const pages = await Promise.all([first, second].map(async (p) => body<TranscriptPage>(await p)))
  assert.equal(client.asks, 1)
  assert.deepEqual(pages.map((p) => [p.id, p.evidence]), [["s1", "transcript"], ["s1", "transcript"]])
})

test("the machine's refusal stays typed; a timeout is nobody answering", async () => {
  const client = new FakeClient()
  const r = reader(client, { t: 0 })
  client.answer = () => Promise.reject(Object.assign(new Error("no session named that"), { code: "not_found", status: 404 }))
  const refused = await r.fetch("/v1/transcript?session=gone")
  assert.equal(refused.status, 404)
  assert.equal((await body<{ error: string }>(refused)).error, "not_found")
  client.answer = () => Promise.reject(Object.assign(new Error("the Mac did not answer this read"), { code: "cloud_read_timeout" }))
  await assert.rejects(r.fetch("/v1/transcript?session=slow"), /cloud_read_timeout/)
})

test("the stream sends one frame per turn for this machine and none for another", async () => {
  const client = new FakeClient()
  client.rows = [row("mac-a", "s1")]
  const r = reader(client, { t: 0 })
  const frames: string[] = []
  const errors: unknown[] = []
  let opened = 0
  const handle = r.stream().open("/v1/events", {
    onFrame: (event: string, data: string) => frames.push(event + ":" + (JSON.parse(data) as SessionsSnapshot).sessions.length),
    onOpen: () => (opened += 1),
    onError: (cause: unknown) => errors.push(cause),
  })
  assert.equal(opened, 1)
  for (let i = 0; i < 5; i++) client.emit({ type: "sessions", identity: { machine: "mac-a", session: "s" + i } })
  client.emit({ type: "sessions", identity: { machine: "mac-b", session: "x" } })
  await new Promise((resolve) => setTimeout(resolve, 5))
  assert.deepEqual(frames, ["sessions:1"])
  client.emit({ type: "sessions", identity: { machine: "mac-b", session: "x" } })
  await new Promise((resolve) => setTimeout(resolve, 5))
  assert.deepEqual(frames, ["sessions:1"])
  client.emit({ type: "connection", state: "offline" })
  assert.equal(errors.length, 1)

  // A renewed client: the open stream follows it, is told the line is up, and gets the rows.
  const renewed = new FakeClient()
  renewed.rows = [row("mac-a", "s1"), row("mac-a", "s2")]
  r.attach(renewed)
  await new Promise((resolve) => setTimeout(resolve, 5))
  assert.equal(opened, 2)
  assert.deepEqual(frames, ["sessions:1", "sessions:2"])
  client.emit({ type: "sessions", identity: { machine: "mac-a", session: "s1" } })
  await new Promise((resolve) => setTimeout(resolve, 5))
  assert.deepEqual(frames, ["sessions:1", "sessions:2"], "the retired client is not listened to")
  handle.close()
})

// F2: "try again" reads the transcript first, and a read already on its way
// can be one asked before the attempt that failed. The copied client shares a
// read with any other for the same session (`readKey`), so asking again while
// one is in flight is handed that older answer. A `no-store` read waits for
// the one on its way to finish and then asks the Mac anew.
test("a fresh read is never handed an answer asked before it", async () => {
  const client = new FakeClient()
  client.rows = [row("mac-a", "s1")]
  const clock = { t: 1000 }
  const r = reader(client, clock)
  // The copied client's sharing: while one ask is out, another gets its answer.
  let release: (v: unknown) => void = () => {}
  let out: Promise<unknown> | null = null
  const stale = { id: "s1", entries: [], signature: "old", evidence: "transcript" }
  const now = { id: "s1", entries: [{ role: "user", text: "yes" }], signature: "new", evidence: "transcript" }
  client.answer = () => {
    if (out) return out
    out = new Promise((resolve) => { release = resolve }).then((v) => { out = null; return v })
    return out
  }
  const polling = r.fetch("/v1/transcript?session=s1&limit=200")
  await new Promise((resolve) => setTimeout(resolve, 0))
  const fresh = r.fetch("/v1/transcript?session=s1&limit=200", { cache: "no-store" })
  await new Promise((resolve) => setTimeout(resolve, 0))
  release(stale)
  await polling
  await new Promise((resolve) => setTimeout(resolve, 0))
  release(now)
  const page = await body<TranscriptPage>(await fresh)
  assert.equal(page.signature, "new", "the fresh read was handed the answer asked before it")
})

// The first thing a phone does when somebody presses "notify me". It is a
// read — the Mac mints the key and nothing else changes — so it is answered
// here rather than by the writer, and it is what the whole registration
// stopped on: before this route existed, `GET /v1/push/key` was refused
// `cloud_not_carried` in the browser and the request never left the phone.
test("the application server key is asked of the machine", async () => {
  const client = new FakeClient()
  const r = reader(client, { t: 1000 })
  const res = await r.fetch("/v1/push/key")
  assert.equal(res.status, 200)
  assert.deepEqual(await res.json(), { key: "BPk" })
  assert.equal(client.pushKeyAsks, 1)
  const last = r.log[r.log.length - 1]
  assert.equal(last.answer, "relay")
  assert.equal(last.word, "push-key")
})

// A copied client too old to know the word, and a machine that answered no.
// Both settle the request typed, in the flat spelling `push/api.ts` reads
// through `isRefusal` — never the static host's page as a body that is not
// JSON.
test("a key nobody can ask for is refused by name, not left to the network", async () => {
  const older = new FakeClient()
  older.pushKey = undefined
  const noWord = await reader(older, { t: 0 }).fetch("/v1/push/key")
  assert.equal(noWord.status, 501)
  assert.equal((await body<{ error: string }>(noWord)).error, "cloud_not_carried")

  const client = new FakeClient()
  client.pushKey = async () => {
    throw Object.assign(new Error("this Mac does not carry notifications"), {
      code: "cloud_feature_unavailable",
      status: 501,
    })
  }
  const refused = await reader(client, { t: 0 }).fetch("/v1/push/key")
  assert.equal(refused.status, 501)
  const refusal = await body<{ error: string; detail: string }>(refused)
  assert.equal(refusal.error, "cloud_feature_unavailable")
  assert.equal(typeof refusal.detail, "string", "`isRefusal` needs a string detail")
})

// The list under the session list, which on a phone was not there at all.
// `carry.ts` had the word in `DEFERRED` saying schedules were not read over
// Cloud yet, and this Mac had been answering `schedules` the whole time — so
// the seam refused the route to itself and the section stayed hidden, with
// nothing in the Mac's log because nothing had been asked for.
test("the schedule list is asked of this machine, and is this machine's rows", async () => {
  const client = new FakeClient()
  client.scheduleAnswer = async () => ({
    schedules: [schedule("mac-a", "s-1"), schedule("mac-b", "s-2"), schedule("mac-a", "s-3")],
    at: 1_700,
  })
  const r = reader(client, { t: 1000 })
  const res = await r.fetch("/v1/orchestrator/schedules")
  assert.equal(res.status, 200)
  const list = await body<{ schedules: { id: string }[]; at: number }>(res)
  assert.deepEqual(list.schedules.map((s) => s.id), ["s-1", "s-3"], "another Mac's schedules are not this list")
  assert.equal(list.at, 1_700)
  // `fresh`, and not for freshness: this daemon publishes no schedules on its
  // `orch/` descriptor, so the retained reading refuses forever — and the
  // fresh one is also what teaches the copied client which Mac an id is on,
  // which is what the four writes are routed by.
  assert.deepEqual(client.scheduleAsks, [{ fresh: true }])
  const last = r.log[r.log.length - 1]
  assert.equal(last.answer, "relay")
  assert.equal(last.word, "schedules")
})

// The distinction this whole read is drawn around: "this Mac has none" and
// "nobody answered" are opposite facts, and `pages/schedules.tsx` draws the
// section for one and leaves it alone for the other. An empty answer is an
// answer; a refusal must never arrive as `{schedules: []}`.
test("an empty list is only ever what the Mac said, never what a refusal became", async () => {
  const empty = new FakeClient()
  const said = await reader(empty, { t: 1000 }).fetch("/v1/orchestrator/schedules")
  assert.equal(said.status, 200)
  assert.deepEqual(await said.json(), { schedules: [], at: 1 }, "an answer with no rows is an answer")

  // The copied client's own refusal for a Mac running a build that publishes
  // no schedules, and for an account no snapshot has arrived from.
  for (const code of ["cloud_schedules_unpublished", "cloud_read_unavailable"]) {
    const client = new FakeClient()
    client.scheduleAnswer = async () => {
      throw Object.assign(new Error(code), { code })
    }
    const res = await reader(client, { t: 1000 }).fetch("/v1/orchestrator/schedules")
    assert.equal(res.status, 502, code + " must not resolve")
    assert.equal((await body<{ error: string }>(res)).error, code)
  }
})

// The fan-out's own trap: `schedules()` settles as soon as *one* machine
// answers, so an account with two Macs and one silent one resolves with the
// silent one's rows simply absent. Read as this machine's list that is the
// page asserting an inventory nobody read.
test("a machine that did not answer is not a machine with no schedules", async () => {
  const silent = new FakeClient()
  silent.scheduleAnswer = async () => ({
    schedules: [schedule("mac-b", "s-2")],
    at: 1_700,
    unanswered: [{ machine: "mac-a", label: "this Mac", error: Object.assign(new Error("timed out"), { code: "cloud_read_timeout" }) }],
  })
  // `cloud_read_timeout` is one of the codes that mean nobody answered, so it
  // rejects the way a dropped connection does and the list is left as it was.
  await assert.rejects(reader(silent, { t: 1000 }).fetch("/v1/orchestrator/schedules"), TypeError)

  const never = new FakeClient()
  never.scheduleAnswer = async () => ({ schedules: [schedule("mac-b", "s-2")], at: 1_700, unconfirmed: ["mac-a"] })
  const res = await reader(never, { t: 1000 }).fetch("/v1/orchestrator/schedules")
  assert.equal(res.status, 503)
  assert.equal((await body<{ error: string }>(res)).error, "cloud_read_unavailable")
})

// A copied client older than the word: refused by name, in the spelling
// `schedules-bridge.ts`'s `jsonFetch` reads, rather than thrown inside the
// one-minute lane where nothing is catching by type.
test("a client that cannot ask for schedules is refused by name", async () => {
  const older = new FakeClient()
  older.schedules = undefined
  const res = await reader(older, { t: 0 }).fetch("/v1/orchestrator/schedules")
  assert.equal(res.status, 501)
  const refusal = await body<{ error: string; detail: string }>(res)
  assert.equal(refusal.error, "cloud_not_carried")
  assert.match(refusal.detail, /schedules/)
})
