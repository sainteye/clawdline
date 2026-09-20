// The relay seam's writes: `node --test web/console/src/cloud/*.test.ts`.
//
// A fake CloudClient stands in for the copied one. Each method records what it
// was asked and answers the way the copied client does: the Mac's own body on
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
import { RelayWriter, writeRoute, type CloudWriteClient } from "./relay-writer.ts"
// The copied client's own failure constructor: what the Mac's refusal really becomes.
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
    return this.act("_read", [identity, type, extra, answer, timeoutMs, options], { ok: true, id: identity.session, action: "keyed" })
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
    ["POST", "/v1/sessions/s1/focus", "focus"],
    ["POST", "/v1/places/p1/start", "start"],
    ["POST", "/v1/places/p1/start/codex/gpt-5", "start"],
    ["POST", "/v1/places/p1/resume/abc", "resume"],
    ["POST", "/v1/places/p1/resume/claude/abc", "resume"],
    ["POST", "/v1/voice", "voice"],
    ["GET", "/v1/places", "places"],
    ["GET", "/v1/places/p1/sessions/claude", "past-sessions"],
    ["GET", "/v1/artifacts/images/img-1", "image"],
    ["POST", "/v1/sessions/s1/interrupt", "interrupt"],
    ["POST", "/v1/push/subscribe", "push-subscribe"],
    ["POST", "/v1/push/unsubscribe", "push-unsubscribe"],
    ["POST", "/v1/push/test", "push-test"],
    ["POST", "/v1/orchestrator/schedules", "schedule-create"],
    ["PATCH", "/v1/orchestrator/schedules/sch-1", "schedule-update"],
    ["DELETE", "/v1/orchestrator/schedules/sch-1", "schedule-delete"],
    ["POST", "/v1/orchestrator/schedules/sch-1/run", "schedule-run"],
    // The list is a read and is answered by `relay-reader.ts`, not here, and
    // so is one schedule in full — which this Mac has no route for at all.
    ["GET", "/v1/orchestrator/schedules", null],
    ["GET", "/v1/orchestrator/schedules/sch-1", null],
    ["POST", "/v1/orchestrator/schedules/sch-1", null],
    ["DELETE", "/v1/orchestrator/schedules", null],
    ["POST", "/v1/snippets", "snippet-create"],
    ["PATCH", "/v1/snippets/sn-1", "snippet-update"],
    ["DELETE", "/v1/snippets/sn-1", "snippet-delete"],
    ["POST", "/v1/snippets/order", "snippet-order"],
    // `order` is a name no snippet id may be, on this side as on the Mac's
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
    op: "resume", word: "resume", place: "p1", assistant: "claude", past: "abc",
  })
  assert.equal(writeRoute("POST", "/v1/sessions/s1/interrupt")?.op, "uncarried")
})

