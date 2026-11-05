import type { CodeGraphView, CodeGraphBuildStatus } from "@clawdline/contract"
import { catalogWord } from "../catalog.js"
import { client } from "../client.js"
import { RefusalError, isRefusal } from "@clawdline/core"

export type CodeGraphTarget = { kind: "project" | "session"; id: string; label?: string }
export const CODEGRAPH_OPEN = "clawdline:codegraph-open"
export const CODEGRAPH_SETTING = "clawdline:codegraph-setting"
export const graphWord = (key: string) => catalogWord("codegraphView", key)
export function openCodeGraph(target: CodeGraphTarget): void {
 window.dispatchEvent(new CustomEvent(CODEGRAPH_OPEN, { detail: target }))
}
export async function readCodeGraph(target: CodeGraphTarget, signal: AbortSignal): Promise<CodeGraphView> {
 const path = `/v1/${target.kind === "project" ? "projects" : "sessions"}/${encodeURIComponent(target.id)}/codegraph`
 // Registered as projects.codegraph_request_ms. One request, no retries.
 const deadline = AbortSignal.timeout(15000)
 const response = await fetch(client.url(path), { signal: AbortSignal.any([signal, deadline]), cache: "no-store" })
 const body: unknown = await response.json()
 if (!response.ok) {
  if (isRefusal(body)) throw new RefusalError(response.status, body, path)
  throw new Error(graphWord("failed"))
 }
 return body as CodeGraphView
}

export async function buildCodeGraph(target:CodeGraphTarget,signal:AbortSignal):Promise<CodeGraphBuildStatus>{
 const path=`/v1/${target.kind === "project" ? "projects" : "sessions"}/${encodeURIComponent(target.id)}/codegraph`
 const response=await fetch(client.url(path),{method:"POST",body:"{}",headers:{"Content-Type":"application/json","Idempotency-Key":crypto.randomUUID()},signal:AbortSignal.any([signal,AbortSignal.timeout(15000)])})
 const body:unknown=await response.json()
 if (!response.ok){if(isRefusal(body))throw new RefusalError(response.status,body,path);throw new Error(graphWord("buildFailed"))}
 return body as CodeGraphBuildStatus
}
export function buildFailureWord(code:string):string{
 if(code==="unsupported_platform")return graphWord("buildPrerequisite")
 if(code==="preparation_failed" || code==="runtime_unverified")return graphWord("buildPreparationFailed")
 if(code==="source_changed")return graphWord("buildSourceChanged")
 if(code==="custom_inputs")return graphWord("buildCustomInputs")
 if(code==="disabled")return graphWord("disabled")
 if(code==="stopped" || code==="unavailable")return graphWord("buildStopped")
 if(code==="timeout")return graphWord("buildTimeout")
 if(["source_limit","graph_limit","runtime_limit"].includes(code))return graphWord("buildLimit")
 if(code==="parser_failed")return graphWord("buildParserFailed")
 if(code==="unsafe_source")return graphWord("buildUnsafeSource")
 if(code==="service_full")return graphWord("buildServiceFull")
 if(code==="busy")return graphWord("buildBusy")
 if(code==="history_full")return graphWord("buildHistoryFull")
 if(code==="no_sources")return graphWord("buildNoSources")
 if(code==="index_exists")return graphWord("buildIndexExists")
 return graphWord("buildFailed")
}
