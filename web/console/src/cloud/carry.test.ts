// What this console carries, what it says when it does not, and the words it
// says it in: `node --test web/console/src/cloud/*.test.ts`.
//
// The other half of these rules is a Go test that reads `carry.ts` against this
// machine's own catalog (internal/app/cloudops/carry_test.go). This half is
// what the page does with the table: that the routes and the table cannot
// disagree, that a refusal names the word, that `info` reaches the Mac and
// comes back as the status line's own read, and that the document's language
// is the catalog this build ships.
import { test } from "node:test"
import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import type { SessionInfo } from "@clawdline/contract"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { ANSWERED_HERE, CARRIED, CARRY_TABLE, DEFERRED, NO_MAC_ROUTE, notCarriedDetail, uncarried, uncarriedWordOf, words } from "./carry.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { RelayReader, type CloudIdentity, type CloudRow, type CloudSessions } from "./relay-reader.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { RelayWriter, writeRoute, type CloudWriteClient } from "./relay-writer.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { BUILTIN_TAG, DEFAULT_TAG, catalogTag, catalogURL } from "./strings.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { contextCell } from "../session/context.ts"

const here = dirname(fileURLToPath(import.meta.url))
const console_ = resolve(here, "../..")

/** The `info` answer this daemon writes (`internal/transport/http/usage.go`). */
const INFO = {
  info: {
    session: { id: "s1", title: "clawdline-go", assistant: "claude", model: "claude-opus-5", cwd: "/tmp" },
    models: [],
    usage: { input: 1, output: 2, cacheRead: 3, cacheWrite: 4, total: 10, costUsd: 1.25 },
    context: { usedPercent: 62.4, usedTokens: 124_800, windowTokens: 200_000 },
  },
}

class FakeMac implements CloudWriteClient {
  ready = true
  allowWrites = true
  sessionInventoryByMachine = new Map<string, unknown>([["mac-a", {}]])
  asked: string[] = []
  commands: string[] | null = null
  rows: CloudRow[] = [{ id: "s1", machine: "mac-a", session: "s1", identity: { machine: "mac-a", session: "s1" } }]
  events() {
    return () => undefined
  }
  sessions(): Promise<CloudSessions> {
    return Promise.resolve({ sessions: this.rows, at: 1, scan: {} })
  }
  transcript() {
    return Promise.resolve({ id: "s1", entries: [], signature: "1", evidence: "transcript" })
  }
  tasks() {
    return Promise.resolve({ tasks: [] })
  }
  machineDescriptor(machine: string) {
    return this.commands && machine === "mac-a" ? { machine: { commands: this.commands } } : null
  }
  info(identity: CloudIdentity) {
    this.asked.push("info:" + identity.session)
    return Promise.resolve(INFO)
  }
  infoSummary(identity: CloudIdentity) {
    this.asked.push("infoSummary:" + identity.session)
    return Promise.resolve(INFO)
  }
  send() {
    return Promise.resolve({})
  }
  answer() {
    return Promise.resolve({})
  }
  end() {
    return Promise.resolve({})
  }
  focus() {
    return Promise.resolve({})
  }
  places() {
    return Promise.resolve({})
  }
  pastSessions() {
    return Promise.resolve({})
  }
  startPlace() {
    return Promise.resolve({})
  }
  resumePlace() {
    return Promise.resolve({})
  }
  voice() {
    return Promise.resolve({})
  }
}

function seam(mac: FakeMac) {
  const reader = new RelayReader("mac-a", { now: () => 1_000, carry: CARRY_TABLE })
  const writer = new RelayWriter(reader.writeHost, { now: () => 1_000 })
  reader.carryWrites({ route: writeRoute, answer: (route, method, url, init) => writer.answer(route, method, url, init) })
  reader.attach(mac)
  return reader
}

