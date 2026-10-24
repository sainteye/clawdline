import test from "node:test"
import assert from "node:assert/strict"
import type { SessionRow } from "@clawdline/contract"
// @ts-expect-error -- Node's strip-types runner reads the source extension.
import { matchesLocalStateFilter, stateFilterCounts } from "./state-filter.ts"
const row = (extra: Partial<SessionRow> = {}): SessionRow => ({ state: "idle", ...extra } as SessionRow)

test("local attention includes replies, person-needed work and unresolved notes", () => {
  for (const extra of [{ state: "waiting" }, { work_person_needed: true }, { attention_count: 1 }] as Partial<SessionRow>[])
    assert.equal(matchesLocalStateFilter(row(extra), "attention"), true)
  assert.equal(matchesLocalStateFilter(row({ state: "working", attention_count: 0 }), "attention"), false)
})

test("working and idle distinguish shells and avoid retained state claims", () => {
  const shell = row({ shells: [{ id: "build", at: 1 }] })
  assert.equal(matchesLocalStateFilter(shell, "working"), true)
  assert.equal(matchesLocalStateFilter(shell, "idle"), false)
  for (const state of ["working", "idle"] as const) {
    const retained = row({ state, source: { freshness: "unverified" } as SessionRow["source"] })
    assert.equal(matchesLocalStateFilter(retained, state), false)
    assert.equal(matchesLocalStateFilter(retained, "all"), true)
  }
})

test("counts use the same predicates; All includes unknown rows and categories may overlap", () => {
  const rows = [row({ state: "working", attention_count: 1 }), row(), row({ state: "unknown" })]
  assert.deepEqual(stateFilterCounts(rows, matchesLocalStateFilter), { all: 3, attention: 1, working: 1, idle: 1 })
  assert.equal(rows.filter((item) => matchesLocalStateFilter(item, "all")).length, 3)
})
