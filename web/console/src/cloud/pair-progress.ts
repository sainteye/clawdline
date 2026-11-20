import type { PairAssistant } from "./pair-agent.js"
import type { PairTarget } from "./PairPanel.js"
import type { PairState } from "./pair.js"

const KEY = "clawdline.pairing.progress.v1"

export type PairProgressStatus = "working" | "checking" | "confirm" | "attention"

export type PairHelperFollow = {
  helperID: string
  requestID: string
  assistant: PairAssistant
  taskID: string
  trust: string
  startedAt: number
  offerDigest: string
}

export type PairProgress = {
  account: string
  device: string
  pairingID: string
  machine: PairTarget | null
  expiresAt: number
  hidden: boolean
  helper?: PairHelperFollow
  handover?: Extract<PairState, { phase: "paired" }>
}

export function readPairProgress(account: string, device: string): PairProgress | null {
  try {
    const saved = JSON.parse(sessionStorage.getItem(KEY) ?? "null") as Partial<PairProgress> | null
    if (!saved || saved.account !== account || saved.device !== device ||
        typeof saved.pairingID !== "string" || !saved.pairingID ||
        !Number.isFinite(saved.expiresAt) || typeof saved.hidden !== "boolean" ||
        !(saved.machine === null || typeof saved.machine?.id === "string" && typeof saved.machine.name === "string")) return null
    if (saved.helper && (typeof saved.helper.helperID !== "string" || typeof saved.helper.taskID !== "string" ||
        typeof saved.helper.requestID !== "string" || !saved.helper.taskID && !saved.helper.requestID ||
        saved.helper.assistant !== "claude" && saved.helper.assistant !== "codex" ||
        typeof saved.helper.trust !== "string" || !Number.isFinite(saved.helper.startedAt) ||
        !/^[0-9a-f]{64}$/u.test(saved.helper.offerDigest))) return null
    if (saved.handover && (saved.handover.phase !== "paired" || typeof saved.handover.machineID !== "string" ||
        typeof saved.handover.fingerprint !== "string" || typeof saved.handover.machineFingerprint !== "string")) return null
    return saved as PairProgress
  } catch {
    return null
  }
}

/** Tab-local presentation only. The private claim key and offer stay in IndexedDB. */
export function writePairProgress(progress: PairProgress | null) {
  try {
    if (progress) sessionStorage.setItem(KEY, JSON.stringify(progress))
    else sessionStorage.removeItem(KEY)
  } catch {
    // The current page still follows the pairing if its tab storage is unavailable.
  }
}
