import { ProjectFileError } from "./project-files-api.js"

export interface TreeEntry {
  name: string
  path: string
  kind: "directory" | "file" | "link" | "other"
  size?: number
}

export interface TreeListing { directory: string; entries: TreeEntry[]; truncated: boolean }
export interface TreeContent { path: string; text: string; size: number; version: string }

async function answer<T>(url: string): Promise<T> {
  let response: Response
  try { response = await fetch(url, { credentials: "same-origin" }) }
  catch { throw new ProjectFileError("network", "連線中斷。") }
  let body: any
  try { body = await response.json() }
  catch { throw new ProjectFileError("invalid_response", "機器回覆無法讀取。") }
  if (!response.ok) throw new ProjectFileError(body?.error ?? "unavailable", body?.detail ?? "檔案目前無法讀取。")
  return body as T
}

const base = (project: string) => `/v1/projects/${encodeURIComponent(project)}/tree`

export const listProjectDirectory = (project: string, directory = "") =>
  answer<TreeListing>(`${base(project)}?directory=${encodeURIComponent(directory)}`)

export const readProjectTreeFile = (project: string, path: string) =>
  answer<TreeContent>(`${base(project)}/file?path=${encodeURIComponent(path)}`)
