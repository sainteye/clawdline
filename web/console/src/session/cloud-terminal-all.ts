import type { Terminal } from "@clawdline/contract"
// @ts-expect-error -- Node's strip-types test runner loads the source path.
import { decodeCloudPlaceID, type CloudPlaceRow } from "../pages/terminal/cloud-project.ts"

export interface CloudTerminalMachine { id: string; name: string }
export interface CloudTerminalRow {
  machine: string
  machineName: string
  project: string
  projectName: string
  terminal: Terminal
  stale: boolean
  confirmedAt: number
}
export interface CloudTerminalError { machine: string; code: string }

/** One bounded read retry after the relay's two-second terminal grant renews. */
export async function readCloudTerminalList<T>(read: () => Promise<T>,
  pause: (ms: number) => Promise<void> = (ms) => new Promise((resolve) => setTimeout(resolve, ms))): Promise<T> {
  try { return await read() }
  catch (error) {
    if ((error as { code?: unknown })?.code !== "rate_limited") throw error
    await pause(2_100)
    return read()
  }
}

/** A failed machine retains only its own prior rows, clearly marked stale. */
export async function collectCloudTerminals(
  machines: readonly CloudTerminalMachine[], places: readonly CloudPlaceRow[] | Promise<readonly CloudPlaceRow[]>, previous: readonly CloudTerminalRow[],
  read: (machine: string) => Promise<Terminal[]>,
  onProgress?: (result: { rows: CloudTerminalRow[]; errors: CloudTerminalError[] }) => void,
): Promise<{ rows: CloudTerminalRow[]; errors: CloudTerminalError[] }> {
  const rows: CloudTerminalRow[][] = Array.from({ length: machines.length }, () => [])
  const errors: CloudTerminalError[] = []
  let next = 0
  // Each read holds two relay subscriptions. Two concurrent reads leave half
  // the eight-channel socket available to other terminal activity.
  await Promise.all(Array.from({ length: Math.min(2, machines.length) }, async () => {
    for (;;) {
      const index = next++
      if (index >= machines.length) return
      const machine = machines[index]!
      try {
        const terminals = await read(machine.id)
        const namedPlaces = await places
        rows[index] = terminals.filter((terminal) => terminal.status === "running").map((terminal) => {
          const place = namedPlaces.find((candidate) => {
            const pair = decodeCloudPlaceID(candidate.id)
            return pair?.[0] === machine.id && pair[1] === terminal.project_id
          })
          return { machine: machine.id, machineName: machine.name, project: place?.id ?? "",
            projectName: place?.label ?? terminal.project_id, terminal, stale: false, confirmedAt: Date.now() }
        })
      } catch (error) {
        const code = (error as { code?: unknown })?.code
        errors.push({ machine: machine.id, code: typeof code === "string" ? code :
          error instanceof Error && error.message ? error.message : "terminal_list_failed" })
        rows[index] = previous.filter((row) => row.machine === machine.id).map((row) => ({ ...row, stale: true }))
      }
      onProgress?.({ rows: rows.flat(), errors: [...errors] })
    }
  }))
  return { rows: rows.flat(), errors }
}

/**
 * Codes that mean a fact the machine (or the relay) holds says no: pairing or
 * send permission is missing. Only these ask the person to pair again.
 */
const TERMINAL_DENIAL_CODES = new Set(["forbidden", "terminal_forbidden", "terminal_access_revoked"])
/**
 * Codes that mean "not right now": the machine could not verify this browser in
 * time (it answers terminal_busy while the account's device roster cannot be
 * read), the terminal host or relay was briefly unreachable or full, or a
 * receipt did not arrive in time. A browser that can send messages to the
 * machine should reach its terminals once the moment passes.
 */
const TERMINAL_RETRYABLE_CODES = new Set(["terminal_busy", "terminal_unreachable", "terminal_receipt_timeout",
  "terminal_subscription_timeout", "terminal_send_failed", "terminal_not_connected", "rate_limited", "over_capacity",
  "cloud_read_busy", "cloud_reconnecting", "machine_stale"])

export type TerminalListErrorKind = "denied" | "retryable" | "other"
export function terminalListErrorKind(code: string): TerminalListErrorKind {
  if (TERMINAL_DENIAL_CODES.has(code)) return "denied"
  return TERMINAL_RETRYABLE_CODES.has(code) ? "retryable" : "other"
}

/** The waits before each automatic re-read of a list the machine could not answer yet. */
export const CLOUD_TERMINAL_LIST_RETRY_DELAYS_MS: readonly number[] = [1_000, 2_000, 4_000]

/**
 * Reads the list, and while every failed machine failed for a retryable reason
 * reads it again after each of CLOUD_TERMINAL_LIST_RETRY_DELAYS_MS. `onWait`
 * says a wait has begun (so the page can say it is retrying); `live` stops the
 * loop once the page has started another read or closed.
 */
export async function withTerminalListRetries<T extends { errors: readonly CloudTerminalError[] }>(
  read: () => Promise<T>, onWait: (wait: { attempt: number; delayMs: number; result: T }) => void,
  live: () => boolean = () => true,
  pause: (ms: number) => Promise<void> = (ms) => new Promise((resolve) => setTimeout(resolve, ms)),
): Promise<T> {
  for (let attempt = 0; ; attempt++) {
    const result = await read()
    const delayMs = CLOUD_TERMINAL_LIST_RETRY_DELAYS_MS[attempt]
    if (delayMs === undefined || !result.errors.length || !live() ||
      !result.errors.every((error) => terminalListErrorKind(error.code) === "retryable")) return result
    onWait({ attempt: attempt + 1, delayMs, result })
    await pause(delayMs)
    if (!live()) return result
  }
}
