import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node executes source extensions.
import { compactFailureText, compactRefusalCode, followCompaction } from "./compact-status.ts"
// @ts-expect-error Node executes source extensions.
import { words } from "./words.ts"

const zh = words("zh-Hant"), en = words("en")
const turn = (state: string, failure_code = "", failure_field = "") => ({id: "t1", state, failure_code, failure_field}) as never
const running = {state: "running" as const, turnID: "t1"}

test("a running compaction stays running until its turn ends", () => {
  assert.equal(followCompaction(running, [turn("running")], [], zh), running)
  assert.equal(followCompaction(running, [], [], zh), running)
  const sending = {state: "sending" as const}
  assert.equal(followCompaction(sending, [], [], zh), sending)
})

test("the compaction notice turns it into the summarized sizes", () => {
  const notice = {id: "compacted-t1", kind: "notice" as const, text: "Earlier context was summarized to make room: 694K → 41K",
    notice: {key: "compacted", tokens_before: 694200, tokens_after: 41000}} as never
  assert.deepEqual(followCompaction(running, [turn("succeeded")], [notice], zh),
    {state: "done", turnID: "t1", text: "已摘要較早的內容，騰出空間：694.2K → 41K"})
  assert.deepEqual(followCompaction(running, [turn("succeeded")], [], en), {state: "done", turnID: "t1", text: en.compactedNotice})
})

test("a failed compaction turn names its most specific reason in plain words", () => {
  const failed = followCompaction(running, [turn("failed", "pa_unsupported", "compaction_not_reported")], [], zh)
  assert.deepEqual(failed, {state: "failed", code: "compaction_not_reported"})
  assert.equal(compactFailureText("compaction_not_reported", zh), zh.compactFailNotReported)
  assert.deepEqual(followCompaction(running, [turn("cancelled")], [], zh), {state: "failed", code: "cancelled"})
  assert.equal(compactFailureText("pa_invalid", en), en.compactFailOther)
})

test("a refused press reads the compaction's field before the generic code", () => {
  assert.equal(compactRefusalCode({error: {code: "pa_unsupported", field: "compact_no_session"}}), "compact_no_session")
  assert.equal(compactRefusalCode({error: {code: "pa_invalid", field: "body"}}), "pa_invalid")
  assert.equal(compactRefusalCode(new Error("x")), "unavailable")
  assert.equal(compactFailureText("compact_no_session", zh), "這個 Bot 還沒有可壓縮的對話，先送一則訊息再試。")
})
