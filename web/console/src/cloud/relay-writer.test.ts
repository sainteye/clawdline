// The relay seam's writes: `node --test web/console/src/cloud/*.test.ts`.
//
// A fake CloudClient stands in for the copied one. Each method records what it
// was asked and answers the way the copied client does: the machine's own body on
// success, a `CloudFailure`-shaped rejection otherwise — code, layer, status
// and the envelope's `ref` — so what is asserted here is what the page's
// readers (`ClawdlineClient`, `start-bridge.ts`, `waiting-bridge.ts`) are
// handed.
import { test } from "node:test"
import assert from "node:assert/strict"
import type { TranscriptPage } from "@clawdline/contract"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { RelayReader, TRANSCRIPT_EXPECT_MS, type CloudEvent, type CloudIdentity, type CloudRow } from "./relay-reader.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { CARRIED_READS, RelayWriter, writeRoute, type CloudWriteClient } from "./relay-writer.ts"
// The copied client's own failure constructor: what the machine's refusal really becomes.
import { failureFromMac } from "../legacy/js/net/cloud-failure.js"

type Call = [string, ...unknown[]]

/** A question's name, as `session/fingerprint.ts` and `session.MenuFingerprint` write one. */
const FINGERPRINT = "8eca80fffc9359d5f0fca31f3e36b741bd50218b658fc086f05931747bd4c5ce"
/** The words the Go daemon's descriptor lists (`cloudops.Implemented`), the ones these tests use. */
const GO_DAEMON = { machine: { commands: ["send", "answer", "key", "end", "focus"] } }
/** The envelope a failure was sent under: sealed, written, and so possibly run. */
const REF = { sender: "web_abcdef123456", seq: 12, request: null }

function refusal(code: string, fields: Record<string, unknown> = {}) {
  return Object.assign(new Error(code + " (English, never shown)"), { code, layer: "mac_preflight", ...fields })
}

class FakeClient implements CloudWriteClient {
  ready = true
  allowWrites = true
  macCapabilities = new Set<string>()
  descriptors = new Map<string, { machine: { commands?: string[] } }>()
  sessionInventoryByMachine = new Map<string, unknown>()
  rows: CloudRow[] = []
  calls: Call[] = []
  transcriptAsks = 0
  signature = "1-1"
  fail: Record<string, Error | undefined> = {}
  events(_listener: (event: CloudEvent) => void) {
    return () => {}
  }
  async sessions() {
    return { sessions: this.rows, at: 100, scan: { emptyAuthoritative: true, recovering: [], failures: [] } }
  }
  async transcript() {
    this.transcriptAsks += 1
    return { id: "s1", entries: [], signature: this.signature, evidence: "transcript" }
  }
  machineDescriptor(machine: string) {
    return this.descriptors.get(machine) ?? null
  }
  private act(name: string, args: unknown[], body: unknown): Promise<unknown> {
    this.calls.push([name, ...args])
    const failure = this.fail[name]
    return failure ? Promise.reject(failure) : Promise.resolve(body)
  }
  send(identity: CloudIdentity, text: string, images: string[]) {
    return this.act("send", [identity, text, images], {
      ok: true, id: identity.session, action: "typed", optimisticIdentity: identity, optimisticRequest: "r-1",
    })
  }
  answer(identity: CloudIdentity, key: string) {
    return this.act("answer", [identity, key], { type: "envelope" })
  }
  _read(identity: CloudIdentity, type: string, extra: Record<string, unknown>, answer: string, timeoutMs?: number, options?: unknown) {
    return this.act("_read", [identity, type, extra, answer, timeoutMs, options], type === "document"
      ? { scope: "project", path: "demo.md", media_type: "text/markdown; charset=utf-8", byte_count: 7, data: "IyBEZW1vCg==" }
      : type === "documents" ? { documents: [{ scope: "project", path: "demo.md", bytes: 7, modified: 1 }] }
      : { ok: true, id: identity.session, action: "keyed" })
  }
  end(identity: CloudIdentity, acceptLoss: boolean, version: string) {
    return this.act("end", [identity, acceptLoss, version], { ok: true, id: identity.session, action: "closed" })
  }
  focus(identity: CloudIdentity) {
    return this.act("focus", [identity], { ok: true, id: identity.session })
  }
  info(identity: CloudIdentity) {
    return this.act("info", [identity], { info: { session: { id: identity.session } } })
  }
  infoSummary(identity: CloudIdentity) {
    return this.act("infoSummary", [identity], { info: { session: { id: identity.session } } })
  }
  git(identity: CloudIdentity) {
    return this.act("git", [identity], { git: { branch: "main", ahead: 0, behind: 0, clean: true, files: [] } })
  }
  screen(identity: CloudIdentity) {
    return this.act("screen", [identity], { screen: { id: identity.session, backend: "iterm2", channel: "on-demand", captures: 0, pending: true, readable: true } })
  }
  places(machine: string) {
    return this.act("places", [machine], { places: [{ id: "mac-a\u0000p1", label: "api" }], assistants: [{ id: "claude" }] })
  }
  pastSessions(place: string, assistant: string) {
    return this.act("pastSessions", [place, assistant], { sessions: [] })
  }
  startPlace(place: string, assistant: string, model: string) {
    return this.act("startPlace", [place, assistant, model], { ok: true, id: "new-1", backend: "iterm" })
  }
  resumePlace(place: string, past: string, assistant: string, requestId?: string) {
    return this.act("resumePlace", [place, past, assistant, requestId], { ok: true, id: "new-2", backend: "iterm" })
  }
  voice(audio: string, rate: number) {
    return this.act("voice", [audio, rate], { text: "hello there", ms: 900 })
  }
  voiceHost() {
    return this.act("voiceHost", [], { machine: "mac-a" }) as Promise<{ machine: string }>
  }
  setVoiceHost(machine: string) {
    this.fail.voice = undefined
    return this.act("setVoiceHost", [machine], {})
  }
  pushSubscribe(subscription: unknown) {
    return this.act("pushSubscribe", [subscription], { ok: true, id: "sub-1" })
  }
  pushUnsubscribe(id: string) {
    return this.act("pushUnsubscribe", [id], { ok: true })
  }
  pushTest(session: string | null) {
    return this.act("pushTest", [session], { ok: true, sent: 1, failed: 0 })
  }
  createSchedule(schedule: unknown) {
    return this.act("createSchedule", [schedule], { ok: true, schedule: { id: "sch-9", title: "a schedule" }, dispatch_enabled: true })
  }
  _scheduleBody(schedule: unknown) {
    return { machine: "mac-a", schedule: schedule as Record<string, unknown> }
  }
  _place(value: unknown) {
    if (value === "cloud-p1") return { machine: "mac-a", id: "p1", path: "/repo" }
    throw refusal("not_found", { status: 404, layer: "browser" })
  }
  _machineRequest(machine: string, word: string, body: Record<string, unknown>, kind: "read" | "action", timeoutMs?: number) {
    const args: unknown[] = [machine, word, body, kind]
    if (timeoutMs !== undefined) args.push(timeoutMs)
    return this.act("_machineRequest", args,
      word === "schedule-run" ? { ok: true, task_id: "t-1" } :
      word === "intents" ? { draft: { place_id: "p1", kind: "work", title: "Voice draft" }, ms: 7 } :
      word === "documents" ? { documents: [{ scope: "project", path: "demo.md", bytes: 7, modified: 1 }] } :
      word === "document" ? { scope: "project", path: "demo.md", media_type: "text/markdown; charset=utf-8", byte_count: 7, data: "IyBEZW1vCg==" } :
      word === "work.v2.image" ? { id: body.id, media_type: "image/png", data: "iVBORw==" } : { ok: true })
  }
  _machineRequestAs(request: string, machine: string, word: string, body: Record<string, unknown>, kind: "read" | "action", timeoutMs?: number) {
    const args: unknown[] = [request, machine, word, body, kind]
    if (timeoutMs !== undefined) args.push(timeoutMs)
    return this.act("_machineRequestAs", args, { ok: true })
  }
  updateSchedule(id: string, schedule: unknown) {
    return this.act("updateSchedule", [id, schedule], { ok: true, schedule: { id, title: "a schedule" } })
  }
  deleteSchedule(id: string) {
    return this.act("deleteSchedule", [id], { ok: true, deleted: id })
  }
  runSchedule(id: string) {
    return this.act("runSchedule", [id], { ok: true, task_id: "t-1" })
  }
  createSnippet(snippet: unknown, identity: CloudIdentity) {
    return this.act("createSnippet", [snippet, identity], { id: "sn-9", title: "a title", body: "a body", scope: "global", position: 0 })
  }
  updateSnippet(id: string, snippet: unknown, identity: CloudIdentity) {
    return this.act("updateSnippet", [id, snippet, identity], { id, title: "a title", body: "a body", scope: "global", position: 0 })
  }
  deleteSnippet(id: string, identity: CloudIdentity) {
    return this.act("deleteSnippet", [id, identity], { ok: true, deleted: id })
  }
  orderSnippets(scope: string, project: string, order: string[], identity: CloudIdentity) {
    return this.act("orderSnippets", [scope, project, order, identity], { ok: true, scope, snippets: [] })
  }
  image(identity: CloudIdentity, id: string) {
    return this.act("image", [identity, id], { id, media_type: "image/png", bytes: new Uint8Array([137, 80, 78, 71]) }) as Promise<{
      id: string
      media_type: string
      bytes: Uint8Array
    }>
  }
}

function row(session: string, extra: Record<string, unknown> = {}): CloudRow {
  return { id: session, machine: "mac-a", session, identity: { machine: "mac-a", session }, state: "idle", ...extra }
}

function seam(client: FakeClient, clock = { t: 1_000 }) {
  const reader = new RelayReader("mac-a", { now: () => clock.t })
  const writer = new RelayWriter(reader.writeHost, { now: () => clock.t, requestID: () => "req-1" })
  reader.carryWrites({ route: writeRoute, answer: (route, method, url, init) => writer.answer(route, method, url, init) })
  reader.attach(client)
  return { reader, clock }
}

const post = (body: unknown, headers: Record<string, string> = {}): RequestInit => ({
  method: "POST",
  headers: { "Content-Type": "application/json", ...headers },
  body: JSON.stringify(body),
})

async function json<T = Record<string, unknown>>(res: Response): Promise<T> {
  return (await res.json()) as T
}

