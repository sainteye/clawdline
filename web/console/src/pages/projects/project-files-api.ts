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
  /** The HTTP status and refused route, kept so `asMachineNeedsUpdate` (core/src/refusal.ts) can read a 501. */
  readonly status: number | undefined
  readonly route: string | undefined
  constructor(code: string, message: string, uncertain = false, status?: number, route?: string) {
    super(message)
    this.code = code
    this.uncertain = uncertain
    this.status = status
    this.route = route
  }
}

async function answer<T>(url: string, init?: RequestInit): Promise<T> {
  let response: Response
  try { response = await fetch(url, { credentials: "same-origin", ...init }) }
  catch { throw new ProjectFileError("network", "連線中斷；請重新讀取確認目前檔案狀態。", init?.method === "PUT") }
  let data: any
  try { data = await response.json() }
  catch { throw new ProjectFileError("invalid_response", "機器回覆無法讀取；請重新讀取確認。", init?.method === "PUT") }
  if (!response.ok) {
    const code = typeof data?.error === "string" ? data.error : data?.error?.code || "unavailable"
    const detail = typeof data?.detail === "string" ? data.detail : data?.error?.message
    const uncertain = init?.method === "PUT" && (data?.outcome === "unknown" || data?.error?.outcome === "unknown")
    const route = typeof data?.route === "string" ? data.route : undefined
    throw new ProjectFileError(code, detail || "目前無法讀取這個檔案。", uncertain, response.status, route)
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
