import type { GraphifyView } from "@clawdline/contract"
import { catalogWord } from "../catalog.js"
import { client } from "../client.js"
import { RefusalError, isRefusal } from "@clawdline/core"

export type GraphifyTarget = { kind: "project" | "session"; id: string; label?: string }
export const GRAPHIFY_OPEN = "clawdline:graphify-open"
export const GRAPHIFY_SETTING = "clawdline:graphify-setting"
export const graphWord = (key: string) => catalogWord("graphifyView", key)
export function openGraphify(target: GraphifyTarget): void {
 window.dispatchEvent(new CustomEvent(GRAPHIFY_OPEN, { detail: target }))
}
export async function readGraphify(target: GraphifyTarget, signal: AbortSignal): Promise<GraphifyView> {
 const path = `/v1/${target.kind === "project" ? "projects" : "sessions"}/${encodeURIComponent(target.id)}/graphify`
 // Registered as projects.graphify_request_ms. One request, no retries.
 const deadline = AbortSignal.timeout(15000)
 const response = await fetch(client.url(path), { signal: AbortSignal.any([signal, deadline]), cache: "no-store" })
 const body: unknown = await response.json()
 if (!response.ok) {
  if (isRefusal(body)) throw new RefusalError(response.status, body, path)
  throw new Error(graphWord("failed"))
 }
 return body as GraphifyView
}