test("each console route is the Cloud word the machine lists, and nothing else", () => {
  const cases: [string, string, string | null][] = [
    ["POST", "/v1/sessions/s%201/send", "send"],
    ["POST", "/v1/sessions/s1/key", "answer"],
    ["POST", "/v1/sessions/s1/close", "end"],
    ["POST", "/v1/sessions/s1/archive", "archive-session"],
    ["GET", "/v1/sessions/archived", "archived-sessions"],
    ["GET", "/v1/sessions/s1/documents", "documents"],
    ["GET", "/v1/sessions/s1/documents/project/demo.md", "document"],
    ["POST", "/v1/sessions/archived/restore", "restore-archived"],
    ["POST", "/v1/sessions/s1/focus", "focus"],
    ["POST", "/v1/places/p1/start", "start"],
    ["POST", "/v1/places/p1/start/codex/gpt-5", "start"],
    ["POST", "/v1/places/p1/resume/abc", "resume"],
    ["POST", "/v1/places/p1/resume/claude/abc", "resume"],
    ["POST", "/v1/voice", "voice"],
    ["GET", "/v1/places", "places"],
    ["GET", "/v1/settings/default-models", "default-models"],
    ["POST", "/v1/settings/default-models", "default-models-update"],
    ["GET", "/v1/settings/work-gates", "work-gate-settings"],
    ["POST", "/v1/settings/work-gates", "work-gate-settings-update"],
    ["POST", "/v1/board", "board-command"],
    ["GET", "/v1/places/p1/sessions/claude", "past-sessions"],
    ["GET", "/v1/artifacts/images/img-1", "image"],
    ["POST", "/v1/sessions/s1/interrupt", "interrupt"],
    ["POST", "/v1/sessions/s1/smart-title", "smart-title"],
    ["POST", "/v1/push/subscribe", "push-subscribe"],
    ["POST", "/v1/push/unsubscribe", "push-unsubscribe"],
    ["POST", "/v1/push/test", "push-test"],
    ["POST", "/v1/orchestrator/schedules", "schedule-create"],
    ["PATCH", "/v1/orchestrator/schedules/sch-1", "schedule-update"],
    ["DELETE", "/v1/orchestrator/schedules/sch-1", "schedule-delete"],
    ["POST", "/v1/orchestrator/schedules/sch-1/run", "schedule-run"],
    ["POST", "/v1/projects/%2Frepo/worktrees/refresh", "project-worktree-lifecycle-refresh"],
    ["PUT", "/v1/projects/cloud-p1/files/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "project-file-save"],
    // The list is a read and is answered by `relay-reader.ts`, not here, and
    // so is one schedule in full — which this machine has no route for at all.
    ["GET", "/v1/orchestrator/schedules", null],
    ["GET", "/v1/orchestrator/schedules/sch-1", null],
    ["POST", "/v1/orchestrator/schedules/sch-1", null],
    ["DELETE", "/v1/orchestrator/schedules", null],
    ["POST", "/v1/snippets", "snippet-create"],
    ["PATCH", "/v1/snippets/sn-1", "snippet-update"],
    ["DELETE", "/v1/snippets/sn-1", "snippet-delete"],
    ["POST", "/v1/snippets/order", "snippet-order"],
    // `order` is a name no snippet id may be, on this side as on the machine's
    // (internal/transport/http/snippets.go says so of its own mux), so the
    // group's order is never parsed as a snippet called "order".
    ["PATCH", "/v1/snippets/order", null],
    ["DELETE", "/v1/snippets/order", null],
    // The list is a read and is answered by `relay-reader.ts`, not here.
    ["GET", "/v1/snippets", null],
    ["GET", "/v1/snippets/sn-1", null],
    ["POST", "/v1/snippets/sn-1", null],
    // The key is a read and is answered by `relay-reader.ts`, not here.
    ["GET", "/v1/push/key", null],
    ["POST", "/v1/push/key", null],
    ["GET", "/v1/sessions", null],
    ["GET", "/v1/transcript", null],
    ["POST", "/v1/orchestrator/tasks", null],
    ["DELETE", "/v1/sessions/s1/send", null],
    ["POST", "/v1/places/p1/resume", null],
  ]
  for (const [method, path, word] of cases) {
    assert.equal(writeRoute(method, path)?.word ?? null, word, method + " " + path)
  }
  assert.deepEqual(writeRoute("POST", "/v1/sessions/s%201/send"), { op: "send", word: "send", session: "s 1" })
  assert.deepEqual(writeRoute("POST", "/v1/places/p1/resume/claude/abc"), {
    op: "resume", word: "resume", place: "p1", assistant: "claude", past: "abc", persona: "",
  })
  assert.deepEqual(writeRoute("POST", "/v1/sessions/s1/interrupt"), {
    op: "interrupt", word: "interrupt", session: "s1",
  })
  assert.equal(writeRoute("POST", "/v1/sessions/s1/rename")?.op, "uncarried")
  assert.deepEqual(writeRoute("POST", "/v1/sessions/s1/smart-title"), {
    op: "smart-title", word: "smart-title", session: "s1",
  })
  assert.equal(writeRoute("GET", "/v1/work/v2/images/img-1")?.word, "work.v2.image")
  assert.equal(writeRoute("POST", "/v1/work/v2/items/w1/images")?.word, "work.v2.image-create")
  assert.equal(writeRoute("POST", "/v1/work/v2/items/w1/persona-suggestion")?.word, "work.v2.persona-suggestion")
  assert.equal(writeRoute("POST", "/v1/work/v2/items/w1/gate-decision")?.word, "work.v2.gate-decision")
  assert.equal(writeRoute("POST", "/v1/work/v2/items/w1/gate-purge")?.word, "work.v2.gate-purge")
  assert.equal(writeRoute("DELETE", "/v1/work/v2/items/w1/images/img-1")?.word, "work.v2.image-delete")
  assert.equal(writeRoute("POST", "/v1/work/v2/session-todos/%251/t1/images")?.word, "work.v2.todo-image-create")
})

test("a Cloud document link reads bytes on its named machine and rejects another machine", async () => {
  const client = new FakeClient()
  const { reader } = seam(client)
  const list = await reader.fetch("/v1/sessions/s1/documents?machine=mac-a")
  assert.equal(list.status, 200)
  assert.deepEqual(await json(list), { documents: [{ scope: "project", path: "demo.md", bytes: 7, modified: 1 }] })
  const document = await reader.fetch("/v1/sessions/s1/documents/project/demo.md?machine=mac-a")
  assert.equal(document.status, 200)
  assert.equal(document.headers.get("content-type"), "text/markdown; charset=utf-8")
  assert.equal(await document.text(), "# Demo\n")
  assert.equal(client.calls.some((call) => call[0] === "_machineRequest" && (call[2] === "document" || call[2] === "documents")), false)
  const listing = client.calls.find((call) => call[0] === "_read" && call[2] === "documents")
  assert.deepEqual(listing?.slice(1, 5), [{ machine: "mac-a", session: "s1" }, "documents", {}, "documents"],
    "the listing uses the Session channel and its legacy answer name")
  const read = client.calls.find((call) => call[0] === "_read" && call[2] === "document")
  assert.deepEqual(read?.[1], { machine: "mac-a", session: "s1" }, "the document answer returns on the Session channel")
  assert.deepEqual({ ...(read?.[3] as Record<string, unknown>), request: null },
    { request: null, scope: "project", task: "", path: "demo.md" })
  assert.equal(read?.[4], "read:" + (read?.[3] as { request: string }).request)
  const calls = client.calls.length
  const wrong = await reader.fetch("/v1/sessions/s1/documents/project/demo.md?machine=mac-b")
  assert.equal(wrong.status, 400)
  assert.equal(client.calls.length, calls, "a mismatched locator never crosses the relay")
})

test("a send goes as the machine's `send` under the row's own identity, and answers as the local route does", async () => {
  const client = new FakeClient()
  client.rows = [row("s1")]
  const { reader } = seam(client)
  const res = await reader.fetch("/v1/sessions/s1/send", post({ text: "hello", images: ["data:image/jpeg;base64,AA=="] }))
  assert.equal(res.status, 200)
  assert.deepEqual(await json(res), { ok: true, id: "s1", action: "typed" }, "the client's bookkeeping is taken off")
  assert.deepEqual(client.calls, [["send", { machine: "mac-a", session: "s1" }, "hello", ["data:image/jpeg;base64,AA=="]]])
  const last = reader.log[reader.log.length - 1]
  assert.equal(last.word, "send")
  assert.equal(last.answer, "relay")
  assert.equal(typeof last.ms, "number")
})

test("smart naming reaches the session once under the confirmation's idempotency key", async () => {
  const client = new FakeClient()
  client.rows = [row("s1")]
  const { reader } = seam(client)
  const res = await reader.fetch(
    "/v1/sessions/s1/smart-title",
    post({}, { "Idempotency-Key": "smart-press-1" }),
  )
  assert.equal(res.status, 200)
  assert.deepEqual(await json(res), { ok: true, id: "s1", action: "keyed" })
  assert.deepEqual(client.calls, [[
    "_read",
    { machine: "mac-a", session: "s1" },
    "smart-title",
    { request: "smart-press-1" },
    "action:smart-press-1",
    undefined,
    undefined,
  ]])

  client.calls = []
  const unkeyed = await reader.fetch("/v1/sessions/s1/smart-title", post({}))
  assert.equal(unkeyed.status, 400)
  assert.equal((await json(unkeyed)).error, "bad_request")
  assert.deepEqual(client.calls, [], "an unreceipted model turn never leaves the browser")
})

test("a stop reaches the session once under the press's idempotency key", async () => {
  const client = new FakeClient()
  client.rows = [row("s1")]
  const { reader } = seam(client)
  const res = await reader.fetch("/v1/sessions/s1/interrupt", post({}, { "Idempotency-Key": "stop-press-1" }))
  assert.equal(res.status, 200)
  assert.deepEqual(client.calls, [[
    "_read",
    { machine: "mac-a", session: "s1" },
    "interrupt",
    { request: "stop-press-1" },
    "action:stop-press-1",
    undefined,
    undefined,
  ]])

  client.calls = []
  const unkeyed = await reader.fetch("/v1/sessions/s1/interrupt", post({}))
  assert.equal(unkeyed.status, 400)
  assert.equal((await json(unkeyed)).error, "bad_request")
  assert.deepEqual(client.calls, [], "a stop that a retry could press twice never leaves the browser")
})

test("a machine's refusal comes back typed, in the flat spelling `ClawdlineClient` recognises", async () => {
  const client = new FakeClient()
  client.rows = [row("s1")]
  client.fail.send = refusal("cloud_commands_disabled", { status: 403, ref: { sender: "web_abcdef123456", seq: 12 } })
  const { reader } = seam(client)
  const res = await reader.fetch("/v1/sessions/s1/send", post({ text: "hello" }))
  assert.equal(res.status, 403)
  const body = await json(res)
  assert.equal(body.error, "cloud_commands_disabled")
  assert.equal(typeof body.detail, "string", "`isRefusal` needs a string detail")
  assert.equal(body.layer, "mac_preflight")
  assert.equal(body.ref, "abcdef12·12")
  assert.equal(body.outcome, "not_done")
  assert.equal(reader.log[reader.log.length - 1].code, "cloud_commands_disabled")
})

test("a command the machine may have run without answering says so, and nothing sent says that", async () => {
  const client = new FakeClient()
  client.rows = [row("s1")]
  client.fail.send = refusal("cloud_read_timeout", { layer: "browser" })
  const { reader } = seam(client)
  const timedOut = await json(await reader.fetch("/v1/sessions/s1/send", post({ text: "hi" })))
  assert.equal(timedOut.error, "cloud_read_timeout")
  assert.equal(timedOut.outcome, "unknown")

  client.ready = false
  const res = await reader.fetch("/v1/sessions/s1/send", post({ text: "hi" }))
  assert.equal(res.status, 503, "a write with the line down is refused, not left to a transport error")
  const offline = await json(res)
  assert.equal(offline.error, "offline")
  assert.equal(offline.outcome, "not_done")
})

test("after a send, the transcript is asked for on every poll until it changes, then reused again", async () => {
  const client = new FakeClient()
  client.rows = [row("s1")]
  const { reader, clock } = seam(client)
  const read = async (init?: RequestInit) => json<TranscriptPage>(await reader.fetch("/v1/transcript?session=s1&limit=200", init))

  await read()
  clock.t += 4_000
  await read()
  assert.equal(client.transcriptAsks, 1, "nothing moved: reused")

  await reader.fetch("/v1/sessions/s1/send", post({ text: "hi" }))
  clock.t += 4_000
  await read()
  clock.t += 4_000
  await read()
  assert.equal(client.transcriptAsks, 3, "the turn is awaited: every poll asks")

  client.signature = "2-2" // the turn arrived
  clock.t += 4_000
  await read()
  clock.t += 4_000
  await read()
  assert.equal(client.transcriptAsks, 4, "it arrived: the ordinary rule again")

  // An awaited change that never comes stops being awaited.
  await reader.fetch("/v1/sessions/s1/send", post({ text: "again" }))
  for (let t = 0; t <= TRANSCRIPT_EXPECT_MS + 8_000; t += 4_000) {
    clock.t += 4_000
    await read()
  }
  const asked = client.transcriptAsks
  clock.t += 4_000
  await read()
  assert.equal(client.transcriptAsks, asked, "past the window the answer is reused")
})

test("a refusal costs one fresh read, not a window of them; `no-store` always asks", async () => {
  const client = new FakeClient()
  client.rows = [row("s1")]
  client.fail.send = refusal("cloud_commands_disabled", { status: 403 })
  const { reader, clock } = seam(client)
  const read = async (init?: RequestInit) => reader.fetch("/v1/transcript?session=s1&limit=200", init)
  await read()
  await reader.fetch("/v1/sessions/s1/send", post({ text: "hi" }))
  clock.t += 4_000
  await read()
  clock.t += 4_000
  await read()
  assert.equal(client.transcriptAsks, 2, "one read after the refusal, then reuse")
  await read({ cache: "no-store" })
  assert.equal(client.transcriptAsks, 3, "a caller that must see the machine's answer is never handed an old one")
})

test("a waiting card's press is answered by the machine itself, never by the relay's `delivered`", async () => {
  const client = new FakeClient()
  client.rows = [row("s1")]
  client.descriptors.set("mac-a", { machine: { commands: ["send", "answer", "key"] } })
  const { reader } = seam(client)
  // The Go daemon: lists `answer`, publishes no cloud_status. Asked with a
  // request id, settled by the machine's own answer; with no key from the card,
  // the writer mints one.
  const res = await reader.fetch("/v1/sessions/s1/key", post({ key: "2", expect: FINGERPRINT }))
  assert.equal(res.status, 200)
  assert.deepEqual(client.calls.pop(), [
    "_read", { machine: "mac-a", session: "s1" }, "answer", { request: "req-1", answer: "2", expect: FINGERPRINT },
    "action:req-1", undefined, { retireUncertain: true },
  ])
  assert.ok(!client.calls.some((c) => c[0] === "answer"), "the copied `answer`, which settles on the relay, is never used")
})

test("a Git file diff carries its exact changed path and waits for the machine's answer", async () => {
  const client = new FakeClient()
  client.rows = [row("s1")]
  const { reader } = seam(client)
  const res = await reader.fetch("/v1/sessions/s1/git/diff?path=web%2Fconsole%2Fsrc%2Fsession%2FDetail.tsx")
  assert.equal(res.status, 200)
  assert.deepEqual(client.calls.pop(), [
    "_read",
    { machine: "mac-a", session: "s1" },
    "git-diff",
    { request: "req-1", path: "web/console/src/session/Detail.tsx" },
    "read:req-1",
    undefined,
    undefined,
  ])
})

test("a close carries the displayed version even when the relay has a newer row", async () => {
  const client = new FakeClient()
  client.rows = [row("s1", { closeability: { version: "cv-7" } })]
  const { reader } = seam(client)
  await reader.fetch("/v1/sessions/s1/close", post({ force: true, expected_closeability_version: "cv-older" }))
  assert.deepEqual(client.calls.pop(), ["end", { machine: "mac-a", session: "s1" }, true, "cv-older"])
  // A blocked close's reasons: see the two F6 tests below, which go through
  // the copied client's own failure path rather than an error built by hand.
})

test("a confirmed close removes an older Cloud row until the terminal speaks again", async () => {
  const client = new FakeClient()
  const clock = { t: 100_500 }
  client.rows = [row("s1", { source: { freshness: "current", observed_at: 100, provenance: "iterm" } })]
  const { reader } = seam(client, clock)
  assert.deepEqual((await reader.snapshot()).sessions.map((s) => s.id), ["s1"])

  const closed = await reader.fetch("/v1/sessions/s1/close", post({ force: false }))
  assert.equal(closed.status, 200)
  assert.deepEqual((await reader.snapshot()).sessions, [],
    "the row retained by the Cloud client predates the terminal's successful close")

  clock.t = 102_000
  client.rows = [row("s1", { source: { freshness: "current", observed_at: 102, provenance: "iterm" } })]
  assert.deepEqual((await reader.snapshot()).sessions.map((s) => s.id), ["s1"],
    "a later current terminal enumeration is the new existence truth")
})

test("start and resume go as the machine's words; a refusal keeps `app` in the nested spelling the sheet reads", async () => {
  const client = new FakeClient()
  const { reader } = seam(client)
  const places = await json(await reader.fetch("/v1/places"))
  assert.deepEqual(client.calls.pop(), ["places", "mac-a"], "the places of the machine this page reads")
  assert.ok(Array.isArray(places.places))

  const started = await json(await reader.fetch("/v1/places/mac-a%00p1/start/codex/gpt-5", post({})))
  assert.equal(started.id, "new-1")
  assert.deepEqual(client.calls.pop(), ["startPlace", "mac-a\u0000p1", "codex", "gpt-5"])

  await reader.fetch("/v1/places/p1/resume/claude/past-9", post({}, { "Idempotency-Key": "press-1" }))
  assert.deepEqual(client.calls.pop(), ["resumePlace", "p1", "past-9", "claude", "press-1"], "one press, one request id")

  client.fail.startPlace = refusal("terminal_closed", { status: 409, layer: "mac_route", detail: { app: "iTerm" } })
  const res = await reader.fetch("/v1/places/p1/start", post({}))
  const body = await json<{ error: { code: string; app?: string; outcome: string } }>(res)
  assert.equal(body.error.code, "terminal_closed")
  assert.equal(body.error.app, "iTerm")
  assert.equal(body.error.outcome, "not_done")
})

test("the machine workspace starts on this machine without entering the Project map", async () => {
  const client = new FakeClient()
  const { reader } = seam(client)
  await reader.fetch("/v1/places/%40machine/start/codex", post({}, { "Idempotency-Key": "machine-press-1" }))
  assert.deepEqual(client.calls.pop(), ["_machineRequestAs", "machine-press-1", "mac-a", "start",
    { place: "@machine", assistant: "codex", model: "" }, "action"])
  assert.equal(client.calls.some((call) => call[0] === "_place" || call[0] === "startPlace"), false)
})

test("`/as/<persona>` is read off a start or a resume exactly where the machine's route reads it", () => {
  const cases: [string, Record<string, unknown> | null][] = [
    ["/v1/places/p1/start/claude/as/architect",
      { op: "start", word: "start", place: "p1", assistant: "claude", model: "", persona: "architect" }],
    ["/v1/places/p1/start/codex/gpt-5/as/security",
      { op: "start", word: "start", place: "p1", assistant: "codex", model: "gpt-5", persona: "security" }],
    ["/v1/places/p1/resume/claude/abc/as/code-reviewer",
      { op: "resume", word: "resume", place: "p1", assistant: "claude", past: "abc", persona: "code-reviewer" }],
    ["/v1/places/p1/start/codex/gpt-5",
      { op: "start", word: "start", place: "p1", assistant: "codex", model: "gpt-5", persona: "" }],
    // `as` only as the second-to-last segment of a route that names its
    // assistant: `/start/claude/as` is a model called "as", and the machine
    // refuses it; `/start/as/x` names an assistant called "as".
    ["/v1/places/p1/start/claude/as",
      { op: "start", word: "start", place: "p1", assistant: "claude", model: "as", persona: "" }],
    ["/v1/places/p1/start/as/architect",
      { op: "start", word: "start", place: "p1", assistant: "as", model: "architect", persona: "" }],
    // A resume with a persona must name its assistant, as the route requires.
    ["/v1/places/p1/resume/abc/as/architect", null],
    ["/v1/places/p1/start/claude/opus/extra/as/architect", null],
    ["/v1/places/p1/resume/claude/abc/as", null],
  ]
  for (const [path, want] of cases) {
    assert.deepEqual(writeRoute("POST", path), want, path)
  }
  assert.equal(writeRoute("POST", "/v1/places/p1/start/claude/as/a%2Fb")?.op, "start")
  assert.equal((writeRoute("POST", "/v1/places/p1/start/claude/as/a%2Fb") as { persona: string }).persona, "a/b")
})

test("a start or a resume as a persona goes as the same word with `persona`, to the Project's machine, under the press's key", async () => {
  const client = new FakeClient()
  client.descriptors.set("mac-a", { machine: { commands: ["start", "resume", "personas"] } })
  const { reader } = seam(client)

  const started = await reader.fetch("/v1/places/cloud-p1/start/codex/gpt-5/as/architect", post({}, { "Idempotency-Key": "press-7" }))
  assert.equal(started.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequestAs", "press-7", "mac-a", "start",
    { place: "p1", assistant: "codex", model: "gpt-5", persona: "architect" }, "action"])

  // Without a key the generic mints its own request, as `startPlace` does.
  await reader.fetch("/v1/places/cloud-p1/start/claude/as/security", post({}))
  assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "start",
    { place: "p1", assistant: "claude", model: "", persona: "security" }, "action"])

  await reader.fetch("/v1/places/cloud-p1/resume/claude/past-9/as/backend", post({}, { "Idempotency-Key": "press-8" }))
  assert.deepEqual(client.calls.pop(), ["_machineRequestAs", "press-8", "mac-a", "resume",
    { place: "p1", past: "past-9", assistant: "claude", persona: "backend" }, "action"], "one press, one request id")

  // Without a persona nothing changes: the copied client's own methods.
  await reader.fetch("/v1/places/cloud-p1/start/claude", post({}))
  assert.deepEqual(client.calls.pop(), ["startPlace", "cloud-p1", "claude", ""])
  await reader.fetch("/v1/places/cloud-p1/resume/claude/past-9", post({}, { "Idempotency-Key": "press-9" }))
  assert.deepEqual(client.calls.pop(), ["resumePlace", "cloud-p1", "past-9", "claude", "press-9"])
})

