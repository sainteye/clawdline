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

/** Name only the model this agent recorded; unfamiliar provider ids stay intact. */
export function agentModelName(model: string | undefined): string {
  const raw = model?.trim() ?? ""
  const undated = raw.replace(/-\d{8}$/, "")
  const current = /^claude-(opus|sonnet|haiku)(?:-(\d+)(?:-(\d+))?)?$/i.exec(undated)
  const older = /^claude-(\d+)(?:-(\d+))?-(opus|sonnet|haiku)$/i.exec(undated)
  const family = current?.[1] ?? older?.[3]
  if (!family) return raw
  const version = current ? [current[2], current[3]] : [older![1], older![2]]
  return [family[0].toUpperCase() + family.slice(1).toLowerCase(), version.filter(Boolean).join(".")].filter(Boolean).join(" ")
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
