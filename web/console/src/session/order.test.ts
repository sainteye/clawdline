// The list's order: `node --test web/console/src/session/*.test.ts`.
//
// Node strips the types and runs this file as it is, and a stripped import
// keeps its spelling, so the module is named by its own `.ts` path. The
// compiler refuses that spelling without `allowImportingTsExtensions`, which
// the console's build has no use for; the one line says so rather than the
// whole project changing its settings for a test.
import { test } from "node:test"
import assert from "node:assert/strict"
import type { SessionRow, TaskRow } from "@clawdline/contract"
// @ts-expect-error -- a `.ts` path, for node; see above.
import { arrangeSessions, compareSessions, groupUnderRoots, movedAt, rankOf, waitingKey } from "./order.ts"

function row(id: string, state: string, label: string, extra: Partial<SessionRow> = {}): SessionRow {
  return { id, state, label, backend: "iterm", work_state: "unknown", evidence: "screen", isClaude: true, ...extra } as SessionRow
}

/**
 * The rows, each carrying the daemon's answer for when it last moved. An id
 * absent from the table is a row the daemon could not read: it carries the
 * reason and no time, which is not the same row as one with an old time.
 */
function withTimes(rows: SessionRow[], times: Record<string, number>): SessionRow[] {
  return rows.map((r) =>
    times[r.id] === undefined
      ? ({ ...r, activity: { known: false, unknown_reason: "unreadable" } } as SessionRow)
      : ({ ...r, activity: { known: true, at: times[r.id], evidence: "transcript" } } as SessionRow),
  )
}

function order(rows: SessionRow[], times: Record<string, number>, extra: { filter?: string; tasks?: TaskRow[] } = {}): string[] {
  return arrangeSessions({
    sessions: withTimes(rows, times),
    filter: extra.filter ?? "",
    tasks: extra.tasks ?? [],
    shaping: () => true,
    hold: null,
  }).map((r: SessionRow) => r.id)
}

test("inside one state, the session that moved most recently comes first", () => {
  const rows = [row("a", "idle", "Alpha"), row("b", "idle", "Bravo"), row("c", "idle", "Charlie")]
  assert.deepEqual(order(rows, { a: 100, b: 300, c: 200 }), ["b", "c", "a"])
})

test("a row never climbs over a better state, however recently it moved", () => {
  const rows = [
    row("idle-new", "idle", "A"),
    row("working-old", "working", "B"),
    row("waiting-old", "waiting", "C"),
    row("unknown-new", "unknown", "D"),
  ]
  const times = { "idle-new": 9_000, "working-old": 10, "waiting-old": 1, "unknown-new": 9_999 }
  assert.deepEqual(order(rows, times), ["waiting-old", "working-old", "idle-new", "unknown-new"])
})

test("the title still decides between rows that moved at the same moment", () => {
  const rows = [row("z", "working", "Zulu"), row("m", "working", "Mike"), row("a", "working", "Alpha")]
  assert.deepEqual(order(rows, { z: 500, m: 500, a: 500 }), ["a", "m", "z"])
})

test("a time that could not be read is not a very old time: the row goes above, not below", () => {
  const rows = [row("n1", "idle", "Bravo"), row("seen", "idle", "Zulu"), row("n2", "idle", "Alpha")]
  // `seen` moved; the other two are rows the daemon could not read. They keep
  // their own order by title and sit above the row with a time, because the
  // bottom of the list is where a row stops being looked at.
  assert.deepEqual(order(rows, { seen: 42 }), ["n2", "n1", "seen"])
})

test("a long quiet session and an unreadable one are two different answers", () => {
  const quiet = { ...row("quiet", "idle", "Quiet"), activity: { known: true, at: 1, evidence: "transcript" } } as SessionRow
  const unread = { ...row("unread", "idle", "Unread"), activity: { known: false, unknown_reason: "unread" } } as SessionRow
  const missing = row("missing", "idle", "Missing")
  assert.equal(movedAt(quiet), 1)
  assert.equal(movedAt(unread), null)
  // A row from a daemon too old to send the field is the same answer as one
  // that could not be read, and never the bottom of the list.
  assert.equal(movedAt(missing), null)
  assert.deepEqual(
    arrangeSessions({ sessions: [quiet, unread, missing], filter: "", tasks: [], shaping: () => true, hold: null }).map(
      (r: SessionRow) => r.id,
    ),
    ["missing", "unread", "quiet"],
  )
})