test("a persona start to a machine from before personas is refused before anything is sealed", async () => {
  const client = new FakeClient()
  client.descriptors.set("mac-a", { machine: { commands: ["start", "resume"] } })
  const { reader } = seam(client)
  const res = await reader.fetch("/v1/places/cloud-p1/start/claude/as/architect", post({}, { "Idempotency-Key": "press-1" }))
  assert.equal(res.status, 501)
  const body = await json<{ error: { code: string; outcome: string } }>(res)
  assert.equal(body.error.code, "cloud_machine_unsupported")
  assert.equal(body.error.outcome, "not_done")
  assert.deepEqual(client.calls, [], "nothing was asked of the machine")

  // A Project this page never read is refused by the client's own code.
  const unknown = await reader.fetch("/v1/places/nowhere/resume/claude/past-9/as/architect", post({}))
  assert.equal((await json<{ error: { code: string } }>(unknown)).error.code, "not_found")
  assert.deepEqual(client.calls, [])
})

test("a machine without `past-sessions` refuses the resume list by the client's own code", async () => {
  const client = new FakeClient()
  client.fail.pastSessions = refusal("cloud_feature_unavailable", { layer: "browser" })
  const { reader } = seam(client)
  const res = await reader.fetch("/v1/places/p1/sessions/claude")
  assert.equal(res.status, 501)
  const body = await json<{ error: { code: string; word: string } }>(res)
  assert.equal(body.error.code, "cloud_feature_unavailable")
  assert.equal(body.error.word, "past-sessions")
})

test("dictation picks the machine this page reads when the voice machine is ambiguous, once", async () => {
  const client = new FakeClient()
  client.fail.voice = refusal("cloud_voice_host_ambiguous", { layer: "browser" })
  const { reader } = seam(client)
  const res = await reader.fetch("/v1/voice", post({ audio: "AAAA", rate: 16000 }))
  assert.equal(res.status, 200)
  assert.deepEqual(await json(res), { text: "hello there", ms: 900 })
  assert.deepEqual(client.calls.map((c) => c[0]), ["voice", "setVoiceHost", "voice"])
  assert.equal(client.calls[1][1], "mac-a")
})

test("a spoken intent follows dictation to the chosen voice machine", async () => {
  const client = new FakeClient()
  const { reader } = seam(client)
  const res = await reader.fetch("/v1/intents", post({ text: "start the review" }))
  assert.equal(res.status, 200)
  assert.deepEqual(client.calls, [
    ["voiceHost"],
    ["_machineRequest", "mac-a", "intents", { text: "start the review" }, "action", 130_000],
  ])
  assert.equal((await json<{ draft: { place_id: string } }>(res)).draft.place_id, "cloud.WyJtYWMtYSIsInAxIl0",
    "the machine-local Project id uses the same Cloud id as the Project picker")
})

test("a route with no Cloud word is refused by name before anything is sealed", async () => {
  const client = new FakeClient()
  const { reader } = seam(client)
  const res = await reader.fetch("/v1/sessions/s1/rename", post({}))
  assert.equal(res.status, 501)
  const body = await json(res)
  assert.equal(body.error, "cloud_not_carried")
  assert.equal(body.word, "title")
  assert.deepEqual(client.calls, [])
})

test("a worktree refresh reaches the selected machine and returns its new observation", async () => {
  const client = new FakeClient()
  const { reader } = seam(client)
  const res = await reader.fetch("/v1/projects/%2Frepo/worktrees/refresh", post({}))
  assert.equal(res.status, 200)
  assert.deepEqual(client.calls.pop(), [
    "_machineRequest",
    "mac-a",
    "project-worktree-lifecycle-refresh",
    { project: "/repo" },
    "action",
  ])
})

