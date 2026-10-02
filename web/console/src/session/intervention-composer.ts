import type { SessionRow } from "@clawdline/contract"
import { sessionSelectionIdentity } from "../legacy/start-bridge.js"

export interface InterventionTarget {
  machine: string
  session: string
  conversation: string
}

export function interventionTarget(row: SessionRow | null): InterventionTarget | null {
  if (!row?.sessionId) return null
  const identity = sessionSelectionIdentity(row)
  if (!identity) return null
  return { machine: identity.machine, session: identity.session, conversation: row.sessionId }
}

export function sameInterventionTarget(a: InterventionTarget | null, b: InterventionTarget | null): boolean {
  return !!a && !!b && a.machine === b.machine && a.session === b.session && a.conversation === b.conversation
}

