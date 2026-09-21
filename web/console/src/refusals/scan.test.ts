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
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { AUDIT_EXPECTATIONS, scanAudits, selfCheckAudits, sourceRisks } from "./audit.ts"

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

test("an ignored refusal that turns into placeholders is red, and the formatted refusal is green", () => {
  const original = `read().catch(() => { setRows([]); setFailed(true) })`
  const fixed = `read().catch((error) => setSaid(failureSentence(error, T.failed)))`
  assert.deepEqual(scanSource("panel.tsx", original).sites.map((site) => site.kind), ["uncertainty_dropped"])
  assert.deepEqual(scanSource("panel.tsx", fixed).sites, [])
})

test("producer English used as the sentence is red, and catalogued failure text is green", () => {
  assert.deepEqual(sourceRisks("panel.tsx", `<p>{error.detail}</p>`), ["producer_prose"])
  assert.deepEqual(sourceRisks("panel.tsx", `<p>{failureSentence(error, T.failed)}</p>`), [])
})

test("a Go response that calls an unperformed read complete is red", () => {
  assert.deepEqual(sourceRisks("query.go", `return map[string]any{"status": "complete", "read": 0}`), ["literal_complete"])
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

test("the generic pass sees the currently measured handler shapes", () => {
  const report = scanConsole(repo)
  assert.equal(selfCheck(), null, "the guard must still catch its own fixture")
  assert.equal(report.indeterminate, null, "a scan that cannot tell is a failure, not a pass")
  assert.ok(report.files >= 100, `only ${report.files} files were read`)
  assert.ok(report.chains >= 20, `only ${report.chains} refusal ladders were found`)
  assert.deepEqual(report.violations.map((s) => `${s.file}:${s.line} ${s.kind}`), [])
})

test("a tree that cannot be read is indeterminate, never clean", () => {
  const report = scanConsole(resolve(repo, "does-not-exist"))
  assert.match(report.indeterminate ?? "", /cannot read/)
})

test("the cross-language audit sees every measured issue and separates byte-locked copies", () => {
  const report = scanAudits(repo)
  assert.equal(selfCheckAudits(), null, "the cross-language guard must catch all of its own fixtures")
  assert.equal(report.indeterminate, null, "an incomplete audit is neither clean nor a finding")
  assert.equal(report.rules, AUDIT_EXPECTATIONS.rules)
  assert.equal(report.open.length, AUDIT_EXPECTATIONS.open)
  assert.equal(report.locked.length, AUDIT_EXPECTATIONS.locked)
  assert.deepEqual(
    report.open.map((item) => item.id),
    ["E02", "E05", "E06", "E07", "E08", "E09", "E10", "E13", "E14"],
  )
  assert.deepEqual(report.locked.map((item) => item.id), ["L01", "L02"])
})
