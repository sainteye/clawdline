import type { Assistant, StartAssistant, StartCapabilities } from "@clawdline/contract"

/** Older machines omit readiness facts; only the two original providers retain that fallback. */
export type AssistantFacts = Pick<StartAssistant, "id"> & Partial<StartAssistant>
export type AssistantOperation = keyof StartCapabilities
export type AssistantUnavailable = "missing" | "integrationMissing" | "required" | "unsupported" | "unknownAuth" | "disabled" | "incompatible" | "unknown"

export function isAssistant(value: unknown): value is Assistant {
  return value === "claude" || value === "codex" || value === "antigravity"
}

export function assistantLabel(value: string | undefined): string {
  return value === "claude" ? "Claude Code" : value === "codex" ? "Codex" : value === "antigravity" ? "Antigravity" : "AI"
}

export function assistantUnavailable(facts: AssistantFacts | null | undefined, operation: AssistantOperation): AssistantUnavailable | null {
  if (!facts || !isAssistant(facts.id)) return "unknown"
  if (facts.installed === false) return "missing"
  if (facts.integrationState === "incompatible") return "incompatible"
  if (facts.integrationState === "missing") return "integrationMissing"
  if (facts.integrationState === "disabled") return "disabled"
  if (facts.authState === "required") return "required"
  if (facts.authState === "unsupported_account") return "unsupported"
  if (facts.authState === "unknown") return "unknownAuth"
  if (facts.capabilities?.[operation] === false) return "disabled"
  if (facts.id !== "antigravity") return null
  if (facts.integrationState !== "active" || facts.authState !== "ready" || facts.capabilities?.[operation] !== true) return "unknown"
  return null
}

export function assistantAdmits(facts: AssistantFacts | null | undefined, operation: AssistantOperation): boolean {
  return assistantUnavailable(facts, operation) === null
}

/** A literal slash can become a paid prompt rather than opening the CLI picker. Keep it a draft. */
export function antigravitySlashDraft(assistant: string | undefined, text: string): boolean {
  return assistant === "antigravity" && text.trimStart().startsWith("/")
}

/** Session hosts supply machine readiness separately from permission to write to that machine. */
export function assistantComposeAdmits(assistant: string | undefined, facts: AssistantFacts | undefined,
  state: string | undefined, hasMenu: boolean): boolean {
  if (assistant !== "antigravity") return true
  return facts?.id === assistant && assistantAdmits(facts, "send") && state === "idle" && !hasMenu
}
