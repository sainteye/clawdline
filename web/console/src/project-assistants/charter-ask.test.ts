import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node executes source extensions.
import { askPolicyFromText, insertAskRow, isAskSectionHeading, parseAskPolicy, removeAskRow, sectionAskPolicy, serializeAskPolicy, shownAskRows } from "./charter-ask.ts"
// @ts-expect-error Node executes source extensions.
import { charterSections, replaceCharterSection, sectionFromParts, sectionParts } from "./charter-text.ts"

const canonical = "## 什麼要問你\n\n### 不用問，直接做\n\n- 已審的修正整合進 main\n- 推 GitHub\n\n### 一定要問\n\n- 公開 Release\n- 花錢\n\n### 例外狀況\n\n- 低於 NTD 5 的 API 呼叫不用先問\n\n### 怎麼問\n\n用 ask_person 問一次。"
const doc = `## 誰負責什麼\n\n- Orbit：審查\n\n${canonical}\n\n## 交辦卡\n\n寫清楚目標。\n`

const live = [
  "不用問，直接做：已審、檢查全綠的修正，整合進 main、推 GitHub、裝 dev；Lens 驗收通過後部署 app.clawdline.com。",
  "一定要問：公開 Release、改 clawdline.com、刪除資料、花錢、產品方向取捨、檢查沒過要不要照上。",
  "例外：一次性、總費用確定低於 NTD 5 的外部 API 呼叫不用先問；先設好費用或次數上限，不自動重試，做完回報實際次數與費用。",
  "要問時用 ask_person 在自己的聊天跳決策卡，同一件事只問一次；不要請使用者轉貼文字，也不要請別的 Bot 替你問。",
].join("\n")

test("a section of exactly the four sub-headings with lists is an ask policy", () => {
  const section = charterSections(doc)[1]
  assert.deepEqual(sectionAskPolicy(section), {
    language: "zh", free: ["已審的修正整合進 main", "推 GitHub"], ask: ["公開 Release", "花錢"],
    exceptions: ["低於 NTD 5 的 API 呼叫不用先問"], how: "用 ask_person 問一次。",
  })
  const english = parseAskPolicy("### No need to ask\n\n- Merge reviewed fixes\n\n### Always ask\n\n- Spending money\n\n### Exceptions\n\n### How to ask\n\nUse ask_person.")
  assert.deepEqual(english, { language: "en", free: ["Merge reviewed fixes"], ask: ["Spending money"], exceptions: [], how: "Use ask_person." })
})

test("any other shape is not an ask policy", () => {
  const body = sectionParts(charterSections(doc)[1]).body
  const not = [
    "部署前先問。",
    live,
    body.replace("### 一定要問", "### 要問"),                       // a different heading
    body.replace("### 例外狀況\n\n- 低於 NTD 5 的 API 呼叫不用先問\n\n", ""), // one missing
    "前言\n\n" + body,                                               // text before the first sub-heading
    body.replace("- 推 GitHub", "推 GitHub"),                         // a line that is not an item
    body.replace("- 推 GitHub", "- 推 GitHub\n  接著說明"),            // a continuation line
    body.replace("- 推 GitHub", "* 推 GitHub"),                       // another list marker
    body + "\n第二行",                                                // a second line of how to ask
    body.replace("### 一定要問", "### Always ask"),                   // languages mixed
    body.replace("- 推 GitHub", "- "),                               // an empty item
  ]
  for (const text of not) assert.equal(parseAskPolicy(text), null, text)
  assert.equal(sectionAskPolicy({ heading: "", text: body }), null)
})

test("parsing and serialising a canonical section reproduces it byte for byte", () => {
  const sections = charterSections(doc)
  const section = sections[1]
  const policy = sectionAskPolicy(section)!
  const text = sectionFromParts(section, section.heading, serializeAskPolicy(policy))
  assert.equal(text, canonical)
  assert.equal(replaceCharterSection(doc, 1, text), doc)
  const english = "### No need to ask\n\n- A\n\n### Always ask\n\n- B\n- C\n\n### Exceptions\n\n### How to ask"
  assert.equal(serializeAskPolicy(parseAskPolicy(english)!), english)
})

