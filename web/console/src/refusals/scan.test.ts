// The guard that keeps a named refusal named:
//   node --test web/console/src/refusals/scan.test.ts
//
// The point of this file is the first test. A guard nobody has driven red is
// not a guard, so `GitPanel.tsx` as it read before `gitSentence()` existed is
// kept here as the known positive and has to come back red every time.
import { test } from "node:test"
import assert from "node:assert/strict"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { KNOWN_NEGATIVE, KNOWN_POSITIVE, scanConsole, scanSource, selfCheck } from "./scan.ts"

const repo = resolve(dirname(fileURLToPath(import.meta.url)), "../../../..")

test("the shape this guard is for still comes back red", () => {
  const found = scanSource("session/GitPanel.tsx", KNOWN_POSITIVE).sites
  const site = found.find((s: { kind: string }) => s.kind === "fixed_catch_all")
  assert.ok(site, "GitPanel.tsx before gitSentence() must be a finding")
  assert.deepEqual(site.codes, ["not_a_repo"])
  assert.match(site.terminal, /webGitFailed/)
})

test("the same panel once the refusal reached the formatter is clean", () => {
  assert.deepEqual(scanSource("session/GitPanel.tsx", KNOWN_NEGATIVE).sites, [])
})

test("a ladder that ends in \"\" is finished by its caller, not swallowed", () => {
  const source = `
    function ownWhy(e: { code?: string }): string {
      if (e.code === "write_disabled") return T().webStartOff
      if (e.code === "not_found") return T().webStartGone
      return ""
    }`
  assert.deepEqual(scanSource("a.ts", source).sites, [])
})

test("a ladder that ends in a sentence of its own is a finding", () => {
  const source = `
    function why(e: { code?: string }): string {
      if (e.code === "write_disabled") return T().webStartOff
      if (e.code === "not_found") return T().webStartGone
      return T().webRequestFailed
    }`
  const found = scanSource("a.ts", source).sites
  assert.equal(found.length, 1)
  assert.equal(found[0].kind, "fixed_catch_all")
  assert.deepEqual(found[0].codes, ["write_disabled", "not_found"])
})

test("a handler that says words without looking at the refusal is a finding", () => {
  const found = scanSource("a.ts", `read().catch(() => setSaid(T.webRequestFailed))`).sites
  assert.equal(found.length, 1)
  assert.equal(found[0].kind, "refusal_dropped")
})

test("a settled read whose reason nothing ever asks for is a finding", () => {
  const source = `
    async function load() {
      const [rows, digest] = await Promise.allSettled([readRows(), readDigest()])
      setRows(rows.status === "fulfilled" ? rows.value : null)
      setDigest(digest.status === "fulfilled" ? digest.value : null)
      if (rows.status === "rejected") setSaid(failureSentence(rows.reason, T.webRequestFailed))
    }`
  const found = scanSource("a.ts", source).sites
  assert.deepEqual(
    found.map((s: { kind: string; terminal: string }) => s.terminal),
    ["digest.reason"],
  )
  assert.equal(found[0].kind, "reason_dropped")
})

test("`refusal-ok:` takes a site off the list and keeps its reason", () => {
  const source = `
    function why(e: { code?: string }): string {
      if (e.code === "write_disabled") return T().webStartOff
      // refusal-ok: the only other value this field takes is the one below
      return T().webRequestFailed
    }`
  const found = scanSource("a.ts", source).sites
  assert.equal(found.length, 1)
  assert.equal(found[0].allowed, "the only other value this field takes is the one below")
})

test("a one-word `refusal-ok` is not a reason", () => {
  const source = `
    function why(e: { code?: string }): string {
      if (e.code === "write_disabled") return T().webStartOff
      return T().webRequestFailed // refusal-ok: no
    }`
  assert.equal(scanSource("a.ts", source).sites[0].allowed, null)
})

test("the console has no refusal it says nothing about", () => {
  const report = scanConsole(repo)
  assert.equal(selfCheck(), null, "the guard must still catch its own fixture")
  assert.equal(report.indeterminate, null, "a scan that cannot tell is a failure, not a pass")
  assert.ok(report.files >= 100, `only ${report.files} files were read`)
  assert.ok(report.chains >= 20, `only ${report.chains} refusal ladders were found`)
  const said = report.violations
    .map((s: { file: string; line: number; kind: string; terminal: string }) =>
      `${s.file}:${s.line} ${s.kind} → ${s.terminal}`,
    )
    .join("\n")
  assert.equal(report.violations.length, 0, "a named refusal is drawn as one fixed sentence here:\n" + said)
})
