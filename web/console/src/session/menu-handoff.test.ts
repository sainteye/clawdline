import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node source tests use the source extension.
import { MENU_HANDOFF_GRACE_MS, menuHandoffDelay, unreadMenuSeen } from "./menu-handoff.ts"

test("a question without options is not handed off before the grace window", () => {
  const seen = unreadMenuSeen(null, "s:1:", 1_000)
  assert.equal(menuHandoffDelay(seen, "read", "", 1_000), MENU_HANDOFF_GRACE_MS)
  assert.equal(menuHandoffDelay(seen, "read", "", 1_000 + MENU_HANDOFF_GRACE_MS - 1), 1)
  assert.equal(menuHandoffDelay(seen, "read", "", 1_000 + MENU_HANDOFF_GRACE_MS), 0)
})

test("later snapshots of the same question keep the first time it was seen", () => {
  const first = unreadMenuSeen(null, "s:1:", 1_000)
  const again = unreadMenuSeen(first, "s:1:", 4_000)
  assert.equal(again?.since, 1_000)
  assert.equal(unreadMenuSeen(again, "s:1:Pick one", 5_000)?.since, 5_000)
})

test("options arriving clears the unread question", () => {
  assert.equal(unreadMenuSeen(unreadMenuSeen(null, "s:1:", 1_000), "", 2_000), null)
  assert.equal(menuHandoffDelay(null, "read", "", 99_000), null)
})

test("an uncaptured screen is a read that has not happened yet", () => {
  const seen = unreadMenuSeen(null, "s:1:", 0)
  assert.equal(menuHandoffDelay(seen, "unavailable", "", 60_000), null)
  assert.equal(menuHandoffDelay(seen, undefined, "", 60_000), null)
})

test("a question is offered to Terminal once", () => {
  const seen = unreadMenuSeen(null, "s:1:", 0)
  assert.equal(menuHandoffDelay(seen, "read", "s:1:", 60_000), null)
})
