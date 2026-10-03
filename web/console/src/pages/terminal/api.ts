import type {
  PlatformCapability,
  Terminal,
  TerminalDiagnostics,
  TerminalHistory,
  TerminalHolder,
  TerminalList,
} from "@clawdline/contract"
import { client, followsRelay } from "../../client.js"
import type { Answer, Transport } from "./input-client.js"

/**
 * The /v1/terminals routes (api/v1/terminals.schema.json) as this console
 * reads them. Every refusal is a `TerminalRequestError` carrying the contract's
 * code, so the page says the code's own sentence (words.ts) and never "failed".
 */

export class TerminalRequestError extends Error {
  constructor(
    readonly code: string,
    readonly status: number,
    readonly holder?: TerminalHolder,
  ) {
    super(code)
  }
}

/** The console Clawdline Cloud serves; terminal requests there use terminal-session.ts. */
export function hostedConsole(): boolean {
  return followsRelay() || !!(import.meta as { env?: Record<string, unknown> }).env?.VITE_HOSTED_CONSOLE
}

/** A random id this tab gives itself; together with the device it names a lease holder. */
export function newClientID(): string {
  const bytes = new Uint8Array(12)
  crypto.getRandomValues(bytes)
  return "tab-" + Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("")
}

async function send(path: string, init: RequestInit = {}): Promise<Answer> {
  const res = await fetch(client.url(path), {
    cache: "no-store",
    ...init,
    headers: init.body !== undefined ? { "Content-Type": "application/json", ...(init.headers ?? {}) } : init.headers,
  })
  let body: unknown = null
  try {
    body = await res.json()
  } catch {
    // refusal-ok: a body that is not JSON leaves the status to speak, and the caller names it.
    body = null
  }
  return { status: res.status, body }
}

function refusalCode(answer: Answer): string {
  const b = answer.body as { error?: unknown } | null
  if (typeof b?.error === "string") return b.error
  const nested = (b?.error as { code?: unknown } | undefined)?.code
  return typeof nested === "string" ? nested : "http_" + answer.status
}

async function call<T>(path: string, init?: RequestInit): Promise<T> {
  let answer: Answer
  try {
    answer = await send(path, init)
  } catch {
    throw new TerminalRequestError("network", 0)
  }
  if (answer.status !== 200) {
    throw new TerminalRequestError(refusalCode(answer), answer.status,
      (answer.body as { holder?: TerminalHolder } | null)?.holder)
  }
  return answer.body as T
}

/** How the input client's requests leave this tab. */
export const fetchTransport: Transport = {
  post(path, body, opts) {
    return send(path, { method: "POST", body: JSON.stringify(body), keepalive: opts?.keepalive })
  },
}

export function listTerminals(project: string, tab: string, signal?: AbortSignal): Promise<TerminalList> {
  const scope = project ? `project=${encodeURIComponent(project)}&` : ""
  return call(`/v1/terminals?${scope}client=${encodeURIComponent(tab)}`, { signal })
}

export function readTerminal(id: string, tab: string): Promise<Terminal> {
  return call(`/v1/terminals/${encodeURIComponent(id)}?client=${encodeURIComponent(tab)}`)
}

export function openTerminal(project: string, cols: number, rows: number): Promise<Terminal> {
  return call("/v1/terminals", { method: "POST", body: JSON.stringify({ project_id: project, cols, rows }) })
}

export function closeTerminal(id: string, epoch: number, tab: string): Promise<{ ok: boolean }> {
  return call(`/v1/terminals/${encodeURIComponent(id)}`, {
    method: "DELETE",
    body: JSON.stringify({ epoch, client: tab }),
  })
}

export function resizeTerminal(id: string, epoch: number, tab: string, cols: number, rows: number): Promise<{ ok: boolean }> {
  return call(`/v1/terminals/${encodeURIComponent(id)}/resize`, {
    method: "POST",
    body: JSON.stringify({ epoch, client: tab, cols, rows }),
  })
}

export function readHistory(id: string, lines = 2000): Promise<TerminalHistory> {
  return call(`/v1/terminals/${encodeURIComponent(id)}/history?lines=${lines}`)
}

export function streamURL(id: string, tab: string): string {
  return client.url(`/v1/terminals/${encodeURIComponent(id)}/stream?client=${encodeURIComponent(tab)}`)
}

/** What this machine says about terminals and platform capability. */
export interface TerminalMachine {
  capability?: PlatformCapability
  terminals?: TerminalDiagnostics
}

export async function readTerminalMachine(signal?: AbortSignal): Promise<TerminalMachine> {
  const d = await call<{ platform?: { capabilities?: PlatformCapability[] }; terminals?: TerminalDiagnostics }>(
    "/v1/diagnostics", { signal },
  )
  return {
    capability: d.platform?.capabilities?.find((c) => c.name === "terminal"),
    terminals: d.terminals,
  }
}
