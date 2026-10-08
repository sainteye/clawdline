/**
 * Handing a pairing offer to the Mac app this page is running in.
 *
 * Inside the Mac app's Cloud tab, and only there, the shell registers a reply
 * handler (`shell/darwin/CloudPairing.swift`, `CloudPairAgent`). The page posts
 * the offer, the machine's id and its name; the shell asks the person in a
 * native sheet, and on yes the daemon starts an assistant on that Mac which
 * reaches the machine over access the Mac already has and runs
 * `clawdline cloud pair -offer …` there. The page sends no prose: the daemon
 * writes the instructions from its own template.
 *
 * Hosted browsers use an encrypted command to a paired helper machine instead;
 * that route is implemented by CloudGate. This module handles only WebKit.
 * The pairing itself still completes through the offer run this card is
 * already waiting on — this only changes who carries the offer.
 */
import type { nextWord } from "../next-strings.js"

export const PAIR_AGENT_HANDLER = "clawdlinePairAgent"

/**
 * What the shell answers. `trust` is the daemon's word for whether the new
 * tab's assistant will open on a composer: `recorded`, `per_launch`, or
 * `not_recorded`, when a terminal may be asking whether to trust a folder.
 */
export type PairAgentReply =
  | { ok: true; mode: "direct" | "agent"; taskID: string; trust: string }
  | { ok: false; error: string }

type ReplyHandler = { postMessage(body: unknown): Promise<unknown> }

type WithWebKit = { webkit?: { messageHandlers?: Record<string, ReplyHandler | undefined> } }

function handlerIn(host: unknown): ReplyHandler | null {
  const handler = (host as WithWebKit | undefined)?.webkit?.messageHandlers?.[PAIR_AGENT_HANDLER]
  return handler && typeof handler.postMessage === "function" ? handler : null
}

/** Whether this page is in the Mac app's Cloud tab, which can take the offer. */
export function pairAgentAvailable(host: unknown = globalThis): boolean {
  return handlerIn(host) !== null
}

/**
 * Posts the offer to the shell and reads its answer. Never throws: a handler
 * that is gone, rejects, or answers something unreadable is a refusal with a
 * code, which the card says.
 */
export async function handPairToAgent(
  input: { offer: string; machineID: string; machineName: string },
  host: unknown = globalThis,
): Promise<PairAgentReply> {
  const handler = handlerIn(host)
  if (!handler) return { ok: false, error: "unavailable" }
  let answer: unknown
  try {
    answer = await handler.postMessage({
      offer: input.offer,
      machine_id: input.machineID,
      machine_name: input.machineName,
    })
  } catch {
    return { ok: false, error: "unreachable" }
  }
  const said = (answer ?? {}) as { ok?: unknown; mode?: unknown; task_id?: unknown; trust?: unknown; error?: unknown }
  if (said.ok === true && (said.mode === "direct" || said.mode === "agent")) {
    return {
      ok: true,
      mode: said.mode,
      taskID: typeof said.task_id === "string" ? said.task_id : "",
      trust: typeof said.trust === "string" ? said.trust : "",
    }
  }
  const error = typeof said.error === "string" && said.error.trim() ? said.error.trim().slice(0, 300) : "refused"
  return { ok: false, error }
}

/** The hand-off's outcome as one word, for the status line's `data-agent`. */
export function agentOutcome(agent: "sending" | PairAgentReply): "sending" | "agent" | "direct" | "cancelled" | "refused" {
  if (agent === "sending") return "sending"
  if (agent.ok) return agent.mode
  return agent.error === "cancelled" ? "cancelled" : "refused"
}

/** What the status line under the hand-off button says. */
export function agentSaid(agent: "sending" | PairAgentReply, machine: string, word: typeof nextWord): string {
  if (agent !== "sending" && !agent.ok && agent.error !== "cancelled") {
    return word("cloudPairAgentRefused", { reason: agent.error })
  }
  switch (agentOutcome(agent)) {
    case "sending":
      return word("cloudPairAgentSending")
    case "agent":
      return word("cloudPairAgentSent", { machine })
    case "direct":
      return word("cloudPairAgentDirect")
    default:
      return word("cloudPairAgentCancelled")
  }
}

/**
 * The event the shell dispatches on `window` each time the hand-off's task
 * moves (`CloudPairAgent.follow`), read from
 * `GET /v1/cloud/pairing/agent/<task_id>`. It never carries the offer.
 */
export const PAIR_AGENT_EVENT = "clawdline-pair-agent"

/** Where the AI task is, as the card says it. */
export type PairAgentStage = "starting" | "dialog" | "working" | "done" | "failed"

export type PairAgentProgress = { stage: PairAgentStage; summary: string; failedReason: string }

const STAGES: readonly PairAgentStage[] = ["starting", "dialog", "working", "done", "failed"]

/**
 * One event's detail, if it is about the task this card handed off and says
 * something the card knows how to say; anything else is null.
 */
export function readPairAgentEvent(detail: unknown, taskID: string): PairAgentProgress | null {
  const d = (detail ?? {}) as { task_id?: unknown; state?: unknown; summary?: unknown; failed_reason?: unknown }
  if (!taskID || d.task_id !== taskID) return null
  if (typeof d.state !== "string" || !STAGES.includes(d.state as PairAgentStage)) return null
  const text = (v: unknown) => (typeof v === "string" ? v.trim().slice(0, 600) : "")
  return { stage: d.state as PairAgentStage, summary: text(d.summary), failedReason: text(d.failed_reason) }
}

type EventHost = {
  addEventListener?: (type: string, listener: (event: unknown) => void) => void
  removeEventListener?: (type: string, listener: (event: unknown) => void) => void
}

/**
 * Listens for the shell's progress on one task, only where the shell's
 * handler exists; answers how to stop listening.
 */
export function listenPairAgent(
  taskID: string,
  onProgress: (progress: PairAgentProgress) => void,
  host: unknown = globalThis,
): () => void {
  const target = host as EventHost
  if (!taskID || !pairAgentAvailable(host) || typeof target.addEventListener !== "function") return () => {}
  const listener = (event: unknown) => {
    const progress = readPairAgentEvent((event as { detail?: unknown } | null)?.detail, taskID)
    if (progress) onProgress(progress)
  }
  target.addEventListener(PAIR_AGENT_EVENT, listener)
  return () => target.removeEventListener?.(PAIR_AGENT_EVENT, listener)
}

/** The progress block's sentence for where the task is. */
export function progressSaid(progress: PairAgentProgress | null, machine: string, word: typeof nextWord): string {
  switch (progress?.stage) {
    case "dialog":
      return word("cloudPairAgentDialog")
    case "working":
      return word("cloudPairAgentWorking", { machine })
    case "done":
      return word("cloudPairAgentDone", { machine })
    case "failed":
      return word("cloudPairAgentFailed", { reason: progress.failedReason || word("cloudPairAgentFailedUnknown") })
    default:
      return word("cloudPairAgentStarting")
  }
}

/** How long the AI has been at it, in whole seconds or minutes. */
export function elapsedSaid(seconds: number, word: typeof nextWord): string {
  const s = Math.max(0, Math.floor(seconds))
  if (s < 60) return word("cloudPairAgentElapsedSeconds", { seconds: s })
  return word("cloudPairAgentElapsedMinutes", { minutes: Math.floor(s / 60), seconds: s % 60 })
}
