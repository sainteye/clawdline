import { catalogWord } from "../../catalog.js"
import type { ProjectUnifyApplied, ProjectUnifyPlan } from "@clawdline/contract"

export interface ProjectFile {
  id: string
  name: string
  location: string
  source: "project" | "global"
  assistant: "codex" | "claude"
  kind: "instruction" | "skill"
  status: "ready" | "missing" | "unsafe" | "unreadable" | "too_large"
  editable: boolean
  size?: number
}

export interface ProjectFileList { files: ProjectFile[]; truncated: boolean; skipped: string[] }
export interface ProjectFileContent { file: ProjectFile; text: string; version: string }

export class ProjectFileError extends Error {
  readonly code: string
  readonly uncertain: boolean
  constructor(code: string, message: string, uncertain = false) {
    super(message)
    this.code = code
    this.uncertain = uncertain
  }
}

async function answer<T>(url: string, init?: RequestInit): Promise<T> {
  let response: Response
  try { response = await fetch(url, { credentials: "same-origin", ...init }) }
  catch { throw new ProjectFileError("network", catalogWord("literal", "f1e5ad92e0dc"), init?.method === "PUT") }
  let data: any
  try { data = await response.json() }
  catch { throw new ProjectFileError("invalid_response", catalogWord("literal", "098b08604e84"), init?.method === "PUT") }
  if (!response.ok) {
    const code = typeof data?.error === "string" ? data.error : data?.error?.code || "unavailable"
    const detail = typeof data?.detail === "string" ? data.detail : data?.error?.message
    const uncertain = init?.method === "PUT" && (data?.outcome === "unknown" || data?.error?.outcome === "unknown")
    throw new ProjectFileError(code, detail || catalogWord("literal", "7f523f32a2c0"), uncertain)
  }
  return data as T
}

const route = (place: string, id?: string) => `/v1/projects/${encodeURIComponent(place)}/files${id ? `/${encodeURIComponent(id)}` : ""}`

export const listProjectFiles = (place: string) => answer<ProjectFileList>(route(place))
export const readProjectFile = (place: string, id: string) => answer<ProjectFileContent>(route(place, id))
export const saveProjectFile = (place: string, id: string, expected: string, content: string, key: string) =>
  answer<ProjectFileContent>(route(place, id), {
    method: "PUT",
    headers: { "Content-Type": "application/json", "Idempotency-Key": key },
    body: JSON.stringify({ expected_version: expected, content }),
  })

// The unify plan and its apply (docs/project-files.md, Unify), in the same
// shape as the file routes above: a refusal is a `ProjectFileError` with the
// machine's code, and an apply whose answer was lost is `uncertain` — the
// screen reads the plan again before it says anything applied. They live in
// this file so that `node --test` loads them with no import to resolve.

const unifyRoute = (place: string) => `/v1/projects/${encodeURIComponent(place)}/unify`

export async function readUnifyPlan(place: string): Promise<ProjectUnifyPlan> {
  let response: Response
  try { response = await fetch(unifyRoute(place), { credentials: "same-origin" }) }
  catch { throw new ProjectFileError("network", catalogWord("literal", "3eab3f330eea")) }
  let data: any
  try { data = await response.json() }
  catch { throw new ProjectFileError("invalid_response", catalogWord("literal", "36d5d75c722f")) }
  if (!response.ok) throw refusal(data, false)
  return data as ProjectUnifyPlan
}

/**
 * Applies the plan the person read. A run that stopped part-way (`500` with
 * `outcome: "stopped"`) is an answer, not a throw: it carries what ran, what
 * failed and the plan read again from disk, which is what the screen shows.
 */
export async function applyUnify(place: string, version: string, key: string): Promise<ProjectUnifyApplied> {
  let response: Response
  try {
    response = await fetch(unifyRoute(place), {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json", "Idempotency-Key": key },
      body: JSON.stringify({ version }),
    })
  } catch { throw new ProjectFileError("network", catalogWord("literal", "460e4fdb7ab7"), true) }
  let data: any
  try { data = await response.json() }
  catch { throw new ProjectFileError("invalid_response", catalogWord("literal", "dfaaeab488da"), true) }
  if (data?.outcome === "stopped" && data?.plan && Array.isArray(data?.ran)) return data as ProjectUnifyApplied
  if (!response.ok) throw refusal(data, true)
  return data as ProjectUnifyApplied
}

function refusal(data: any, write: boolean): ProjectFileError {
  const code = typeof data?.error === "string" ? data.error : data?.error?.code || "unavailable"
  const detail = typeof data?.detail === "string" ? data.detail : data?.error?.message
  const uncertain = write && (data?.outcome === "unknown" || data?.error?.outcome === "unknown")
  return new ProjectFileError(code, detail || catalogWord("literal", "101992e0e2e3"), uncertain)
}
