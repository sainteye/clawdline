import type { SessionsSnapshot, SessionRow, TaskList, WorkState } from "@clawdline/contract"
import type { ClawdlineClient } from "./client.js"
import { routes } from "./routes.js"
import type { StreamHandle, StreamTransport } from "./stream.js"
import { taskListFrame } from "./task-frame.js"
import { acceptSnapshot } from "./fleet-accept.js"

export { taskListFrame }

/**
 * What a reader knows about the fleet, including what it does not know.
 *
 * `snapshot` being null and `snapshot.sessions` being empty are different
 * states and are kept apart here, because the whole point of the scan payload
 * is that an empty list is only meaningful when the reading was complete.
 */
export interface FleetState {
  snapshot: SessionsSnapshot | null
  /** True once a reading has arrived, whatever it contained. */
  loaded: boolean
  /** Set when the last attempt failed. Cleared by the next success. */
  error: string | null
  /** Whether the live connection is currently up. */
  live: boolean
  /**
   * The task list the stream's `orchestrator` frame carried, or null when none
   * has arrived on this connection. The daemon sends one as the stream opens
   * and again whenever a task moves (`ownEvents`), so while `tasksLive` holds,
   * this is as current as `GET /v1/orchestrator/tasks` and a page need not ask.
   */
  tasks: TaskList | null
  /**
   * An `orchestrator` frame arrived since the connection last opened, and the
   * connection has not dropped since. A host whose stream carries no such frame
   * — the relay, the quiet reads while a terminal holds the connection, a
   * daemon that never sent one — leaves this false, and a page reads the list
   * itself as it always did.
   */
  tasksLive: boolean
}

export type FleetListener = (state: FleetState) => void

/**
 * Holds one fleet reading and keeps it current.
 *
 * This is deliberately framework-free. React subscribes to it with
 * useSyncExternalStore and React Native can subscribe to it the same way; if
 * this held hooks, the phone app would need its own copy of the rules about
 * when a snapshot may be replaced.
 */
export class FleetStore {
  private state: FleetState = { snapshot: null, loaded: false, error: null, live: false, tasks: null, tasksLive: false }
  private listeners = new Set<FleetListener>()
  private handle: StreamHandle | undefined
  private run = 0
  private titles = new Map<string, { title: string; conversation: string | undefined; execution: string | undefined }>()

  constructor(
    private readonly client: ClawdlineClient,
    private readonly transport?: StreamTransport,
  ) {}

  get(): FleetState {
    return this.state
  }

  subscribe(fn: FleetListener): () => void {
    this.listeners.add(fn)
    return () => this.listeners.delete(fn)
  }

  /** Starts the initial read and the live connection independently. */
  async start(): Promise<void> {
    const run = ++this.run
    const previous = this.state.snapshot
    const read = this.client.sessions(true).then(
      snapshot => ({ snapshot, failure: null }),
      cause => ({ snapshot: null, failure: { cause } }),
    )
    // The first Cloud read assembles the retained rows into one list. Keep
    // that first paint while allowing connection and task events through.
    const initial: { frame?: SessionsSnapshot | null } = { frame: null }
    this.handle?.close()
    this.handle = this.transport?.open(this.client.url(routes.events), {
      // A reopened connection has not yet said what the tasks are: until its
      // first `orchestrator` frame, the list held is from the old one.
      onOpen: () => { if (run === this.run) this.set({ live: true, tasksLive: false }) },
      onError: () => { if (run === this.run) this.set({ live: false, tasksLive: false }) },
      onFrame: (event, data) => {
        if (run !== this.run) return
        if (event === "orchestrator") {
          const tasks = taskListFrame(data)
          if (tasks) this.set({ tasks, tasksLive: true })
          return
        }
        if (event !== "sessions" && event !== "message") return
        try {
          const next = JSON.parse(data) as SessionsSnapshot
          if (!next?.scan || !Array.isArray(next.sessions)) return
          if (initial.frame === undefined) this.accept(next)
          else initial.frame = acceptSnapshot(initial.frame, next) ?? initial.frame
        } catch {
          // A frame that will not parse is dropped, not treated as an empty
          // fleet. Half a message is not news that everything went away.
        }
      },
    })
    const { snapshot, failure } = await read
    const buffered = initial.frame
    initial.frame = undefined
    if (run !== this.run) return
    try {
      // A stream reopen can race a body from the previous daemon. Its start
      // time (epoch) orders the processes; acceptSnapshot orders their reads.
      const next = buffered && (!snapshot || buffered.scan.epoch >= snapshot.scan.epoch)
        ? acceptSnapshot(snapshot, buffered) ?? snapshot
        : snapshot
      const current = this.state.snapshot
      if (next && (!current || current === previous || next.scan.epoch >= current.scan.epoch)) this.accept(next)
      else if (failure && current === previous) this.readFailed(failure.cause)
    } catch (err) {
      this.readFailed(err)
    }
  }

