import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { localizeLedgerCatalog } from "./zh-Hant.ts"

test("the Ledger completes the locked Traditional Chinese catalog before drawing", () => {
  const strings = {
    webLedgerFeature: "Feature",
    webLedgerFindings: "Finding",
    webLedgerTasks: "{n} 個 task",
    webLedgerVerdicts: "Verdict",
  }
  localizeLedgerCatalog(strings, "zh-Hant")
  assert.deepEqual(strings, {
    webLedgerFeature: "功能",
    webLedgerFindings: "發現",
    webLedgerTasks: "{n} 個任務",
    webLedgerVerdicts: "裁定",
    webLedgerEmpty: "這台機器還沒有任何功能附有複審或驗證收據。",
    webLedgerLede: "每個功能的複審發現、驗證所花的工時，以及 Token 用量如何分布在實作與讀取之間。",
    webLedgerLoading: "正在讀取這台機器的複審與驗證收據…",
    webLedgerNoRecordSay: "這台機器沒有這個功能的這種收據。收據寫入失敗時只會留下一行紀錄，所以這不等於沒有人複審過。",
    webLedgerNotFound: "沒有任何收據或資料列指向那個功能。",
    webLedgerRead: "掃描了 {rows} 列，找到 {features} 個功能。",
    webLedgerReviewedClean: "複審過，沒有發現",
    webLedgerUnattributed: "沒有指向任何功能的記錄",
    webLedgerUnattributedSay: "這台機器有 {rows} 列沒有帶功能鍵，所以下面每一個數字都無法涵蓋它們。回填會在應用程式啟動時執行。",
    webLedgerUnavailable: "這條連線讀不到驗證帳本。請在這台機器所在的網路開啟應用程式自己的位址。",
    webLedgerUndeclaredSay: "這些列所屬的任務沒有記下自己是哪一種工作。分開計算，是因為把它們算成實作，等於替資料列上沒有依據的事下結論。",
  })
})

test("the Ledger leaves an English catalog unchanged", () => {
  const strings = { webLedgerFindings: "Findings" }
  localizeLedgerCatalog(strings, "en")
  assert.deepEqual(strings, { webLedgerFindings: "Findings" })
})
