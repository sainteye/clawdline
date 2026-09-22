import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node's strip-types runner.
import { conversationBecameKnown, factsMissConversation } from "./info-freshness.ts"

test("a provider conversation appearing makes the pre-rollout answer stale", () => {
  assert.equal(conversationBecameKnown("", "conversation-1"), true)
  assert.equal(conversationBecameKnown(undefined, "conversation-1"), true)
  assert.equal(conversationBecameKnown("conversation-1", "conversation-1"), false)
  assert.equal(conversationBecameKnown("", ""), false)
})

test("a held info answer must name the conversation the row now knows", () => {
  assert.equal(factsMissConversation("conversation-1", { session: {} }), true)
  assert.equal(factsMissConversation("conversation-1", { session: { sessionId: "conversation-2" } }), true)
  assert.equal(factsMissConversation("conversation-1", { session: { sessionId: "conversation-1" } }), false)
  assert.equal(factsMissConversation("conversation-1", null), false, "no answer is the ordinary first read")
  assert.equal(factsMissConversation("", { session: {} }), false, "an untouched Codex session has nothing to retry yet")
})
