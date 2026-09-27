// The Archive page's rules, without a browser:
//
//   node --test web/console/src/pages/archive/model.test.ts
//
// What the page draws is held in `session/archive.e2e.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see `session/order.test.ts`.
import { archivedName, archivedWhen, entryLine, withoutRestored, type Outcome } from "./model.ts"

const words = (key: string, holes: Record<string, string> = {}) =>
  key + (Object.keys(holes).length ? " " + JSON.stringify(holes) : "")

const entry = (id: string, over: Record<string, unknown> = {}) => ({
  conversation_id: id, assistant: "claude", place: "p1", place_label: "api", cwd: "/w/api",
  title: "Fix the login", archived_at: 1_790_000_000, ...over,
})

test("an entry goes by the title its row showed, else its folder", () => {
  assert.equal(archivedName(entry("c1")), "Fix the login")
  assert.equal(archivedName(entry("c1", { title: "  " })), "api")
  assert.equal(archivedName(entry("c1", { title: "", place_label: "" })), "/w/api")
})

test("when it was archived is said twice: how long ago, and the date and time", () => {
  const at = Date.UTC(2026, 8, 24, 10, 5) / 1000
  const when = archivedWhen(at, at * 1000 + 3 * 86400_000, "en", "UTC")
  assert.equal(when.relative, "3 days ago")
  assert.equal(when.absolute, "2026-09-24 10:05")
  assert.equal(archivedWhen(at, at * 1000 + 30_000, "en", "UTC").relative, "this minute")
})

test("a clock a little behind the machine's still reads as now, not the future", () => {
  const at = Date.UTC(2026, 8, 24, 10, 5) / 1000
  assert.equal(archivedWhen(at, at * 1000 - 20_000, "en", "UTC").relative, "this minute")
})

test("a missing time says nothing rather than 1970", () => {
  assert.deepEqual(archivedWhen(0, Date.now(), "en"), { relative: "", absolute: "" })
})

test("each restore code says its own sentence, and an unknown one says the machine's", () => {
  const failed = (code: string | undefined, message?: string): Outcome => ({ kind: "failed", code, message })
  assert.equal(entryLine(failed("not_archived"), words), "archiveFailedNotArchived")
  assert.equal(entryLine(failed("already_open"), words), "archiveFailedAlreadyOpen")
  assert.equal(entryLine(failed("place_unavailable"), words), "restoreFailedPlaceUnavailable")
  assert.equal(entryLine(failed("conversation_not_found"), words), "restoreFailedNotFound")
  assert.equal(entryLine(failed("over_capacity"), words), "restoreFailedBusy")
  assert.equal(entryLine(failed("open_failed", "tmux is gone"), words), "restoreFailedOpen tmux is gone")
  assert.equal(entryLine(failed("brand_new", "said so"), words), 'restoreFailedSaid {"why":"said so"}')
  assert.equal(entryLine(failed(undefined), words), "restoreFailedUnknown")
  assert.equal(entryLine({ kind: "restoring" }, words), "archiveRestoring")
  assert.equal(entryLine(undefined, words), "")
})

test("an entry that reopened leaves the list, and only that one", () => {
  const list = [entry("c1"), entry("c2"), entry("c3")]
  assert.deepEqual(withoutRestored(list, "c2").map((e) => e.conversation_id), ["c1", "c3"])
  assert.equal(withoutRestored(list, "zz").length, 3)
})