test("one word, one list, and every route names a word the table carries", () => {
  const seen = new Set<string>()
  for (const word of words()) {
    assert.equal(seen.has(word), false, word + " is in more than one list")
    seen.add(word)
  }
  // Every word a route can produce is a carried one. The types say so at
  // compile time (`Carried<K>`); this says so of the parser's actual output,
  // which is what reaches the Mac.
  const routes: [string, string][] = [
    ["POST", "/v1/sessions/s1/send"],
    ["POST", "/v1/sessions/s1/key"],
    ["POST", "/v1/sessions/s1/close"],
    ["POST", "/v1/sessions/s1/focus"],
    ["GET", "/v1/sessions/s1/info"],
    ["POST", "/v1/places/p1/start"],
    ["POST", "/v1/places/p1/resume/claude/abc"],
    ["POST", "/v1/voice"],
    ["GET", "/v1/places"],
    ["GET", "/v1/places/p1/sessions/claude"],
    ["GET", "/v1/artifacts/images/img-1"],
    ["POST", "/v1/push/subscribe"],
    ["POST", "/v1/push/unsubscribe"],
    ["POST", "/v1/push/test"],
    ["POST", "/v1/orchestrator/schedules"],
    ["PATCH", "/v1/orchestrator/schedules/sch-1"],
    ["DELETE", "/v1/orchestrator/schedules/sch-1"],
    ["POST", "/v1/orchestrator/schedules/sch-1/run"],
  ]
  for (const [method, path] of routes) {
    const word = writeRoute(method, path)?.word
    assert.ok(word, method + " " + path + " parses to no route")
    assert.ok(word! in CARRIED, method + " " + path + " asks for " + word + ", which CARRIED does not list")
  }
  // `/v1/transcript` and the schedule list are carried by the reader rather
  // than by a route, and they are in the table for the same reason the rest
  // are: the guard reads the table.
  assert.ok("transcript" in CARRIED)
  assert.ok("schedules" in CARRIED)
  // 33, not the 21 this was last measured at. Twelve reads moved in at once:
  // the five words of the work system, this daemon's Project catalog, the two
  // Project worktree reads, the verification ledger, the Project timeline and
  // the Swift board's two. Before them a phone drew none of it — the whole
  // work system was `cloud_not_carried`, which is what a person opening
  // "Projects" on a phone met. The count is re-measured rather than carried
  // over — a number copied across a change is the one nobody checks.
  assert.equal(Object.keys(CARRIED).length, 33)
})

test("every route the table says is answered here is answered here, with no word behind it", async () => {
  // `ANSWERED_HERE` used to be read by nothing, which is the same shape as the
  // defect this table was built for: a hand-written list of GET paths and no
  // way for it to notice it had stopped being true. The list is a claim about
  // the reader, so it is made of the reader.
  const mac = new FakeMac()
  const reader = seam(mac)
  for (const path of ANSWERED_HERE) {
    const res = await reader.fetch(path)
    assert.equal(res.status, 200, path + " is in ANSWERED_HERE and the seam does not answer it")
    const row = reader.log[reader.log.length - 1]
    assert.equal(row.path, path)
    assert.equal(row.answer, "local", path + " is answered here, so nothing was asked of the Mac for it")
    assert.equal(row.word, undefined, path + " is in ANSWERED_HERE and names a Cloud word")
    assert.equal(uncarriedWordOf("GET", path), "", path + " stands for a word, so it is not answered here")
  }
  // And a route in neither list is still refused, so the list is what a page
  // can reach and not merely some of it. `/v1/board` stood here until this
  // console began asking for it; `/v1/devstacks` is no Cloud word at all.
  assert.equal((await reader.fetch("/v1/devstacks")).status, 501)
})

test("a route this console does not carry is refused by the word it stands for", async () => {
  const mac = new FakeMac()
  const reader = seam(mac)
  // The one the person met: 常用句, a word this Mac has no route for at all.
  const res = await reader.fetch("/v1/snippets?session=s1")
  assert.equal(res.status, 501)
  const body = (await res.json()) as { error: string; detail: string }
  assert.equal(body.error, "cloud_not_carried", "the code the screens choose their sentence by")
  assert.equal(body.detail, NO_MAC_ROUTE.snippets)
  assert.match(body.detail, /on the Mac/, "a named refusal says what can be done instead")
  // A word this Mac answers and this console has not carried yet says its own
  // sentence too, not the same one.
  assert.equal(uncarriedWordOf("GET", "/v1/sessions/s1/git"), "git")
  assert.equal(notCarriedDetail("GET", "/v1/sessions/s1/git"), DEFERRED.git)
  // A word this console now carries stands for nothing here, because what is
  // carried is parsed once by the reader's own case: the work board and a
  // Project's timeline were both in this function and are not any more.
  assert.equal(uncarriedWordOf("GET", "/v1/work/board"), "", "the work board is carried")
  assert.equal(uncarriedWordOf("GET", "/v1/timeline?project=p"), "", "a Project's timeline is carried")
  assert.equal(uncarried("work.board"), "", "a carried word has no refusal sentence")
  // One schedule in full is the one schedule word this Mac has no route for
  // (`op{name: "schedule"}` in cloudops/ops.go carries no `route`), so the
  // sheets behind a schedule row say that and not "not read yet", while the
  // list beside it is carried and says nothing at all.
  assert.equal(uncarriedWordOf("GET", "/v1/orchestrator/schedules/sch-1"), "schedule")
  assert.equal(notCarriedDetail("GET", "/v1/orchestrator/schedules/sch-1"), NO_MAC_ROUTE.schedule)
  assert.equal(uncarriedWordOf("GET", "/v1/orchestrator/schedules"), "", "the list is carried, so it stands for nothing here")
  assert.equal(uncarried("schedules"), "", "a carried word has no refusal sentence")
  // And a route that is no Cloud word at all still says where to go.
  assert.match(notCarriedDetail("GET", "/v1/devstacks"), /is not carried over Clawdline Cloud/)
  assert.equal(uncarried("info"), "", "a carried word has no refusal sentence")
})

