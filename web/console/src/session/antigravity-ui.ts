import type { Assistant, StartAssistant, StartCapabilities, SessionRow } from "@clawdline/contract"

/** Older machines omit readiness facts; only the two original providers retain that fallback. */
export type AssistantFacts = Pick<StartAssistant, "id"> & Partial<StartAssistant>
export type AssistantOperation = keyof StartCapabilities
export type AssistantUnavailable = "missing" | "integrationMissing" | "required" | "unsupported" | "unknownAuth" | "disabled" | "incompatible" | "unknown"

export function isAssistant(value: unknown): value is Assistant {
  return value === "claude" || value === "codex" || value === "antigravity"
}

export function assistantLabel(value: string | undefined): string {
  return value === "claude" ? "Claude Code" : value === "codex" ? "Codex" : value === "antigravity" ? "Gemini" : "AI"
}

export function assistantUnavailable(facts: AssistantFacts | null | undefined, operation: AssistantOperation): AssistantUnavailable | null {
  if (!facts || !isAssistant(facts.id)) return "unknown"
  if (facts.installed === false) return "missing"
  if (facts.integrationState === "incompatible") return "incompatible"
  if (facts.integrationState === "missing") return "integrationMissing"
  if (facts.integrationState === "disabled") return "disabled"
  // A safe bootstrap is explicitly advertised by the backend, never inferred from auth.
  if ((operation === "start" || operation === "resume") && facts.capabilities?.[operation] === true &&
    (facts.id !== "antigravity" || facts.installed === true) && facts.integrationState === "active" && facts.authState !== "unsupported_account") return null
  if (facts.authState === "required") return "required"
  if (facts.authState === "unsupported_account") return "unsupported"
  if (facts.authState === "unknown") return "unknownAuth"
  if (facts.capabilities?.[operation] === false) return "disabled"
  if (facts.id !== "antigravity") return null
  if (facts.installed !== true) return "unknown"
  if (facts.integrationState !== "active" || facts.authState !== "ready" || facts.capabilities?.[operation] !== true) return "unknown"
  return null
}

export function assistantAdmits(facts: AssistantFacts | null | undefined, operation: AssistantOperation): boolean {
  return assistantUnavailable(facts, operation) === null
}

/** Terminal controls are not chat prompts. Absolute paths remain ordinary text.
 * Command names follow the official Antigravity CLI command reference.
 */
export function antigravitySlashDraft(assistant: string | undefined, text: string): boolean {
  const draft = text.trimStart()
  return assistant === "antigravity" && (draft.trim() === "/" ||
    /^\/(?:btw|changelog|clear|codesearch|cs|search|compact|config|settings|context|diff|effort|exit|quit|fast|feedback|fork|branch|goal|help|hooks|keybindings|learn|logout|mcp|model|open|permissions|planning|plugin|plugins|rename|remote-control|resume|switch|conversation|rewind|undo|schedule|skills|statusline|tasks|teamwork-preview|teamwork|title|usage|quota|voice|record)(?:\s|$)/.test(draft))
}

/** Session hosts supply machine readiness separately from permission to write to that machine. */
export function assistantComposeAdmits(assistant: string | undefined, facts: AssistantFacts | undefined,
  state: string | undefined, hasMenu: boolean, protocolOwned = false): boolean {
  if (assistant !== "antigravity") return true
  // Owned writes are checked at the server; do not preflight the account again in the view.
  // A stopped or interrupted owned conversation resumes on the person's next
  // explicit message. The Host reconciles native history before that one send.
  if (protocolOwned) return (state === "idle" || state === "unknown") && !hasMenu
  return facts?.id === assistant && assistantAdmits(facts, "send") && state === "idle" && !hasMenu
}

/** Owned offers use native revisions; terminal menus also require a current screen identity. */
export function assistantMenuAdmits(row: SessionRow | null, facts?: AssistantFacts): boolean {
  if (row?.assistant !== "antigravity") return true
  // Owned ACP offers are checked by the server against their native revision.
  // They have no terminal screen or separate account-readiness preflight.
  if (assistantProtocolOwned(row)) return row.state === "waiting" && !!row.menu?.revision &&
    !!row.menu?.options?.length && row.menu.source !== "transcript"
  return assistantStateCurrent(row) &&
    row.state === "waiting" && !!row.line && !!row.menu?.options?.length && row.menu.source !== "transcript" &&
    facts?.id === "antigravity" && assistantAdmits(facts, "menus")
}

/** Same bound as capacity antigravity.state_age_seconds and the native observation owner. */
export const ANTIGRAVITY_STATE_FRESH_MS = 15_000
export function assistantProtocolOwned(row: SessionRow | null): boolean {
  const binding = (row as (SessionRow & { sessionBinding?: SessionRow["identity"] }) | null)?.sessionBinding ?? row?.identity
  return row?.backend === "owned" && binding === "protocol"
}
export function assistantStateCurrent(row: SessionRow | null, now = Date.now()): boolean {
  const cloud = row as (SessionRow & { machine?: string; sessionBinding?: SessionRow["identity"] }) | null
  const binding = cloud?.sessionBinding ?? row?.identity
  return (binding === "launch_hook" || assistantProtocolOwned(row)) && !!row?.sessionId && !!row.execution_generation && row.source?.freshness === "current" &&
    Number.isFinite(row.source.observed_at) && Math.abs(now - row.source.observed_at * 1000) <= ANTIGRAVITY_STATE_FRESH_MS
}

/** Old daemon versions must not offer a role their ACP start route rejects. */
export function assistantRolesAdmit(assistant: string | null, facts?: AssistantFacts): boolean {
  return assistant !== "antigravity" || facts?.capabilities?.roles === true
}
