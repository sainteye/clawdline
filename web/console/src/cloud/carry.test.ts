// What this console carries, what it says when it does not, and the words it
// says it in: `node --test web/console/src/cloud/*.test.ts`.
//
// The other half of these rules is a Go test that reads `carry.ts` against this
// machine's own catalog (internal/app/cloudops/carry_test.go). This half is
// what the page does with the table: that the routes and the table cannot
// disagree, that a refusal names the word, that `info` reaches the machine and
// comes back as the status line's own read, and that the document's language
// is the catalog this build ships.
import { test } from "node:test"
import assert from "node:assert/strict"
import { readFileSync, readdirSync } from "node:fs"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import type { SessionInfo } from "@clawdline/contract"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { ANSWERED_HERE, CARRIED, CARRY_TABLE, DEFERRED, DEFERRED_ASKED, NO_MACHINE_ROUTE, notCarriedDetail, uncarried, uncarriedWordOf, words } from "./carry.ts"
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
const core_ = resolve(console_, "../core")

/** The `info` answer this daemon writes (`internal/transport/http/usage.go`). */
const INFO = {
  info: {
    session: { id: "s1", title: "clawdline-go", assistant: "claude", model: "claude-opus-5", cwd: "/tmp" },
    models: [],
    usage: { input: 1, output: 2, cacheRead: 3, cacheWrite: 4, total: 10, costUsd: 1.25 },
    context: { usedPercent: 62.4, usedTokens: 124_800, windowTokens: 200_000 },
  },
}

