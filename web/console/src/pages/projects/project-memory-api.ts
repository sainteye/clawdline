import type { ProjectMemoryDream, ProjectMemoryEntry, ProjectMemoryList, ProjectMemoryReview, ProjectMemorySearch, ProjectMemoryWriteAnswer } from "@clawdline/contract"

export class ProjectMemoryError extends Error {
  constructor(readonly code: string, readonly status: number) { super(code) }
}

async function memoryRequest<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, { credentials: "same-origin", cache: "no-store", ...init })
  const data = await response.json().catch(() => null)
  if (!response.ok || !data) {
    const code = typeof data?.error === "string" ? data.error : data?.error?.code
    throw new ProjectMemoryError(code || "unavailable", response.status)
  }
  return data as T
}

const path = (project: string) => `/v1/projects/${encodeURIComponent(project)}/memory`
const entryPath = (project: string, entry: string) => `${path(project)}/${encodeURIComponent(entry)}`

export const listProjectMemory = (project: string, signal: AbortSignal) =>
  memoryRequest<ProjectMemoryList>(path(project), { signal })
export const readProjectDream = (project: string, signal: AbortSignal) =>
  memoryRequest<ProjectMemoryDream>(path(project) + "-dream", { signal })
export const searchProjectMemory = (project: string, query: string, signal: AbortSignal) =>
  memoryRequest<ProjectMemorySearch>(path(project) + "-search?" + new URLSearchParams({ query }), { signal })
export const readProjectMemory = (project: string, entry: string, signal: AbortSignal) =>
  memoryRequest<ProjectMemoryEntry>(entryPath(project, entry), { signal })

function writeMemory(url: string, key: string, review?: ProjectMemoryReview) {
  return memoryRequest<ProjectMemoryWriteAnswer>(url, {
    method: "POST",
    headers: { "Idempotency-Key": key, ...(review ? { "Content-Type": "application/json" } : {}) },
    ...(review ? { body: JSON.stringify(review) } : {}),
  })
}
export const restoreProjectMemory = (project: string, entry: string, key: string) =>
  writeMemory(entryPath(project, entry) + "/restore", key)
export const reviewProjectMemory = (project: string, entry: string, key: string, review: ProjectMemoryReview) =>
  writeMemory(entryPath(project, entry) + "/review", key, review)
