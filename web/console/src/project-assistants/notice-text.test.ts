import { test } from "node:test"
import assert from "node:assert/strict"
import type * as W from "@clawdline/contract"
// @ts-expect-error Node executes source extensions.
import { compactedNoticeText } from "./notice-text.ts"
// @ts-expect-error Node executes source extensions.
import { words } from "./words.ts"

const notice = (text: string, ref?: W.ProjectAssistantNoticeRef, id = "compacted-t1") => ({id, kind: "notice" as const, text, notice: ref})

test("a compaction notice reads in one language, the reader's", () => {
  const stored = notice("Earlier context was summarized to make room: 345K → 41K", {key: "compacted", tokens_before: 345000, tokens_after: 41000})
  assert.equal(compactedNoticeText(stored, words("zh-Hant")), "已摘要較早的內容，騰出空間：345K → 41K")
  assert.equal(compactedNoticeText(stored, words("en")), "Earlier context was summarized to make room: 345K → 41K")
  assert.equal(compactedNoticeText(notice("Earlier context was summarized to make room", {key: "compacted"}), words("zh-Hant")), "已摘要較早的內容，騰出空間")
})

test("a notice stored with both languages reads in the reader's alone", () => {
  const old = notice("Earlier context was summarized to make room / 已摘要較早的內容，騰出空間：345K → 41K")
  assert.equal(compactedNoticeText(old, words("en")), "Earlier context was summarized to make room: 345K → 41K")
  assert.equal(compactedNoticeText(old, words("zh-Hant")), "已摘要較早的內容，騰出空間：345K → 41K")
  assert.equal(compactedNoticeText(notice("Earlier context was summarized to make room / 已摘要較早的內容，騰出空間"), words("zh-Hant")), "已摘要較早的內容，騰出空間")
})

test("other notices are shown as stored", () => {
  assert.equal(compactedNoticeText(notice("Daily review", undefined, "routine-1"), words("zh-Hant")), null)
  assert.equal(compactedNoticeText({...notice("x"), kind: "assistant" as const}, words("zh-Hant")), null)
})
