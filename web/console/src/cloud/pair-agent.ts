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
 * Every other browser has no such handler, and the copy path is all it gets.
 * The pairing itself still completes through the offer run this card is
 * already waiting on — this only changes who carries the offer.
 */
import type { nextWord } from "../next-strings.js"

export const PAIR_AGENT_HANDLER = "clawdlinePairAgent"

/** What the shell answers. */
export type PairAgentReply =
  | { ok: true; mode: "direct" | "agent"; taskID: string }
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
  const said = (answer ?? {}) as { ok?: unknown; mode?: unknown; task_id?: unknown; error?: unknown }
  if (said.ok === true && (said.mode === "direct" || said.mode === "agent")) {
    return { ok: true, mode: said.mode, taskID: typeof said.task_id === "string" ? said.task_id : "" }
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
