// The whole write path, both ends, in one run:
//
//   node --test web/console/src/cloud/write-path.e2e.ts
//
// The page's own code — the card (`session/pending.ts`, `session/sender.ts`),
// the press (`session/press-holds.ts`), the question's name
// (`session/fingerprint.ts`), the relay seam (`relay-reader.ts`,
// `relay-writer.ts`) — is driven against the real Mac half: this repository's
// `cloudops` bridge, its own HTTP routes, its receipts and its key presser,
// served on loopback by `TestServeTheWritePathForAPage`
// (internal/transport/http/cloud_write_path_serve_test.go), which this file
// starts with `go test`.
//
// Only the relay and the sealing are missing. In their place `GoClient` hands
// the bridge exactly the plaintext the copied `CloudClient` would have sealed
// (`_publishCommand`: `{type, session, …extra}`) and turns the Answer back
// into what the copied client resolves or rejects with — its own
// `failureFromMac`. What is being proved is the two things the review found:
// a digit lands only on the question it was chosen for (F1), and a card whose
// answer was lost is typed once however often it is sent (F2).
//
// Named `.e2e.ts` so the unit run over `cloud/*.test.ts` does not start Go.
import { test, before, after } from "node:test"
import assert from "node:assert/strict"
import { spawn, type ChildProcess } from "node:child_process"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { RelayReader, type CloudIdentity, type CloudRow } from "./relay-reader.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { RelayWriter, writeRoute, type CloudWriteClient } from "./relay-writer.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { menuFingerprint } from "../session/fingerprint.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { outcomeOf } from "../session/outcome.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { PendingSends } from "../session/pending.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { PressHolds, postPress } from "../session/press-holds.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { Sender, postCard, readTranscript } from "../session/sender.ts"
// The copied client's own failure constructors: a machine refusal reaches the
// writer through these and nowhere else.
import { cloudFailure, failureFromMac } from "../legacy/js/net/cloud-failure.js"

// The tree whose Mac half is driven. `CLAWDLINE_WRITE_PATH_REPO` points it at
// another checkout — which is how this file was watched to fail against the
// tree before the fix, with the page's own code unchanged.
const repo = process.env.CLAWDLINE_WRITE_PATH_REPO || resolve(dirname(fileURLToPath(import.meta.url)), "../../../..")
const address = "127.0.0.1:" + (18900 + (process.pid % 90))
const mac = "http://" + address

/** The machine, as the page's client sees it: plaintext in, one Answer out. */
class GoClient implements CloudWriteClient {
  ready = true
  allowWrites = true
  macCapabilities = new Set<string>()
  sessionInventoryByMachine = new Map<string, unknown>([["mac-a", { ids: new Set(["%4"]) }]])
  /** Commands the machine was really asked for, in order. */
  asked: { type: string; body: Record<string, unknown> }[] = []
  /** The next command runs on the machine and its answer never comes back. */
  loseNext = false
  private seq = 0

