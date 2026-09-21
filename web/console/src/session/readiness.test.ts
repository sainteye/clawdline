import test from "node:test"
import assert from "node:assert/strict"
import type { SessionRow } from "@clawdline/contract"
// @ts-expect-error -- a `.ts` path, for node; see order.test.ts.
import { conversationNotStarted } from "./readiness.ts"

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
