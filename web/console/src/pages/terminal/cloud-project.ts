/**
 * Which Project the hosted terminal page is about, and how it reads the list
 * that says so.
 *
 * In Cloud, `GET /v1/places` is answered by the copied client
 * (`legacy/js/net/cloud-client.js` `_placesAnswer`), which rewrites every
 * machine-local id into `cloud.` + base64url(JSON [machine, local]). The page
 * address keeps that wrapped id; the machine's terminal channel
 * (`internal/transport/cloud/terminal.go` `list` / `open`) knows only the local
 * one. A route is accepted only when it names a row of that list on the
 * terminal host's own machine — never a bare local id, whose source the hosted
 * page cannot verify.
 */

export interface CloudPlaceRow { id: string; path?: string; label?: string }

/** The address names no Project; the page asks for one at once. */
export type CloudTerminalProject =
  | { kind: "choose" }
  | { kind: "unknown" }
  | { kind: "found"; page: string; local: string; label: string }

/** `[machine, local]` from a wrapped Cloud id, or null for anything else. */
export function decodeCloudPlaceID(id: string): [string, string] | null {
  if (!id.startsWith("cloud.")) return null
  const body = id.slice("cloud.".length)
  if (!/^[A-Za-z0-9_-]+$/.test(body)) return null
  try {
    const raw = atob(body.replace(/-/g, "+").replace(/_/g, "/") + "=".repeat((4 - body.length % 4) % 4))
    const bytes = Uint8Array.from(raw, (char) => char.charCodeAt(0))
    const value: unknown = JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(bytes))
    if (!Array.isArray(value) || value.length !== 2) return null
    const [machine, local] = value
    if (typeof machine !== "string" || !machine || typeof local !== "string" || !local) return null
    return [machine, local]
  } catch {
    // refusal-ok: an id that does not decode is not a Cloud Project id; the caller says "unknown".
    return null
  }
}

/** A wrapped Cloud Project keeps its own machine even while another console is selected. */
export function cloudTerminalMachine(routeProject: string, selectedMachine: string): string {
  return decodeCloudPlaceID(routeProject)?.[0] ?? selectedMachine
}

/** The route's Project resolved against the Cloud list, on the terminal host's machine. */
export function resolveCloudTerminalProject(routeProject: string, places: readonly CloudPlaceRow[], machine: string): CloudTerminalProject {
  const wanted = routeProject.trim()
  if (!wanted) return { kind: "choose" }
  const mine = (row: CloudPlaceRow) => {
    const pair = decodeCloudPlaceID(row.id)
    return pair && pair[0] === machine ? pair[1] : ""
  }
  const byID = places.find((row) => row.id === wanted)
  if (byID) {
    const local = mine(byID)
    return local ? { kind: "found", page: byID.id, local, label: byID.label ?? "" } : { kind: "unknown" }
  }
  // The Projects page links by folder; a folder is a Project only when exactly
  // one listed row of this machine has it.
  const byPath = places.filter((row) => row.path === wanted && mine(row))
  if (byPath.length !== 1) return { kind: "unknown" }
  const row = byPath[0]
  return { kind: "found", page: row.id, local: mine(row), label: row.label ?? "" }
}

/** Failures that mean this page's own Cloud line is not there right now. */
const RECONNECTING = new Set(["cloud_reconnecting", "offline", "socket_error", "cloud_starting"])

/** Why the Project read failed: `temporary` is this page's connection, anything else carries its code. */
export function projectReadFailure(error: unknown): { temporary: boolean; code: string } {
  const e = error as { name?: unknown; code?: unknown; cause?: unknown } | null
  if (error instanceof TypeError || e?.cause instanceof TypeError) return { temporary: true, code: "network" }
  // A transport failure that is not the network (an answer that is not JSON, a
  // timeout) names itself; its message is a sentence, not a code.
  if (e?.name === "TransportError") return { temporary: false, code: "transport" }
  const code = typeof e?.code === "string" && e.code ? e.code : error instanceof Error && error.message ? error.message : "failed"
  return { temporary: RECONNECTING.has(code), code }
}

/**
 * How long to wait before each automatic re-read, in order. After the last one
 * the page stops and leaves Try again; the terminal host coming back still
 * re-reads at once. A Console-side bound: `TestEveryBoundIsRegistered` scans
 * Go under `internal/` and `cmd/` only.
 */
export const PROJECT_RETRY_MS: readonly number[] = [1_000, 2_000, 4_000, 8_000, 16_000, 30_000]

/** The wait before automatic attempt `n` (0-based), or null once the bound is spent. */
export function projectRetryDelay(attempt: number): number | null {
  return attempt >= 0 && attempt < PROJECT_RETRY_MS.length ? PROJECT_RETRY_MS[attempt] : null
}

export type ProjectReadState<Page> =
  | { state: "loading" }
  | { state: "ready"; page: Page }
  | { state: "failed"; temporary: boolean; code: string; retryInMs: number | null }

export interface RetryClock { after(ms: number, run: () => void): () => void }

/**
 * Reads the Project list, retries on the PROJECT_RETRY_MS schedule, and reads
 * again at once when the terminal host returns. One instance per route; the
 * page disposes it on unmount or route change, which cancels any waiting retry
 * and drops any answer still in flight.
 */
export class CloudProjectReader<Page> {
  private attempt = 0
  private generation = 0
  private cancel: (() => void) | null = null
  private current: ProjectReadState<Page> = { state: "loading" }
  private seenHost: unknown = undefined
  private disposed = false

  private readonly read: () => Promise<Page>
  private readonly onState: (state: ProjectReadState<Page>) => void
  private readonly clock: RetryClock

  constructor(read: () => Promise<Page>, onState: (state: ProjectReadState<Page>) => void,
    clock: RetryClock = { after: (ms, run) => { const t = setTimeout(run, ms); return () => clearTimeout(t) } }) {
    this.read = read
    this.onState = onState
    this.clock = clock
  }

  get state(): ProjectReadState<Page> { return this.current }

  start(): void { this.now(false) }

  /** Try again by hand: a fresh schedule. */
  retry(): void { this.now(true) }

  /**
   * The terminal host as `watchTerminalHost` delivers it. A host that comes
   * back — after an absence, or as a re-attached client replacing the old one —
   * re-reads at once unless the list is already in hand.
   */
  host(current: object | null): void {
    const returned = current !== null && this.seenHost !== undefined && current !== this.seenHost
    this.seenHost = current
    if (returned && this.current.state !== "ready") this.now(true)
  }

  dispose(): void {
    this.disposed = true
    this.generation++
    this.cancel?.(); this.cancel = null
  }

  private set(state: ProjectReadState<Page>): void {
    this.current = state
    if (!this.disposed) this.onState(state)
  }

  private now(fresh: boolean): void {
    if (this.disposed) return
    if (fresh) this.attempt = 0
    this.cancel?.(); this.cancel = null
    const generation = ++this.generation
    this.set({ state: "loading" })
    this.read().then(
      (page) => { if (generation === this.generation) { this.attempt = 0; this.set({ state: "ready", page }) } },
      (error) => {
        if (generation !== this.generation) return
        const why = projectReadFailure(error)
        const wait = projectRetryDelay(this.attempt)
        if (wait !== null) {
          this.attempt++
          this.cancel = this.clock.after(wait, () => { this.cancel = null; this.now(false) })
        }
        this.set({ state: "failed", ...why, retryInMs: wait })
      },
    )
  }
}