test("Work v2 person actions keep their exact route subject and body across Cloud", async () => {
  const client = new FakeClient()
  const { reader } = seam(client)
  const cases: [string, Record<string, unknown>, string, Record<string, unknown>][] = [
    ["/v1/work/v2/items", { project_id: "cloud-p1", kind: "feature", title: "A", description: "B", acceptance_criteria: "- Done", deployment_policy: "agent_decides" },
      "work.v2.create", { item: { project_id: "p1", kind: "feature", title: "A", description: "B", acceptance_criteria: "- Done", deployment_policy: "agent_decides" } }],
    ["/v1/work/v2/items", { project_id: "cloud-p1", kind: "feature", title: "A", description: "B", deployment_policy: "agent_decides", review_required: true },
      "work.v2.create", { item: { project_id: "p1", kind: "feature", title: "A", description: "B", deployment_policy: "agent_decides", review_required: true } }],
    ["/v1/work/v2/items/w1/gate-decision", { expected_version: 7, action: "override", reason: "Evidence reviewed" },
      "work.v2.gate-decision", { id: "w1", item: { expected_version: 7, action: "override", reason: "Evidence reviewed" } }],
    ["/v1/work/v2/items/w1/gate-purge", { expected_version: 7, sha256: "abc" },
      "work.v2.gate-purge", { id: "w1", item: { expected_version: 7, sha256: "abc" } }],
    ["/v1/work/v2/items/w1/assign", { expected_version: 1, mode: "new_session", assistant: "codex", model: "default" },
      "work.v2.assign", { id: "w1", item: { expected_version: 1, mode: "new_session", assistant: "codex", model: "default" } }],
    ["/v1/work/v2/items/w1/convert", { expected_version: 2, kind: "plan" },
      "work.v2.convert", { id: "w1", item: { expected_version: 2, kind: "plan" } }],
    ["/v1/work/v2/items/w1/persona-suggestion", { expected_version: 1 },
      "work.v2.persona-suggestion", { id: "w1", item: { expected_version: 1 } }],
    ["/v1/work/v2/items/w1/remind", { expected_version: 2 },
      "work.v2.remind", { id: "w1", item: { expected_version: 2 } }],
    ["/v1/work/v2/items/w1/images", { expected_version: 2, title: "state.png", data_url: "data:image/png;base64,cG5n" },
      "work.v2.image-create", { id: "w1", item: { expected_version: 2, title: "state.png", data_url: "data:image/png;base64,cG5n" } }],
    ["/v1/work/v2/proposals/pr1/accept", {}, "work.v2.proposal-resolve", { id: "pr1", decision: "accept", item: {} }],
    ["/v1/work/v2/session-todos/%251", { text: "Ship it" }, "work.v2.todo-create", { terminal: "%1", item: { text: "Ship it" } }],
    ["/v1/work/v2/session-todos/%251/t1/images", { expected_version: 1, title: "screen.png", data_url: "data:image/png;base64,cG5n" },
      "work.v2.todo-image-create", { terminal: "%1", id: "t1", item: { expected_version: 1, title: "screen.png", data_url: "data:image/png;base64,cG5n" } }],
    ["/v1/work/v2/session-todos/%251/t1/send", {}, "work.v2.todo-action", { terminal: "%1", id: "t1", action: "send", item: {} }],
    ["/v1/work/v2/session-todos/%251/t1/reopen", {}, "work.v2.todo-action", { terminal: "%1", id: "t1", action: "reopen", item: {} }],
    ["/v1/work/v2/human-interventions/conversation%3A10000000-0000-4000-8000-000000000002/10000000-0000-4000-8000-000000000003/read", { expected_version: 1 }, "work.v2.human-intervention-action", { terminal: "conversation:10000000-0000-4000-8000-000000000002", id: "10000000-0000-4000-8000-000000000003", action: "read", item: { expected_version: 1 } }],
  ]
  for (const [path, sent, word, body] of cases) {
    const response = await reader.fetch(path, post(sent))
    assert.equal(response.status, 200, path)
    assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", word, body, "action"], path)
  }
  const removed = await reader.fetch("/v1/work/v2/items/w1/images/img1", {
    ...post({ expected_version: 3 }), method: "DELETE",
  })
  assert.equal(removed.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "work.v2.image-delete",
    { id: "w1", image: "img1", item: { expected_version: 3 } }, "action"])

  const edited = await reader.fetch("/v1/work/v2/items/w1", {
    ...post({ expected_version: 4, title: "Edited", description: "Changed", acceptance_criteria: "# Changed" }), method: "PATCH",
  })
  assert.equal(edited.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "work.v2.edit", {
    id: "w1", item: { expected_version: 4, title: "Edited", description: "Changed", acceptance_criteria: "# Changed" },
  }, "action"])

  const reviewRequired = await reader.fetch("/v1/work/v2/items/w1", {
    ...post({ expected_version: 5, review_required: true }), method: "PATCH",
  })
  assert.equal(reviewRequired.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "work.v2.edit", {
    id: "w1", item: { expected_version: 5, review_required: true },
  }, "action"])

  const cancelled = await reader.fetch("/v1/work/v2/items/w1/cancel", post({
    expected_version: 5, reason: "Deleted by the person from the Board.",
  }))
  assert.equal(cancelled.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "work.v2.cancel", {
    id: "w1", item: { expected_version: 5, reason: "Deleted by the person from the Board." },
  }, "action"])

  const completed = await reader.fetch("/v1/work/v2/items/w1/complete", post({ expected_version: 6 }))
  assert.equal(completed.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "work.v2.complete", {
    id: "w1", item: { expected_version: 6 },
  }, "action"])

  for (const phase of ["deploying", "done"]) {
    const seen = await reader.fetch("/v1/work/v2/items/w1/seen", post({ phase }))
    assert.equal(seen.status, 200, phase)
    assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "work.v2.seen", {
      id: "w1", item: { phase },
    }, "action"], phase)
  }
})

test("a Work v2 create resolves the Cloud Project id and keeps the press id through the machine", async () => {
  const client = new FakeClient()
  const { reader } = seam(client)
  const response = await reader.fetch("/v1/work/v2/items", post({
    project_id: "cloud-p1", kind: "issue", title: "A", description: "B", deployment_policy: "agent_decides",
  }, { "Idempotency-Key": "press-create-1" }))
  assert.equal(response.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequestAs", "press-create-1", "mac-a", "work.v2.create", {
    item: { project_id: "p1", kind: "issue", title: "A", description: "B", deployment_policy: "agent_decides" },
  }, "action"])
})

test("a decision answer keeps its question and press id across Cloud", async () => {
  const id = "10000000-0000-4000-8000-000000000001"
  const path = `/v1/work/decisions/${id}`
  assert.equal(writeRoute("POST", path)?.word, "work.decision-answer")
  for (const invalid of ["../other", "INVALID", `${id}/resolve`, `${id}/extra`]) {
    assert.equal(writeRoute("POST", `/v1/work/decisions/${invalid}`), null)
  }
  const client = new FakeClient()
  const { reader } = seam(client)
  const first = await reader.fetch(path, post({ answer: "Proceed" }, { "Idempotency-Key": "press-decision-1" }))
  assert.equal(first.status, 200)
  const retry = await reader.fetch(path, post({ answer: "Proceed" }, { "Idempotency-Key": "press-decision-1" }))
  assert.equal(retry.status, 200)
  assert.deepEqual(client.calls, [
    ["_machineRequestAs", "press-decision-1", "mac-a", "work.decision-answer", { id, answer: "Proceed" }, "action"],
    ["_machineRequestAs", "press-decision-1", "mac-a", "work.decision-answer", { id, answer: "Proceed" }, "action"],
  ])
  client.fail._machineRequestAs = failureFromMac({ code: "decision_already_answered", layer: "mac_route", message: "This decision was already answered." }, 409, REF)
  const answered = await reader.fetch(path, post({ answer: "Proceed" }, { "Idempotency-Key": "press-decision-2" }))
  assert.equal(answered.status, 409)
  assert.equal(((await json(answered)).error as { code: string }).code, "decision_already_answered")
  client.fail._machineRequestAs = failureFromMac({ code: "store_unavailable", layer: "mac_route", message: "Try again." }, 503, REF)
  const failed = await reader.fetch(path, post({ answer: "Proceed" }, { "Idempotency-Key": "press-decision-3" }))
  assert.equal(failed.status, 503)
  assert.equal(((await json(failed)).error as { code: string }).code, "store_unavailable")
})

test("a refused Work v2 create settles as the route's flat refusal", async () => {
  const client = new FakeClient()
  client.fail._machineRequestAs = failureFromMac({
    code: "project_not_found", layer: "mac_route", message: "Choose a current Project.",
  }, 422, REF)
  const { reader } = seam(client)
  const response = await reader.fetch("/v1/work/v2/items", post({
    project_id: "cloud-p1", kind: "issue", title: "A", description: "B", deployment_policy: "agent_decides",
  }, { "Idempotency-Key": "press-create-2" }))
  assert.equal(response.status, 422)
  assert.deepEqual(await json(response), {
    error: "project_not_found", detail: "Choose a current Project.", route: "/v1/work/v2/items",
    layer: "mac_route", ref: "abcdef12·12", retryable: false, word: "work.v2.create",
  })
})

test("a Board reference picture is read as machine-scoped bytes", async () => {
  const client = new FakeClient()
  const { reader } = seam(client)
  const res = await reader.fetch("/v1/work/v2/images/img-1")
  assert.equal(res.status, 200)
  assert.equal(res.headers.get("content-type"), "image/png")
  assert.deepEqual([...new Uint8Array(await res.arrayBuffer())], [137, 80, 78, 71])
  assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "work.v2.image", { id: "img-1" }, "read"])
})

test("a Board card's thumbnail carries its size to the machine, and the answer's JPEG type crosses whole", async () => {
  const client = new FakeClient()
  const machineRequest = client._machineRequest.bind(client)
  client._machineRequest = async (machine, word, body, kind, timeoutMs) => {
    const answer = await machineRequest(machine, word, body, kind, timeoutMs)
    return word === "work.v2.image" && body.size === "thumb" ? { ...(answer as object), media_type: "image/jpeg" } : answer
  }
  const { reader } = seam(client)
  const res = await reader.fetch("/v1/work/v2/images/img-1?size=thumb")
  assert.equal(res.status, 200)
  assert.equal(res.headers.get("content-type"), "image/jpeg")
  assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "work.v2.image", { id: "img-1", size: "thumb" }, "read"])
})

test("a reference picture's query the wire has no field for is refused, not dropped", async () => {
  const client = new FakeClient()
  const { reader } = seam(client)
  for (const query of ["?size=large", "?width=480", "?size=thumb&width=480"]) {
    const res = await reader.fetch("/v1/work/v2/images/img-1" + query)
    assert.equal(res.status, 501, query)
    assert.equal((await json<{ error: { code: string } }>(res)).error.code, "cloud_not_carried", query)
  }
  assert.equal(client.calls.filter((call) => call[0] === "_machineRequest").length, 0)
})

test("a reference picture the machine could not fit on its reply channel answers 429 cloud_read_busy", async () => {
  const client = new FakeClient()
  client.fail._machineRequest = failureFromMac({
    code: "cloud_read_busy", layer: "mac_transport", message: "the channel that answer goes on is full",
    detail: { retry_after: 5, lane: "egress", limit: 4 << 20 },
  }, 429, REF)
  const { reader } = seam(client)
  const res = await reader.fetch("/v1/work/v2/images/img-1?size=thumb")
  assert.equal(res.status, 429)
  assert.equal((await json<{ error: { code: string } }>(res)).error.code, "cloud_read_busy")
})

test("a transcript's picture is read as bytes through the machine's `image`", async () => {
  const client = new FakeClient()
  client.rows = [row("s1")]
  const { reader } = seam(client)
  const res = await reader.fetch("/v1/artifacts/images/img-1?session=s1")
  assert.equal(res.status, 200)
  assert.equal(res.headers.get("content-type"), "image/png")
  assert.deepEqual([...new Uint8Array(await res.arrayBuffer())], [137, 80, 78, 71])
  assert.deepEqual(client.calls.pop(), ["image", { machine: "mac-a", session: "s1" }, "img-1"])
  const bare = await reader.fetch("/v1/artifacts/images/img-1")
  assert.equal((await json<{ error: { code: string } }>(bare)).error.code, "malformed_read")
})

// ---- F1, F2, F3, F5, F6, F12: what the writer must say, and send, and not send.


test("F2: every attempt of one card is one Cloud request, the card's own", async () => {
  const client = new FakeClient()
  client.rows = [row("s1")]
  const { reader } = seam(client)
  for (let attempt = 0; attempt < 2; attempt++) {
    await reader.fetch("/v1/sessions/s1/send", post({ text: "delete it" }, { "Idempotency-Key": "card-7" }))
  }
  const sends = client.calls.filter((c) => c[0] === "_read")
  assert.equal(sends.length, 2, "each attempt is asked of the machine and settled by its answer")
  for (const call of sends) {
    assert.deepEqual(call.slice(1, 5), [
      { machine: "mac-a", session: "s1" }, "send", { request: "card-7", text: "delete it", images: [] }, "action:card-7",
    ])
  }
})

test("F1: a press names the question it answers, under the press's own request", async () => {
  const client = new FakeClient()
  client.rows = [row("s1")]
  client.descriptors.set("mac-a", GO_DAEMON)
  const { reader } = seam(client)
  const res = await reader.fetch("/v1/sessions/s1/key", post({ key: "2", expect: FINGERPRINT }, { "Idempotency-Key": "press-3" }))
  assert.equal(res.status, 200)
  assert.deepEqual(client.calls.pop(), [
    "_read", { machine: "mac-a", session: "s1" }, "answer", { request: "press-3", answer: "2", expect: FINGERPRINT },
    "action:press-3", undefined, { retireUncertain: true },
  ])
})

test("an Enter on words typed and never submitted names the words, not a question", async () => {
  const client = new FakeClient()
  client.rows = [row("s1")]
  client.descriptors.set("mac-a", GO_DAEMON)
  const { reader } = seam(client)
  const res = await reader.fetch("/v1/sessions/s1/key", post({ key: "enter", typed: "readCHILD.md" }, { "Idempotency-Key": "enter-1" }))
  assert.equal(res.status, 200)
  assert.deepEqual(client.calls.pop(), [
    "_read", { machine: "mac-a", session: "s1" }, "answer", { request: "enter-1", answer: "enter", typed: "readCHILD.md" },
    "action:enter-1", undefined, { retireUncertain: true },
  ])
  for (const [name, body] of [
    ["an Enter naming no words", { key: "enter" }],
    ["an Enter naming a question instead", { key: "enter", typed: "x", expect: FINGERPRINT }],
    ["a digit naming words instead of its question", { key: "2", typed: "x" }],
  ] as [string, Record<string, unknown>][]) {
    const refused = await reader.fetch("/v1/sessions/s1/key", post(body))
    assert.equal(refused.status, 428, name)
  }
  assert.deepEqual(client.calls.filter((c) => c[0] === "_read"), [], "nothing else was sealed")
})

test("F1, F7: a press that cannot be checked against the machine's screen is refused here, and nothing is sent", async () => {
  const cases: [string, (c: FakeClient) => void, Record<string, unknown>][] = [
    ["no question named", (c) => c.descriptors.set("mac-a", GO_DAEMON), { key: "2" }],
    ["a machine whose words are not known yet", () => {}, { key: "2", expect: FINGERPRINT }],
    ["a Mac that answers without checking (a Swift app)", (c) => {
      c.descriptors.set("mac-a", GO_DAEMON)
      c.macCapabilities.add("mac-a")
    }, { key: "2", expect: FINGERPRINT }],
  ]
  for (const [name, set, body] of cases) {
    const client = new FakeClient()
    client.rows = [row("s1")]
    set(client)
    const { reader } = seam(client)
    const res = await reader.fetch("/v1/sessions/s1/key", post(body))
    assert.equal(res.status, 428, name)
    const refused = await json(res)
    assert.equal(refused.error, "menu_unverified", name)
    assert.equal(refused.outcome, "not_done", name)
    assert.deepEqual(client.calls, [], name + ": nothing was sealed")
  }
})

