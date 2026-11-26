// The one place the console reads and writes a Project's team charter.
//
// The daemon side and its generated contract are built separately. Until they
// land, this module owns a typed interface of its own; integration replaces the
// body of `charterApiFor` (and, if the generated names differ, the types here)
// without touching the screens. The phone reads the same charter over Cloud, so
// a Cloud transport also belongs here, behind the same interface.
import type { SourceClient } from "./client"

/** A Project on one machine. */
export interface CharterProject { machine_id: string; project_id: string }

/** One row of the per-Project charter list. */
export interface CharterSummary {
  project: CharterProject
  /** 0 when the Project has no charter yet. */
  version: number
  /** Bots in the Project, and how many used the current version on their last turn. */
  bots: number
  readers_current: number
  pending_proposals: number
}

export type CharterChangeKind = "create" | "edit" | "restore" | "proposal"

export interface CharterVersion {
  version: number
  /** Who saved it, as the source names them ("you", a Bot's name). */
  author: string
  author_kind: "person" | "bot"
  at: string
  kind: CharterChangeKind
  /** A short plain sentence about what changed. */
  reason: string
  /** For a restore, the version whose text it copied. */
  restored_from?: number
}

export interface CharterDocument {
  project: CharterProject
  version: number
  text: string
  updated_at: string
  /** Newest first. */
  history: CharterVersion[]
}

/** An older version's text with its predecessor, for the history diff. */
export interface CharterOldVersion { version: number; text: string; previous_text: string }

export interface CharterReader {
  bot_id: string
  name: string
  /** The charter version this Bot's latest turn used; 0 when it has not read one. */
  version_used: number
  /** A turn is running now, or one is queued behind it. */
  running: boolean
  queued: boolean
}

export interface CharterProposal {
  id: string
  project: CharterProject
  /** The version the proposal was written against. */
  base_version: number
  author: string
  /** The lead's short explanation of the change. */
  explanation: string
  /** The whole proposed document. */
  text: string
  at: string
}

export interface CharterSave { expected_version: number; text: string; reason: string }
export interface CharterRestore { expected_version: number; version: number }

export type CharterRefusalCode = "version_conflict" | "too_large" | "not_found" | "unknown"

/** A typed refusal; `current_version` accompanies a version conflict when known. */
export class CharterRefusal extends Error {
  readonly code: CharterRefusalCode
  readonly current_version?: number
  constructor(code: CharterRefusalCode, current_version?: number) {
    super(code)
    this.name = "CharterRefusal"
    this.code = code
    this.current_version = current_version
  }
}

export function charterRefusalCode(error: unknown): CharterRefusalCode {
  if (error instanceof CharterRefusal) return error.code
  const code = error && typeof error === "object" ? (error as { code?: unknown }).code : undefined
  return code === "version_conflict" || code === "too_large" || code === "not_found" ? code : "unknown"
}

export interface CharterApi {
  list(signal: AbortSignal): Promise<CharterSummary[]>
  read(project: CharterProject, signal: AbortSignal): Promise<CharterDocument>
  readVersion(project: CharterProject, version: number, signal: AbortSignal): Promise<CharterOldVersion>
  save(project: CharterProject, request: CharterSave, signal: AbortSignal): Promise<CharterDocument>
  restore(project: CharterProject, request: CharterRestore, signal: AbortSignal): Promise<CharterDocument>
  readers(project: CharterProject, signal: AbortSignal): Promise<CharterReader[]>
  proposals(project: CharterProject, signal: AbortSignal): Promise<CharterProposal[]>
  applyProposal(project: CharterProject, id: string, request: { expected_version: number }, signal: AbortSignal): Promise<CharterDocument>
  dismissProposal(project: CharterProject, id: string, signal: AbortSignal): Promise<void>
  /** A change asked for in words; the lead answers with a proposal. */
  requestChange(project: CharterProject, request: { text: string }, signal: AbortSignal): Promise<void>
}

/** A source that carries a charter implementation (the synthetic fixture does). */
type CharterCarrier = SourceClient & { charter?: CharterApi }

/**
 * The charter client for one machine, or undefined when it has none.
 * Integration point: route these calls to the daemon (and Cloud) here.
 */
export function charterApiFor(source: SourceClient | undefined): CharterApi | undefined {
  return (source as CharterCarrier | undefined)?.charter
}

export const charterProjectKey = (project: CharterProject) => JSON.stringify([project.machine_id, project.project_id])
