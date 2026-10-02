// Who made a Board item, read from its creator:
// `node --test --experimental-strip-types web/console/src/pages/work/origin.test.ts`.
import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { clockOf, originLine, workOrigin } from "./words.ts"

const at = Math.floor(new Date(2026, 8, 25, 9, 5).getTime() / 1000)

test("the creator's prefix, not created_via, says who made an item", () => {
  assert.equal(workOrigin("user_via_session:conv-1"), "session")
  assert.equal(workOrigin("epic_owner:sess-1"), "epic_owner")
  assert.equal(workOrigin("user"), "person")
  assert.equal(workOrigin(""), "person")
  assert.equal(workOrigin(undefined), "person")
  // A person's item that somehow carries created_via is still the person's.
  assert.equal(originLine({ created_by: "user", created_via: { run: "r", session_id: "s", at } }, "en"), null)
})

test("a Session's item names the time of the person's message when the wire carries it", () => {
  assert.equal(clockOf(at), "09:05")
  const created_via = { run: "r", session_id: "s", at }
  assert.equal(originLine({ created_by: "user_via_session:s", created_via }, "zh-Hant"), "Session 依你 09:05 的訊息建立")
  assert.equal(originLine({ created_by: "user_via_session:s", created_via }, "en"), "Created by the Session from your message at 09:05")
  assert.equal(originLine({ created_by: "user_via_session:s" }, "zh-Hant"), "Session 依你的訊息建立")
  assert.equal(originLine({ created_by: "user_via_session:s" }, "en"), "Created by the Session from your message")
})

test("an item the Epic's owner Session split out says so", () => {
  assert.equal(originLine({ created_by: "epic_owner:s" }, "zh-Hant"), "由 Epic 的負責 Session 拆分建立")
  assert.equal(originLine({ created_by: "epic_owner:s" }, "en"), "Split out by the Epic's owner Session")
  assert.equal(originLine({ created_by: "epic_owner:s", created_via: { run: "", session_id: "s", at, epic_id: "e" } }, "en"),
    "Split out by the Epic's owner Session at 09:05")
})
