import type * as W from "@clawdline/contract"

// The console side of the daemon's one model catalog (docs/model-catalog.md).
// Every screen that offers a model for an agent reads it through this reader,
// for one machine, locally or over Cloud: the source decides the road.

/** What a machine source must offer for the catalog; SourceClient implements it. */
export interface ModelCatalogSource {
  readonly machineID: string
  /** False when the machine's daemon predates the catalog (API 120). */
  canModelCatalog?(): boolean
  modelCatalog?(agent: string | undefined, signal: AbortSignal): Promise<W.ModelCatalogAnswer>
}

/** One agent's list as a picker draws it. */
export type AgentModels =
  | { status: "loading" }
  /** waiting is true while the daemon is still asking the agent and the reader polls. */
  | { status: "ready"; catalog: W.ModelCatalogAgent; waiting: boolean }
  | { status: "failed"; code: string }
  /** The machine cannot list models: offer only the agent default, with a note. */
  | { status: "unsupported" }

export const MODEL_CATALOG_POLL_MS = 1500
// A discovery is bounded on the machine (90 s); the reader stops a little after.
export const MODEL_CATALOG_MAX_POLLS = 64

type Timers = { set(run: () => void, ms: number): unknown; clear(handle: unknown): void }
const realTimers: Timers = { set: (run, ms) => setTimeout(run, ms), clear: handle => clearTimeout(handle as ReturnType<typeof setTimeout>) }

function codeOf(error: unknown): string {
  const e = error as { error?: { code?: unknown }; code?: unknown } | null
  const code = e?.error?.code ?? e?.code
  return typeof code === "string" && code ? code : "unavailable"
}

/**
 * Reads every agent's catalog for one source in one request, and polls while
 * any agent is still being asked. A failed read keeps what was read before.
 */
export class ModelCatalogReader {
  private agents = new Map<string, W.ModelCatalogAgent>()
  private problem: AgentModels | undefined = undefined
  private polls = 0
  private timer: unknown = undefined
  private control = new AbortController()
  private stopped = false

  private readonly source: ModelCatalogSource | undefined
  private readonly changed: () => void
  private readonly timers: Timers

  constructor(source: ModelCatalogSource | undefined, changed: () => void, timers: Timers = realTimers) {
    this.source = source
    this.changed = changed
    this.timers = timers
  }

  start() { void this.read() }

  stop() {
    this.stopped = true
    this.control.abort()
    if (this.timer !== undefined) this.timers.clear(this.timer)
  }

  /** Asks again now, after a failure or when a person presses Retry. */
  retry() {
    if (this.stopped) return
    if (this.timer !== undefined) this.timers.clear(this.timer)
    this.timer = undefined
    this.polls = 0
    this.problem = undefined
    this.changed()
    void this.read()
  }

  get(agent: string): AgentModels {
    const catalog = this.agents.get(agent)
    if (catalog) return { status: "ready", catalog, waiting: catalog.refreshing && this.polls < MODEL_CATALOG_MAX_POLLS }
    return this.problem ?? { status: "loading" }
  }

  private async read() {
    const source = this.source
    if (!source?.modelCatalog || source.canModelCatalog?.() === false) {
      this.problem = { status: "unsupported" }
      this.changed()
      return
    }
    this.control.abort()
    const control = this.control = new AbortController()
    try {
      const answer = await source.modelCatalog(undefined, control.signal)
      if (this.stopped || control.signal.aborted) return
      if (!answer || !Array.isArray(answer.agents)) throw { code: "pa_invalid" }
      for (const agent of answer.agents) this.agents.set(agent.agent, agent)
      this.problem = undefined
    } catch (error) {
      if (this.stopped || control.signal.aborted) return
      const code = codeOf(error)
      this.problem = code === "pa_unsupported" || code === "cloud_not_carried" ? { status: "unsupported" } : { status: "failed", code }
    }
    this.changed()
    const refreshing = [...this.agents.values()].some(a => a.refreshing)
    if (refreshing && !this.problem && this.polls < MODEL_CATALOG_MAX_POLLS) {
      this.polls++
      this.timer = this.timers.set(() => { this.timer = undefined; void this.read() }, MODEL_CATALOG_POLL_MS)
    }
  }
}
