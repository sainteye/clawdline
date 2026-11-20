import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node executes source extensions.
import { showLeadStarter } from "./lead-starter.ts"
// @ts-expect-error Node executes source extensions.
import { en, zhHant } from "./words.ts"

const fresh = { thread: false, lead: true, loaded: true, writable: true, messages: 0, earlier: false, sending: false, queued: 0, draft: "" }

test("a new, empty lead conversation suggests a first message", () => {
  assert.equal(showLeadStarter(fresh), true)
  assert.equal(showLeadStarter({ ...fresh, draft: "   " }), true)
})

test("the suggestion disappears once the conversation is not new", () => {
  for (const change of [
    { lead: false }, { thread: true }, { loaded: false }, { writable: false }, { messages: 1 },
    { earlier: true }, { sending: true }, { queued: 1 }, { draft: "hi" },
  ]) assert.equal(showLeadStarter({ ...fresh, ...change }), false, JSON.stringify(change))
})

test("the suggested message reads naturally in English and Traditional Chinese", () => {
  assert.equal(zhHant.leadStarterMessage, "哈囉，你可以介紹一下，你可以幫我做什麼嗎？")
  assert.match(en.leadStarterMessage, /^Hi/)
  assert.notEqual(en.leadStarterHint, zhHant.leadStarterHint)
})