test("every kind of nothing sorts the same way, whatever its reason", () => {
  const reasons = ["no_record", "unreadable", "unread", "unsupported"]
  for (const reason of reasons) {
    const one = { ...row("x", "idle", "Zulu"), activity: { known: false, unknown_reason: reason } } as SessionRow
    const moved = { ...row("y", "idle", "Alpha"), activity: { known: true, at: 9_999, evidence: "transcript" } } as SessionRow
    assert.deepEqual(
      arrangeSessions({ sessions: [moved, one], filter: "", tasks: [], shaping: () => true, hold: null }).map(
        (r: SessionRow) => r.id,
      ),
      ["x", "y"],
      reason,
    )
  }
})

test("the id is the last word when title and time are equal", () => {
  const rows = [row("b", "idle", "Same"), row("a", "idle", "Same")]
  assert.deepEqual(order(rows, { a: 7, b: 7 }), ["a", "b"])
  assert.equal(compareSessions(rows[0], rows[0]), 0)
})

test("Clawdfather stays first, even behind a waiting row that just moved", () => {
  const coordinator = row("cf", "idle", "Clawdfather", { coordinator: { commands: [], label: "Clawdfather", status: "online" } })
  const rows = [row("w", "waiting", "Waiting"), coordinator]
  assert.deepEqual(order(rows, { w: 1_000, cf: 0 }), ["cf", "w"])
})

test("an unknown state sorts after every known one", () => {
  assert.equal(rankOf({ state: "waiting" } as SessionRow), 0)
  assert.equal(rankOf({ state: "unknown" } as SessionRow), 3)
  assert.equal(rankOf({ state: "something-new" } as unknown as SessionRow), 9)
  assert.equal(rankOf({ state: "toString" } as unknown as SessionRow), 9)
})

test("the filter reads label, folder, tty and backend", () => {
  const rows = [
    row("a", "idle", "Alpha", { cwd: "/code/one" }),
    row("b", "idle", "Bravo", { tty: "/dev/ttys004" }),
    row("c", "idle", "Charlie", { backend: "tmux" }),
  ]
  assert.deepEqual(order(rows, {}, { filter: " ONE " }), ["a"])
  assert.deepEqual(order(rows, {}, { filter: "ttys004" }), ["b"])
  assert.deepEqual(order(rows, {}, { filter: "tmux" }), ["c"])
})

test("a held order does not move, and a row that arrives goes after it", () => {
  // `b` and `c` moved since the hold was taken; the held rows stay where they were.
  const rows = withTimes([row("a", "idle", "Alpha"), row("b", "idle", "Bravo"), row("c", "idle", "Charlie")], {
    a: 1,
    b: 50,
    c: 99,
  })
  const hold = { order: ["a", "b"], waiting: waitingKey(rows) }
  const held = arrangeSessions({
    sessions: rows,
    filter: "",
    tasks: [],
    shaping: () => true,
    hold,
  }).map((r: SessionRow) => r.id)
  assert.deepEqual(held, ["a", "b", "c"])
})

test("the waiting key changes only with the set of waiting sessions", () => {
  const before = [row("a", "idle", "A"), row("b", "waiting", "B")]
  const after = [row("a", "waiting", "A"), row("b", "waiting", "B")]
  assert.equal(waitingKey(before), "b")
  assert.equal(waitingKey(after), "a,b")
  assert.equal(waitingKey([...before].reverse()), waitingKey(before))
})

function task(child: string, root: string): TaskRow {
  return { id: child + "-task", child: { terminalId: child }, root: { terminalId: root } } as TaskRow
}

test("a child is placed under the session that asked for it, whatever its own time", () => {
  const rows = [row("root", "idle", "Root"), row("other", "idle", "Other"), row("kid", "working", "Kid")]
  // The child is working (rank 1), so on its own it would be first; its root
  // decides where it sits.
  assert.deepEqual(order(rows, { other: 50, root: 10 }, { tasks: [task("kid", "root")] }), ["other", "root", "kid"])
})

test("a link that goes round in a circle or too deep is dropped, and no row is lost", () => {
  const rows = [row("a", "idle", "A"), row("b", "idle", "B"), row("c", "idle", "C"), row("d", "idle", "D")]
  const circle = groupUnderRoots(rows, [task("a", "b"), task("b", "a")], () => true).map((r: SessionRow) => r.id)
  assert.equal(circle.length, 4)
  assert.deepEqual([...circle].sort(), ["a", "b", "c", "d"])
  const deep = groupUnderRoots(rows, [task("b", "a"), task("c", "b"), task("d", "c")], () => true).map((r: SessionRow) => r.id)
  // Three levels is one more than the app dispatches to: the deepest link goes.
  assert.deepEqual(deep, ["a", "b", "c", "d"])
})
