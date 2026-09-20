/*
 * The session list's order.
 *
 * `legacy/js/view/derive.js` `ordered()`, restated with one rule added: inside
 * one state, the session that moved most recently comes first. Everything
 * else is the original's, rule for rule — Clawdfather first, then waiting,
 * working, idle and unknown (`RANK`), then the title, then the id; the filter
 * over label, folder, tty and backend; the order held while a pointer is over
 * the list and let go the moment the set of waiting sessions changes; and a
 * dispatched child placed under the session that asked for it (`grouped`).
 *
 * **When a session last moved is the daemon's answer, not this page's.** It
 * used to be a clock each browser kept in its own `localStorage`, watching the
 * rows change: a phone and a Mac reading the same daemon put the same sessions
 * in different orders, and a browser that had never seen a row had no time for
 * it at all. The row now carries `activity` — the moment the session's own
 * conversation record last grew — so every device sorts the same list the same
 * way and a browser opened for the first time is not starting over.
 *
 * Restated rather than wrapped, because the copied function has no seam for a
 * comparator and `derive.js` stays byte for byte (`tools/check-legacy-css.sh`).
 * Every function here takes what it reads as arguments and nothing is imported
 * at run time, so `node --test` loads this file as it is. The seam that feeds
 * it the copied modules' state is `legacy/order-bridge.ts`.
 */
import type { SessionRow, TaskRow } from "@clawdline/contract"

/** `RANK`: waiting first, always. The list exists to answer "which one stopped and wants me". */
const RANK: Readonly<Record<string, number>> = { waiting: 0, working: 1, idle: 2, unknown: 3 }

/** `rankOf`: a state the table does not know sorts after all of them. */
export function rankOf(row: Pick<SessionRow, "state">): number {
  return Object.hasOwn(RANK, row.state) ? RANK[row.state] : 9
}

/** `coordinatorSession`: the Clawdfather row carries an object, and nothing else does. */
function isCoordinator(row: Pick<SessionRow, "coordinator">): boolean {
  const value: unknown = row.coordinator
  return !!value && typeof value === "object" && !Array.isArray(value)
}

/** `coordinatorFirst`. */
function coordinatorFirst(a: SessionRow, b: SessionRow): number {
  return (isCoordinator(a) ? 0 : 1) - (isCoordinator(b) ? 0 : 1)
}

/**
 * When a row last moved, in Unix seconds, and `null` when the daemon could not
 * say.
 *
 * `null` is not a very old time and is never turned into one. A row the daemon
 * could not read — its record missing, unreadable, or past the reading's own
 * bound (`unknown_reason`) — would be buried at the bottom of the list by a
 * failure nobody was told about, and the bottom of the list is where a row
 * stops being looked at. A row with no `activity` key at all is the same
 * answer: a daemon too old to send one has told this page nothing.
 */
export function movedAt(row: SessionRow): number | null {
  const a = row.activity
  if (!a || !a.known || typeof a.at !== "number") return null
  return a.at
}

/** Rows whose time could not be read sort ahead of every row that has one. */
function movedBand(row: SessionRow): number {
  return movedAt(row) === null ? 0 : 1
}

/**
 * Clawdfather → state → unknown-before-known → most recent movement → title → id.
 *
 * The time is compared only between rows in the same state, so a session that
 * moved a second ago never climbs over one that is waiting for somebody. The
 * title is still what separates two rows that moved together — every working
 * session, which is moving right now — so those keep the steady order they
 * had before.
 *
 * **A row with no readable time goes to the top of its state, not the
 * bottom.** Of the two ways to be wrong about a row nobody could read, only
 * "it has been quiet for a week" hides the row somebody has to act on, and the
 * commonest reason for it — a session that has written no record yet — is a
 * tab somebody opened a moment ago and is looking for. Those rows keep their
 * own order by title.
 */
export function compareSessions(a: SessionRow, b: SessionRow): number {
  const at = movedAt(a)
  const bt = movedAt(b)
  return (
    coordinatorFirst(a, b) ||
    rankOf(a) - rankOf(b) ||
    movedBand(a) - movedBand(b) ||
    (at === null || bt === null ? 0 : bt - at) ||
    (a.label || "").localeCompare(b.label || "") ||
    (a.id < b.id ? -1 : a.id > b.id ? 1 : 0)
  )
}

