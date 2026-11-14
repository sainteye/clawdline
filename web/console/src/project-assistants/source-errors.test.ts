import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node executes source extensions.
import { shownSourceError } from "./source-errors.ts"
test("a machine without Bots is left out of the roster's errors, a real failure is not",()=>{
  assert.equal(shownSourceError("pa_unsupported"), false)
  assert.equal(shownSourceError("pa_disabled"), false)
  assert.equal(shownSourceError(undefined), false)
  assert.equal(shownSourceError("pa_offline"), true)
  assert.equal(shownSourceError("pa_revoked"), true)
})
// @ts-expect-error Node executes source extensions.
import { readOnlyReason } from "./source-errors.ts"
test("a Bot that cannot be changed says why",()=>{
  const w = { offline: "離線", stale: "快取資料；尚未確認是否最新", history_only: "唯讀歷史", readOnlySource: "這台機器關閉了從 Clawdline Cloud 操作", loading: "正在載入" }
  const ok = { state: "online", fresh: true, writable: true, access: "interactive" }
  assert.equal(readOnlyReason(ok, w), "")
  assert.equal(readOnlyReason({ ...ok, state: "offline" }, w), "離線")
  assert.equal(readOnlyReason({ ...ok, fresh: false }, w), "快取資料；尚未確認是否最新")
  assert.equal(readOnlyReason({ ...ok, access: "history_only" }, w), "唯讀歷史")
  assert.equal(readOnlyReason({ ...ok, writable: false }, w), "這台機器關閉了從 Clawdline Cloud 操作")
  assert.equal(readOnlyReason(undefined, w), "正在載入")
})
