// When a direct to-do can be sent again: `node --test web/console/src/session/todo-send.test.ts`.
import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { DIRECT_TODO_RESEND_AFTER_SECONDS, todoSend } from "./todo-send.ts"

const at = 1_790_000_000

test("an unsent to-do offers Send", () => {
  assert.deepEqual(todoSend({ sent_at: null, read_at: null }, at), { kind: "send", label: "Send" })
})

test("an unread delivery waits out the double-send window, then can be sent again", () => {
  const sent = { sent_at: at, read_at: null }
  assert.equal(todoSend(sent, at + 1).kind, "wait")
  assert.equal(todoSend(sent, at + DIRECT_TODO_RESEND_AFTER_SECONDS - 1).kind, "wait")
  // The case that was stuck for hours: sent, never read.
  assert.equal(todoSend(sent, at + DIRECT_TODO_RESEND_AFTER_SECONDS).kind, "again")
  assert.equal(todoSend(sent, at + 3 * 3600).kind, "again")
})

test("a read delivery can always be sent again, and a completed one never", () => {
  assert.equal(todoSend({ sent_at: at, read_at: at + 1 }, at + 2).kind, "again")
  assert.equal(todoSend({ sent_at: at, read_at: at + 1, completed_at: at + 3 }, at + 4).kind, "none")
})

test("the window matches the daemon's", async () => {
  const { readFileSync } = await import("node:fs")
  const app = readFileSync(new URL("../../../../internal/app/work_v2.go", import.meta.url), "utf8")
  assert.match(app, /const DirectTodoResendAfter = 2 \* time\.Minute/)
  assert.equal(DIRECT_TODO_RESEND_AFTER_SECONDS, 120)
})
