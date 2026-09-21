import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { nowWord } from "../now/words.ts"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { workWord } from "./words.ts"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { proposalFoldShouldOpen } from "./fold.ts"

test("Now names proposals separately from work that is already waiting to close", () => {
  const prior = Object.getOwnPropertyDescriptor(globalThis, "navigator")
  Object.defineProperty(globalThis, "navigator", {
    configurable: true,
    value: { language: "zh-TW" },
  })
  try {
    assert.equal(nowWord("waitingTitle"), "提議待確認、問題待回答")
    assert.equal(nowWord("waitingGo"), "處理待確認的提議")
    assert.equal(workWord("sectionDecide"), "交付待收尾、問題待決定")
    assert.notEqual(nowWord("waitingTitle"), workWord("sectionDecide"))
  } finally {
    if (prior) Object.defineProperty(globalThis, "navigator", prior)
    else Reflect.deleteProperty(globalThis, "navigator")
  }
})

test("pending proposals open their controls on arrival without defeating a manual close", () => {
  assert.equal(proposalFoldShouldOpen(null, 0), false)
  assert.equal(proposalFoldShouldOpen(0, 25), true)
  assert.equal(proposalFoldShouldOpen(25, 25), false)
  assert.equal(proposalFoldShouldOpen(25, 24), true)
})