  events() {
    return () => {}
  }
  machineDescriptor() {
    return { machine: { commands: ["send", "answer", "key", "end", "focus"] } }
  }
  async sessions() {
    const row = (await (await fetch(mac + "/row")).json()) as { id: string; state: string; menu?: unknown }
    const one: CloudRow = {
      id: row.id,
      machine: "mac-a",
      session: row.id,
      identity: { machine: "mac-a", session: row.id },
      state: row.state,
      ...(row.menu ? { menu: row.menu } : {}),
    } as CloudRow
    return { sessions: [one], at: Math.floor(Date.now() / 1000), scan: { emptyAuthoritative: true, recovering: [], failures: [] } }
  }
  async transcript() {
    return (await (await fetch(mac + "/transcript"))).json()
  }
  async _read(identity: CloudIdentity, type: string, extra: Record<string, unknown>): Promise<unknown> {
    const body = { type, session: identity.session, ...extra }
    this.asked.push({ type, body })
    this.seq += 1
    const seq = this.seq
    const ref = { sender: "web_e2e0001", seq, request: typeof extra.request === "string" ? extra.request : null }
    const answered = (await (
      await fetch(mac + "/command", { method: "POST", body: JSON.stringify({ seq, body }) })
    ).json()) as { status: number; code: string; payload?: { body?: unknown; error?: Record<string, unknown> } }
    if (this.loseNext) {
      // The machine ran it; the answer did not come back. This is what the copied
      // client raises when its read times out (`_readTimedOut`).
      this.loseNext = false
      throw cloudFailure("cloud_read_timeout", "the machine did not answer this read", { layer: "browser", ref })
    }
    const failure = answered.payload?.error
    if (failure) throw failureFromMac(failure, answered.status, ref)
    return answered.payload?.body ?? {}
  }
  // Not used on this path: the page asks through `_read` so that the machine's own
  // answer settles every write.
  send() {
    return Promise.reject(new Error("the page must ask through _read"))
  }
  answer() {
    return Promise.reject(new Error("the page must ask through _read"))
  }
  end() {
    return Promise.reject(new Error("the page must ask through _read"))
  }
  focus() {
    return Promise.reject(new Error("not asked here"))
  }
  // The status line's read, spelled as the copied client spells it: two names
  // for two answers on one word (`cloud-client.js`). It goes to the same Mac
  // the writes do, so `info reaches the machine's own route` below is this
  // daemon's bridge answering, not a fake.
  info(identity: CloudIdentity) {
    return this._read(identity, "info", { parts: "full" })
  }
  infoSummary(identity: CloudIdentity) {
    return this._read(identity, "info", { parts: "summary" })
  }
  // The Git panel's read, spelled as the copied client spells it. It reaches
  // this daemon's own `/v1/sessions/{id}/git` through the bridge, as `info`
  // above does.
  git(identity: CloudIdentity) {
    return this._read(identity, "git", {})
  }
  // The live screen's read, spelled as the copied client spells it: the
  // session and nothing else, which is all `cloudops`' `screen` decodes.
  screen(identity: CloudIdentity) {
    return this._read(identity, "screen", {})
  }
  places() {
    return Promise.reject(new Error("not asked here"))
  }
  pastSessions() {
    return Promise.reject(new Error("not asked here"))
  }
  startPlace() {
    return Promise.reject(new Error("not asked here"))
  }
  resumePlace() {
    return Promise.reject(new Error("not asked here"))
  }
  voice() {
    return Promise.reject(new Error("not asked here"))
  }
}

let daemon: ChildProcess
let client: GoClient
let seam: RelayReader
/** The page's `fetch`, as `cloud/install.ts` installs it. */
let doFetch: typeof fetch

const url = (path: string) => path
const say = async (path: string, body?: unknown) =>
  (await fetch(mac + path, { method: "POST", body: JSON.stringify(body ?? {}) })).json()
const acts = async (): Promise<string[]> => (await (await fetch(mac + "/acts")).json()) as string[]
const rowMenu = async () => (await client.sessions()).sessions[0].menu as Parameters<typeof menuFingerprint>[0]

before(async () => {
  daemon = spawn("go", ["test", "./internal/transport/http", "-run", "TestServeTheWritePathForAPage", "-count=1"], {
    cwd: repo,
    env: { ...process.env, CLAWDLINE_WRITE_PATH_ADDR: address },
    stdio: ["ignore", "inherit", "inherit"],
  })
  const deadline = Date.now() + 120_000
  for (;;) {
    try {
      await fetch(mac + "/acts")
      break
    } catch {
      if (Date.now() > deadline) throw new Error("the machine half never came up on " + address)
      await new Promise((r) => setTimeout(r, 250))
    }
  }
  client = new GoClient()
  seam = new RelayReader("mac-a")
  const writer = new RelayWriter(seam.writeHost)
  seam.carryWrites({ route: writeRoute, answer: (route, method, u, init) => writer.answer(route, method, u, init) })
  seam.attach(client)
  doFetch = ((input, init) => seam.fetch(input, init)) as typeof fetch
})

after(async () => {
  try {
    await say("/stop")
  } catch {
    /* it is going away anyway */
  }
  daemon?.kill()
})

