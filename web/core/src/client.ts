import type {
  ActionResult,
  BoardSnapshot,
  BoardWriteResult,
  CoordinatorSnapshot,
  DispatchRequest,
  DispatchResult,
  Health,
  Inventory,
  ObligationList,
  ScheduleList,
  ScheduleRequest,
  ScheduleSaved,
  SessionsSnapshot,
  SettleResult,
  TranscriptPage,
  UsageReport,
} from "@clawdline/contract"
import { RefusalError, TransportError, isRefusal } from "./refusal.js"
import { routes, sessionRoutes } from "./routes.js"

/**
 * What this package needs from its host. `fetch` is the whole list, and both a
 * browser and React Native have it, which is why the client works on either
 * without a platform branch.
 */
export interface ClientOptions {
  baseUrl?: string
  fetch?: typeof globalThis.fetch
  /** Milliseconds before a request is abandoned. A hung read is a failure. */
  timeoutMs?: number
}

export class ClawdlineClient {
  private readonly baseUrl: string
  private readonly doFetch: typeof globalThis.fetch
  private readonly timeoutMs: number

  constructor(opts: ClientOptions = {}) {
    this.baseUrl = (opts.baseUrl ?? "").replace(/\/+$/, "")
    this.doFetch = opts.fetch ?? globalThis.fetch.bind(globalThis)
    this.timeoutMs = opts.timeoutMs ?? 10_000
  }

  url(path: string): string {
    return `${this.baseUrl}${path}`
  }

  health(): Promise<Health> {
    return this.get(routes.health)
  }
  sessions(): Promise<SessionsSnapshot> {
    return this.get(routes.sessions)
  }
  inventory(): Promise<Inventory> {
    return this.get(routes.inventory)
  }
  obligations(): Promise<ObligationList> {
    return this.get(routes.obligations)
  }
  tasks(): Promise<import("@clawdline/contract").TaskList> {
    return this.get(routes.tasks)
  }
  board(): Promise<BoardSnapshot> {
    return this.get(routes.board)
  }
  schedules(): Promise<ScheduleList> {
    return this.get(routes.schedules)
  }
  coordinator(): Promise<CoordinatorSnapshot> {
    return this.get(routes.coordinator)
  }
  usage(): Promise<UsageReport> {
    return this.get(routes.usage)
  }

  transcript(id: string, limit = 40): Promise<TranscriptPage> {
    return this.get(`${routes.transcript}?session=${encodeURIComponent(id)}&limit=${limit}`)
  }

  strings(): Promise<Record<string, string>> {
    return this.get(routes.strings)
  }

  /**
   * Types one line into a session and submits it.
   *
   * A resolved promise means the bytes reached the tty. It does not mean the
   * assistant read them — that is a separate fact, and the fleet list is where
   * it is answered. Nothing built on this may report it as delivery.
   */
  send(id: string, text: string): Promise<ActionResult> {
    return this.post(sessionRoutes.send(id), { text })
  }

  interrupt(id: string): Promise<ActionResult> {
    return this.post(sessionRoutes.interrupt(id), {})
  }

  /**
   * Takes a session away.
   *
   * Rejects with a RefusalError carrying `close_blocked` when something is
   * still owed; the reasons come back on the error so a caller can show them
   * rather than only that it refused.
   *
   * `request` is the decision's own id, and it becomes the `Idempotency-Key`.
   * One decision is one key however many times it is asked: the daemon answers
   * a retry under it with the first answer instead of climbing the close ladder
   * a second time (`sessionWrite`, internal/transport/http/actions.go), and so
   * does a Mac read across Clawdline Cloud (`cloud/relay-writer.ts`, `end`).
   * Without one, an answer lost on a phone's connection leaves the caller with
   * a choice between never knowing and closing something twice.
   */
  close(id: string, force = false, request?: string): Promise<ActionResult> {
    return this.post(sessionRoutes.close(id), { force }, request ? { "Idempotency-Key": request } : undefined)
  }

  /**
   * Dispatches one task.
   *
   * `claims` absent and `claims: []` are different requests and this does not
   * flatten them: an undeclared dispatch cannot be arbitrated against another
   * root, so the daemon refuses it rather than assuming none.
   */
  dispatch(body: DispatchRequest): Promise<DispatchResult> {
    return this.post(routes.tasks, body)
  }

  settle(id: string): Promise<SettleResult> {
    return this.post(`${routes.tasks}/${encodeURIComponent(id)}/settle`, {})
  }

  saveSchedule(body: ScheduleRequest): Promise<ScheduleSaved> {
    return this.post(routes.schedules, body)
  }
  writeBoard(body: unknown): Promise<BoardWriteResult> {
    return this.post(routes.board, body)
  }

  private get<T>(path: string): Promise<T> {
    return this.request<T>(path, { method: "GET" })
  }

  private post<T>(path: string, body: unknown, headers?: Record<string, string>): Promise<T> {
    return this.request<T>(path, {
      method: "POST",
      headers: { "Content-Type": "application/json", ...headers },
      body: JSON.stringify(body),
    })
  }

  private async request<T>(path: string, init: RequestInit): Promise<T> {
    const controller = new AbortController()
    const timer = setTimeout(() => controller.abort(), this.timeoutMs)
    let res: Response
    try {
      res = await this.doFetch(this.url(path), { ...init, signal: controller.signal })
    } catch (cause) {
      throw new TransportError(`${init.method ?? "GET"} ${path} did not complete`, cause)
    } finally {
      clearTimeout(timer)
    }

    const text = await res.text()
    let parsed: unknown
    try {
      parsed = text.length > 0 ? JSON.parse(text) : null
    } catch (cause) {
      throw new TransportError(`${path} answered with something that is not JSON`, cause)
    }

    // A refusal is recognised by its shape, not by the status code. The daemon
    // uses several codes for refusals and one of them is 200-adjacent enough
    // that keying off status alone would let one through as data.
    if (!res.ok) {
      if (isRefusal(parsed)) throw new RefusalError(res.status, parsed, path)
      throw new TransportError(`${path} answered ${res.status} with no refusal in it`)
    }
    return parsed as T
  }
}
