// The relay seam's rules: `node --test web/console/src/cloud/*.test.ts`.
//
// A fake CloudClient stands in for the copied one, holding rows the way it
// does (`_sessionResponse`) and counting transcript asks, which is the number
// that costs an envelope each way across the relay.
import { test } from "node:test"
import assert from "node:assert/strict"
import type { SessionsSnapshot, TranscriptPage } from "@clawdline/contract"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { RelayReader, TRANSCRIPT_LINE_REREAD_MS, TRANSCRIPT_MAX_REUSE_MS, type CloudEvent, type CloudIdentity, type CloudReadClient, type CloudRow, type CloudSchedules, type CloudSnippets } from "./relay-reader.ts"

class FakeClient implements CloudReadClient {
  ready = true
  sessionInventoryByMachine = new Map<string, unknown>()
  machineRows: { id: string; freshness: "current" | "stale" | "unknown" }[] = [
    { id: "mac-a", freshness: "current" },
  ]
  rows: CloudRow[] = []
  recovering: string[] = []
  asks = 0
  answer: () => Promise<unknown> = async () => ({ id: "s1", entries: [{ role: "user", text: "hi" }], signature: "1-1", evidence: "transcript" })
  private listeners = new Set<(event: CloudEvent) => void>()
  events(listener: (event: CloudEvent) => void) {
    this.listeners.add(listener)
    return () => this.listeners.delete(listener)
  }
  async machines() {
    return { machines: this.machineRows, syncing: false, retryAfterMs: 0 }
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
  /** Every reading of the snippet list: the identity it named and the options. */
  snippetAsks: { identity: CloudIdentity; options?: { fresh?: boolean } }[] = []
  snippetAnswer: () => Promise<CloudSnippets> = async () => ({ snippets: [], at: 0 })
  snippets?: (identity: CloudIdentity, options?: { fresh?: boolean }) => Promise<CloudSnippets> = (identity, options) => {
    this.snippetAsks.push({ identity, options })
    return this.snippetAnswer()
  }
  // The two shapes below stand for two different client methods and are both
  // kept: `snippets()` is the copied client's own named reader, and
  // `_machineRequest` is the generic underneath every word that has no named
  // method. A test that asks for the snippet list reads `snippetAsks`; a test
  // that asks for a work-system word reads `reads`.
  /** Every read sent by word, with the machine and the body it carried. */
  reads: { machine: string; word: string; body: Record<string, unknown> }[] = []
  readAnswer: (word: string) => Promise<unknown> = async (word) => ({ read: word })
  _machineRequest?: (machine: string, word: string, body: Record<string, unknown>, kind: "read") => Promise<unknown> = (
    machine,
    word,
    reqBody,
  ) => {
    this.reads.push({ machine, word, body: reqBody })
    return this.readAnswer(word)
  }
  _place(value: unknown) {
    if (value === "cloud-p1") return { machine: "mac-a", id: "p1", path: "/repo" }
    throw Object.assign(new Error("Project not found"), { code: "not_found", status: 404 })
  }
}

/** A snippet row as the copied client tags it, with nothing of anybody's in it. */
function snippet(machine: string, id: string, extra: Record<string, unknown> = {}) {
  return { id, machine, title: "a title", body: "a body", scope: "global", position: 0, ...extra }
}

/** A schedule row as the copied client tags it: the machine's own row, plus the machine. */
function schedule(machine: string, id: string, extra: Record<string, unknown> = {}) {
  return { id, machine, title: "a schedule", enabled: true, project_dir: "/tmp/p", ...extra }
}

function row(machine: string, session: string, extra: Record<string, unknown> = {}): CloudRow {
  return { id: session, machine, session, identity: { machine, session }, state: "idle", work_state: "ready", ...extra }
}

function inventory(machine: string, ...sessions: string[]) {
  return { ids: new Set(sessions.map((session) => machine + "\u0000" + session)) }
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

test("an agent transcript is asked of the chosen machine with both decoded ids and the local limit", async () => {
  const client = new FakeClient()
  const r = reader(client, { t: 1000 })

  const answer = await r.fetch("/v1/sessions/root%20pane/agents/agent%204?limit=5000")
  assert.equal(answer.status, 200)
  assert.deepEqual(client.reads, [{
    machine: "mac-a",
    word: "agent",
    body: { session: "root pane", agent: "agent 4", limit: 1000 },
  }])
  assert.deepEqual(await answer.json(), { read: "agent" })
  assert.equal(r.log.at(-1)?.word, "agent")
})

test("empty is believed only after the machine's inventory, and not while it is still sending rows", async () => {
  const client = new FakeClient()
  const r = reader(client, { t: 1000 })
  const before = await r.snapshot()
  assert.equal(before.scan.emptyAuthoritative, false)
  client.sessionInventoryByMachine.set("mac-a", inventory("mac-a"))
  assert.equal((await r.snapshot()).scan.emptyAuthoritative, true)
  client.recovering = ["mac-a"]
  const recovering = await r.snapshot()
  assert.equal(recovering.scan.emptyAuthoritative, false)
  assert.ok(recovering.scan.generation > before.scan.generation, "each snapshot is newer than the last")
  assert.equal(recovering.scan.epoch, before.scan.epoch)
})

test("the first list paints once after every row named by the inventory has arrived", async () => {
  const client = new FakeClient()
  client.rows = [row("mac-a", "s1")]
  const r = reader(client, { t: 1000 })
  const controller = new AbortController()
  let answered = false
  const pending = r.fetch("/v1/sessions", { signal: controller.signal }).then(async (response) => {
    answered = true
    return body<SessionsSnapshot>(response)
  })

  await new Promise((resolve) => setTimeout(resolve, 5))
  assert.equal(answered, false, "a partial retained row is not the first paint")

  client.sessionInventoryByMachine.set("mac-a", inventory("mac-a", "s1", "s2"))
  client.emit({ type: "sessions", identity: { machine: "mac-a", session: "__clawdline_inventory_v1__" } })
  await new Promise((resolve) => setTimeout(resolve, 5))
  assert.equal(answered, false, "the marker is a receipt for rows, not a substitute for them")

  client.rows = [row("mac-a", "s1"), row("mac-a", "s3")]
  client.emit({ type: "sessions", identity: { machine: "mac-a", session: "s3" } })
  await new Promise((resolve) => setTimeout(resolve, 5))
  assert.equal(answered, false, "the same number of rows is not the marker's exact set")

  client.rows = [row("mac-a", "s1"), row("mac-a", "s2")]
  client.emit({ type: "sessions", identity: { machine: "mac-a", session: "s2" } })
  const snapshot = await pending
  assert.deepEqual(snapshot.sessions.map((session) => session.id), ["s1", "s2"])
  assert.equal(snapshot.scan.complete, true)
})

test("the first list falls back to its best partial reading at the existing request deadline", async () => {
  const client = new FakeClient()
  client.rows = [row("mac-a", "s1")]
  const r = reader(client, { t: 1000 })
  const controller = new AbortController()
  const pending = r.fetch("/v1/sessions", { signal: controller.signal })

  await new Promise((resolve) => setTimeout(resolve, 5))
  controller.abort()
  const snapshot = await body<SessionsSnapshot>(await pending)
  assert.deepEqual(snapshot.sessions.map((session) => session.id), ["s1"])
  assert.equal(snapshot.scan.complete, false)
})

// A viewer that connected after the machine's last change holds no inventory
// marker for it. Two facts follow, and the page needs both: the list it can
// draw is whatever rows did arrive, and it must not be read as the whole set.
//
// This is what the empty state's wording rests on. A viewer with rows draws
// them — the "waiting" sentence is only ever for a genuinely empty list — and
// a viewer with neither rows nor marker is waiting on the machine, not on an
// app it has no line to.
test("rows that arrived without the machine's marker are drawn, and are still not the whole set", async () => {
  const client = new FakeClient()
  client.rows = [row("mac-a", "s1"), row("mac-a", "s3")]
  const r = reader(client, { t: 1000 })

  const waiting = await r.snapshot()
  assert.deepEqual(waiting.sessions.map((s) => s.id), ["s1", "s3"], "the rows it has are handed over")
  assert.equal(waiting.scan.complete, false, "without the marker no reading is the whole set")
  assert.equal(waiting.scan.emptyAuthoritative, false)

  // The marker arriving is what makes it whole; nothing else does.
  client.sessionInventoryByMachine.set("mac-a", inventory("mac-a", "s1", "s3"))
  const whole = await r.snapshot()
  assert.equal(whole.scan.complete, true)
  assert.deepEqual(whole.sessions.map((s) => s.id), ["s1", "s3"])
})

test("with no writer carried, a write is refused as not carried, as an uncarried read is, both typed", async () => {
  const r = reader(new FakeClient(), { t: 0 })
  const post = await r.fetch("/v1/sessions/s1/send", { method: "POST", body: "{}" })
  assert.equal(post.status, 501)
  assert.deepEqual(await post.json(), {
    error: "cloud_not_carried",
    detail: "POST /v1/sessions/s1/send is not carried over Clawdline Cloud: do it on the machine itself.",
    route: "/v1/sessions/s1/send",
  })
  // `/v1/board` stood here until this console began asking for it. The read
  // that stands for "most of this daemon's API" now is one that is no Cloud
  // word at all (`carry.ts`, `notCarriedDetail`'s second sentence).
  const uncarried = await r.fetch("/v1/devstacks")
  assert.equal(uncarried.status, 501)
  assert.equal((await body<{ error: string }>(uncarried)).error, "cloud_not_carried")
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

test("health belongs to the chosen machine, not merely the relay socket", async () => {
  const client = new FakeClient()
  const r = reader(client, { t: 12_000 })

  const current = await r.fetch("/v1/health")
  assert.equal(current.status, 200)
  assert.equal((await body<{ ok: boolean }>(current)).ok, true)

  client.machineRows = [{ id: "mac-a", freshness: "stale" }]
  const stale = await r.fetch("/v1/health")
  assert.equal(stale.status, 503)
  assert.equal((await body<{ error: string }>(stale)).error, "machine_stale")

  client.machineRows = [{ id: "mac-a", freshness: "unknown" }]
  const unknown = await r.fetch("/v1/health")
  assert.equal(unknown.status, 503)
  assert.equal((await body<{ error: string }>(unknown)).error, "machine_freshness_unknown")

  client.machineRows = []
  const absent = await r.fetch("/v1/health")
  assert.equal(absent.status, 503)
  assert.equal((await body<{ error: string }>(absent)).error, "machine_not_reported")
})

test("a transcript is asked again only when its row could say it changed", async () => {
  const client = new FakeClient()
  const source = { freshness: "current", observed_at: 10, provenance: "tmux" }
  client.rows = [row("mac-a", "s1", { line: "Working (1s)", source })]
  const clock = { t: 10_000 }
  const r = reader(client, clock)
  const read = async () => body<TranscriptPage>(await r.fetch("/v1/transcript?session=s1&limit=200"))

  assert.equal((await read()).id, "s1")
  assert.equal(client.asks, 1)

  clock.t += 4_000 // the console's next poll, nothing moved
  await read()
  assert.equal(client.asks, 1, "an unchanged row reuses the answer")

  client.rows = [row("mac-a", "s1", {
    line: "Working (1s)",
    source: { ...source, observed_at: 14 },
  })]
  clock.t += 4_000
  await read()
  assert.equal(client.asks, 1, "a newer terminal observation does not make the transcript newer")

  client.rows = [row("mac-a", "s1", { line: "Working (5s)", source: { ...source, observed_at: 18 } })]
  clock.t += 4_000
  await read()
  assert.equal(client.asks, 1, "the status line alone is not worth a read inside the window")

  clock.t = 10_000 + TRANSCRIPT_LINE_REREAD_MS
  await read()
  assert.equal(client.asks, 2, "past the window the moving line costs one read")

  client.rows = [row("mac-a", "s1", {
    line: "Working (5s)", state: "waiting", work_state: "waiting_you", source: { ...source, observed_at: 19 },
  })]
  clock.t += 1_000
  await read()
  assert.equal(client.asks, 3, "any other change is read at once")

  clock.t += TRANSCRIPT_MAX_REUSE_MS
  await read()
  assert.equal(client.asks, 4, "and nothing is reused past the ceiling")
  assert.deepEqual(r.log.filter((x: { path: string }) => x.path === "/v1/transcript").map((x: { answer: string }) => x.answer), [
    "relay", "cache", "cache", "cache", "relay", "relay", "relay",
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
  client.answer = () => Promise.reject(Object.assign(new Error("the machine did not answer this read"), { code: "cloud_read_timeout" }))
  await assert.rejects(r.fetch("/v1/transcript?session=slow"), /cloud_read_timeout/)
})

// The Mac's channel filled and the answer could not leave. What the page gets
// back is that sentence, at once, with the bound it met — not sixty seconds of
// "loading" and then `cloud_read_timeout`, which was the whole of it before:
// measured 2026-09-21, one long session's transcript channel was refused 34
// times and every one of them was a log line on the Mac and nothing here.
test("a full channel on the machine is a typed refusal here, not a read that times out", async () => {
  const client = new FakeClient()
  const r = reader(client, { t: 0 })
  client.answer = () =>
    Promise.reject(
      Object.assign(new Error("This machine answered, and the channel that answer goes on is full; try again shortly."), {
        code: "cloud_read_busy",
        status: 429,
        layer: "mac_transport",
        detail: { lane: "egress", limit: 4 << 20, retry_after: 5 },
      }),
    )
  const refused = await r.fetch("/v1/transcript?session=%19")
  assert.equal(refused.status, 429)
  const answered = await body<{ error: string; detail: string }>(refused)
  assert.equal(answered.error, "cloud_read_busy")
  assert.match(answered.detail, /full/)
  // It is a refusal the page can draw, not a transport failure it draws as
  // "away": the Mac is there and said something.
  assert.deepEqual(
    r.log.map((row) => [row.answer, row.code]),
    [["refused", "cloud_read_busy"]],
  )
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
// the one on its way to finish and then asks the machine anew.
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
// read — the machine mints the key and nothing else changes — so it is answered
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
    throw Object.assign(new Error("this machine does not carry notifications"), {
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
// Cloud yet, and this machine had been answering `schedules` the whole time — so
// the seam refused the route to itself and the section stayed hidden, with
// nothing in the machine's log because nothing had been asked for.
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
  assert.deepEqual(list.schedules.map((s) => s.id), ["s-1", "s-3"], "another machine's schedules are not this list")
  assert.equal(list.at, 1_700)
  // `fresh`, and not for freshness: this daemon publishes no schedules on its
  // `orch/` descriptor, so the retained reading refuses forever — and the
  // fresh one is also what teaches the copied client which machine an id is on,
  // which is what the four writes are routed by.
  assert.deepEqual(client.scheduleAsks, [{ fresh: true }])
  const last = r.log[r.log.length - 1]
  assert.equal(last.answer, "relay")
  assert.equal(last.word, "schedules")
})

test("one schedule is asked of the chosen machine so its history and webhook panel can open", async () => {
  const client = new FakeClient()
  client.readAnswer = async (word) => word === "schedule"
    ? { schedule: { id: "schedule 9", webhook_binding_availability: "unbound" } }
    : { read: word }
  const r = reader(client, { t: 1000 })

  const res = await r.fetch("/v1/orchestrator/schedules/schedule%209")
  assert.equal(res.status, 200)
  assert.deepEqual(await res.json(), {
    schedule: { id: "schedule 9", webhook_binding_availability: "unbound" },
  })
  assert.deepEqual(client.reads, [{
    machine: "mac-a",
    word: "schedule",
    body: { id: "schedule 9" },
  }])
})

// The distinction this whole read is drawn around: "this machine has none" and
// "nobody answered" are opposite facts, and `pages/schedules.tsx` draws the
// section for one and leaves it alone for the other. An empty answer is an
// answer; a refusal must never arrive as `{schedules: []}`.
test("an empty list is only ever what the machine said, never what a refusal became", async () => {
  const empty = new FakeClient()
  const said = await reader(empty, { t: 1000 }).fetch("/v1/orchestrator/schedules")
  assert.equal(said.status, 200)
  assert.deepEqual(await said.json(), { schedules: [], at: 1 }, "an answer with no rows is an answer")

  // The copied client's own refusal for a machine running a build that publishes
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
// answers, so an account with two machines and one silent one resolves with the
// silent one's rows simply absent. Read as this machine's list that is the
// page asserting an inventory nobody read.
test("a machine that did not answer is not a machine with no schedules", async () => {
  const silent = new FakeClient()
  silent.scheduleAnswer = async () => ({
    schedules: [schedule("mac-b", "s-2")],
    at: 1_700,
    unanswered: [{ machine: "mac-a", label: "this machine", error: Object.assign(new Error("timed out"), { code: "cloud_read_timeout" }) }],
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

// 常用句 on a phone: the sheet that answered "這件事還不能經由 Clawdline Cloud
// 做，請直接在 Mac 上操作。" That sentence was true — this machine's catalog knew the
// word `snippets` and had no route behind it, so the seam refused the read to
// itself rather than asking. Both halves have changed, and this is the read.
test("the snippet list is asked of the machine the session is on, and is that machine's rows", async () => {
  const client = new FakeClient()
  client.snippetAnswer = async () => ({
    snippets: [snippet("mac-a", "sn-1"), snippet("mac-b", "sn-2"), snippet("mac-a", "sn-3")],
    at: 1_700,
  })
  const r = reader(client, { t: 1000 })
  const res = await r.fetch("/v1/snippets?session=s1")
  assert.equal(res.status, 200)
  const list = await body<{ snippets: { id: string }[]; project?: unknown }>(res)
  assert.deepEqual(list.snippets.map((row) => row.id), ["sn-1", "sn-3"], "another machine's snippets are not this list")
  // **No project.** The local route resolves one from the session and names
  // it; the wire carries no session, so an answer that named a project would
  // be this page guessing one. `snippetGroups` groups by the session's own
  // `cwd` instead.
  assert.equal("project" in list, false)
  assert.deepEqual(client.snippetAsks, [{ identity: { machine: "mac-a", session: "s1" }, options: { fresh: true } }])
  const last = r.log[r.log.length - 1]
  assert.equal(last.answer, "relay")
  assert.equal(last.word, "snippets")
})

// The same distinction the schedule list is drawn around, and the sheet has
// two different things to draw for it: an empty group offers the starters,
// and a refusal says what could not be read. A refusal must never arrive as
// `{snippets: []}`.
test("a machine with no snippets and a machine nobody could ask are not the same answer", async () => {
  const empty = new FakeClient()
  const said = await reader(empty, { t: 1000 }).fetch("/v1/snippets?session=s1")
  assert.equal(said.status, 200)
  assert.deepEqual(await said.json(), { snippets: [] }, "an answer with no rows is an answer")

  // The copied client's own two, which it raises for this one machine rather
  // than for the account: a machine whose inventory carries no such field, and a
  // Mac nothing has arrived from at all.
  for (const code of ["cloud_snippets_unpublished", "cloud_read_unavailable"]) {
    const client = new FakeClient()
    client.snippetAnswer = async () => {
      throw Object.assign(new Error(code), { code })
    }
    const res = await reader(client, { t: 1000 }).fetch("/v1/snippets?session=s1")
    assert.equal(res.status, 502, code + " must not resolve")
    assert.equal((await body<{ error: string }>(res)).error, code)
  }
})

test("a snippet list with no session named is refused, and an older client by name", async () => {
  const client = new FakeClient()
  const unnamed = await reader(client, { t: 0 }).fetch("/v1/snippets")
  assert.equal(unnamed.status, 400)
  assert.equal((await body<{ error: string }>(unnamed)).error, "bad_request")
  assert.deepEqual(client.snippetAsks, [], "nothing is asked of the machine for a request that named no session")

  const older = new FakeClient()
  older.snippets = undefined
  const res = await reader(older, { t: 0 }).fetch("/v1/snippets?session=s1")
  assert.equal(res.status, 501)
  const refusal = await body<{ error: string; detail: string }>(res)
  assert.equal(refusal.error, "cloud_not_carried")
  assert.match(refusal.detail, /snippets/)
})

/* ---- the work system, the Projects page and what they carry ---------------
   Before this, every one of these was the `default:` case — 501
   `cloud_not_carried` — so a phone showed the Mac's work board, its Backlog,
   its proposals, its decisions, its digests, its Project catalog, its
   worktree lifecycle and its timeline as screens that could not be read. */

/** Every read carried by word, the URL the console asks it with, and the body that must reach the machine. */
const CARRIED_READS: [string, string, Record<string, unknown>][] = [
  ["/v1/work/board?project=%2Fp", "work.board", { project: "/p", cursor: "" }],
  ["/v1/work/board", "work.board", { project: "", cursor: "" }],
  ["/v1/work/backlog?project=%2Fp&cursor=c2", "work.backlog", { project: "/p", cursor: "c2" }],
  ["/v1/work/proposals?project=%2Fp", "work.proposals", { project: "/p" }],
  ["/v1/work/decisions", "work.decisions", {}],
  ["/v1/work/digests?kind=daily", "work.digests", { kind: "daily" }],
  ["/v1/work/v2/items", "work.v2.items", { project: "" }],
  ["/v1/work/v2/items?project=cloud-p1", "work.v2.items", { project: "p1" }],
  ["/v1/work/v2/items?project=cloud-p1&status=done&q=needle", "work.v2.search", { project: "p1", status: "done", query: "needle" }],
  ["/v1/work/v2/items/work-1", "work.v2.item", { id: "work-1" }],
  ["/v1/work/v2/proposals?state=pending", "work.v2.proposals", { state: "pending" }],
  ["/v1/work/v2/session-todos/%251", "work.v2.session-todos", { terminal: "%1" }],
  ["/v1/projects", "projects", {}],
  ["/v1/projects/%2Fp/worktrees", "project-worktree-lifecycle", { project: "/p" }],
  ["/v1/board?project=p1", "board", { project: "p1", item: "" }],
  ["/v1/board?project=p1&item=i1", "board", { project: "p1", item: "i1" }],
  [
    "/v1/board?project=p1&audience=human",
    "board.items",
    { project: "p1", audience: "human", cursor: 0, limit: 30 },
  ],
  [
    "/v1/board?project=p1&audience=agent&cursor=60&limit=90",
    "board.items",
    { project: "p1", audience: "agent", cursor: 60, limit: 90 },
  ],
  [
    "/v1/timeline?project=clawdline-go&upcoming=false",
    "timeline",
    { project: "clawdline-go", entry: "", cursor: "", environment: "", category: "", upcoming: false },
  ],
  [
    "/v1/timeline?project=clawdline-go&environment=staging&category=feature&upcoming=true",
    "timeline",
    { project: "clawdline-go", entry: "", cursor: "", environment: "staging", category: "feature", upcoming: true },
  ],
]

test("the work system, the projects and the timeline reach the machine as their own words", async () => {
  for (const [path, word, sent] of CARRIED_READS) {
    const client = new FakeClient()
    const r = reader(client, { t: 1000 })
    const res = await r.fetch(path)
    assert.equal(res.status, 200, path + " was not answered")
    assert.deepEqual(await body(res), { read: word }, path + " did not answer what the machine said")
    assert.equal(client.reads.length, 1, path + " asked the machine " + client.reads.length + " times")
    assert.equal(client.reads[0].machine, "mac-a", path + " asked a machine this page is not reading")
    assert.equal(client.reads[0].word, word, path + " asked for " + client.reads[0].word)
    assert.deepEqual(client.reads[0].body, sent, path + " carried the wrong body")
    const logged = r.log[r.log.length - 1]
    assert.equal(logged.answer, "relay", path + " was answered somewhere other than the machine")
    assert.equal(logged.word, word, path + " is logged as " + logged.word)
  }
})

test("a carried read stops when its caller's signal fires, instead of waiting out the relay's minute", async () => {
  const client = new FakeClient()
  let settle: (value: unknown) => void = () => {}
  client.readAnswer = () => new Promise((resolve) => { settle = resolve })
  const r = reader(client, { t: 1000 })
  const controller = new AbortController()
  const asked = r.fetch("/v1/work/v2/session-todos/%251", { signal: controller.signal })
  controller.abort()
  // Raced, so a seam that ignores the signal is a red test and not a hung one.
  const outcome = await Promise.race([
    asked.then(() => "answered", (error: unknown) => error),
    new Promise((resolve) => setTimeout(() => resolve("still waiting"), 200)),
  ]) as Error & { code?: string }
  assert.ok(outcome instanceof Error, `a fired signal left the read ${String(outcome)}`)
  assert.equal(outcome.name, "AbortError", "a fired signal rejects the way fetch does")
  assert.equal(outcome.code, "cloud_read_abandoned")
  assert.match(outcome.message, /work\.v2\.session-todos/)
  const logged = r.log[r.log.length - 1]
  assert.equal(logged.answer, "unanswered")
  assert.equal(logged.code, "cloud_read_abandoned")
  assert.equal(logged.word, "work.v2.session-todos")
  // The machine's late answer settles nothing and throws nowhere.
  settle({ direct_todos: [] })
  await new Promise((resolve) => setTimeout(resolve, 0))

  // A signal that has already fired is not sent at all past the ask, and one
  // that never fires changes nothing.
  const before = new AbortController()
  before.abort()
  await assert.rejects(r.fetch("/v1/work/v2/session-todos/%251", { signal: before.signal }), { name: "AbortError" })
  client.readAnswer = async (word) => ({ read: word })
  const calm = await r.fetch("/v1/work/v2/session-todos/%251", { signal: new AbortController().signal })
  assert.equal(calm.status, 200)
  assert.deepEqual(await body(calm), { read: "work.v2.session-todos" })
})

test("a query field no word carries is refused by name, never quietly dropped", async () => {
  const client = new FakeClient()
  const r = reader(client, { t: 1000 })
  // `section` is a real field of this daemon's own route and no part of the
  // word: carried silently it would answer one section as though it were the
  // board, which is worse than saying so.
  const res = await r.fetch("/v1/work/board?project=%2Fp&section=active")
  assert.equal(res.status, 501)
  const refusal = await body<{ error: string; detail: string }>(res)
  assert.equal(refusal.error, "cloud_not_carried")
  assert.match(refusal.detail, /section=/)
  assert.equal(client.reads.length, 0, "the machine was asked a question it could not have been told")
})

test("a client with no generic read refuses each word by name rather than throwing", async () => {
  const client = new FakeClient()
  client._machineRequest = undefined
  const r = reader(client, { t: 1000 })
  for (const [path, word] of CARRIED_READS) {
    const res = await r.fetch(path)
    assert.equal(res.status, 501, path)
    const refusal = await body<{ error: string; detail: string }>(res)
    assert.equal(refusal.error, "cloud_not_carried", path)
    assert.match(refusal.detail, new RegExp(word.replace(".", "\\.")), path + " did not name the word")
  }
})

test("a refusal from the machine's own route stays that route's refusal", async () => {
  const client = new FakeClient()
  client.readAnswer = async () => {
    throw Object.assign(new Error("A timeline is one Project's; name it with ?project=."), {
      code: "project_required",
      status: 400,
    })
  }
  const r = reader(client, { t: 1000 })
  const res = await r.fetch("/v1/timeline?upcoming=true")
  assert.equal(res.status, 400, "the machine's own status is what the page sees")
  const refusal = await body<{ error: string }>(res)
  assert.equal(refusal.error, "project_required", "a route's refusal is not turned into a seam refusal")
})
