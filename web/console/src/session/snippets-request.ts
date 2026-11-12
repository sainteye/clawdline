/**
 * What an `OPEN_SNIPPETS` event may ask of the Snippet / Playbook sheet beyond
 * opening it (`legacy/snippets-bridge.ts`). The voice copilot opens the sheet
 * on a tab, and may name a Playbook to start: the sheet then starts it through
 * its own Start (`startPlaybook` in `Snippets.tsx`), so the project-file check,
 * the one-running-per-Session refusal and the to-do are the sheet's, never a
 * second copy of them. Nothing is imported here, so `node --test` loads it.
 */
export interface SnippetsRequest {
  tab?: "snippet" | "playbook"
  /** A Playbook's id to select. */
  playbook?: string
  /** Start that Playbook once the sheet has read it. */
  start?: boolean
  /** Told once what became of the request (`SnippetsOutcome`). */
  answer?: (outcome: SnippetsOutcome) => void
}

/** What the sheet made of a request: opened, started, or why it did not start. */
export type SnippetsOutcome =
  | "opened"
  | "started"
  | "playbook_not_found"
  | "already_running"
  | "source_unavailable"
  | "source_changed"
  | "source_scope_conflict"
  | "not_ready"
  | "start_unknown"

/** The request an event carries, or null when it carries none that can be read. */
export function snippetsRequestOf(event: Event): SnippetsRequest | null {
  const detail = (event as CustomEvent<unknown>).detail
  if (!detail || typeof detail !== "object") return null
  const asked = detail as Record<string, unknown>
  const tab = asked.tab === "snippet" || asked.tab === "playbook" ? asked.tab : undefined
  const playbook = typeof asked.playbook === "string" && asked.playbook ? asked.playbook : undefined
  const answer = typeof asked.answer === "function" ? (asked.answer as (outcome: SnippetsOutcome) => void) : undefined
  return { tab, playbook, start: asked.start === true && !!playbook, answer }
}