test("F3: a write is `not_done` only when this page can prove it never reached the machine", async () => {
  // [what failed, the outcome the page must be told]
  const cases: [string, Error, string | undefined][] = [
    ["never sealed: the copied client refused before publishing", refusal("cloud_read_only", { layer: "browser", ref: null }), "not_done"],
    ["the relay said the machine is not connected", refusal("machine_offline", { layer: "relay", ref: REF }), "not_done"],
    ["the machine refused before acting", refusal("cloud_commands_disabled", { layer: "mac_preflight", ref: REF }), "not_done"],
    ["the machine's queue was full", refusal("cloud_ingress_busy", { layer: "mac_transport", ref: REF }), "not_done"],
    ["the socket dropped after the envelope was written", refusal("offline", { layer: "browser", ref: REF }), "unknown"],
    ["the token was replaced mid-flight", refusal("token_superseded", { layer: "relay", ref: REF }), "unknown"],
    ["the relay closed with an internal error", refusal("internal", { layer: "relay", ref: REF }), "unknown"],
    ["an error nobody named", refusal("unexpected_error", { layer: "browser", ref: REF }), "unknown"],
    ["the machine ran it and the reply was lost", refusal("command_answer_undeliverable", { layer: "mac_reply", ref: REF }), "unknown"],
    ["nothing answered in time", refusal("cloud_read_timeout", { layer: "browser", ref: null }), "unknown"],
    // The route's own refusal is read by its code, as the same refusal from a
    // daemon on this machine is (`session/outcome.ts`).
    ["the machine's route refused", refusal("terminal_io_failed", { layer: "mac_route", ref: REF }), undefined],
  ]
  for (const [name, failure, outcome] of cases) {
    const client = new FakeClient()
    client.rows = [row("s1")]
    client.fail._read = failure
    const { reader } = seam(client)
    const body = await json(await reader.fetch("/v1/sessions/s1/send", post({ text: "hi" }, { "Idempotency-Key": "card-1" })))
    assert.equal(body.outcome, outcome, name)
  }
})

test("F5: a write for a session this page has no row for is refused here, not sent to the raw id", async () => {
  const client = new FakeClient()
  client.rows = [row("s1")]
  const { reader } = seam(client)
  const res = await reader.fetch("/v1/sessions/gone/send", post({ text: "hi" }, { "Idempotency-Key": "card-1" }))
  assert.equal(res.status, 404)
  const body = await json(res)
  assert.equal(body.error, "session_not_found")
  assert.equal(body.outcome, "not_done")
  assert.deepEqual(client.calls, [])
})

test("F12: showing a session on the machine does not make every poll re-read its transcript", async () => {
  const client = new FakeClient()
  client.rows = [row("s1")]
  const { reader, clock } = seam(client)
  const read = async () => reader.fetch("/v1/transcript?session=s1&limit=200")
  await read()
  await reader.fetch("/v1/sessions/s1/focus", post({}))
  for (let i = 0; i < 3; i++) {
    clock.t += 4_000
    await read()
  }
  assert.equal(client.transcriptAsks, 1, "focus changes nothing a transcript holds")
})

// F6. The machine's refusal reaches the writer through the copied client's own
// `failureFromMac`, which this test now uses instead of an error built by
// hand — the hand-built one carried `reasons` the real path never does.
test("F6: a blocked close, through the copied client's real failure path", async () => {
  const client = new FakeClient()
  client.rows = [row("s1")]
  const reasons = [{ kind: "obligation", code: "landing", subject_id: "t1", subject_kind: "task" }]
  client.fail.end = failureFromMac({ code: "close_blocked", layer: "mac_route", message: "still owed", reasons }, 409, REF)
  const { reader } = seam(client)
  const res = await reader.fetch("/v1/sessions/s1/close", post({ force: false }))
  assert.equal(res.status, 409)
  const body = await json(res)
  assert.equal(body.error, "close_blocked")
  // What the page is really handed today. `cloud-failure.js` keeps only the
  // §11.6 detail fields and `close_blocked` has none there, so the reasons
  // stop at the copied client. The close sheet reads them back from the row
  // it was refused against (`overlays/close-release.ts` `refusedReasons`);
  // carrying them here belongs in that copied file's source (the Swift app's
  // console) and is named by the todo below.
  assert.equal(body.reasons, undefined)
})

test("F6: a blocked close keeps its reasons across Clawdline Cloud",
  { todo: "cloud-failure.js (copied byte for byte from the Swift app) drops them; fix it at its source" },
  async () => {
    const client = new FakeClient()
    client.rows = [row("s1")]
    const reasons = [{ kind: "obligation", code: "landing", subject_id: "t1", subject_kind: "task" }]
    client.fail.end = failureFromMac({ code: "close_blocked", layer: "mac_route", message: "still owed", reasons }, 409, REF)
    const { reader } = seam(client)
    const body = await json(await reader.fetch("/v1/sessions/s1/close", post({ force: false })))
    assert.deepEqual(body.reasons, reasons)
  })

// The three requests that change something about notifications. Each goes as
// the word the machine lists, with the browser's own subscription handed over
// whole: it is the browser's endpoint and the browser's keys, and anything
// reshaped on the way past is a chance to get a credential wrong.
test("registering for notifications goes as the machine's own three words", async () => {
  const client = new FakeClient()
  const { reader } = seam(client)
  const subscription = {
    endpoint: "https://web.push.apple.com/QWxpY2U",
    keys: { p256dh: "BPk", auth: "c2VjcmV0" },
  }

  const subscribed = await reader.fetch("/v1/push/subscribe", post(subscription))
  assert.equal(subscribed.status, 200)
  assert.deepEqual(await json(subscribed), { ok: true, id: "sub-1" })

  const tested = await reader.fetch("/v1/push/test", post({ session_id: "s1" }))
  assert.equal(tested.status, 200)
  assert.deepEqual(await json(tested), { ok: true, sent: 1, failed: 0 })

  // No session to tap back to is the account's test, and the word carries it
  // as an empty target rather than leaving the key out.
  await reader.fetch("/v1/push/test", post({}))
  await reader.fetch("/v1/push/unsubscribe", post({ id: "sub-1" }))

  assert.deepEqual(client.calls, [
    ["pushSubscribe", subscription],
    ["pushTest", "s1"],
    ["pushTest", ""],
    ["pushUnsubscribe", "sub-1"],
  ])
  assert.deepEqual(reader.log.map((x: { word?: string }) => x.word), [
    "push-subscribe", "push-test", "push-test", "push-unsubscribe",
  ])
})

// `push/api.ts` says it at the top: these four routes answer the gate's
// nested envelope, not the flat `{error, detail}` the rest of this daemon
// uses. It reads both, and what must not happen is a third spelling.
test("a refused registration comes back in the nested spelling `push/api.ts` reads", async () => {
  const client = new FakeClient()
  client.fail.pushSubscribe = failureFromMac(
    { code: "subscriptions_full", layer: "mac_route", message: "already notifies as many devices as it keeps" },
    507, REF,
  )
  const { reader } = seam(client)
  const res = await reader.fetch("/v1/push/subscribe", post({ endpoint: "https://web.push.apple.com/QWxpY2U" }))
  assert.equal(res.status, 507)
  const body = await json<{ error: { code: string; message: string; layer: string; word: string } }>(res)
  assert.equal(typeof body.error, "object", "the flat spelling would put a string here")
  assert.equal(body.error.code, "subscriptions_full")
  assert.equal(typeof body.error.message, "string")
  assert.equal(body.error.layer, "mac_route")
  assert.equal(body.error.word, "push-subscribe")
  assert.equal(reader.log[reader.log.length - 1].code, "subscriptions_full")
})

// With the line down, the page is told so rather than left waiting: a write
// settles with a typed refusal, never a silence.
test("a registration with the line down is refused, not left to a transport error", async () => {
  const client = new FakeClient()
  client.ready = false
  const { reader } = seam(client)
  const res = await reader.fetch("/v1/push/subscribe", post({ endpoint: "https://web.push.apple.com/QWxpY2U" }))
  assert.equal(res.status, 503)
  const body = await json<{ error: { code: string } }>(res)
  assert.equal(body.error.code, "offline")
})

// Making, saving, removing and running a schedule from a phone. The form's
// own requests, unchanged — `schedules-bridge.ts` spells them against a
// daemon on this machine's own network — reaching the copied client's four
// schedule methods, which route each one to the machine that owns it.
test("the schedule form's four writes reach the machine as its own four words", async () => {
  const client = new FakeClient()
  const { reader } = seam(client)
  const form = { title: "a schedule", at: "09:00", days: "daily", place_id: "mac-a\u0000p1", assistant: "claude",
    model: "", instructions: "do the thing", enabled: true, close_tab: "on_success", catch_up_hours: 6,
    notify_on_failure: true, timeout_minutes: 30 }

  const made = await reader.fetch("/v1/orchestrator/schedules", post(form, { "Idempotency-Key": "press-1" }))
  assert.equal(made.status, 200)
  assert.deepEqual(await json(made), { ok: true, schedule: { id: "sch-9", title: "a schedule" }, dispatch_enabled: true })

  const saved = await reader.fetch("/v1/orchestrator/schedules/sch%209", {
    ...post(form, { "Idempotency-Key": "press-2" }), method: "PATCH",
  })
  assert.equal(saved.status, 200)

  const removed = await reader.fetch("/v1/orchestrator/schedules/sch-9", {
    method: "DELETE", headers: { "Idempotency-Key": "press-3" },
  })
  assert.equal(removed.status, 200)

  const ran = await reader.fetch("/v1/orchestrator/schedules/sch-9/run", post({}, { "Idempotency-Key": "press-4" }))
  assert.equal(ran.status, 200)
  assert.deepEqual(await json(ran), { ok: true, task_id: "t-1" })

  assert.deepEqual(client.calls, [
    // The form's body whole: the copied client reads `place_id` out of it to
    // find the machine, so nothing may be reshaped on the way past.
    ["createSchedule", form],
    ["_machineRequest", "mac-a", "schedule-update", { id: "sch 9", schedule: form }, "action"],
    ["_machineRequest", "mac-a", "schedule-delete", { id: "sch-9" }, "action"],
    ["_machineRequest", "mac-a", "schedule-run", { id: "sch-9" }, "action"],
  ])
  assert.deepEqual(reader.log.map((x: { word?: string; answer: string }) => [x.word, x.answer]), [
    ["schedule-create", "relay"], ["schedule-update", "relay"],
    ["schedule-delete", "relay"], ["schedule-run", "relay"],
  ])
})

// Reading one schedule goes directly to the selected machine, so it can still
// open after a retained inventory has been replaced by a snapshot that does
// not carry schedules. Saving must use that same selected machine instead of
// looking the id up in the now-empty inventory a second time.
test("saving an open schedule does not depend on a retained schedule inventory", async () => {
  const client = new FakeClient()
  client.fail.updateSchedule = Object.assign(new Error("this schedule is not in the Cloud inventory"), {
    code: "not_found",
  })
  const { reader } = seam(client)
  const form = { title: "a schedule", place_id: "mac-a\u0000p1" }
  const saved = await reader.fetch("/v1/orchestrator/schedules/sch-9", {
    ...post(form), method: "PATCH",
  })
  assert.equal(saved.status, 200)
  assert.deepEqual(client.calls, [
    ["_machineRequest", "mac-a", "schedule-update", { id: "sch-9", schedule: form }, "action"],
  ])
})

// A schedule lives on the machine that runs it, and the list shows every
// machine's rows (docs/schedules.md). Each action on a row names the row's
// machine, and goes there rather than to this page's machine; a save naming
// another machine's Project is still refused, because a move is three writes,
// not one save (`cloud/schedule-move.ts`).
test("an action on another machine's schedule goes to that machine", async () => {
  const client = new FakeClient()
  client._scheduleBody = (schedule: unknown) => {
    const body = schedule as Record<string, unknown>
    return { machine: String(body.place_id).split("\u0000")[0], schedule: body }
  }
  const { reader } = seam(client)
  const form = { title: "a schedule", place_id: "linux-b\u0000p2" }
  const saved = await reader.fetch("/v1/orchestrator/schedules/sch-9?machine=linux-b", { ...post(form), method: "PATCH" })
  assert.equal(saved.status, 200)
  const removed = await reader.fetch("/v1/orchestrator/schedules/sch-9?machine=linux-b", { method: "DELETE" })
  assert.equal(removed.status, 200)
  const ran = await reader.fetch("/v1/orchestrator/schedules/sch-9/run?machine=linux-b", post({}))
  assert.equal(ran.status, 200)
  const places = await reader.fetch("/v1/places?machine=linux-b")
  assert.equal(places.status, 200)
  assert.deepEqual(client.calls, [
    ["_machineRequest", "linux-b", "schedule-update", { id: "sch-9", schedule: form }, "action"],
    ["_machineRequest", "linux-b", "schedule-delete", { id: "sch-9" }, "action"],
    ["_machineRequest", "linux-b", "schedule-run", { id: "sch-9" }, "action"],
    ["places", "linux-b"],
  ])

  client.calls.length = 0
  const crossed = await reader.fetch("/v1/orchestrator/schedules/sch-9?machine=mac-a", { ...post(form), method: "PATCH" })
  assert.equal(crossed.status, 409)
  assert.equal((await json<{ error: { code: string } }>(crossed)).error.code, "cloud_schedule_machine_mismatch")
  assert.deepEqual(client.calls, [], "nothing is written for a save that names another machine's Project")
})