/** The `git` answer this daemon writes (`internal/transport/http/git.go`). */
const GIT = { git: { branch: "main", head: "46203ec", ahead: 46, behind: 0, clean: false, files: [{ path: "a.ts", kind: "modified", staged: false, unstaged: true }] } }

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
  machines() {
    return Promise.resolve({
      machines: [{ id: "mac-a", freshness: "current" as const }],
      syncing: false,
      retryAfterMs: 0,
    })
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
  git(identity: CloudIdentity) {
    this.asked.push("git:" + identity.session)
    return Promise.resolve(GIT)
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
  snippets(identity: CloudIdentity) {
    this.asked.push("snippets:" + identity.machine + "/" + identity.session)
    return Promise.resolve({
      snippets: [
        { id: "sn-1", machine: "mac-a", scope: "global", title: "a title", body: "a body" },
        { id: "sn-2", machine: "mac-b", scope: "global", title: "elsewhere", body: "another machine's" },
      ],
      at: 9,
    })
  }
  createSnippet() {
    return Promise.resolve({})
  }
  updateSnippet() {
    return Promise.resolve({})
  }
  deleteSnippet() {
    return Promise.resolve({})
  }
  orderSnippets() {
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
  // which is what reaches the machine.
  const routes: [string, string][] = [
    ["POST", "/v1/sessions/s1/send"],
    ["POST", "/v1/sessions/s1/key"],
    ["POST", "/v1/sessions/s1/close"],
    ["POST", "/v1/sessions/s1/focus"],
    ["GET", "/v1/sessions/s1/info"],
    ["GET", "/v1/sessions/s1/git"],
    ["GET", "/v1/sessions/s1/git/diff"],
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
    ["POST", "/v1/snippets"],
    ["PATCH", "/v1/snippets/sn-1"],
    ["DELETE", "/v1/snippets/sn-1"],
    ["POST", "/v1/snippets/order"],
  ]
  for (const [method, path] of routes) {
    const word = writeRoute(method, path)?.word
    assert.ok(word, method + " " + path + " parses to no route")
    assert.ok(word! in CARRIED, method + " " + path + " asks for " + word + ", which CARRIED does not list")
  }
  // `/v1/transcript` and the two lists are carried by the reader rather than
  // by a route, and they are in the table for the same reason the rest are:
  // the guard reads the table.
  assert.ok("transcript" in CARRIED)
  assert.ok("schedules" in CARRIED)
  assert.ok("snippets" in CARRIED)
  assert.ok("timeline" in CARRIED)
  // 44, counted on this tree — including the single-schedule read, the
  // versioned webhook-binding write and Git's per-file diff. Keep the count beside the catalog so
  // a merge that adds a word cannot quietly leave this assertion behind.
  assert.ok("agent" in CARRIED)
  assert.equal(Object.keys(CARRIED).length, 44)
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
    assert.equal(row.answer, "local", path + " is answered here, so nothing was asked of the machine for it")
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
  // 常用句 used to be here — a word this machine knew and had no route for, so
  // the page refused the read to itself. It is carried now, and the shape it
  // left behind is the one this asserts on a word that still is not: a
  // session's skills.
  const res = await reader.fetch("/v1/sessions/s1/skills")
  assert.equal(res.status, 501)
  const body = (await res.json()) as { error: string; detail: string }
  assert.equal(body.error, "cloud_not_carried", "the code the screens choose their sentence by")
  assert.equal(body.detail, NO_MACHINE_ROUTE.skills)
  assert.match(body.detail, /on the machine/, "a named refusal says what can be done instead")
  assert.equal(uncarriedWordOf("GET", "/v1/snippets"), "", "the snippet list is carried, so it stands for nothing here")
  assert.equal(uncarried("snippets"), "", "a carried word has no refusal sentence")
  // A word this machine answers and this console has not carried yet says its own
  // sentence too, not the same one. This used to be `git`, whose sentence had
  // been false for months; what is left in `DEFERRED` with a console route
  // behind it is the terminal's own picture.
  assert.equal(uncarriedWordOf("GET", "/v1/sessions/s1/screen"), "screen")
  assert.equal(notCarriedDetail("GET", "/v1/sessions/s1/screen"), DEFERRED.screen)
  assert.equal(uncarriedWordOf("GET", "/v1/sessions/s1/git"), "", "the Git panel's read is carried")
  assert.equal(uncarried("git"), "", "a carried word has no refusal sentence")
  // A word this console now carries stands for nothing here, because what is
  // carried is parsed once by the reader's own case: the work board and a
  // Project's timeline were both in this function and are not any more.
  assert.equal(uncarriedWordOf("GET", "/v1/work/board"), "", "the work board is carried")
  assert.equal(uncarriedWordOf("GET", "/v1/timeline?project=p"), "", "a Project's timeline is carried")
  assert.equal(uncarried("work.board"), "", "a carried word has no refusal sentence")
  // Both a schedule in full and the schedule list are carried. The detail
  // sheet therefore reaches the selected machine instead of manufacturing a
  // Cloud refusal before it can show webhook state.
  assert.equal(uncarriedWordOf("GET", "/v1/orchestrator/schedules/sch-1"), "")
  assert.equal(uncarried("schedule"), "", "a carried single-schedule read has no refusal sentence")
  assert.equal(uncarriedWordOf("GET", "/v1/orchestrator/schedules"), "", "the list is carried, so it stands for nothing here")
  assert.equal(uncarried("schedules"), "", "a carried word has no refusal sentence")
  // And a route that is no Cloud word at all still says where to go.
  assert.match(notCarriedDetail("GET", "/v1/devstacks"), /is not carried over Clawdline Cloud/)
  assert.equal(uncarried("info"), "", "a carried word has no refusal sentence")
})

/**
 * Every `/v1/…` path this console bundle spells, with the pieces it builds one
 * out of joined back together. That includes the shared core client where the
 * session route helpers live.
 *
 * A console route is rarely one literal: `"/v1/sessions/" + encodeURIComponent(id) + "/git"`
 * is three. So each line's double-quoted fragments are joined in the order
 * they appear with `X` where an expression stood, which turns that line into
 * `/v1/sessions/X/git` — a path `uncarriedWordOf` can be asked about. It is a
 * text scan and it is meant to be: it reads what a page would send, not what
 * a module exports, so a route reached through a helper is still found as
 * long as the helper spells the path.
 */
function consolePaths(): { path: string; where: string }[] {
  const out: { path: string; where: string }[] = []
  const walk = (dir: string, root: string, label: string) => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const full = resolve(dir, entry.name)
      if (entry.isDirectory()) {
        // The seam is where a refusal is decided, not where a screen asks, and
        // its own tests spell every route on purpose.
        if (entry.name !== "cloud" && entry.name !== "node_modules") walk(full, root, label)
        continue
      }
      if (!/\.(ts|tsx|js)$/.test(entry.name) || /\.test\.[tj]sx?$/.test(entry.name)) continue
      const source = readFileSync(full, "utf8")
      source.split("\n").forEach((line, index) => {
        if (!line.includes('"/v1/')) return
        const fragments = [...line.matchAll(/"([^"\\]*)"/g)].map((m) => m[1])
        const start = fragments.findIndex((f) => f.startsWith("/v1/"))
        if (start < 0) return
        const lastQuote = line.lastIndexOf('"')
        const terminalExpression = /\+\s*[^,;]+[,;]?\s*$/.test(line.slice(lastQuote + 1)) ? "X" : ""
        const joined = fragments.slice(start).join("X") + terminalExpression
        out.push({ path: joined, where: label + "/" + full.slice(root.length + 1) + ":" + (index + 1) })
      })
    }
  }
  walk(resolve(console_, "src"), console_, "console")
  walk(resolve(core_, "src"), core_, "core")
  return out
}