test("the status line's read reaches the Mac, and its context cell draws", async () => {
  const mac = new FakeMac()
  const reader = seam(mac)

  const summary = await reader.fetch("/v1/sessions/s1/info?parts=summary")
  assert.equal(summary.status, 200)
  const full = await reader.fetch("/v1/sessions/s1/info")
  assert.equal(full.status, 200)
  assert.deepEqual(mac.asked, ["infoSummary:s1", "info:s1"], "each half is asked as its own read")

  // What `overlays/facts.ts` takes out of the answer, and what `StatusLine`
  // draws from it: before this the page never asked, so the cell stayed at
  // "Loading…" for as long as the session was open.
  const info = ((await full.json()) as { info?: SessionInfo }).info
  assert.ok(info)
  const cell = contextCell(info!.context, "tokens")
  assert.deepEqual(cell, { percent: 62, level: "warn", title: "ctx 62% (124,800 / 200,000 tokens)" })
  assert.equal(info!.usage?.costUsd, 1.25)

  const row = reader.log[reader.log.length - 1]
  assert.equal(row.word, "info")
  assert.equal(row.answer, "relay")
})

test("the seam says what this Mac can do that this bundle never asks for", async () => {
  const mac = new FakeMac()
  const reader = seam(mac)
  assert.equal(reader.drift(), null, "no descriptor is not agreement")
  mac.commands = [...Object.keys(CARRIED), "snippets", "git"]
  assert.deepEqual(reader.drift(), { notCarried: ["git", "snippets"], notOnThisMac: [] })
  mac.commands = Object.keys(CARRIED).filter((word) => word !== "info")
  assert.deepEqual(reader.drift(), { notCarried: [], notOnThisMac: ["info"] })

  // And it reaches this page's own log, once, the first time the list is read.
  mac.commands = [...Object.keys(CARRIED), "snippets"]
  const fresh = seam(mac)
  await fresh.fetch("/v1/sessions")
  await fresh.fetch("/v1/sessions")
  const said = fresh.log.filter((row) => row.code === "cloud_vocabulary_drift")
  assert.equal(said.length, 1, "said once, not on every reading")
  assert.equal(said[0].word, "snippets")
})

test("the words are this build's own catalog, and the document says which", async () => {
  // One file, shipped in the bundle and served by the daemon from the same
  // place (`public/strings/`), so there is nothing for it to drift from.
  const shipped = JSON.parse(readFileSync(resolve(console_, "public/strings", DEFAULT_TAG + ".json"), "utf8")) as Record<string, string>
  assert.equal(shipped.lang, DEFAULT_TAG, "the shipped catalog names the language it is in")
  assert.equal(shipped.dir, "ltr")

  // The document ships the tag the default catalog is in, so the page is not
  // in one language before the words land and another after.
  const document = readFileSync(resolve(console_, "index.html"), "utf8")
  assert.match(document, new RegExp(`<html lang="${DEFAULT_TAG}"`), "index.html does not ship " + DEFAULT_TAG)
  assert.notEqual(DEFAULT_TAG, BUILTIN_TAG)

  // A declaration with no aliases is the one on the hosted build, and it used
  // to mean no catalog at all rather than the default one.
  assert.equal(catalogTag({ build: "b1", strings: {} }, ["en-US"]), DEFAULT_TAG)
  assert.equal(catalogTag({ build: "b1", strings: {} }, []), DEFAULT_TAG)
  // A declared alias still decides, longest first.
  const declared = { build: "b1", strings: { zh: "zh-Hans", "zh-hant": "zh-Hant", ja: "ja" } }
  assert.equal(catalogTag(declared, ["zh-Hant-TW", "en"]), "zh-Hant")
  assert.equal(catalogTag(declared, ["zh-CN"]), "zh-Hans")
  assert.equal(catalogTag(declared, ["de"]), DEFAULT_TAG)

  // Where it is read from: the build's immutable directory when the
  // declaration names one, the document's own otherwise.
  assert.equal(catalogURL(declared, "zh-Hant", "https://app.example/x/"), "/app/b1/strings/zh-Hant.json")
  assert.equal(
    catalogURL({ build: "", strings: {} }, "zh-Hant", "https://app.example/x/"),
    "https://app.example/x/strings/zh-Hant.json",
  )
})