// The schedule routes refuse through `writeAuthRefusal` and
// `writeBrokerRefusal` (internal/transport/http/schedules.go), both of which
// send `{"error":{"code","message",…}}` — and the form reads a broker
// refusal's extra fields out of that same object.
test("a refused schedule write comes back in the spelling its own route answers", async () => {
  const client = new FakeClient()
  client.fail._machineRequest = failureFromMac(
    { code: "task_already_running", layer: "mac_route", message: "that schedule is already running" },
    409, REF,
  )
  const { reader } = seam(client)
  const res = await reader.fetch("/v1/orchestrator/schedules/sch-9/run", post({}))
  assert.equal(res.status, 409)
  const body = await json<{ error: { code: string; message: string; layer: string; word: string } }>(res)
  assert.equal(typeof body.error, "object", "the flat spelling would put a string here")
  assert.equal(body.error.code, "task_already_running")
  assert.equal(body.error.word, "schedule-run")
  assert.equal(reader.log[reader.log.length - 1].code, "task_already_running")
})

// A copied client older than the four words. Refused by name rather than
// thrown as a `TypeError` inside the form's own `then`.
test("a client that cannot write schedules is refused by name", async () => {
  const client = new FakeClient()
  const older = client as unknown as Record<string, unknown>
  for (const name of ["createSchedule", "_scheduleBody", "_machineRequest"]) older[name] = undefined
  const { reader } = seam(client)
  for (const [method, path] of [
    ["POST", "/v1/orchestrator/schedules"],
    ["PATCH", "/v1/orchestrator/schedules/sch-9"],
    ["DELETE", "/v1/orchestrator/schedules/sch-9"],
    ["POST", "/v1/orchestrator/schedules/sch-9/run"],
  ]) {
    const res = await reader.fetch(path, { ...post({}), method })
    assert.equal(res.status, 501, method + " " + path)
    assert.equal((await json<{ error: { code: string } }>(res)).error.code, "cloud_not_carried")
  }
  assert.deepEqual(client.calls, [], "nothing was asked of the machine")
})

// 常用句 from a phone: the sheet's own five requests, unchanged
// (`session/snippets-api.ts` spells them against a daemon on this machine's
// own network), reaching the copied client's four snippet methods — each of
// which names the machine whose settings change through the session the sheet was
// opened on.
test("the snippet sheet's four writes reach the machine as its own four words", async () => {
  const client = new FakeClient()
  const { reader } = seam(client)
  const identity = { machine: "mac-a", session: "s1" }
  const draft = { title: "a title", body: "a body", scope: "global" }

  const made = await reader.fetch("/v1/snippets?session=s1", post(draft, { "Idempotency-Key": "press-1" }))
  assert.equal(made.status, 200)
  assert.deepEqual(await json(made), { id: "sn-9", title: "a title", body: "a body", scope: "global", position: 0 })

  const saved = await reader.fetch("/v1/snippets/sn%209?session=s1", {
    ...post(draft, { "Idempotency-Key": "press-2" }), method: "PATCH",
  })
  assert.equal(saved.status, 200)

  const removed = await reader.fetch("/v1/snippets/sn-9?session=s1", {
    method: "DELETE", headers: { "Idempotency-Key": "press-3" },
  })
  assert.equal(removed.status, 200)

  const ordered = await reader.fetch("/v1/snippets/order?session=s1",
    post({ scope: "project", project: "/tmp/p", order: ["sn-9", "sn-8"] }, { "Idempotency-Key": "press-4" }))
  assert.equal(ordered.status, 200)

  assert.deepEqual(client.calls, [
    ["createSnippet", draft, identity],
    ["updateSnippet", "sn 9", draft, identity],
    ["deleteSnippet", "sn-9", identity],
    // Taken apart here and put back together by the copied client, which is
    // the producer for this word: `ordering` is its spelling and the local
    // route reads the same three fields under no name at all.
    ["orderSnippets", "project", "/tmp/p", ["sn-9", "sn-8"], identity],
  ])
  assert.deepEqual(reader.log.map((x: { word?: string; answer: string }) => [x.word, x.answer]), [
    ["snippet-create", "relay"], ["snippet-update", "relay"],
    ["snippet-delete", "relay"], ["snippet-order", "relay"],
  ])
})

// The snippet routes answer the flat `{"error":"code","detail":"…"}`
// (internal/transport/http/snippets.go), which is what `snippets-api.ts`
// branches on — and the limits ride beside it in `counts`, where the sheet
// reads them.
test("a refused snippet write comes back in the spelling its own route answers", async () => {
  const client = new FakeClient()
  client.fail.createSnippet = failureFromMac(
    { code: "snippet_limit_reached", layer: "mac_route", message: "this machine holds as many snippets as it may" },
    409, REF,
  )
  const { reader } = seam(client)
  const res = await reader.fetch("/v1/snippets?session=s1", post({ title: "a title", body: "a body", scope: "global" }))
  assert.equal(res.status, 409)
  const body = await json<{ error: string; detail: string; word: string }>(res)
  assert.equal(typeof body.error, "string", "the nested spelling would put an object here")
  assert.equal(body.error, "snippet_limit_reached")
  assert.equal(reader.log[reader.log.length - 1].code, "snippet_limit_reached")
})

// A write that names no session is aimed at no machine, and is refused here
// rather than sent to a guess.
test("a snippet write that names no session is refused before anything is sealed", async () => {
  const client = new FakeClient()
  const { reader } = seam(client)
  const res = await reader.fetch("/v1/snippets", post({ title: "a title", body: "a body", scope: "global" }))
  assert.equal(res.status, 400)
  assert.equal((await json<{ error: string }>(res)).error, "bad_request")
  assert.deepEqual(client.calls, [], "nothing was asked of the machine")
})

// A copied client older than the four words, refused by name rather than
// thrown as a `TypeError` inside the sheet's own `then`.
test("a client that cannot write snippets is refused by name", async () => {
  const client = new FakeClient()
  const older = client as unknown as Record<string, unknown>
  for (const name of ["createSnippet", "updateSnippet", "deleteSnippet", "orderSnippets"]) older[name] = undefined
  const { reader } = seam(client)
  for (const [method, path] of [
    ["POST", "/v1/snippets?session=s1"],
    ["PATCH", "/v1/snippets/sn-9?session=s1"],
    ["DELETE", "/v1/snippets/sn-9?session=s1"],
    ["POST", "/v1/snippets/order?session=s1"],
  ]) {
    const res = await reader.fetch(path, { ...post({}), method })
    assert.equal(res.status, 501, method + " " + path)
    assert.equal((await json<{ error: string }>(res)).error, "cloud_not_carried")
  }
  assert.deepEqual(client.calls, [], "nothing was asked of the machine")
})

test("project icon copy resolves the receiving place and carries only the mark", async () => {
  const client = new FakeClient()
  const { reader } = seam(client)
  const item = { icon: { accent: "#123456", cells: [["#123456", null]] }, expected: { accent: "#FFFFFF", cells: [[null]] } }
  const response = await reader.fetch("/v1/projects/cloud-p1/icon", { ...post(item), method: "PUT" })
  assert.equal(response.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "project-icon-copy", { id: "p1", item }, "action"])
  client._place = () => ({ machine: "different-machine", id: "p1", path: "/fixture" })
  const refused = await reader.fetch("/v1/projects/cloud-p1/icon", { ...post(item), method: "PUT" })
  assert.equal(refused.status, 409)
})

test("a Project file save keeps its key, Project machine and exact file id", async () => {
  const client = new FakeClient()
  const { reader } = seam(client)
  const path = "/v1/projects/cloud-p1/files/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  const item = { expected_version: "v1", content: "updated" }
  const saved = await reader.fetch(path, { ...post(item), method: "PUT", headers: { "Idempotency-Key": "save-1" } })
  assert.equal(saved.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequestAs", "save-1", "mac-a", "project-file-save",
    { project: "p1", file: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", item }, "action"])
  const missingKey = await reader.fetch(path, { ...post(item), method: "PUT" })
  assert.equal(missingKey.status, 400)
  client._place = () => ({ machine: "different-machine", id: "p1", path: "/fixture" })
  const wrongMachine = await reader.fetch(path, { ...post(item), method: "PUT", headers: { "Idempotency-Key": "save-2" } })
  assert.equal(wrongMachine.status, 409)
  assert.equal(writeRoute("PUT", "/v1/projects/cloud-p1/files/%2Fetc%2Fpasswd"), null)
})

test("a project settings apply and detach reach the mirror as its own words, and a read reaches the machine", async () => {
  const client = new FakeClient()
  const { reader } = seam(client)
  const item = { source: { machine: "mac-b", name: "Studio" }, project: { repo: "github.com/o/n" }, clone: false }
  const applied = await reader.fetch("/v1/project-sync/mirror", post(item))
  assert.equal(applied.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "project-mirror-apply", { item }, "action"])
  const detached = await reader.fetch("/v1/project-sync/mirror?repo=github.com%2Fo%2Fn", { method: "DELETE" })
  assert.equal(detached.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "project-mirror-detach", { repo: "github.com/o/n" }, "action"])
  assert.equal(writeRoute("GET", "/v1/project-sync/mirror"), null)
  assert.equal(writeRoute("PUT", "/v1/project-sync/mirror"), null)
})

test("the token bill's three reads cross as the machine's usage words, with the id and nothing else", async () => {
  const client = new FakeClient()
  const { reader } = seam(client)
  const cases: [string, string, string][] = [
    ["/v1/usage/sessions/c0ffee00-0000-4000-8000-000000000005", "usage.session", "c0ffee00-0000-4000-8000-000000000005"],
    ["/v1/usage/tasks/7a000000-0000-4000-8000-000000000001", "usage.task", "7a000000-0000-4000-8000-000000000001"],
    ["/v1/usage/items/w1", "usage.item", "w1"],
    // What `pages/work/api.ts` sends is `encodeURIComponent`'d; the machine is
    // asked for the id, not for its spelling in a URL.
    ["/v1/usage/items/w.1_a-b", "usage.item", "w.1_a-b"],
  ]
  for (const [path, word, id] of cases) {
    assert.equal(writeRoute("GET", path)?.word, word, path)
    const res = await reader.fetch(path)
    assert.equal(res.status, 200, path)
    assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", word, { id }, "read"], path)
  }
  // Nothing else under /v1/usage is a word, and a write to one is not a read.
  assert.equal(writeRoute("GET", "/v1/usage/sessions"), null)
  assert.equal(writeRoute("GET", "/v1/usage/sessions/c1/more"), null)
  assert.equal(writeRoute("GET", "/v1/usage/other/c1"), null)
  assert.equal(writeRoute("POST", "/v1/usage/items/w1"), null)

  // A query this wire has no field for is refused by name rather than dropped.
  const queried = await reader.fetch("/v1/usage/items/w1?since=1")
  assert.equal(queried.status, 501)
  assert.equal((await json(queried)).error, "cloud_not_carried")

  // The route's own refusal crosses with its code, in the flat spelling the
  // bill's reader takes (`pages/work/api.ts`, `RefusalError`).
  client.fail._machineRequest = refusal("unknown_item", { status: 404, layer: "mac_route", message: "No Board item has this id." })
  const unknown = await reader.fetch("/v1/usage/items/w404")
  assert.equal(unknown.status, 404)
  assert.equal((await json(unknown)).error, "unknown_item")
})

test("the compaction comparison crosses as its machine word, with since and nothing else", async () => {
  const client = new FakeClient()
  const { reader } = seam(client)
  assert.equal(writeRoute("GET", "/v1/usage/compare-compaction")?.word, "usage.compare-compaction")
  const plain = await reader.fetch("/v1/usage/compare-compaction")
  assert.equal(plain.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "usage.compare-compaction", {}, "read"])
  const ranged = await reader.fetch("/v1/usage/compare-compaction?since=30d")
  assert.equal(ranged.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "usage.compare-compaction", { since: "30d" }, "read"])

  // Nothing under it is a word, and a write to it is not a read.
  assert.equal(writeRoute("GET", "/v1/usage/compare-compaction/more"), null)
  assert.equal(writeRoute("POST", "/v1/usage/compare-compaction"), null)

  // A field the word does not carry, or since twice, is refused by name.
  for (const path of ["/v1/usage/compare-compaction?limit=3", "/v1/usage/compare-compaction?since=1d&since=2d"]) {
    const res = await reader.fetch(path)
    assert.equal(res.status, 501, path)
    assert.equal((await json(res)).error, "cloud_not_carried", path)
  }

  // The route's own refusal of a since it cannot read crosses with its code.
  client.fail._machineRequest = refusal("bad_request", { status: 400, layer: "mac_route", message: "since: out of range." })
  const bad = await reader.fetch("/v1/usage/compare-compaction?since=0d")
  assert.equal(bad.status, 400)
  assert.equal((await json(bad)).error, "bad_request")
})

