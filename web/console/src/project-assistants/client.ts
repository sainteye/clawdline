import type * as W from "@clawdline/contract"

/** Source authority is captured by the host, never read from the active-machine singleton. */
export interface SourceClient {
  readonly machineID: string
  readonly authorizationRevision: string
  status(signal: AbortSignal): Promise<W.ProjectAssistantSourcesResponse>
  projects(signal: AbortSignal): Promise<W.ProjectCatalogAnswer>
  list(request: W.ProjectAssistantListRequest, signal: AbortSignal): Promise<W.ProjectAssistantListResponse>
  create(request: W.ProjectAssistantCreateRequest, signal: AbortSignal): Promise<W.ProjectAssistantBotResponse>
  get(request: W.ProjectAssistantBotKey, signal: AbortSignal): Promise<W.ProjectAssistantGetResponse>
  edit(request: W.ProjectAssistantEditRequest, signal: AbortSignal): Promise<W.ProjectAssistantBotResponse>
  history(request: W.ProjectAssistantHistoryRequest, signal: AbortSignal): Promise<W.ProjectAssistantHistoryResponse>
  send(request: W.ProjectAssistantSendRequest, signal: AbortSignal): Promise<W.ProjectAssistantReceiptResponse>
  stop(request: W.ProjectAssistantStopRequest, signal: AbortSignal): Promise<W.ProjectAssistantReceiptResponse>
  answer(request: W.ProjectAssistantPermissionAnswerRequest, signal: AbortSignal): Promise<W.ProjectAssistantReceiptResponse>
  markRead(request: W.ProjectAssistantReadRequest, signal: AbortSignal): Promise<W.ProjectAssistantReadResponse>
  observe(request: W.ProjectAssistantObserveRequest, signal: AbortSignal): Promise<W.ProjectAssistantReceiptResponse>
  events(request: W.ProjectAssistantEventsRequest, signal: AbortSignal): Promise<W.ProjectAssistantEventsResponse>
  forget(request: W.ProjectAssistantForgetRequest, signal: AbortSignal): Promise<W.ProjectAssistantMemoryResponse>
  saveMemory(request: W.ProjectAssistantMemoryWriteRequest, signal: AbortSignal): Promise<W.ProjectAssistantMemoryResponse>
  export(request: W.ProjectAssistantBotKey, signal: AbortSignal): Promise<W.ProjectAssistantExport>
}

export function failure(code: W.ProjectAssistantErrorCode): W.ProjectAssistantErrorResponse {
  return { contract_version: "project-assistants.v1", error: { code, field: "", retry_after_seconds: 0 } }
}

export function errorCode(error: unknown): W.ProjectAssistantErrorCode | "unavailable" {
  const value = error as Partial<W.ProjectAssistantErrorResponse> | null
  return value?.error?.code ?? "unavailable"
}

/** Fence both before and after an await; grant changes invalidate late operations. */
export async function authorized<T>(source: SourceClient, current: () => readonly SourceClient[],
  run: (signal: AbortSignal) => Promise<T>, signal: AbortSignal): Promise<T> {
  const revision = source.authorizationRevision
  const check = () => {
    if (signal.aborted) throw new DOMException("Aborted", "AbortError")
    if (!current().some(s => s === source && s.machineID === source.machineID && s.authorizationRevision === revision)) throw failure("pa_revoked")
  }
  check()
  const result = await run(signal)
  check()
  return result
}

export function command(key: W.ProjectAssistantBotKey, operation: W.ProjectAssistantOperation, id: string = crypto.randomUUID()): W.ProjectAssistantCommand {
  // The source recomputes and validates the content hash, per the frozen contract.
  return { key, operation, idempotency_key: id, content_hash: "" }
}
