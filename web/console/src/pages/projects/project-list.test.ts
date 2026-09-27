import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { activityUnknownScope, currentProjectSummary, projectListWords, replaceSuffix } from "./project-list.ts"

test("an unavailable activity read says which number is missing without judging Project progress", () => {
  const zh = projectListWords("zh-TW")
  const en = projectListWords("en")
  assert.equal(zh.unknownAfter, "進行中數量待更新")
  assert.doesNotMatch(zh.unknownAfter, /進度尚未確認/)
  assert.equal(en.unknownAfter, "In-progress count unavailable")
})

test("the current item total opens the work board instead of claiming to be progress", () => {
  const words = projectListWords("zh-TW")
  assert.equal(
    replaceSuffix("503 個工作項目 · 查看進度 →", words.boardBefore, words.boardAfter),
    "503 個工作項目 · 開啟工作看板 →",
  )
  assert.equal(replaceSuffix("/work/project", words.boardBefore, words.boardAfter), "/work/project")
})

test("a missing count repeated on every Project is explained once for the list", () => {
  assert.equal(activityUnknownScope(4, 4), "list")
  assert.equal(activityUnknownScope(4, 1), "row")
  assert.equal(activityUnknownScope(4, 0), "none")
})

test("only current work-system counts reach a Project row", () => {
  assert.deepEqual(currentProjectSummary({ id: "p", itemCount: 503, activeItemCount: 0, summaryCoverage: "unknown" }), {
    id: "p",
    summaryCoverage: "unknown",
  })
  assert.deepEqual(currentProjectSummary({ id: "p", itemCount: 8, activeItemCount: 4, summaryCoverage: "complete" }), {
    id: "p",
    itemCount: 8,
    activeItemCount: 4,
    summaryCoverage: "complete",
  })
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