test("the work samples cross as their machine word, with since, until and nothing else", async () => {
  const client = new FakeClient()
  const { reader } = seam(client)
  assert.equal(writeRoute("GET", "/v1/usage/work-samples")?.word, "usage.work-samples")
  const plain = await reader.fetch("/v1/usage/work-samples")
  assert.equal(plain.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "usage.work-samples", {}, "read"])
  const ranged = await reader.fetch("/v1/usage/work-samples?since=1790000000&until=1791000000")
  assert.equal(ranged.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "usage.work-samples", { since: "1790000000", until: "1791000000" }, "read"])

  assert.equal(writeRoute("GET", "/v1/usage/work-samples/more"), null)
  assert.equal(writeRoute("POST", "/v1/usage/work-samples"), null)

  for (const path of ["/v1/usage/work-samples?limit=3", "/v1/usage/work-samples?until=1&until=2"]) {
    const res = await reader.fetch(path)
    assert.equal(res.status, 501, path)
    assert.equal((await json(res)).error, "cloud_not_carried", path)
  }
})

test("a verification crosses as its machine word, with the record's id and its body whole", async () => {
  const client = new FakeClient()
  const { reader } = seam(client)
  const id = "7e000000-0000-4000-8000-000000000006"
  const listed = await reader.fetch("/v1/verifications")
  assert.equal(listed.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "verification.list", {}, "read"])
  const opened = await reader.fetch("/v1/verifications/" + id)
  assert.equal(opened.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "verification.get", { id }, "read"])

  const writes: [string, string, Record<string, unknown>, string, Record<string, unknown>][] = [
    ["POST", "/v1/verifications", { title: "t", due_at: 2, criteria: ["c"] },
      "verification.create", { verification: { title: "t", due_at: 2, criteria: ["c"] } }],
    ["POST", `/v1/verifications/${id}/notes`, { text: "looked" },
      "verification.note", { id, verification: { text: "looked" } }],
    ["POST", `/v1/verifications/${id}/criteria/2`, { state: "passed" },
      "verification.criterion", { id, index: 2, verification: { state: "passed" } }],
    ["POST", `/v1/verifications/${id}/close`, { status: "rejected", reason: "cost more" },
      "verification.close", { id, verification: { status: "rejected", reason: "cost more" } }],
  ]
  for (const [method, path, sent, word, body] of writes) {
    const res = await reader.fetch(path, { ...post(sent), method })
    assert.equal(res.status, 200, path)
    assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", word, body, "action"], path)
  }
  const removed = await reader.fetch(`/v1/verifications/${id}`, { method: "DELETE" })
  assert.equal(removed.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "verification.delete", { id, force: false }, "action"])
  const forced = await reader.fetch(`/v1/verifications/${id}?force=1`, { method: "DELETE" })
  assert.equal(forced.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "verification.delete", { id, force: true }, "action"])
  // A press keeps its id through the machine.
  await reader.fetch(`/v1/verifications/${id}/notes`, post({ text: "once" }, { "Idempotency-Key": "press-note-1" }))
  assert.deepEqual(client.calls.pop(), ["_machineRequestAs", "press-note-1", "mac-a", "verification.note",
    { id, verification: { text: "once" } }, "action"])

  // Nothing else is a word: no bulk delete, no criterion past two digits, no
  // write the routes do not have.
  assert.equal(writeRoute("DELETE", "/v1/verifications"), null)
  assert.equal(writeRoute("POST", `/v1/verifications/${id}/criteria/01`), null)
  assert.equal(writeRoute("POST", `/v1/verifications/${id}/criteria/100`), null)
  assert.equal(writeRoute("POST", `/v1/verifications/${id}/reopen`), null)
  assert.equal(writeRoute("GET", `/v1/verifications/${id}/notes`), null)
  assert.equal(writeRoute("PATCH", `/v1/verifications/${id}`), null)

  // A query the word has no field for is refused by name, not dropped.
  for (const [path, method] of [["/v1/verifications?status=open", "GET"], [`/v1/verifications/${id}?force=yes`, "DELETE"],
    [`/v1/verifications/${id}?all=1`, "DELETE"]]) {
    const res = await reader.fetch(path, { method })
    assert.equal(res.status, 501, path)
    assert.equal((await json(res)).error, "cloud_not_carried", path)
  }

  // The route's own refusal crosses flat, with its code.
  client.fail._machineRequest = refusal("verification_open", { status: 409, layer: "mac_route", message: "Close it first, or force." })
  const open = await reader.fetch(`/v1/verifications/${id}`, { method: "DELETE" })
  assert.equal(open.status, 409)
  assert.equal((await json(open)).error, "verification_open")
})

test("the capacity block crosses as its machine word, with no field of its own", async () => {
  const client = new FakeClient()
  const { reader } = seam(client)
  assert.equal(writeRoute("GET", "/v1/capacity")?.word, "capacity")
  const read = await reader.fetch("/v1/capacity")
  assert.equal(read.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "capacity", {}, "read"])

  // Nothing under it is a word, and a write to it is not a read.
  assert.equal(writeRoute("GET", "/v1/capacity/artifacts.drops"), null)
  assert.equal(writeRoute("POST", "/v1/capacity"), null)

  // A query the route does not read is refused by name, not dropped.
  const extra = await reader.fetch("/v1/capacity?all=1")
  assert.equal(extra.status, 501)
  assert.equal((await json<{ error: { code: string } }>(extra)).error.code, "cloud_not_carried")

  // The route's own refusal crosses with its code.
  client.fail._machineRequest = refusal("bad_request", { status: 405, layer: "mac_route", message: "The capacity panel is read with GET." })
  const bad = await reader.fetch("/v1/capacity")
  assert.equal(bad.status, 405)
  assert.equal((await json<{ error: { code: string } }>(bad)).error.code, "bad_request")
})

test("a phone reads and changes only the two default models", async () => {
  const client = new FakeClient()
  const { reader } = seam(client)

  assert.deepEqual(writeRoute("GET", "/v1/settings/default-models"), {
    op: "default-models", word: "default-models",
  })
  const read = await reader.fetch("/v1/settings/default-models")
  assert.equal(read.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "default-models", {}, "read"])

  const write = await reader.fetch("/v1/settings/default-models", post({ codex_default_model: "gpt-6" }))
  assert.equal(write.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "default-models-update", {
    changes: { codex_default_model: "gpt-6" },
  }, "action"])

  for (const path of ["/v1/settings/default-models/extra", "/v1/settings/default-models?all=1"]) {
    const res = await reader.fetch(path)
    assert.equal(res.status, 501, path)
  }
  assert.equal(writeRoute("POST", "/v1/settings"), null)
})

test("a phone reads and changes only the two work gates", async () => {
  const client = new FakeClient()
  const { reader } = seam(client)

  assert.deepEqual(writeRoute("GET", "/v1/settings/work-gates"), {
    op: "work-gate-settings", word: "work-gate-settings",
  })
  const read = await reader.fetch("/v1/settings/work-gates")
  assert.equal(read.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "work-gate-settings", {}, "read"])

  const write = await reader.fetch("/v1/settings/work-gates", post({ planning_gate: true, verify_gate: true }))
  assert.equal(write.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "work-gate-settings-update", {
    changes: { planning_gate: true, verify_gate: true },
  }, "action"])

  for (const path of ["/v1/settings/work-gates/extra", "/v1/settings/work-gates?all=1"]) {
    const res = await reader.fetch(path)
    assert.equal(res.status, 501, path)
  }
  assert.equal(writeRoute("POST", "/v1/settings"), null)
})

test("a phone can save the Board AI consent under the command's own receipt", async () => {
  const client = new FakeClient()
  const { reader } = seam(client)
  const command = {
    operation: "set_ai_consent",
    provider: "codex",
    enabled: true,
    policy: "board-reading-v1",
    expectedRevision: 4,
    requestId: "board-setting-1",
  }

  const write = await reader.fetch("/v1/board", post(command))
  assert.equal(write.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequestAs", "board-setting-1", "mac-a", "board-command", {
    command,
  }, "action"])

  const missing = await reader.fetch("/v1/board", post({ ...command, requestId: "" }))
  assert.equal(missing.status, 400)
  assert.equal(client.calls.length, 0)
})

test("the sessions a reboot took away cross as the machine's three words, under the press's key", async () => {
  // Parsed before the session writes, which would take `restorable` for a
  // session id and `restore` for an action it does not have.
  assert.deepEqual(writeRoute("GET", "/v1/sessions/restorable"), { op: "restorable", word: "restorable-sessions" })
  assert.deepEqual(writeRoute("POST", "/v1/sessions/restorable/restore"), { op: "restore", word: "restore-sessions" })
  assert.deepEqual(writeRoute("POST", "/v1/sessions/restorable/dismiss"), { op: "restore", word: "dismiss-restorable" })
  assert.equal(writeRoute("POST", "/v1/sessions/restorable"), null)
  assert.equal(writeRoute("GET", "/v1/sessions/restorable/restore"), null)
  assert.equal(writeRoute("POST", "/v1/sessions/restorable/other"), null)
  // A session that happens to be called `restorable` still has a git read.
  assert.equal(writeRoute("GET", "/v1/sessions/restorable/git")?.word, "git")

  const client = new FakeClient()
  const { reader } = seam(client)
  const list = await reader.fetch("/v1/sessions/restorable")
  assert.equal(list.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "restorable-sessions", {}, "read"])

  const restored = await reader.fetch("/v1/sessions/restorable/restore",
    post({ conversations: ["c1", "c2"] }, { "Idempotency-Key": "press-restore-1" }))
  assert.equal(restored.status, 200)
  assert.deepEqual(client.calls.pop(),
    ["_machineRequestAs", "press-restore-1", "mac-a", "restore-sessions", { conversations: ["c1", "c2"] }, "action"])

  // "Skip all" names no list, and the word keeps that: absent, not empty.
  const all = await reader.fetch("/v1/sessions/restorable/dismiss", post({}, { "Idempotency-Key": "press-dismiss-1" }))
  assert.equal(all.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequestAs", "press-dismiss-1", "mac-a", "dismiss-restorable", {}, "action"])
  const some = await reader.fetch("/v1/sessions/restorable/dismiss",
    post({ conversations: ["c3"] }, { "Idempotency-Key": "press-dismiss-2" }))
  assert.equal(some.status, 200)
  assert.deepEqual(client.calls.pop(),
    ["_machineRequestAs", "press-dismiss-2", "mac-a", "dismiss-restorable", { conversations: ["c3"] }, "action"])

  // A command without the press's key is refused here, as the route refuses
  // it, and nothing is sealed.
  const keyless = await reader.fetch("/v1/sessions/restorable/restore", post({ conversations: ["c1"] }))
  assert.equal(keyless.status, 400)
  assert.equal((await json<{ error: string }>(keyless)).error, "bad_request")
  assert.equal(client.calls.length, 0)

  // A query the route does not read is refused by name, not dropped.
  const extra = await reader.fetch("/v1/sessions/restorable?all=1")
  assert.equal(extra.status, 501)

  // The route's refusal crosses flat, with its code.
  client.fail._machineRequestAs = failureFromMac({
    code: "restore_batch_too_large", layer: "mac_route", message: "At most 20 conversations.",
  }, 400, REF)
  const tooMany = await reader.fetch("/v1/sessions/restorable/restore",
    post({ conversations: ["c1"] }, { "Idempotency-Key": "press-restore-2" }))
  assert.equal(tooMany.status, 400)
  assert.equal((await json<{ error: string }>(tooMany)).error, "restore_batch_too_large")
})