// The read the status line under every session is drawn from, on the wire.
//
// What is worth proving here is not that the seam calls a method — `carry.test.ts`
// does that against a fixture, and draws the cell out of the answer — but that
// the word the page now sends is one this machine admits, decodes and routes.
// Before this the page never sent it, so `cloudops`' `info` was reachable only
// from a Swift console: the machine answered nothing because nothing asked, and the
// hosted status line said "Loading…" with no refusal recorded at either end.
//
// The machine half here is a pane server with a small router
// (`TestServeTheWritePathForAPage`), not this daemon's own `/info` handler, so
// what comes back is that router's word about the route. That is exactly the
// line this asserts: past admission, past the decoder, refused — or answered —
// by the machine's own route and by nothing before it. `cloudops_test.go` pins what
// the route itself is.
test("the status line's read reaches this machine's own route, by both its names", async () => {
  const before = client.asked.length
  const summary = await doFetch(url("/v1/sessions/%254/info?parts=summary"))
  const full = await doFetch(url("/v1/sessions/%254/info"))

  const asked = client.asked.slice(before)
  assert.deepEqual(
    asked.map((one) => one.body),
    [
      { type: "info", session: "%4", parts: "summary" },
      { type: "info", session: "%4", parts: "full" },
    ],
    "two reads, one word: a full answer settled by a summary would be held as complete",
  )

  for (const [half, res] of [["summary", summary], ["full", full]] as const) {
    if (res.status === 200) {
      const body = (await res.json()) as { info?: unknown }
      assert.equal(typeof body.info, "object", half + ": the answer is this daemon's `{info: …}`")
      continue
    }
    const refusal = (await res.json()) as { error?: string; layer?: string; word?: string }
    // The three that would mean it never got that far: a word this machine does
    // not know, a body it could not read, and a machine the copied client
    // refused to ask at all.
    assert.ok(
      !["unknown_command", "malformed_command", "cloud_feature_unavailable", "cloud_machine_unsupported"].includes(
        refusal.error ?? "",
      ),
      half + ": the machine did not admit `info`: " + JSON.stringify(refusal),
    )
    assert.equal(refusal.layer, "mac_route", half + ": " + JSON.stringify(refusal))
    assert.equal(refusal.word, "info")
  }
})

// The live screen, on the wire: the page's own route becomes a `screen` read
// this machine admits and decodes, and whatever comes back is the machine's
// route answering — not the page refusing itself, which is what 「即時畫面」
// over Clawdline Cloud said until 2026-09-26.
test("the live screen's read reaches this machine's own route", async () => {
  const before = client.asked.length
  const res = await doFetch(url("/v1/sessions/%254/screen"))
  assert.deepEqual(client.asked.slice(before).map((one) => one.body), [{ type: "screen", session: "%4" }])
  if (res.status === 200) {
    const body = (await res.json()) as { screen?: unknown }
    assert.equal(typeof body.screen, "object", "the answer is this daemon's `{screen: …}`")
    return
  }
  const refusal = (await res.json()) as { error?: string; layer?: string; word?: string }
  assert.ok(
    !["cloud_not_carried", "unknown_command", "malformed_command", "cloud_feature_unavailable", "cloud_machine_unsupported"].includes(
      refusal.error ?? "",
    ),
    "the machine did not admit `screen`: " + JSON.stringify(refusal),
  )
  assert.equal(refusal.layer, "mac_route", JSON.stringify(refusal))
  assert.equal(refusal.word, "screen")
})

// F1, both ends: the page reads one permission prompt, the machine moves on to the
// next tool call's prompt before the press lands, and nothing is typed. The
// same press at the question it names is typed and committed.
test("a press is typed only at the question the page named", async () => {
  await say("/prompt", { command: "rm -rf build" })
  const expect = menuFingerprint(await rowMenu())
  const holds = new PressHolds()

  // The machine is asking about something else by the time the press arrives.
  await say("/prompt", { command: "rm -rf /" })
  const press = holds.start("%4", "menu-1", Date.now())
  const moved = await postPress(doFetch, "%4", "1", expect, press.request)
  assert.equal(moved.ok, false)
  assert.equal(moved.ok === false && moved.code, "menu_moved")
  assert.equal(moved.ok === false && outcomeOf({ status: moved.status, code: moved.code, said: moved.outcome }), "not_done")
  assert.deepEqual(await acts(), [], "a digit was typed at a question nobody read")

  // The question it names, back on the screen: typed, and committed.
  await say("/prompt", { command: "rm -rf build" })
  const pressed = await postPress(doFetch, "%4", "1", expect, holds.start("%4", "menu-1", Date.now()).request)
  assert.equal(pressed.ok, true)
  assert.deepEqual(await acts(), ["key:1", "key:\r"])
})

