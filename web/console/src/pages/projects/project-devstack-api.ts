import type { DevStack, ProjectLink, ProjectServerSetup } from "@clawdline/contract"
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { catalogWord } from "../../catalog.ts"
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { ProjectFileError } from "./project-files-api.ts"

export interface ProjectDevStackReply {
  links: ProjectLink[]
  observedAt: number
  repository?: string
  devStack: ProjectServerSetup
  serverCount: number
  stack?: DevStack
  raw?: string
  root?: string
}

export async function readProjectDevStack(place: string): Promise<ProjectDevStackReply> {
  const url = `/v1/projects/${encodeURIComponent(place)}/devstack`
  let response: Response
  try {
    response = await fetch(url, { credentials: "same-origin" })
  } catch {
    throw new ProjectFileError("network", catalogWord("literal", "f1e5ad92e0dc"))
  }
  let data: any
  try {
    data = await response.json()
  } catch {
    throw new ProjectFileError("invalid_response", catalogWord("literal", "098b08604e84"))
  }
  if (!response.ok) {
    const code = typeof data?.error === "string" ? data.error : data?.error?.code || "unavailable"
    const detail = typeof data?.detail === "string" ? data.detail : undefined
    throw new ProjectFileError(code, detail || catalogWord("literal", "7f523f32a2c0"), false, response.status)
  }
  return data as ProjectDevStackReply
}
