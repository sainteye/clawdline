import test from "node:test"
import assert from "node:assert/strict"
import type { SessionRow } from "@clawdline/contract"
// @ts-expect-error -- a `.ts` path, for node; see order.test.ts.
import { conversationNotStarted } from "./readiness.ts"
import { sessionWorkStateHTML, projectSessionWorkState } from "../legacy/js/view/derive.js"

function row(extra: Partial<SessionRow>): SessionRow {
  return {
    id: "%0",
    backend: "tmux",
    state: "unknown",
    work_state: "unknown",
    evidence: "process",
    isClaude: false,
    closeability: {} as SessionRow["closeability"],
    ...extra,
  }
}

test("a native capacity block needs attention independently of terminal activity", () => {
  for (const state of ["idle", "unknown", "working"] as const) {
    const blocked = row({ state, provider_block: "model_capacity", work_state: "waiting_you", work_person_needed: true })
    assert.equal(projectSessionWorkState(blocked).state, "waiting_you")
    assert.equal(blocked.menu, undefined)
  }
  assert.equal(projectSessionWorkState(row({ state: "working", work_state: "working" })).state, "working")
})

test("a provider with no first record is not an unreadable screen", () => {
  assert.equal(conversationNotStarted(row({ identity: "no_record" })), true, "fresh Codex identity")
  assert.equal(
    conversationNotStarted(row({ isClaude: true, activity: { known: false, unknown_reason: "no_record" } })),
    true,
    "fresh Claude activity",
  )
  assert.equal(
    conversationNotStarted(row({ activity: { known: false, unknown_reason: "unreadable" } })),
    false,
    "a real read failure stays a failure",
  )
})

test("an ended turn is distinct from an idle new session and an unreadable session", () => {
  const finished = sessionWorkStateHTML(row({ state: "idle", activity: { known: true, at: 1 } }))
  assert.match(finished, /turn ended · delivery not reported/)
  assert.doesNotMatch(finished, /session-work-check/)

  const fresh = sessionWorkStateHTML(row({ state: "idle", activity: { known: false, unknown_reason: "no_record" } }))
  assert.match(fresh, /idle · delivery not reported/)
  assert.doesNotMatch(fresh, /turn ended/)

  const unreadable = sessionWorkStateHTML(row({ state: "unknown", activity: { known: true, at: 1 } }))
  assert.match(unreadable, /status unknown/)
  assert.doesNotMatch(unreadable, /turn ended/)
})
