import type {
  ActionResult,
  BoardSnapshot,
  BoardWriteResult,
  CoordinatorSnapshot,
  Health,
  Inventory,
  ObligationList,
  ScheduleList,
  ScheduleRequest,
  ScheduleSaved,
  SessionsSnapshot,
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
   */
  close(id: string, force = false): Promise<ActionResult> {
    return this.post(sessionRoutes.close(id), { force })
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

  private post<T>(path: string, body: unknown): Promise<T> {
    return this.request<T>(path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
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
