import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"
// @ts-expect-error -- Node strip-types reads the TypeScript source.
import { decodeReport, supportBody } from "./model.ts"

const fixture = () => JSON.parse(readFileSync(new URL("../../../../internal/adapters/doctor/testdata/report.json", import.meta.url), "utf8"))

test("the shared report retains real zero and keeps absent quota unknown", () => {
  const r = decodeReport(JSON.stringify(fixture()))
  assert.equal(r.providers[0]!.used_percent, 0)
  assert.equal(r.providers[1]!.used_percent, null)
  assert.equal(r.coverage.lost_unknown, false)
  assert.match(supportBody(r, "Quota vanished after signing in again."), /clawdline.doctor\/1/)
})

test("imports refuse private fields, arbitrary provider text, paths and oversized files", () => {
  for (const mutate of [
    (r: ReturnType<typeof fixture>) => { r.token = "private" },
    (r: ReturnType<typeof fixture>) => { r.providers[0].credential = "private" },
    (r: ReturnType<typeof fixture>) => { r.providers[0].problem = "Bearer private" },
    (r: ReturnType<typeof fixture>) => { r.version = "/private/location" },
    (r: ReturnType<typeof fixture>) => { r.providers[0].used_percent = -1 },
    (r: ReturnType<typeof fixture>) => { r.history = Array.from({ length: 25 }, () => r.history[0]) },
  ]) { const r = fixture(); mutate(r); assert.throws(() => decodeReport(JSON.stringify(r))) }
  assert.throws(() => decodeReport(" ".repeat(8193)))
  assert.throws(() => supportBody(decodeReport(JSON.stringify(fixture())), "x".repeat(501)))
})

test("a full retained history plus a Chinese description fits the private service's 12000-byte message", () => {
  const r = fixture()
  r.history = Array.from({ length: 24 }, (_, i) => ({ at: 1793200000 + i, provider: { ...r.providers[0], problem: "reauth_required", state: "failed" } }))
  const body = supportBody(decodeReport(JSON.stringify(r)), "額".repeat(500))
  assert.ok(new TextEncoder().encode(body).length < 12000)
})
