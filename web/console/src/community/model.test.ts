import test from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node runs TypeScript source directly.
import { communityTitle, communityEntry, communityRoute, publicConversation, topicClosed, type PublicMessage } from "./model.ts"
test("Issues and Ideas have independent routes and new discussions keep their selected type", () => {
  assert.equal(communityRoute("/community/issues").requestType, "issue")
  assert.equal(communityRoute("/community/ideas").requestType, "feature_request")
  assert.equal(communityRoute("/community/new?request_type=question").requestType, "question")
  assert.equal(communityRoute("/community/new?request_type=admin").requestType, "issue")
  assert.equal(communityRoute("/community/mine").page, "mine")
})
test("a reply cannot stand in for a shipped Issue or Idea", () => {
  for (const request_type of ["issue", "feature_request"] as const) {
    assert.equal(topicClosed({ request_type, state: "answered", progress: { stage: "in_progress" } }), false)
    assert.equal(topicClosed({ request_type, state: "resolved", progress: { stage: "ready_for_release" } }), false)
    assert.equal(topicClosed({ request_type, state: "answered", progress: { stage: "released" } }), true)
  }
  assert.equal(topicClosed({ request_type: "question", state: "answered", progress: null }), false)
  assert.equal(topicClosed({ request_type: "question", state: "resolved", progress: null }), true)
})
test("the newest community reply includes members and preserves original context", () => {
  const make = (id:string,kind:PublicMessage["kind"]):PublicMessage => ({ id,kind,author:{display_name:null,avatar_url:null},body:id,at:"2026-01-01T00:00:00Z",locale:"en",sources:[],images:[] })
  const messages = [make("original","member"),make("agent","agent"),make("community-member","member")]
  const view = publicConversation(messages)
  assert.equal(view.original?.id, "original")
  assert.deepEqual(view.replies.map(m=>m.id), ["community-member","agent"])
  assert.equal(view.latestReplyID, "community-member")
  assert.deepEqual(messages.map(m=>m.id), ["original","agent","community-member"])
  assert.deepEqual(publicConversation(), { original:null, replies:[], latestReplyID:null })
})

test("legacy member redirects never swallow the public questions route or Admin pages", () => {
  assert.equal(communityEntry("/help/ask"), "/community/new?request_type=question")
  assert.equal(communityEntry("/help/questions"), "/community/mine")
  assert.equal(communityEntry("/help/questions/"), "/community/mine")
  assert.equal(communityEntry("/help/ask/"), "/community/new?request_type=question")
  assert.equal(communityEntry("/community/questions?x=1"), "/community/questions?x=1")
  assert.equal(communityEntry("/help/admin/questions"), null)
  assert.equal(communityEntry("/community-other"), null)
})

test("a generated title stays in the initial report rather than appended report fields", () => {
  assert.equal(communityTitle("Button stays disabled.\n\n## Expected\nSubmit the draft.",160), "Button stays disabled.")
  assert.equal(communityTitle("   ",160), undefined)
  assert.equal(communityTitle("abcdef",3), "abc")
})
