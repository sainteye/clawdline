import type { SessionsSnapshot, SessionRow, WorkState } from "@clawdline/contract"
import type { ClawdlineClient } from "./client.js"
import { routes } from "./routes.js"
import type { StreamHandle, StreamTransport } from "./stream.js"

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
  private state: FleetState = { snapshot: null, loaded: false, error: null, live: false }
  private listeners = new Set<FleetListener>()
  private handle: StreamHandle | undefined

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

  /** Reads once, then follows the stream if this host has one. */
  async start(): Promise<void> {
    await this.refresh()
    if (!this.transport) return
    this.handle = this.transport.open(this.client.url(routes.events), {
      onOpen: () => this.set({ live: true }),
      onError: () => this.set({ live: false }),
      onFrame: (event, data) => {
        if (event !== "sessions" && event !== "message") return
        try {
          const next = JSON.parse(data) as SessionsSnapshot
          this.accept(next)
        } catch {
          // A frame that will not parse is dropped, not treated as an empty
          // fleet. Half a message is not news that everything went away.
        }
      },
    })
  }

  stop(): void {
    this.handle?.close()
    this.handle = undefined
    this.set({ live: false })
  }

  async refresh(): Promise<void> {
    try {
      this.accept(await this.client.sessions())
    } catch (err) {
      this.set({ error: err instanceof Error ? err.message : String(err), loaded: true })
    }
  }

  /**
   * Takes a snapshot, unless it is older than the one already held.
   *
   * Generation only orders readings from the same process, so epoch is checked
   * first: after a restart the counter starts over, and a client that compared
   * generations alone would refuse every reading from the new daemon.
   */
  private accept(next: SessionsSnapshot): void {
    const held = this.state.snapshot
    if (held && held.scan.epoch === next.scan.epoch && next.scan.generation < held.scan.generation) {
      return
    }
    this.set({ snapshot: next, loaded: true, error: null })
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
