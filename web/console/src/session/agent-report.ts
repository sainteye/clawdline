import { catalogFormat } from "../catalog.js"
import { catalogWord } from "../catalog.js"
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
  void language
  const speaker = catalogWord("literal", "2a45c79c1f67")
  if (agent) {
    const what = agent.what?.trim()
    const generic = catalogWord("literal", "e5db0352b7e8")
    const missing = catalogWord("literal", "ac7ebb4a6c29")
    const label = what && what !== agent.type
      ? what
      : assistant === "codex" ? `${missing} · ${ordinal}` : what || `${generic} ${ordinal}`
    return { id, known: true, label, speaker, detail: id }
  }
  const shown = id || (catalogWord("literal", "c9911c4b0f8f"))
  return {
    id,
    known: false,
    label: shown,
    speaker,
    detail: catalogFormat("template", "ff865ce6ed4f", [shown]),
  }
}
