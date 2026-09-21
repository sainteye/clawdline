import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { translateUsage, translateUsageText } from "./zh-Hant.ts"

test("the Usage page translates both copied markup and values drawn later", () => {
  const examples = new Map([
    ["Project Portfolio", "專案總覽"],
    ["Back to sessions", "回到 Session 清單"],
    ["Generated output is an operational signal, not a productivity score.", "產出量是運作訊號，不是生產力分數。"],
    ["No Project work in this range", "這段期間沒有專案工作"],
    ["3 Projects", "3 個專案"],
    ["4 scheduled runs · 2 Unknown output", "4 次排程執行 · 2 次產出量不明"],
    ["Ledger freshness: stale", "用量帳本新鮮度：已過期"],
    ["Open Example details", "開啟 Example 的詳細資料"],
    ["5 Features · 2 hidden", "5 個功能 · 隱藏 2 個"],
    ["Partial · 3 unknown output", "部分 · 3 次產出量不明"],
    ["Unavailable · partial cost coverage", "無法取得 · 費用涵蓋不完整"],
    ["4,770.66 USD · list_price_estimate", "4,770.66 USD · 牌價估算"],
    ["Coverage · source_missing", "涵蓋率 · 缺少來源"],
    ["no usage recorded", "沒有用量記錄"],
    ["source unreadable at close", "結束時無法讀取來源"],
    [
      "219 generated output across 41 agent-work runs · Change unavailable · output coverage is incomplete · Unavailable · partial cost coverage.",
      "219 產出量，來自 41 次 Agent 工作 · 目前無法比較變化 · 產出量涵蓋不完整 · 無法取得 · 費用涵蓋不完整。",
    ],
    ["top mover", "產出量變化最大"],
    ["Largest output change", "產出量變化幅度最大"],
    [
      "Example changed by 15583987 generated tokens versus the equal previous range. Inspect the work mix before drawing a conclusion.",
      "Example 與等長的前一段期間相比，產出 token 變化了 15583987。下結論前，請先檢視工作組成。",
    ],
    ["context to output", "上下文與產出比"],
    ["High context-to-output ratio", "上下文與產出的比例偏高"],
    [
      "Example read 18.5x as much new-plus-cached context as it generated. This can be normal for review or retrieval-heavy work; inspect recent runs.",
      "Example 讀取的新增與快取上下文，是其產出的 18.5 倍。這在複審或需大量擷取的工作中可能正常；請檢視近期執行紀錄。",
    ],
    ["Show 4 more", "再顯示 4 個"],
    ["Usage Analytics is busy; sessions remain available. Try again shortly.", "用量分析忙碌中；Session 仍可使用，請稍後再試。"],
  ])

  for (const [english, traditionalChinese] of examples) {
    assert.equal(translateUsageText(english), traditionalChinese, english)
  }
})

test("user and server content that is not interface copy is left untouched", () => {
  assert.equal(translateUsageText("Project Phoenix"), "Project Phoenix")
})

test("formatting whitespace is stable when the mutation observer repaints", () => {
  const whitespace = { nodeType: 3, nodeValue: "\n    ", childNodes: [] } as unknown as Node
  translateUsage(whitespace, "zh-Hant")
  assert.equal(whitespace.nodeValue, "\n    ")
})