  stop(): void {
    this.run++
    this.handle?.close()
    this.handle = undefined
    this.set({ live: false, tasksLive: false })
  }

  async refresh(initial = false): Promise<void> {
    try {
      this.accept(await this.client.sessions(initial))
    } catch (err) {
      this.readFailed(err)
    }
  }

  private readFailed(err: unknown): void {
    this.set({ error: err instanceof Error ? err.message : String(err), loaded: true })
  }

  /** A successful write is newer than the producer's next, possibly held list. */
  titleSaved(id: string, title: string | undefined): void {
    const snapshot = this.state.snapshot
    const row = snapshot?.sessions.find(row => row.id === id)
    if (title === undefined || !snapshot || !row) return
    this.titles.set(id, { title, conversation: row.sessionId, execution: row.execution_generation })
    this.set({ snapshot: { ...snapshot, sessions: snapshot.sessions.map(row => row.id === id ? { ...row, label: title } : row) } })
  }

  /** Takes a snapshot, unless it is older than the one already held (`fleet-accept.ts`). */
  private accept(next: SessionsSnapshot): void {
    const accepted = acceptSnapshot(this.state.snapshot, next)
    if (!accepted) return
    for (const [id, saved] of this.titles) {
      const row = accepted.sessions.find(row => row.id === id)
      if (!row || row.sessionId !== saved.conversation || row.execution_generation !== saved.execution ||
        next.sessions.some(incoming => incoming.id === id && incoming.label === saved.title))
        this.titles.delete(id)
    }
    const sessions = accepted.sessions.map(row => {
      const saved = this.titles.get(row.id)
      return saved ? { ...row, label: saved.title } : row
    })
    this.set({ snapshot: { ...accepted, sessions }, loaded: true, error: null })
  }

  private set(patch: Partial<FleetState>): void {
    this.state = { ...this.state, ...patch }
    for (const fn of this.listeners) fn(this.state)
  }
}

/**
 * The order the fleet list is shown in.
 *
 * What needs a person comes first, then what is running, then what is idle,
 * and `unknown` sits above idle rather than below it: a row nobody could read
 * is a question, not a resting state.
 */
const workRank: Record<WorkState, number> = {
  waiting_you: 0,
  milestone_complete: 1,
  waiting_session: 2,
  holding: 3,
  working: 4,
  unknown: 5,
  work_complete: 6,
  ready: 7,
}

export function sortSessions(rows: readonly SessionRow[]): SessionRow[] {
  return [...rows].sort((a, b) => {
    const d = workRank[a.work_state] - workRank[b.work_state]
    if (d !== 0) return d
    return (a.label || a.id).localeCompare(b.label || b.id)
  })
}

/** How many rows need a person right now. */
export function needsYou(rows: readonly SessionRow[]): number {
  return rows.filter((r) => r.work_state === "waiting_you" || r.work_state === "milestone_complete")
    .length
}