test("an archive rides its Session's channel, and the archive's list and restore are the machine's", async () => {
  // Parsed before the session writes, which would take `archived` for a
  // session id and `restore` for an action it does not have.
  assert.deepEqual(writeRoute("POST", "/v1/sessions/s1/archive"), { op: "archive", word: "archive-session", session: "s1" })
  assert.deepEqual(writeRoute("GET", "/v1/sessions/archived"), { op: "archived", word: "archived-sessions" })
  assert.deepEqual(writeRoute("POST", "/v1/sessions/archived/restore"), { op: "restore-archived", word: "restore-archived" })
  assert.equal(writeRoute("POST", "/v1/sessions/archived"), null)
  assert.equal(writeRoute("GET", "/v1/sessions/archived/restore"), null)
  assert.equal(writeRoute("POST", "/v1/sessions/archived/other"), null)

  const client = new FakeClient()
  client.rows = [row("s1", { closeability: { version: "cv-archive" } })]
  const { reader } = seam(client)
  assert.deepEqual((await reader.snapshot()).sessions.map((s) => s.id), ["s1"])

  // The archive is the close's decision: its key is the command's request and
  // `force` is carried as the word spells it.
  const archived = await reader.fetch("/v1/sessions/s1/archive", post({ force: true, expected_closeability_version: "cv-stale" }, { "Idempotency-Key": "press-archive-1" }))
  assert.equal(archived.status, 200)
  assert.deepEqual(client.calls.pop(), [
    "_read", { machine: "mac-a", session: "s1" }, "archive-session",
    { request: "press-archive-1", force: true, expected_closeability_version: "cv-stale" }, "action:press-archive-1", undefined, undefined,
  ])
  assert.deepEqual((await reader.snapshot()).sessions, [], "an archive is a close: the row goes with it")

  // Without the press's key nothing is sealed.
  client.rows = [row("s2")]
  const keyless = await reader.fetch("/v1/sessions/s2/archive", post({}))
  assert.equal(keyless.status, 400)
  assert.equal(client.calls.filter((c) => c[0] === "_read").length, 0)

  const list = await reader.fetch("/v1/sessions/archived")
  assert.equal(list.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "archived-sessions", {}, "read"])
  const extra = await reader.fetch("/v1/sessions/archived?all=1")
  assert.equal(extra.status, 501)

  const restored = await reader.fetch("/v1/sessions/archived/restore",
    post({ conversations: ["c1"] }, { "Idempotency-Key": "press-restore-archived-1" }))
  assert.equal(restored.status, 200)
  assert.deepEqual(client.calls.pop(),
    ["_machineRequestAs", "press-restore-archived-1", "mac-a", "restore-archived", { conversations: ["c1"] }, "action"])
  const restoreKeyless = await reader.fetch("/v1/sessions/archived/restore", post({ conversations: ["c1"] }))
  assert.equal(restoreKeyless.status, 400)

  // The route's refusal crosses flat, with its code.
  client.fail._machineRequestAs = failureFromMac({
    code: "archive_batch_too_large", layer: "mac_route", message: "At most 20 conversations.",
  }, 400, REF)
  const tooMany = await reader.fetch("/v1/sessions/archived/restore",
    post({ conversations: ["c1"] }, { "Idempotency-Key": "press-restore-archived-2" }))
  assert.equal(tooMany.status, 400)
  assert.equal((await json<{ error: string }>(tooMany)).error, "archive_batch_too_large")
})

test("the machine dashboard crosses as its machine word, with no field of its own", async () => {
  const client = new FakeClient()
  const { reader } = seam(client)
  assert.equal(writeRoute("GET", "/v1/machine/usage")?.word, "machine-usage")
  const read = await reader.fetch("/v1/machine/usage")
  assert.equal(read.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "machine-usage", {}, "read"])

  assert.equal(writeRoute("GET", "/v1/machine"), null)
  assert.equal(writeRoute("GET", "/v1/machine/usage/1"), null)
  assert.equal(writeRoute("POST", "/v1/machine/usage"), null)

  const extra = await reader.fetch("/v1/machine/usage?pid=1")
  assert.equal(extra.status, 501)
  assert.equal((await json<{ error: { code: string } }>(extra)).error.code, "cloud_not_carried")

  // A machine without a reader says so with its own code.
  client.fail._machineRequest = refusal("machine_usage_unsupported", { status: 501, layer: "mac_route", message: "machine usage is read on Linux only" })
  const bad = await reader.fetch("/v1/machine/usage")
  assert.equal(bad.status, 501)
  assert.equal((await json<{ error: { code: string } }>(bad)).error.code, "machine_usage_unsupported")
})

test("the update read crosses as its machine word, with no field of its own", async () => {
  const client = new FakeClient()
  const { reader } = seam(client)
  assert.equal(writeRoute("GET", "/v1/update")?.word, "update")
  const read = await reader.fetch("/v1/update")
  assert.equal(read.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "update", {}, "read"])

  assert.equal(writeRoute("GET", "/v1/update/1"), null)
  assert.equal(writeRoute("POST", "/v1/update"), null)

  const extra = await reader.fetch("/v1/update?apply=1")
  assert.equal(extra.status, 501)
  assert.equal((await json<{ error: { code: string } }>(extra)).error.code, "cloud_not_carried")

  // A machine whose daemon predates the word says so with its own code.
  client.fail._machineRequest = refusal("unknown_command", { status: 501, layer: "mac_route", message: "update" })
  const old = await reader.fetch("/v1/update")
  assert.equal(old.status, 501)
  assert.equal((await json<{ error: { code: string } }>(old)).error.code, "unknown_command")
})

test("squad settings carry per-field patches and package reads never gain write authority", async () => {
  const client = new FakeClient()
  const { reader } = seam(client)
  const patch = { definition_id: "role-a", expected_version: 3, overrides: { handbook: { present: true, value: "" } } }
  const saved = await reader.fetch("/v1/squad/settings", {
    method: "PUT", headers: { "Content-Type": "application/json", "Idempotency-Key": "save-1" }, body: JSON.stringify(patch),
  })
  assert.equal(saved.status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequestAs", "save-1", "mac-a", "squad.settings.update", { changes: patch }, "action"])

  const archive = { archive_base64: "UEs=", scope_id: "global" }
  assert.equal((await reader.fetch("/v1/squad-packages/preview", post(archive))).status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "squad.packages.preview", { package: archive }, "read"])

  const publicExport = { private_scopes: [], confirm_private: false }
  assert.equal((await reader.fetch("/v1/squad-packages/export", post(publicExport))).status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequest", "mac-a", "squad.packages.export", { package: publicExport }, "read"])

  const privateExport = { private_scopes: ["global", "project-a"], confirm_private: true }
  assert.equal((await reader.fetch("/v1/squad-packages/export", post(privateExport, { "Idempotency-Key": "export-1" }))).status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequestAs", "export-1", "mac-a", "squad.packages.export.private", { package: privateExport }, "action"])

  const adopt = { ...archive, preview_token: "token", choices: { "role-a": "keep" } }
  assert.equal((await reader.fetch("/v1/squad-packages/adopt", post(adopt, { "Idempotency-Key": "adopt-1" }))).status, 200)
  assert.deepEqual(client.calls.pop(), ["_machineRequestAs", "adopt-1", "mac-a", "squad.packages.adopt", { package: adopt }, "action"])

  const sent = client.calls.length
  assert.equal((await reader.fetch("/v1/squad-packages/export", post({ private_scopes: ["global"], confirm_private: false }))).status, 400)
  assert.equal((await reader.fetch("/v1/squad-packages/adopt", post(adopt))).status, 400)
  assert.equal((await reader.fetch("/v1/squad/session-events", post({ skill_id: "x" }))).status, 501)
  assert.equal(client.calls.length, sent, "refused commands never reach the machine")
})

// Measured in a production browser: while the page's own relay socket was
// renewing, `GET /v1/places` answered 503 `offline` (layer `browser`) for
// minutes, because the writer carries that read and asked `connected()`, which
// throws the moment the attached client is not ready. The reader's own machine
// reads already waited for the client `keepConnected` attaches next; the reads
// the writer carries now wait the same bound, and its writes still do not.
type Pausable = FakeClient & { lifecycle?: (reason: string) => boolean | "hidden" }

function pausedSeam(coming: boolean | "hidden", waitMs: number) {
  const retired = new FakeClient() as Pausable
  retired.ready = false
  const demands: string[] = []
  retired.lifecycle = (reason) => {
    demands.push(reason)
    return coming
  }
  const reader = new RelayReader("mac-a", { now: () => 1_000, reconnectWaitMs: waitMs })
  const writer = new RelayWriter(reader.writeHost, { now: () => 1_000, requestID: () => "req-1" })
  reader.carryWrites({ route: writeRoute, answer: (route, method, url, init) => writer.answer(route, method, url, init) })
  reader.attach(retired)
  return { reader, retired, demands }
}

test("a carried read asked while the page's connection renews waits for the next client", async () => {
  const { reader, retired, demands } = pausedSeam(true, 5_000)
  const renewed = new FakeClient()
  const asked = reader.fetch("/v1/places")
  await new Promise((resolve) => setTimeout(resolve, 5))
  reader.attach(renewed)
  const res = await asked
  assert.equal(res.status, 200)
  assert.ok(Array.isArray((await json(res)).places))
  assert.deepEqual(demands, ["demand"], "the paused client is asked to reconnect")
  assert.deepEqual(retired.calls, [], "nothing is sent through the retired client")
  assert.deepEqual(renewed.calls, [["places", "mac-a"]])
})

test("a carried read nobody reconnects for is refused as reconnecting and retryable, not offline", async () => {
  const { reader, retired } = pausedSeam(true, 20)
  const started = Date.now()
  const res = await reader.fetch("/v1/places")
  assert.ok(Date.now() - started >= 15, "it waited the bound")
  assert.equal(res.status, 503)
  const body = await json<{ error: { code: string; message: string; layer: string; retryable: boolean; outcome: string } }>(res)
  assert.equal(body.error.code, "cloud_reconnecting")
  assert.equal(body.error.layer, "browser")
  assert.equal(body.error.retryable, true)
  assert.equal(body.error.outcome, "not_done")
  assert.match(body.error.message, /renewing/)
  assert.match(body.error.message, /machine was not asked/)
  assert.deepEqual(retired.calls, [])
})

test("a carried read is refused as reconnecting at once when no client is coming", async () => {
  const { reader, retired } = pausedSeam(false, 5_000)
  const started = Date.now()
  const res = await reader.fetch("/v1/places/p1/sessions/claude")
  assert.ok(Date.now() - started < 1_000, "it did not wait")
  assert.equal(res.status, 503)
  const body = await json<{ error: { code: string; retryable: boolean; layer: string } }>(res)
  assert.equal(body.error.code, "cloud_reconnecting")
  assert.equal(body.error.retryable, true)
  assert.equal(body.error.layer, "browser")
  assert.deepEqual(retired.calls, [])
})

test("the caller's signal ends a carried read's wait for the connection", async () => {
  const { reader, retired } = pausedSeam(true, 5_000)
  const controller = new AbortController()
  const started = Date.now()
  const asked = reader.fetch("/v1/places", { signal: controller.signal })
  setTimeout(() => controller.abort(), 5)
  await assert.rejects(asked, (error: Error) => error.name === "AbortError")
  assert.ok(Date.now() - started < 1_000, "it stopped waiting when asked to")
  reader.attach(new FakeClient())
  assert.deepEqual(retired.calls, [])
})

test("a write during the gap is refused at once and is never carried after the reconnect", async () => {
  const { reader, retired, demands } = pausedSeam(true, 5_000)
  const started = Date.now()
  const res = await reader.fetch("/v1/sessions/s1/send", post({ text: "hi" }))
  assert.ok(Date.now() - started < 1_000, "a write does not wait")
  assert.equal(res.status, 503)
  const body = await json(res)
  assert.equal(body.error, "offline")
  assert.equal(body.outcome, "not_done")
  assert.deepEqual(demands, [], "a write does not ask the line to come back")

  const renewed = new FakeClient()
  renewed.rows = [row("s1")]
  reader.attach(renewed)
  await new Promise((resolve) => setTimeout(resolve, 10))
  assert.deepEqual(retired.calls, [])
  assert.equal(renewed.calls.length, 0, "the refused send is not carried by the new client")

  const sent = await reader.fetch("/v1/sessions/s1/send", post({ text: "again" }))
  assert.equal(sent.status, 200, "a send with the line up is carried as before")
  assert.equal(renewed.calls.filter(([name]) => name === "send").length, 1, "exactly the second press, once")
})

test("every read the writer carries is one it waits for, and no write is", () => {
  const reads: [string, string][] = [
    ["/v1/work/v2/images/a1", "work-v2-image"],
    ["/v1/usage/compare-compaction", "usage-compare"],
    ["/v1/usage/compare-handoff", "usage-compare"],
    ["/v1/usage/work-samples", "usage-work-samples"],
    ["/v1/usage/sessions/s1", "usage"],
    ["/v1/capacity", "capacity"],
    ["/v1/settings/default-models", "default-models"],
    ["/v1/settings/work-gates", "work-gate-settings"],
    ["/v1/sessions/restorable", "restorable"],
    ["/v1/sessions/archived", "archived"],
    ["/v1/machine/usage", "machine-usage"],
    ["/v1/update", "update"],
    ["/v1/verifications", "verification-read"],
    ["/v1/verifications/v1", "verification-read"],
    ["/v1/places", "places"],
    ["/v1/places/p1/sessions/claude", "past"],
    ["/v1/artifacts/images/a1", "image"],
    ["/v1/sessions/s1/info", "info"],
    ["/v1/sessions/s1/git", "git"],
    ["/v1/sessions/s1/git/diff", "git-diff"],
    ["/v1/sessions/s1/screen", "screen"],
    ["/v1/sessions/s1/documents", "documents"],
    ["/v1/sessions/s1/documents/project/demo.md", "document"],
  ]
  for (const [path, op] of reads) {
    const route = writeRoute("GET", path)
    assert.equal(route?.op, op, path)
    assert.ok(CARRIED_READS.has(route!.op), `${op} waits for the connection`)
  }
  assert.deepEqual([...CARRIED_READS].sort(), [...new Set(reads.map(([, op]) => op))].sort(), "the list is exactly the GET routes")
  for (const [method, path] of [["POST", "/v1/sessions/s1/send"], ["POST", "/v1/places/p1/start"], ["POST", "/v1/verifications"]]) {
    assert.ok(!CARRIED_READS.has(writeRoute(method, path)!.op), `${method} ${path} never waits`)
  }
})
