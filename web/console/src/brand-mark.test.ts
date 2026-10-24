import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- Node's type-stripping runner resolves the source .ts file.
import { BRAND_MARK, detailSessionName, projectedSessionIdentity } from "./brand-mark.ts"
import type { SessionRow } from "@clawdline/contract"

test("a pane keeps a readable title until its conversation name arrives", () => {
  const row = { id: "%" + 677, label: "%" + 677, assistant: "codex", identity: "no_record" } as SessionRow
  assert.equal(detailSessionName(row), "Codex Session")
  assert.equal(detailSessionName({ ...row, label: "A named task" }), "A named task")
  assert.equal(detailSessionName({ ...row, identity: "registry" }), "Codex Session")
  assert.equal(detailSessionName({ ...row, machine_scope: true }), "Clawdfather")
  assert.equal(detailSessionName({ ...row, assistant: "antigravity" }), "Gemini Session")
})

test("Cloud Session cards keep the machine identity through registration", () => {
  const provider = { accent: "#55aa77", cells: [["#55aa77"]] }
  const pending = projectedSessionIdentity("Codex · machine-workspace", provider, true, false)
  assert.equal(pending.title, "Clawdfather")
  assert.equal(pending.icon, BRAND_MARK)
  assert.equal(pending.crown, true)
  assert.equal(pending.pending, true)
  const bound = projectedSessionIdentity("A renamed conversation", provider, true, true)
  assert.equal(bound.title, "Clawdfather")
  assert.equal(bound.icon, BRAND_MARK)
  assert.equal(bound.crown, true)
  assert.equal(bound.pending, false)
  const ordinary = projectedSessionIdentity("Work", provider, false, false)
  assert.equal(ordinary.title, "Work")
  assert.equal(ordinary.icon, provider)
  assert.equal(ordinary.crown, false)
})
