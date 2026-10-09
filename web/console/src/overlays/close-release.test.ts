// What the close sheet decides from a closeability reading:
// `node --test --experimental-strip-types web/console/src/overlays/close-release.test.ts`
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see `session/order.test.ts`.
import { refusedReasons, releasableItems } from "./close-release.ts"

const mover = { kind: "session", self: true, person_needed: false }
const unstarted = (id: string) => ({ code: "board_item_unstarted", kind: "obligation", subject_kind: "work", subject_id: id, mover })
const started = (id: string) => ({ code: "board_item_open", kind: "obligation", subject_kind: "work", subject_id: id, mover })
const blockedRow = (version: string, reasons: unknown[]) => ({ id: "%4", closeability: { state: "blocked", version, reasons } })

// The phone's report: over Clawdline Cloud the copied `cloud-failure.js`
// drops a `close_blocked` refusal's reasons, and the sheet answered that empty
// list with "cannot confirm". The daemon refuses a close against any other
// reading with `close_not_proven`, so a `close_blocked` against version v is
// blocked by exactly the reasons the page's row shows at v.
test("a close_blocked that lost its reasons is answered with the reading it was refused against", () => {
  const row = blockedRow("v1", [unstarted("a"), started("b")])
  assert.deepEqual(refusedReasons([], row, "v1"), [unstarted("a"), started("b")])
})

test("the refusal's own reasons win, and a moved or unblocked reading says nothing", () => {
  const row = blockedRow("v1", [unstarted("a")])
  assert.deepEqual(refusedReasons([started("b")], row, "v1"), [started("b")])
  assert.deepEqual(refusedReasons([], row, "v2"), [])
  assert.deepEqual(refusedReasons([], row, null), [])
  assert.deepEqual(refusedReasons([], { closeability: { state: "safe", version: "v1", reasons: [] } }, "v1"), [])
  assert.deepEqual(refusedReasons([], null, "v1"), [])
})

test("only a close blocked by unstarted Board items alone may release them", () => {
  assert.deepEqual(releasableItems({ state: "blocked", reasons: [unstarted("a"), unstarted("c")] }), ["a", "c"])
  assert.deepEqual(releasableItems({ state: "blocked", reasons: [unstarted("a"), started("b")] }), [])
  assert.deepEqual(releasableItems({ state: "blocked", reasons: [unstarted("a"),
    { code: "session_todo_open", kind: "obligation", subject_id: "t", mover }] }), [])
  assert.deepEqual(releasableItems({ state: "unknown", reasons: [unstarted("a"),
    { code: "session_inventory_stale", kind: "evidence", mover }] }), [])
  assert.deepEqual(releasableItems({ state: "safe", reasons: [] }), [])
  assert.deepEqual(releasableItems(null), [])
  // An id the reading does not name cannot be listed to the person.
  assert.deepEqual(releasableItems({ state: "blocked", reasons: [{ ...unstarted("a"), subject_id: "" }] }), [])
})

// The phone's second report (2026-10-09): a claude left running in a tmux whose
// socket had been deleted reads `unknown`, and the daemon answers its first
// close with `close_blocked` so that only a second, forced press signals it.
// Over Cloud that refusal lost its reasons, the row did not read `blocked`,
// and the sheet said "cannot confirm" with no way past it.
test("a process-only close refused over Cloud reopens with the unknown row's reasons", () => {
  const evidence = [
    { code: "session_identity_unbound", kind: "evidence", mover: { kind: "broker", self: false, person_needed: false } },
    { code: "terminal_unreadable", kind: "evidence", subject_kind: "session", subject_id: "ttys011",
      mover: { kind: "broker", self: false, person_needed: false } },
  ]
  const row = { id: "ttys011", closeability: { state: "unknown", version: "v1", reasons: evidence } }
  assert.deepEqual(refusedReasons([], row, "v1"), evidence)
  assert.deepEqual(refusedReasons([], row, "v2"), [])
})
