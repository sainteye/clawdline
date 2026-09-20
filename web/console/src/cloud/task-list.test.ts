// The dispatched-work hierarchy across the relay: `node --test web/console/src/cloud/*.test.ts`.
//
// One Mac snapshot, read twice: as a phone read it while this seam carried no
// task list, and as it reads it now. What is being proved is the thing the
// person saw — a flat list of sessions on app.clawdline.com where the console
// on the machine itself indents each child under the session that dispatched it.
//
// The rows in these fixtures are the machine's own projection, field for field
// (`internal/transport/cloud/tasklist.go` `cloudTaskFields`): nine paths, and
// no more. If that projection stopped carrying what the indent is computed
// from, these would fail here rather than on somebody's phone.
//
// The grouping itself is not re-stated: `arrangeSessions` is the list's own
// rule (`session/order.ts`) and `taskShaping` is the copied module the machine's
// console uses (`legacy/js/view/derive.js`), both called as the page calls
// them.
import { test } from "node:test"
import assert from "node:assert/strict"
import type { SessionRow, TaskList, TaskRow } from "@clawdline/contract"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { RelayReader, type CloudEvent, type CloudReadClient, type CloudRow } from "./relay-reader.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { arrangeSessions } from "../session/order.ts"
import { taskShaping } from "../legacy/js/view/derive.js"
import { S } from "../legacy/js/core/state.js"

/**
 * A machine as the copied `CloudClient` holds it: session rows from the `s/`
 * channel, and the `orch/` snapshot's task list, which `tasks()` answers out
 * of what has already been decrypted (`_allOrchestratorRows`).
 */
class FakeMac implements CloudReadClient {
  ready = true
  sessionInventoryByMachine = new Map<string, unknown>([["mac-a", {}]])
  rows: CloudRow[] = []
  /** Every machine's tasks, as the copied client merges them: the row plus which machine it came from. */
  taskRows: Record<string, unknown>[] = []
  /** How many times the page asked for the list at all. */
  asks = 0
  events(_listener: (event: CloudEvent) => void) {
    return () => undefined
  }
  async sessions() {
    return { sessions: this.rows, at: 100, scan: { emptyAuthoritative: true, recovering: [], failures: [] } }
  }
  transcript() {
    return Promise.resolve({ id: "s1", entries: [], signature: "1", evidence: "transcript" })
  }
  tasks() {
    this.asks += 1
    return Promise.resolve({ tasks: this.taskRows })
  }
}

function session(id: string, label: string, state = "idle"): CloudRow {
  return { id, machine: "mac-a", session: id, identity: { machine: "mac-a", session: id }, state, label, work_state: "ready" }
}

/** One task as the machine publishes it to a viewer: `cloudTaskFields`, and nothing else. */
function published(
  machine: string,
  id: string,
  root: string,
  child: string,
  state = "briefed",
  extra: Record<string, unknown> = {},
): Record<string, unknown> {
  return {
    machine,
    id,
    state,
    title: "task " + id,
    created: 1_700_000_000,
    root: { terminalId: root },
    child: { terminalId: child },
    usage: { total: 10 },
    ...extra,
  }
}

function seam(mac: FakeMac): RelayReader {
  const reader = new RelayReader("mac-a", { now: () => 1_000_000 })
  reader.attach(mac)
  return reader
}

/** The list as the page draws it, from the rows and whatever task list it was given. */
function drawn(rows: SessionRow[], tasks: TaskRow[]): string[] {
  // `taskShaping` reads the copied state for a finished task whose child tab is
  // still open, so the state is what the page publishes before it draws.
  ;(S as { sessions: SessionRow[] }).sessions = rows
  return arrangeSessions({
    sessions: rows,
    filter: "",
    tasks,
    shaping: taskShaping as (task: TaskRow) => boolean,
    hold: null,
  }).map((row: SessionRow) => row.id)
}

/** The rows, in the order the seam answers them. */
async function listOf(reader: RelayReader): Promise<SessionRow[]> {
  const res = await reader.fetch("/v1/sessions")
  return ((await res.json()) as { sessions: SessionRow[] }).sessions
}