test("a deferred word a screen in this console already asks for says so", () => {
  // **The guard `git` did not have.** `DEFERRED` says these are words "this
  // console does not ask for yet", and for `git` that had been false since
  // `legacy/git-bridge.ts` was copied in: 「Git 變更」 asked on every press,
  // the seam refused its own page, and nothing anywhere compared the sentence
  // with the tree it was written about. Classification was checked; the claim
  // was not.
  //
  // So the claim is checked, in both directions, against the paths this
  // console actually spells. Deferring stays legal — this is not a red light
  // for a word being in `DEFERRED` — but a deferral that is costing somebody a
  // screen has to be named in `DEFERRED_ASKED`, which makes it the roster a
  // person re-reads instead of a sentence nobody revisits.
  const asked = new Map<string, string>()
  for (const { path, where } of consolePaths()) {
    const word = uncarriedWordOf("GET", path)
    if (word && word in DEFERRED && !asked.has(word)) asked.set(word, where)
  }
  const listed = new Set<string>(DEFERRED_ASKED)
  for (const [word, where] of asked) {
    assert.ok(
      listed.has(word),
      word + " is in DEFERRED and " + where + " asks for it: the refusal is this console's own. " +
        "Carry the word, or name it in DEFERRED_ASKED so the deferral is visible.",
    )
  }
  for (const word of listed) {
    assert.ok(word in DEFERRED, word + " is in DEFERRED_ASKED and not in DEFERRED")
    assert.ok(asked.has(word), "DEFERRED_ASKED names " + word + " and no screen in this console asks for it any more")
  }
  // And the scan itself has to be able to see a route, or every assertion
  // above passes by finding nothing. The terminal's picture is the one this
  // round leaves deferred with a screen in front of it.
  assert.ok(asked.has("screen"), "the path scan found no console route for `screen`; it has stopped reading this tree")
})

test("the Git panel's read reaches the machine, and its refusals keep their names", async () => {
  const mac = new FakeMac()
  const reader = seam(mac)

  const res = await reader.fetch("/v1/sessions/s1/git")
  assert.equal(res.status, 200)
  assert.deepEqual(mac.asked, ["git:s1"], "asked of the machine, under this page's identity for the row")
  const body = (await res.json()) as { git?: { ahead?: number; files?: unknown[] } }
  assert.equal(body.git?.ahead, 46)
  assert.equal(body.git?.files?.length, 1)
  const row = reader.log[reader.log.length - 1]
  assert.equal(row.word, "git")
  assert.equal(row.answer, "relay")

  // A refusal from the machine's own route crosses as its code, because that is
  // what the panel branches on (`legacy/git-bridge.ts`, `gitSentence`).
  mac.git = () => Promise.reject(Object.assign(new Error("not a repository"), { code: "not_a_repo", status: 404 }))
  const refused = await reader.fetch("/v1/sessions/s1/git")
  assert.equal(refused.status, 404)
  assert.equal(((await refused.json()) as { error: string }).error, "not_a_repo")

  // And a session this page holds no row for is refused rather than addressed
  // by the raw id (F5), as every other session read is.
  const gone = await reader.fetch("/v1/sessions/s9/git")
  assert.equal(gone.status, 404)
  assert.equal(((await gone.json()) as { error: string }).error, "session_not_found")
})

test("the status line's read reaches the machine, and its context cell draws", async () => {
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

test("the seam says what this machine can do that this bundle never asks for", async () => {
  const mac = new FakeMac()
  const reader = seam(mac)
  assert.equal(reader.drift(), null, "no descriptor is not agreement")
  // Two words this machine knows and this bundle does not ask for, one from each
  // uncarried list: `screen` is DEFERRED and `shell` is NO_MACHINE_ROUTE. Every
  // pair this test has used before — `board`, `snippets`, and now `git` —
  // became a carried word, which is exactly the drift this assertion is about.
  mac.commands = [...Object.keys(CARRIED), "shell", "screen"]
  assert.deepEqual(reader.drift(), { notCarried: ["screen", "shell"], notOnThisMachine: [] })
  mac.commands = Object.keys(CARRIED).filter((word) => word !== "info")
  assert.deepEqual(reader.drift(), { notCarried: [], notOnThisMachine: ["info"] })

  // And it reaches this page's own log, once, the first time the list is read.
  mac.commands = [...Object.keys(CARRIED), "shell"]
  const fresh = seam(mac)
  await fresh.fetch("/v1/sessions")
  await fresh.fetch("/v1/sessions")
  const said = fresh.log.filter((row) => row.code === "cloud_vocabulary_drift")
  assert.equal(said.length, 1, "said once, not on every reading")
  assert.equal(said[0].word, "shell")
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
