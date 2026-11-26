import {test} from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node executes source extensions.
import {decisionReplyOf} from "./decision-reply.ts"

// The person's answer to a decision card reads as that answer, not as the
// quoted prompt the Bot receives.
test("a marked answer shows the card's question and the reply", () => {
  const view = decisionReplyOf({kind: "user", text: "> Your question for me:\n> Deploy?\n\nYes", decision_reply: {offer_id: "d1", question: "Deploy now?", answer: "Yes, deploy"}})
  assert.deepEqual(view, {question: "Deploy now?", answer: "Yes, deploy"})
})

test("an earlier answer is recognized from the daemon's prompt", () => {
  const text = "> Request from another Bot:\n> Xavia, this is Atlas.\n>\n> **ed2771fb**\n> Your question for me:\n> Merge ed2771fb onto main?\n> It is reviewed.\n\nYes, merge it\nand deploy."
  assert.deepEqual(decisionReplyOf({kind: "user", text}), {question: "Merge ed2771fb onto main?\nIt is reviewed.", answer: "Yes, merge it\nand deploy."})
  assert.deepEqual(decisionReplyOf({kind: "user", text: "> Your question for me:\n> Ship?\n\nNo"}), {question: "Ship?", answer: "No"})
})

test("ordinary messages and quotes stay ordinary", () => {
  assert.equal(decisionReplyOf({kind: "user", text: "> just a quote\n\nmy words"}), null)
  assert.equal(decisionReplyOf({kind: "user", text: "> Request from another Bot:\n> no question header\n\nanswer"}), null)
  assert.equal(decisionReplyOf({kind: "assistant", text: "> Your question for me:\n> Ship?\n\nNo"}), null)
  assert.equal(decisionReplyOf({kind: "user", text: "> Your question for me:\n> Ship?\n\n"}), null)
})
