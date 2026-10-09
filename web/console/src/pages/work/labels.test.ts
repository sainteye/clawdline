import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { workProjectName, workWordIn } from "./words.ts"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { proposalFoldShouldOpen } from "./fold.ts"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { nextWord } from "../../next-strings.ts"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { inCatalog } from "../../catalog-testing.ts"

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

test("Work screens translate an uncarried Cloud route instead of printing its internal code", () => {
  const source = readFileSync(new URL("./shared.ts", import.meta.url), "utf8")
  assert.match(source, /e\.code === "cloud_not_carried"/)
  assert.match(source, /nextWord\("cloudNotCarried"\)/)
})

test("declining for now and recording an evidence-backed resolution are visibly different", () => {
  // The selected catalog, not navigator.language, chooses the words since 33e1c4b1.
  inCatalog("zh-Hant", () => {
    assert.equal(nextWord("proposalNotNow"), "現在不要")
    // The zh-Hant catalog (0f9f6416) words it 不再適用 where the source said 已不存在.
    assert.equal(nextWord("proposalResolve"), "已完成／不再適用")
    assert.notEqual(nextWord("proposalNotNow"), nextWord("proposalResolve"))
    assert.match(nextWord("proposalResolveEvidence"), /檔案:行號|指令輸出|daemon/)
  })
})

test("the Needs independent review checkbox is worded in both languages", () => {
  assert.equal(workWordIn("en", "reviewRequiredLabel"), "Needs independent review")
  assert.equal(workWordIn("en", "reviewRequiredHint"), "When checked, a plan must be written and reviewed by an independent child before work starts.")
  assert.equal(workWordIn("zh-Hant", "reviewRequiredLabel"), "需要獨立審查")
  assert.equal(workWordIn("zh-Hant", "reviewRequiredHint"), "勾選後，開工前要先寫計畫並由獨立 child 審查。")
})
