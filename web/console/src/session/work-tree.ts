import type { SessionAgent, SessionRow } from "@clawdline/contract"

export type WorkState = "running" | "done" | "failed" | "unknown"

export type WorkNode = {
  source: "provider"
  id: string
  state: WorkState
  exactState: string
  agent: SessionAgent
}

/**
 * Provider-native work only. Broker children already have first-class rows in
 * the session list, so repeating them here makes Clawdline's own tasks look
 * like the invisible work this fold exists to reveal.
 */
export function workTree(row: SessionRow): WorkNode[] {
  return (row.agents ?? []).map((agent) => ({
    source: "provider",
    id: agent.id,
    state: agent.state,
    exactState: agent.state,
    agent,
  }))
}

export function runningAgentCount(row: SessionRow): number {
  return (row.agents ?? []).filter((agent) => agent.state === "running").length
}

export function agentDisplayName(
  agent: SessionAgent,
  assistant: string | undefined,
  ordinal: number,
  codexMissing: string,
  generic: string,
): string {
  const what = agent.what?.trim()
  if (what && what !== agent.type) return what
  if (assistant === "codex") return `${codexMissing} · ${ordinal}`
  return what || `${generic} ${ordinal}`
}
