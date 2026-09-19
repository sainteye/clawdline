// The relay seam's rules: `node --test web/console/src/cloud/*.test.ts`.
//
// A fake CloudClient stands in for the copied one, holding rows the way it
// does (`_sessionResponse`) and counting transcript asks, which is the number
// that costs an envelope each way across the relay.
import { test } from "node:test"
import assert from "node:assert/strict"
import type { SessionsSnapshot, TranscriptPage } from "@clawdline/contract"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { RelayReader, TRANSCRIPT_LINE_REREAD_MS, TRANSCRIPT_MAX_REUSE_MS, type CloudEvent, type CloudReadClient, type CloudRow } from "./relay-reader.ts"

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

test("a write is refused as read-only and an uncarried read as not carried, both typed", async () => {
  const r = reader(new FakeClient(), { t: 0 })
  const post = await r.fetch("/v1/sessions/s1/send", { method: "POST", body: "{}" })
  assert.equal(post.status, 403)
  assert.deepEqual(await post.json(), {
    error: "cloud_read_only",
    detail: "This console reads a machine through Clawdline Cloud and does not change it yet.",
    route: "/v1/sessions/s1/send",
  })
  const board = await r.fetch("/v1/board")
  assert.equal(board.status, 501)
  assert.equal((await body<{ error: string }>(board)).error, "cloud_not_carried")
  assert.deepEqual(r.log.map((x: { answer: string; code?: string }) => [x.answer, x.code]), [
    ["refused", "cloud_read_only"],
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