// F1(a) with F3: the press landed and its answer did not come back. The card
// does not open its options — it says it does not know — and when the person
// chooses again, the machine refuses the second press because its screen has moved
// on. The digit is typed once.
test("a press whose answer was lost is not quietly pressed again", async () => {
  await say("/prompt", { command: "npm test" })
  const expect = menuFingerprint(await rowMenu())
  const holds = new PressHolds()
  const press = holds.start("%4", "menu-2", Date.now())
  const before = (await acts()).length

  client.loseNext = true
  const lost = await postPress(doFetch, "%4", "1", expect, press.request)
  assert.equal(lost.ok, false)
  const outcome = lost.ok === false ? outcomeOf({ status: lost.status, code: lost.code, said: lost.outcome }) : "not_done"
  assert.equal(outcome, "unknown", "an answer that never came back is not proof the press did not land")
  holds.failed(press, outcome)
  assert.equal(holds.current("%4", "menu-2", true, Date.now())?.state, "unknown", "the options were opened again")
  const typed = (await acts()).slice(before)
  assert.deepEqual(typed, ["key:1", "key:\r"], "the machine did press it")

  // The person chooses again, from a card still drawing the old question.
  holds.release("%4")
  const again = holds.start("%4", "menu-2", Date.now())
  const second = await postPress(doFetch, "%4", "1", expect, again.request)
  assert.equal(second.ok === false && second.code, "menu_moved", "the second press was typed at the next question")
  assert.equal((await acts()).length, before + 2, "nothing more was typed")
})

// F2, both ends: a card's words reach the machine and the answer is lost. The card
// says it does not know; looking reads a transcript that has not caught up;
// sending again goes to the machine as the same request, and the machine answers it
// with the first attempt's answer. The words are typed once.
test("a card whose answer was lost is typed once, however often it is sent", async () => {
  await say("/prompt", { command: "" })
  await say("/lag", { on: true })
  const cards = new PendingSends()
  const sender = new Sender({
    cards,
    now: () => Date.now(),
    post: (card) => postCard(doFetch, url, card),
    readBack: (session) => readTranscript(doFetch, url, session),
    outcomeOf,
  })
  const before = (await acts()).filter((a) => a.startsWith("send:")).length

  client.loseNext = true
  const card = cards.add("%4", "delete the build directory", [], Date.now())
  await sender.deliver(card)
  assert.equal(cards.card(card.token)?.state, "unknown", "a lost answer is not a failure")

  await sender.look(card.token)
  assert.equal(cards.card(card.token)?.absent, true, "the transcript has not caught up, and says so")

  await sender.resend(card.token)
  assert.equal(cards.card(card.token)?.state, "accepted", "the machine answered the second attempt with the first one's answer")
  const sends = (await acts()).filter((a) => a.startsWith("send:")).length
  assert.equal(sends, before + 1, "the words were typed twice")
  assert.equal(client.asked.filter((c) => c.type === "send").length, 2, "both attempts really reached the machine")
  assert.equal(
    new Set(client.asked.filter((c) => c.type === "send").map((c) => c.body.request)).size,
    1,
    "both attempts are one request",
  )

  // The transcript catches up, and the next poll — what `Transcript.tsx`
  // does with every read — takes the card away, as it does any other.
  await say("/lag", { on: false })
  const turns = await readTranscript(doFetch, url, "%4")
  assert.ok(turns, "the transcript is readable again")
  cards.reconcile("%4", turns!, Date.now())
  assert.equal(cards.card(card.token), undefined, "the turn is in the conversation: the card has done its job")
})