/** The list's filter box: label, folder, tty and backend, as one lower-cased line. */
export function matchesFilter(row: SessionRow, filter: string): boolean {
  const q = filter.trim().toLowerCase()
  if (!q) return true
  return `${row.label || ""} ${row.cwd || ""} ${row.tty || ""} ${row.backend || ""}`.toLowerCase().includes(q)
}

/** The order a pointer froze, and the waiting set it froze beside. */
export interface OrderHold {
  order: string[]
  waiting: string
}

/**
 * `waitingKey`: the waiting sessions, as one comparable string. A hold is given
 * up when this changes — a new question outranks a steady list.
 */
export function waitingKey(rows: readonly SessionRow[]): string {
  return rows
    .filter((row) => row.state === "waiting")
    .map((row) => row.id)
    .sort()
    .join(",")
}

export interface Arrangement {
  sessions: readonly SessionRow[]
  filter: string
  tasks: readonly TaskRow[]
  /** `taskShaping`: whether a task still decides where its child's row sits. */
  shaping: (task: TaskRow) => boolean
  /** A hold that is still current; the caller drops a stale one (`waitingKey`). */
  hold: OrderHold | null
}

/**
 * `ordered()`: the rows the list draws, in the order it draws them.
 *
 * Under a hold the frozen position decides, and a row that was not there when
 * it froze goes after every row that was, by the ordinary rule.
 */
export function arrangeSessions(a: Arrangement): SessionRow[] {
  const list = a.sessions.filter((row) => matchesFilter(row, a.filter))
  const hold = a.hold
  if (hold) {
    const at = new Map(hold.order.map((id, i) => [id, i]))
    list.sort(
      (x, y) => coordinatorFirst(x, y) || (at.get(x.id) ?? 1e9) - (at.get(y.id) ?? 1e9) || compareSessions(x, y),
    )
  } else {
    list.sort((x, y) => compareSessions(x, y))
  }
  return groupUnderRoots(list, a.tasks, a.shaping)
}

/**
 * `grouped`: each dispatched child placed directly under the session that
 * asked for it, children in the order the list already has them.
 *
 * Two levels, the same floor the app dispatches to. A link that would put a
 * row deeper than that, or that leads round in a circle, is dropped and the row
 * stands on its own. If the count of rows moved, the grouping was wrong about
 * something and the ungrouped list is the honest answer.
 */
export function groupUnderRoots(
  list: SessionRow[],
  tasks: readonly TaskRow[],
  shaping: (task: TaskRow) => boolean,
): SessionRow[] {
  if (!tasks.length || list.length < 2) return list
  const here = new Map(list.map((row) => [row.id, row]))
  const childOf = new Map<string, string>()
  for (const t of tasks) {
    if (!shaping(t) || !t.child || !t.root) continue
    const kid = t.child.terminalId
    const root = t.root.terminalId
    if (!kid || !root || kid === root) continue
    const row = here.get(kid)
    if (!row || !here.has(root)) continue
    if (isCoordinator(row)) continue
    childOf.set(kid, root)
  }
  // Depths are read off the links as they stand while this walks, as the
  // original's are, so a broken chain costs its own rows their indent and
  // never anybody else's.
  for (const kid of [...childOf.keys()]) {
    let n = 0
    let at = kid
    const seen = new Set<string>()
    while (childOf.has(at) && n <= 2) {
      if (seen.has(at)) {
        n = 99
        break
      }
      seen.add(at)
      at = childOf.get(at)!
      n++
    }
    if (n > 2) childOf.delete(kid)
  }
  if (!childOf.size) return list

  const kids = new Map<string, SessionRow[]>()
  for (const row of list) {
    const root = childOf.get(row.id)
    if (!root) continue
    const under = kids.get(root)
    if (under) under.push(row)
    else kids.set(root, [row])
  }
  const out: SessionRow[] = []
  const place = (row: SessionRow) => {
    out.push(row)
    for (const kid of kids.get(row.id) ?? []) place(kid)
  }
  for (const row of list) if (!childOf.has(row.id)) place(row)
  return out.length === list.length ? out : list
}
