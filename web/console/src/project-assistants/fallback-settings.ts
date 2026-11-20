import type * as W from "@clawdline/contract"

/**
 * The identities a source accepts as alternates (the shipped `claude`/`codex`
 * enums). Mirrors `pa.MaxFallbackAgents`, registered as `bots.fallback_agents`;
 * the source refuses anything longer, so this is not a new bound.
 */
export const FALLBACK_AGENTS: readonly W.ProjectAssistantAgent[] = ["claude", "codex"]
export const MAX_FALLBACK_AGENTS = 3

export function agentName(agent: string): string {
  return agent === "claude" ? "Claude Code" : agent === "codex" ? "Codex" : agent === "grok" ? "Grok Build" : agent
}

/** Agents that may still be added: never the primary, never one already in the chain. */
export function fallbackChoices(primary: W.ProjectAssistantAgent, chain: readonly W.ProjectAssistantAgent[]): W.ProjectAssistantAgent[] {
  if (chain.length >= MAX_FALLBACK_AGENTS) return []
  return FALLBACK_AGENTS.filter(a => a !== primary && !chain.includes(a))
}

/** Only Auto has a qualified mapping onto an alternate; a fixed mode keeps, but does not grow, the chain. */
export function fallbackModeSupported(mode?: string): boolean {
  return !mode || mode === "auto"
}

/** Codync `moveFallback`: swap one entry with its neighbour; out-of-range moves change nothing. */
export function moveFallback<T>(chain: readonly T[], index: number, by: -1 | 1): T[] {
  const to = index + by
  if (index < 0 || index >= chain.length || to < 0 || to >= chain.length) return [...chain]
  const next = [...chain]
  ;[next[index], next[to]] = [next[to], next[index]]
  return next
}

/** The agent that runs before `index` in the chain: the Bot's own for the first alternate. */
export function previousAgent(primary: W.ProjectAssistantAgent, chain: readonly W.ProjectAssistantAgent[], index: number): W.ProjectAssistantAgent {
  return index <= 0 ? primary : chain[index - 1]
}

/** Preserve stored values unless the person deliberately changed this control; clearing is explicit. */
export function fallbackEdit(original: readonly W.ProjectAssistantAgent[] | undefined, selected: readonly W.ProjectAssistantAgent[], available: boolean): Pick<W.ProjectAssistantEditSettings, "fallback_agents"> {
  const before = original ?? []
  if (!available || (before.length === selected.length && before.every((a, i) => a === selected[i]))) return {}
  return { fallback_agents: [...selected] }
}
