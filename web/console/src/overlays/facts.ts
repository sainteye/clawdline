import type { SessionInfo } from "@clawdline/contract"
import { RefusalError, TransportError, isRefusal } from "@clawdline/core"
import { client } from "../client.js"

/**
 * `SessionFacts` (`input/status-line.js`, built by `createTieredSessionFacts`
 * in `session/transcript-requests.js`): one small cache in front of the info
 * read, shared by the status line and the Session info card, so opening the
 * card right after the status line has read does not read the same transcript
 * twice.
 *
 * Two tiers, as there: the status line asks for `?parts=summary`, the card for
 * the whole answer, and a fresh whole answer satisfies either. An answer is
 * fresh for a minute; `drop` and `force` throw it away. Each id has a
 * generation, so a read that was overtaken by a `drop` or a `receiveFull` does
 * not land on top of the newer one.
 *
 * The machine-wide plan windows the original overlays onto each answer are not
 * here: this daemon's answer carries no `limits`.
 */
const TTL = 60_000

type Tier = "full" | "summary"
interface Held {
  generation: number
  data: SessionInfo | null
  at: number
}
interface Pending {
  promise: Promise<SessionInfo | null>
}
interface State {
  generation: number
  full: Held | null
  summary: Held | null
  fullPending: Pending | null
  summaryPending: Pending | null
}

const states = new Map<string, State>()
const listeners = new Set<(id: string, data: SessionInfo) => void>()

function stateFor(id: string): State {
  let state = states.get(id)
  if (!state) {
    state = { generation: 0, full: null, summary: null, fullPending: null, summaryPending: null }
    states.set(id, state)
  }
  return state
}

const fresh = (entry: Held | null) => !!entry && Date.now() - entry.at < TTL

function invalidate(state: State, clear: boolean): number {
  state.generation += 1
  state.fullPending = null
  state.summaryPending = null
  if (clear) {
    state.full = null
    state.summary = null
  }
  return state.generation
}

/**
 * The read itself. A refusal comes back as the daemon's typed error, so the
 * card's sentence can be chosen by its code; anything else is a transport
 * failure, which is a different fact.
 */
async function fetchInfo(id: string, tier: Tier): Promise<SessionInfo | null> {
  const path = `/v1/sessions/${encodeURIComponent(id)}/info${tier === "summary" ? "?parts=summary" : ""}`
  let res: Response
  try {
    res = await fetch(client.url(path))
  } catch (err) {
    throw new TransportError(`${path} could not be reached`, err)
  }
  let parsed: unknown = null
  try {
    parsed = await res.json()
  } catch {
    parsed = null
  }
  if (!res.ok) {
    if (isRefusal(parsed)) throw new RefusalError(res.status, parsed, path)
    throw new TransportError(`${path} answered ${res.status} with no refusal in it`)
  }
  return (parsed as { info?: SessionInfo } | null)?.info ?? null
}

function read(id: string, tier: Tier, force: boolean): Promise<SessionInfo | null> {
  if (!id) return Promise.resolve(null)
  const state = stateFor(id)
  if (force) invalidate(state, true)
  if (!force && fresh(state.full)) return Promise.resolve(state.full!.data)
  if (tier === "summary" && !force && fresh(state.summary)) return Promise.resolve(state.summary!.data)
  const key = tier === "full" ? "fullPending" : "summaryPending"
  const out = state[key]
  if (out) return out.promise
  const generation = state.generation
  const record: Pending = { promise: Promise.resolve(null) }
  record.promise = Promise.resolve()
    .then(() => fetchInfo(id, tier))
    .then(
      (data) => {
        if (state.generation !== generation || state[key] !== record) return null
        state[key] = null
        if (tier === "full") {
          state.full = { generation, data, at: Date.now() }
          state.summary = null
        } else {
          // A summary never downgrades a full answer while that answer is still
          // the better one; past its minute it is neither.
          if (state.full && state.full.generation === generation && !fresh(state.full)) state.full = null
          if (!state.full || state.full.generation !== generation) {
            state.summary = { generation, data, at: Date.now() }
          }
        }
        return tier === "summary" && state.full ? state.full.data : data
      },
      (error) => {
        if (state.generation === generation && state[key] === record) state[key] = null
        throw error
      },
    )
  state[key] = record
  return record.promise
}

export const SessionFacts = {
  peek(id: string | null): SessionInfo | null {
    const state = id ? states.get(id) : undefined
    const held = state && (state.full || state.summary)
    return held ? held.data : null
  },
  drop(id: string | null): void {
    if (id) invalidate(stateFor(id), true)
  },
  get(id: string, force = false): Promise<SessionInfo | null> {
    return read(id, "full", force)
  },
  getSummary(id: string, force = false): Promise<SessionInfo | null> {
    return read(id, "summary", force)
  },
  /**
   * `StatusLine.receive`: the card read the whole answer, so the status line
   * shows it too rather than being the last to know.
   */
  receiveFull(id: string, data: SessionInfo | null): SessionInfo | null {
    if (!id || !data) return data
    const state = stateFor(id)
    const generation = invalidate(state, false)
    state.full = { generation, data, at: Date.now() }
    state.summary = null
    for (const listen of listeners) listen(id, data)
    return data
  },
  /** Told of every `receiveFull`. Returns the way to stop listening. */
  subscribe(listen: (id: string, data: SessionInfo) => void): () => void {
    listeners.add(listen)
    return () => {
      listeners.delete(listen)
    }
  },
}
