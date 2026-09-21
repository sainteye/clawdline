import type { CloudMachine } from "./copied.js"

/** The account's machine list, with absence and failure kept as different facts. */
export type MachineListState =
  | { phase: "loading" }
  | { phase: "ready"; machines: CloudMachine[] }
  | { phase: "empty_authoritative" }
  | { phase: "refused"; code: string; next: "sign_in" | "pair" | "retry" }

export interface MachineListAnswer {
  machines: CloudMachine[]
  syncing: boolean
}

/** A successful list says either what arrived, that more may arrive, or that empty is final. */
export function machineListAnswer(answer: MachineListAnswer): MachineListState {
  if (answer.machines.length) return { phase: "ready", machines: answer.machines }
  return answer.syncing ? { phase: "loading" } : { phase: "empty_authoritative" }
}

/** A rejected list keeps its reason and never becomes an authoritative empty account. */
export function machineListRefusal(error: unknown): MachineListState {
  const code = failureCode(error) ?? "machine_list_unanswered"
  const next = SIGN_IN_CODES.has(code) ? "sign_in" : PAIR_CODES.has(code) ? "pair" : "retry"
  return { phase: "refused", code, next }
}

/** Why a reconnect is waiting, without turning an untyped browser failure into server downtime. */
export type RetryReason =
  | { kind: "named"; code: string }
  | { kind: "browser_offline" }
  | { kind: "unknown" }

export function retryReason(error: unknown, browserOnline: boolean): RetryReason {
  const code = failureCode(error)
  if (code) return { kind: "named", code }
  if (!browserOnline) return { kind: "browser_offline" }
  return { kind: "unknown" }
}

export interface AccessProblem {
  machine: string
  code: string
}

export interface AccessEvent {
  type: string
  machine?: string
  identity?: { machine?: string }
  error?: unknown
}

export interface ViewerEventLog {
  snapshot(): {
    rows?: {
      n?: number
      data?: { machine?: unknown; routed_machine?: unknown }
    }[]
  }
}

const ACCESS_PROBLEMS = new Set([
  "forbidden", "unauthorized", "revoked", "missing_capability", "capability_denied",
  "unknown_key", "unknown_sender", "extractable_key", "unreadable_envelope",
  "machine_pairing_required",
])
const SIGN_IN_CODES = new Set(["forbidden", "unauthorized", "revoked", "device_revoked", "session_expired"])
const PAIR_CODES = new Set([
  "missing_capability", "capability_denied", "unknown_key", "unknown_sender",
  "extractable_key", "unreadable_envelope", "machine_pairing_required",
])

/**
 * A readable access failure and the machine whose authenticated channel caused
 * it. The copied client records that id before replacing a WebCrypto exception;
 * the error's small receipt points back to that row. If no source can name a
 * machine, there is no problem sentence: guessing one out of a multi-machine
 * account would be worse than withholding a sentence until the next envelope.
 */
export function machineAccessProblem(
  event: AccessEvent,
  viewerEvents?: ViewerEventLog,
  onlyListedMachine?: string | null,
): AccessProblem | null {
  if (event.type !== "error") return null
  const code = failureCode(event.error)
  if (!code || !ACCESS_PROBLEMS.has(code)) return null
  const error = object(event.error)
  const detail = object(error?.detail)
  const direct = string(detail?.machine) ?? string(event.machine) ?? string(event.identity?.machine)
  const recorded = direct ? null : recordedMachine(error, viewerEvents)
  const machine = direct ?? recorded ?? onlyListedMachine ?? null
  return machine ? { machine, code } : null
}

function recordedMachine(error: Record<string, unknown> | null, log?: ViewerEventLog): string | null {
  const receipt = object(error?.viewerEvent)
  const n = typeof receipt?.n === "number" ? receipt.n : null
  if (n === null || !log) return null
  try {
    const row = log.snapshot().rows?.find((candidate) => candidate.n === n)
    return string(row?.data?.routed_machine) ?? string(row?.data?.machine)
  } catch {
    return null
  }
}

function failureCode(value: unknown): string | null {
  const outer = object(value)
  const direct = string(outer?.code)
  if (direct) return direct
  return string(object(outer?.error)?.code)
}

function object(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null
}

function string(value: unknown): string | null {
  return typeof value === "string" && value ? value : null
}
