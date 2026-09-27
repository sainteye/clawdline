import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { projectListWords, replaceSuffix } from "./project-list.ts"

test("an unavailable activity read says which number is missing without judging Project progress", () => {
  const zh = projectListWords("zh-TW")
  const en = projectListWords("en")
  assert.equal(zh.unknownAfter, "進行中數量待更新")
  assert.doesNotMatch(zh.unknownAfter, /進度尚未確認/)
  assert.equal(en.unknownAfter, "In-progress count unavailable")
})

test("the historical item total opens the work board instead of claiming to be progress", () => {
  const words = projectListWords("zh-TW")
  assert.equal(
    replaceSuffix("503 個工作項目 · 查看進度 →", words.boardBefore, words.boardAfter),
    "503 個工作項目 · 開啟工作看板 →",
  )
  assert.equal(replaceSuffix("/work/project", words.boardBefore, words.boardAfter), "/work/project")
})

test("Project maintenance is one secondary group while the Project list stays outside it", () => {
  const page = readFileSync(new URL("../projects.tsx", import.meta.url), "utf8")
  const tools = readFileSync(new URL("./ProjectTools.tsx", import.meta.url), "utf8")
  assert.match(page, /<ProjectTools/)
  assert.doesNotMatch(page, /<ProjectSetup/)
  assert.match(tools, /<details className="project-tools"/)
  assert.match(tools, /<ProjectSetup/)
  assert.match(tools, /<ProjectSync/)
  assert.match(tools, /<IconCopy/)
})