test("a send goes as the Mac's `send` under the row's own identity, and answers as the local route does", async () => {
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

test("a Mac's refusal comes back typed, in the flat spelling `ClawdlineClient` recognises", async () => {
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

test("a command the Mac may have run without answering says so, and nothing sent says that", async () => {
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

test("a waiting card's press is answered by the Mac itself, never by the relay's `delivered`", async () => {
  const client = new FakeClient()
  client.rows = [row("s1")]
  client.descriptors.set("mac-a", { machine: { commands: ["send", "answer", "key"] } })
  const { reader } = seam(client)
  // The Go daemon: lists `answer`, publishes no cloud_status. Asked with a
  // request id, settled by the Mac's own answer; with no key from the card,
  // the writer mints one.
  const res = await reader.fetch("/v1/sessions/s1/key", post({ key: "2", expect: FINGERPRINT }))
  assert.equal(res.status, 200)
  assert.deepEqual(client.calls.pop(), [
    "_read", { machine: "mac-a", session: "s1" }, "answer", { request: "req-1", answer: "2", expect: FINGERPRINT },
    "action:req-1", undefined, { retireUncertain: true },
  ])
  assert.ok(!client.calls.some((c) => c[0] === "answer"), "the copied `answer`, which settles on the relay, is never used")
})

test("a close carries force and the close gates last read", async () => {
  const client = new FakeClient()
  client.rows = [row("s1", { closeability: { version: "cv-7" } })]
  const { reader } = seam(client)
  await reader.fetch("/v1/sessions/s1/close", post({ force: true }))
  assert.deepEqual(client.calls.pop(), ["end", { machine: "mac-a", session: "s1" }, true, "cv-7"])
  // A blocked close's reasons: see the two F6 tests below, which go through
  // the copied client's own failure path rather than an error built by hand.
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

test("a Mac without `past-sessions` refuses the resume list by the client's own code", async () => {
  const client = new FakeClient()
  client.fail.pastSessions = refusal("cloud_feature_unavailable", { layer: "browser" })
  const { reader } = seam(client)
  const res = await reader.fetch("/v1/places/p1/sessions/claude")
  assert.equal(res.status, 501)
  const body = await json<{ error: { code: string; word: string } }>(res)
  assert.equal(body.error.code, "cloud_feature_unavailable")
  assert.equal(body.error.word, "past-sessions")
})

test("dictation picks the machine this page reads when the voice Mac is ambiguous, once", async () => {
  const client = new FakeClient()
  client.fail.voice = refusal("cloud_voice_host_ambiguous", { layer: "browser" })
  const { reader } = seam(client)
  const res = await reader.fetch("/v1/voice", post({ audio: "AAAA", rate: 16000 }))
  assert.equal(res.status, 200)
  assert.deepEqual(await json(res), { text: "hello there", ms: 900 })
  assert.deepEqual(client.calls.map((c) => c[0]), ["voice", "setVoiceHost", "voice"])
  assert.equal(client.calls[1][1], "mac-a")
})

test("a route with no Cloud word is refused by name before anything is sealed", async () => {
  const client = new FakeClient()
  const { reader } = seam(client)
  const res = await reader.fetch("/v1/sessions/s1/interrupt", post({}))
  assert.equal(res.status, 501)
  const body = await json(res)
  assert.equal(body.error, "cloud_not_carried")
  assert.equal(body.word, "interrupt")
  assert.deepEqual(client.calls, [])
})

test("a transcript's picture is read as bytes through the Mac's `image`", async () => {
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
  assert.equal(sends.length, 2, "each attempt is asked of the Mac and settled by its answer")
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

test("F1, F7: a press that cannot be checked against the Mac's screen is refused here, and nothing is sent", async () => {
  const cases: [string, (c: FakeClient) => void, Record<string, unknown>][] = [
    ["no question named", (c) => c.descriptors.set("mac-a", GO_DAEMON), { key: "2" }],
    ["a Mac whose words are not known yet", () => {}, { key: "2", expect: FINGERPRINT }],
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

test("F3: a write is `not_done` only when this page can prove it never reached the Mac", async () => {
  // [what failed, the outcome the page must be told]
  const cases: [string, Error, string | undefined][] = [
    ["never sealed: the copied client refused before publishing", refusal("cloud_read_only", { layer: "browser", ref: null }), "not_done"],
    ["the relay said the Mac is not connected", refusal("machine_offline", { layer: "relay", ref: REF }), "not_done"],
    ["the Mac refused before acting", refusal("cloud_commands_disabled", { layer: "mac_preflight", ref: REF }), "not_done"],
    ["the Mac's queue was full", refusal("cloud_ingress_busy", { layer: "mac_transport", ref: REF }), "not_done"],
    ["the socket dropped after the envelope was written", refusal("offline", { layer: "browser", ref: REF }), "unknown"],
    ["the token was replaced mid-flight", refusal("token_superseded", { layer: "relay", ref: REF }), "unknown"],
    ["the relay closed with an internal error", refusal("internal", { layer: "relay", ref: REF }), "unknown"],
    ["an error nobody named", refusal("unexpected_error", { layer: "browser", ref: REF }), "unknown"],
    ["the Mac ran it and the reply was lost", refusal("command_answer_undeliverable", { layer: "mac_reply", ref: REF }), "unknown"],
    ["nothing answered in time", refusal("cloud_read_timeout", { layer: "browser", ref: null }), "unknown"],
    // The route's own refusal is read by its code, as the same refusal from a
    // daemon on this machine is (`session/outcome.ts`).
    ["the Mac's route refused", refusal("terminal_io_failed", { layer: "mac_route", ref: REF }), undefined],
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

test("F12: showing a session on the Mac does not make every poll re-read its transcript", async () => {
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

// F6. The Mac's refusal reaches the writer through the copied client's own
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
  // stop at the copied client; the sheet reopens "blocked" with no list. The
  // fix belongs in that copied file's source (the Swift app's console) and is
  // named by the todo below.
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
// the word the Mac lists, with the browser's own subscription handed over
// whole: it is the browser's endpoint and the browser's keys, and anything
// reshaped on the way past is a chance to get a credential wrong.
test("registering for notifications goes as the Mac's own three words", async () => {
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
// schedule methods, which route each one to the Mac that owns it.
test("the schedule form's four writes reach the Mac as its own four words", async () => {
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
    // find the Mac, so nothing may be reshaped on the way past.
    ["createSchedule", form],
    ["updateSchedule", "sch 9", form],
    ["deleteSchedule", "sch-9"],
    ["runSchedule", "sch-9"],
  ])
  assert.deepEqual(reader.log.map((x: { word?: string; answer: string }) => [x.word, x.answer]), [
    ["schedule-create", "relay"], ["schedule-update", "relay"],
    ["schedule-delete", "relay"], ["schedule-run", "relay"],
  ])
})

// The schedule routes refuse through `writeAuthRefusal` and
// `writeBrokerRefusal` (internal/transport/http/schedules.go), both of which
// send `{"error":{"code","message",…}}` — and the form reads a broker
// refusal's extra fields out of that same object.
test("a refused schedule write comes back in the spelling its own route answers", async () => {
  const client = new FakeClient()
  client.fail.runSchedule = failureFromMac(
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
  for (const name of ["createSchedule", "updateSchedule", "deleteSchedule", "runSchedule"]) older[name] = undefined
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
  assert.deepEqual(client.calls, [], "nothing was asked of the Mac")
})

// 常用句 from a phone: the sheet's own five requests, unchanged
// (`session/snippets-api.ts` spells them against a daemon on this machine's
// own network), reaching the copied client's four snippet methods — each of
// which names the Mac whose settings change through the session the sheet was
// opened on.
test("the snippet sheet's four writes reach the Mac as its own four words", async () => {
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
  assert.deepEqual(client.calls, [], "nothing was asked of the Mac")
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
  assert.deepEqual(client.calls, [], "nothing was asked of the Mac")
})