test("the machine's published task list reaches the page, and no envelope is spent on it", async () => {
  const mac = new FakeMac()
  mac.rows = [session("%1", "root"), session("%2", "child")]
  mac.taskRows = [published("mac-a", "t1", "%1", "%2")]
  const reader = seam(mac)

  const res = await reader.fetch("/v1/orchestrator/tasks")
  assert.equal(res.status, 200, "the route the list is read from is carried")
  const list = (await res.json()) as TaskList
  assert.deepEqual(
    list.tasks.map((t) => [t.id, t.root?.terminalId, t.child?.terminalId]),
    [["t1", "%1", "%2"]],
  )
  // The answer is built from what this page already holds, so it is the
  // machine's own words and nothing was asked of it: the seam says `local`,
  // as it does for the session list.
  const row = reader.log[reader.log.length - 1]
  assert.equal(row.path, "/v1/orchestrator/tasks")
  assert.equal(row.answer, "local")
  assert.equal(row.code, undefined)
  // And it says how the rows were cut, because they are not the route's rows.
  assert.equal(list.page.fields, "cloud")
  assert.equal(list.store, "unknown", "this page has not read the machine's store and does not say it has")
})

test("the same snapshot draws flat without the task list and indented with it", async () => {
  const mac = new FakeMac()
  // Ordinary order puts the child last: waiting first, then by title. Only the
  // task list moves it up under the session that asked for it.
  mac.rows = [session("%9", "asking", "waiting"), session("%1", "aaa root"), session("%3", "mmm other"), session("%2", "zzz child")]
  mac.taskRows = [published("mac-a", "t1", "%1", "%2")]
  const reader = seam(mac)
  const rows = await listOf(reader)

  assert.deepEqual(drawn(rows, []), ["%9", "%1", "%3", "%2"], "with no task list the page draws a flat list — the phone's bug")

  const list = (await (await reader.fetch("/v1/orchestrator/tasks")).json()) as TaskList
  assert.deepEqual(drawn(rows, list.tasks), ["%9", "%1", "%2", "%3"], "the child sits under the session that dispatched it")
})

test("a finished task still holds its child's row while the tab is open, and a deeper chain does not", async () => {
  const mac = new FakeMac()
  mac.rows = [session("%1", "aaa root"), session("%3", "mmm other"), session("%2", "zzz child")]
  // Finished, with the child's tab still open: `taskShaping`'s second half,
  // which needs `finishedAt` and the child terminal — both in the projection.
  mac.taskRows = [published("mac-a", "t1", "%1", "%2", "success", { finishedAt: 1_700_000_100 })]
  const reader = seam(mac)
  const rows = await listOf(reader)
  const held = (await (await reader.fetch("/v1/orchestrator/tasks")).json()) as TaskList
  assert.deepEqual(drawn(rows, held.tasks), ["%1", "%2", "%3"])

  // Three deep is past the floor the app dispatches to, and the honest answer
  // is no grouping at all. Carrying the list does not relax that.
  mac.rows = [session("%1", "aaa root"), session("%2", "bbb child"), session("%3", "ccc grandchild")]
  mac.taskRows = [published("mac-a", "t1", "%1", "%2"), published("mac-a", "t2", "%2", "%3")]
  const deep = seam(mac)
  const deepRows = await listOf(deep)
  const deepList = (await (await deep.fetch("/v1/orchestrator/tasks")).json()) as TaskList
  assert.deepEqual(drawn(deepRows, deepList.tasks), ["%1", "%2", "%3"], "the rows stand where they were, ungrouped")
})

test("another machine's tasks never shape this machine's list", async () => {
  const mac = new FakeMac()
  mac.rows = [session("%1", "aaa root"), session("%3", "mmm other"), session("%2", "zzz child")]
  // A terminal id is a tmux pane name and two machines both have a `%1`. A task
  // published by the machine this page is not reading must not move a row here.
  mac.taskRows = [published("mac-b", "t9", "%1", "%2")]
  const reader = seam(mac)
  const rows = await listOf(reader)
  const list = (await (await reader.fetch("/v1/orchestrator/tasks")).json()) as TaskList
  assert.deepEqual(list.tasks, [], "the list is this machine's")
  assert.deepEqual(drawn(rows, list.tasks), ["%1", "%3", "%2"])
})
