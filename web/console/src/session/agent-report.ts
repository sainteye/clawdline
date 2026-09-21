import type { SessionAgent } from "@clawdline/contract"

export interface AgentReportIdentity {
  id: string
  known: boolean
  label: string
  speaker: string
  detail: string
}

/**
 * The provider's hand-back envelope and the session's background-agent list
 * use the same id. This is the one join between them, kept independent of the
 * React renderer so an unknown id cannot silently become the person speaking.
 */
export function agentReportIdentity(
  source: unknown,
  agents: SessionAgent[] | undefined,
  assistant: string | undefined,
  language: string,
): AgentReportIdentity {
  const id = typeof source === "string" ? source.trim() : ""
  const ordinal = (agents?.findIndex((candidate) => candidate.id === id) ?? -1) + 1
  const agent = ordinal > 0 ? agents?.[ordinal - 1] : undefined
  const zh = /^zh(?:-|$)/i.test(language)
  const speaker = zh ? "背景 agent" : "Background agent"
  if (agent) {
    const what = agent.what?.trim()
    const generic = zh ? "背景 agent" : "Background agents"
    const missing = zh ? "Codex 沒有記下這個 thread 在做什麼" : "Codex did not record what this thread is doing"
    const label = what && what !== agent.type
      ? what
      : assistant === "codex" ? `${missing} · ${ordinal}` : what || `${generic} ${ordinal}`
    return { id, known: true, label, speaker, detail: id }
  }
  const shown = id || (zh ? "未知 id" : "unknown id")
  return {
    id,
    known: false,
    label: shown,
    speaker,
    detail: zh ? `來源是 ${shown}，這台機器不認得它` : `Source: ${shown}; this machine does not recognize it`,
  }
}