test("saving drops empty rows and keeps the heading of an empty list", () => {
  const body = serializeAskPolicy({ language: "zh", free: ["  做 A ", "", "做 B"], ask: ["", " "], exceptions: [], how: "" })
  assert.equal(body, "### 不用問，直接做\n\n- 做 A\n- 做 B\n\n### 一定要問\n\n### 例外狀況\n\n### 怎麼問")
  assert.deepEqual(parseAskPolicy(body), { language: "zh", free: ["做 A", "做 B"], ask: [], exceptions: [], how: "" })
  // A line break pasted into a row stays one item.
  assert.equal(serializeAskPolicy({ language: "en", free: ["one\ntwo"], ask: [], exceptions: [], how: "a\nb" }), "### No need to ask\n\n- one two\n\n### Always ask\n\n### Exceptions\n\n### How to ask\n\na b")
})

test("the live free text converts into the two columns, the exceptions and how to ask", () => {
  assert.deepEqual(askPolicyFromText(live, "zh"), {
    language: "zh",
    free: ["已審、檢查全綠的修正，整合進 main、推 GitHub、裝 dev", "Lens 驗收通過後部署 app.clawdline.com"],
    ask: ["公開 Release", "改 clawdline.com", "刪除資料", "花錢", "產品方向取捨", "檢查沒過要不要照上"],
    exceptions: ["一次性、總費用確定低於 NTD 5 的外部 API 呼叫不用先問；先設好費用或次數上限，不自動重試，做完回報實際次數與費用。"],
    how: "要問時用 ask_person 在自己的聊天跳決策卡，同一件事只問一次；不要請使用者轉貼文字，也不要請別的 Bot 替你問。",
  })
  // The converted policy saves as the recognised shape.
  const saved = serializeAskPolicy(askPolicyFromText(live, "zh"))
  assert.deepEqual(parseAskPolicy(saved), askPolicyFromText(live, "zh"))
})

test("a line the conversion does not recognise is kept whole as an exception", () => {
  const policy = askPolicyFromText("部署前先問。\n\n- 不要改別人的檔案\n不用問：跑測試", "zh")
  assert.deepEqual(policy, { language: "zh", free: ["跑測試"], ask: [], exceptions: ["部署前先問。", "不要改別人的檔案"], how: "" })
  const english = askPolicyFromText("No need to ask: run tests; merge fixes\nAlways ask: deploys, spending\nWhen you ask, use ask_person.\nBe kind.", "en")
  assert.deepEqual(english, { language: "en", free: ["run tests", "merge fixes"], ask: ["deploys", "spending"], exceptions: ["Be kind."], how: "When you ask, use ask_person." })
})

test("Enter adds a row below and focuses it; Backspace in an empty row removes it and focuses the one before", () => {
  assert.deepEqual(insertAskRow(["a", "b"], 0), { items: ["a", "", "b"], focus: 1 })
  assert.deepEqual(insertAskRow(["a", "b"], 1), { items: ["a", "b", ""], focus: 2 })
  assert.deepEqual(removeAskRow(["a", "", "b"], 1), { items: ["a", "b"], focus: 0 })
  assert.deepEqual(removeAskRow(["", "b"], 0), { items: ["b"], focus: 0 })
  assert.deepEqual(removeAskRow([""], 0), { items: [], focus: -1 })
  assert.deepEqual(removeAskRow(["a"], 3), { items: ["a"], focus: -1 })
  assert.deepEqual(shownAskRows([]), [""])
  assert.deepEqual(shownAskRows(["a"]), ["a"])
})

test("the conversion is offered under the section's own names", () => {
  assert.ok(isAskSectionHeading("什麼要問你"))
  assert.ok(isAskSectionHeading("What to ask you about"))
  assert.ok(!isAskSectionHeading("交辦卡"))
})
