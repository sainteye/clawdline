// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { catalogWord } from "../../catalog.ts"
import type { Icon } from "@clawdline/contract"

/** Presentation data. The wire adapter owns the mapping from the squad contract. */
export interface SquadProject {
  id: string
  kind: "repo" | "place"
  name: string
  path: string
}

export interface Effective<T> {
  global: T
  value: T
  source: "default" | "global" | "project"
  present: boolean
  version: number
}

export interface SquadTeam {
  id: string
  name: string
  description?: string
}

export interface SquadSkill {
  id: string
  name: string
  purpose: string
  body: string
  folder?: boolean
  files?: { path: string; content_base64: string }[]
  source: string
  version: string
  license: string
  icon?: Icon
  status: "available" | "unavailable" | "pending_review"
  enabled: Effective<boolean>
  order: number
}

export interface SquadCatalogSkill {
  id: string
  name: string
  purpose: string
  body: string
  folder?: boolean
  files?: { path: string; content_base64: string }[]
  source: string
  version: string
  license: string
  icon: Icon
}

export interface SquadPersona {
  id: string
  settingsVersion: number
  shortId?: string
  name: string
  subtitle: string
  summary: string
  body: string
  source: string
  version: string
  icon: Icon
  teamIds: string[]
  enabled: Effective<boolean>
  handbook: Effective<string>
  skillsSetting: Effective<{ id: string; version: string; enabled: boolean }[]>
  skills: SquadSkill[]
}

export interface SquadView {
  scopeId: string
  project: SquadProject | null
  projects: SquadProject[]
  teams: SquadTeam[]
  personas: SquadPersona[]
  catalogSkills: SquadCatalogSkill[]
  sessions: SquadSession[]
  motion: Effective<boolean>
  motionSettingsVersion: number
  catalogVersion: number
  canWrite: boolean
  partial: boolean
}

export interface SquadSession {
  sessionId: string
  conversationId: string
  definitionId: string
  scopeId: string
  snapshotId: string
  label: string
}

export type SquadReadState =
  | { kind: "loading"; previous?: SquadView }
  | { kind: "ready"; data: SquadView }
  | { kind: "error"; code: string; detail: string; previous?: SquadView }

export interface SquadDraft {
  scopeId: string
  personaId: string
  field: "handbook"
  baseVersion: number
  text: string
  serverText: string
  serverVersion: number
  conflict: boolean
}

export interface SquadReceipt {
  seq: number
  receiptId: string
  snapshotId: string
  conversationId: string
  definitionId: string
  scopeId: string
  skillId: string
  skillVersion: string
  status: "read" | "applied" | "failed"
  at: number
}

export function visiblePersonas(data: SquadView, teamId: string, search: string): SquadPersona[] {
  const query = search.trim().toLocaleLowerCase()
  return data.personas.filter((persona) =>
    (!teamId || persona.teamIds.includes(teamId)) &&
    (!query || [persona.name, persona.subtitle, persona.summary, persona.shortId ?? ""].some((part) => part.toLocaleLowerCase().includes(query))),
  )
}

export function sourceLabel(source: Effective<unknown>["source"]): string {
  switch (source) {
    case "default": return catalogWord("literal", "f32daac07918")
    case "global": return catalogWord("literal", "b545baf83246")
    case "project": return catalogWord("literal", "d77dd218a98a")
  }
}

/** The initial cursor establishes a baseline; catch-up after a disconnect is quiet. */
export class ReceiptGate {
  private seen = new Set<string>()
  private cursor = 0

  baseline(seq: number): void { this.cursor = Math.max(this.cursor, seq); this.seen.clear() }
  advance(seq: number): void { this.cursor = Math.max(this.cursor, seq) }
  get after(): number { return this.cursor }

  take(receipt: SquadReceipt, live: boolean, sessions: readonly SquadSession[], scopeId: string, personaId: string, skills: readonly SquadSkill[]): boolean {
    if (!Number.isSafeInteger(receipt.seq) || receipt.seq <= this.cursor) return false
    this.cursor = receipt.seq
    if (this.seen.has(receipt.receiptId)) return false
    this.seen.add(receipt.receiptId)
    return live && receipt.status === "applied" && receipt.scopeId === scopeId &&
      sessions.some((session) => session.conversationId === receipt.conversationId && session.snapshotId === receipt.snapshotId &&
        session.definitionId === receipt.definitionId && session.scopeId === receipt.scopeId) &&
      receipt.definitionId === personaId && skills.some((skill) => skill.id === receipt.skillId && skill.version === receipt.skillVersion)
  }
}
