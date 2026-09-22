import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { nowWord } from "../now/words.ts"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { workProjectName, workWord } from "./words.ts"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { proposalFoldShouldOpen } from "./fold.ts"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { nextWord } from "../../next-strings.ts"

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

test("proposal cards name a project instead of printing its machine path", () => {
  assert.equal(workProjectName("/srv/work/clawdline-go"), "clawdline-go")
  assert.equal(workProjectName("C:\\work\\clawdline-go\\"), "clawdline-go")
  assert.equal(workProjectName("clawdline-go"), "clawdline-go")
})

test("proposal reasons are section headings instead of the same sentence on every card", () => {
  const source = readFileSync(new URL("./Board.tsx", import.meta.url), "utf8")
  assert.doesNotMatch(source, /proposalWhy|SIGNAL_WORD/)
  assert.match(source, /<p className="work-note">\{workWord\(g\.word\)\}<\/p>/)
})

test("declining for now and recording an evidence-backed resolution are visibly different", () => {
  const prior = Object.getOwnPropertyDescriptor(globalThis, "navigator")
  Object.defineProperty(globalThis, "navigator", {
    configurable: true,
    value: { language: "zh-TW" },
  })
  try {
    assert.equal(nextWord("proposalNotNow"), "現在不要")
    assert.equal(nextWord("proposalResolve"), "已完成／已不存在")
    assert.notEqual(nextWord("proposalNotNow"), nextWord("proposalResolve"))
    assert.match(nextWord("proposalResolveEvidence"), /檔案:行號|指令輸出|daemon/)
  } finally {
    if (prior) Object.defineProperty(globalThis, "navigator", prior)
    else Reflect.deleteProperty(globalThis, "navigator")
  }
})
