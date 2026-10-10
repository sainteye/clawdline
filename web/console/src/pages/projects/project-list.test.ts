import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { activityUnknownScope, currentProjectSummary, localizePinnedProjectStatus, projectActivityWords, projectListWords, projectOpenLabel, replaceSuffix } from "./project-list.ts"

test("an unavailable activity read says which number is missing without judging Project progress", () => {
  const zh = projectListWords("zh-TW")
  const en = projectListWords("en")
  assert.equal(zh.unknownAfter, "進行中數量待更新")
  assert.doesNotMatch(zh.unknownAfter, /進度尚未確認/)
  assert.equal(en.unknownAfter, "In-progress count needs updating")
})

test("pinned Project activity is rebuilt from the same read facts for every language", () => {
  assert.equal(projectActivityWords({ boardProjectId: "p" })?.text, "In-progress count needs updating")
  assert.equal(projectActivityWords({ boardProjectId: "p", activeItemCount: 2, summaryCoverage: "complete", activityReadStatus: "ready" })?.text, "2 in progress")
  assert.equal(projectActivityWords({ boardProjectId: "p", activeItemCount: 2, summaryCoverage: "complete", activityReadStatus: "stale" })?.text, "Last known: 2 in progress")
  assert.equal(projectActivityWords({ boardProjectId: "p", activeItemCount: 2, summaryCoverage: "partial", activityReadStatus: "ready" })?.text, "Partial: 2 in progress")
  assert.equal(projectActivityWords({ boardProjectId: "p", activeItemCount: 0, summaryCoverage: "complete", activityReadStatus: "ready" })?.text, "None in progress")
  assert.equal(projectActivityWords({ activeItemCount: 2 }), null)
})

test("the Project row label keeps its accessible name and omits a repeated unknown badge", () => {
  assert.equal(projectOpenLabel("Atlas", "Last known: 2 in progress", false), "Open Atlas, Last known: 2 in progress")
  assert.equal(projectOpenLabel("Atlas", "In-progress count needs updating", true), "Open Atlas")
})

test("pinned loading and refusal prefixes are translated without changing the following code", () => {
  assert.equal(localizePinnedProjectStatus("看板暫時無法讀取，目前顯示一般專案。設定未變更。 (board_unavailable)"),
    "Board unavailable; showing standard Projects. Your setting has not changed. (board_unavailable)")
  assert.equal(localizePinnedProjectStatus("更新失敗，目前保留上次的專案目錄。Access denied"),
    "Update failed; showing the last available project directory. Access denied")
  assert.equal(localizePinnedProjectStatus("User title: Update failed; showing the last available project directory."),
    "User title: Update failed; showing the last available project directory.")
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
  assert.match(tools, /<details className="project-tools"/)
  assert.match(tools, /<ProjectSetup\b/)
  assert.match(tools, /<ProjectSync/)
  assert.match(tools, /<IconCopy/)
})
