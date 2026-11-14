import type * as W from "@clawdline/contract"

/** Source authority is captured by the host, never read from the active-machine singleton. */
export interface SourceClient {
  readonly machineID: string
  readonly authorizationRevision: string
  status(signal: AbortSignal): Promise<W.ProjectAssistantSourcesResponse>
  projects(request: W.ProjectAssistantProjectsRequest, signal: AbortSignal): Promise<W.ProjectAssistantProjectsResponse>
  list(request: W.ProjectAssistantListRequest, signal: AbortSignal): Promise<W.ProjectAssistantListResponse>
  create(request: W.ProjectAssistantCreateRequest, signal: AbortSignal): Promise<W.ProjectAssistantBotResponse>
  get(request: W.ProjectAssistantBotKey, signal: AbortSignal): Promise<W.ProjectAssistantGetResponse>
  edit(request: W.ProjectAssistantEditRequest, signal: AbortSignal): Promise<W.ProjectAssistantBotResponse>
  history(request: W.ProjectAssistantHistoryRequest, signal: AbortSignal): Promise<W.ProjectAssistantHistoryResponse>
  view(request: W.ProjectAssistantReadViewRequest, signal: AbortSignal): Promise<W.ProjectAssistantReadViewResponse>
  send(request: W.ProjectAssistantSendRequest, signal: AbortSignal): Promise<W.ProjectAssistantReceiptResponse>
  stop(request: W.ProjectAssistantStopRequest, signal: AbortSignal): Promise<W.ProjectAssistantReceiptResponse>
  answer(request: W.ProjectAssistantPermissionAnswerRequest, signal: AbortSignal): Promise<W.ProjectAssistantReceiptResponse>
  markRead(request: W.ProjectAssistantReadRequest, signal: AbortSignal): Promise<W.ProjectAssistantReadResponse>
  observe(request: W.ProjectAssistantObserveRequest, signal: AbortSignal): Promise<W.ProjectAssistantReceiptResponse>
  events(request: W.ProjectAssistantEventsRequest, signal: AbortSignal): Promise<W.ProjectAssistantEventsResponse>
  forget(request: W.ProjectAssistantForgetRequest, signal: AbortSignal): Promise<W.ProjectAssistantMemoryResponse>
  saveMemory(request: W.ProjectAssistantMemoryWriteRequest, signal: AbortSignal): Promise<W.ProjectAssistantMemoryResponse>
  export(request: W.ProjectAssistantBotKey, signal: AbortSignal): Promise<W.ProjectAssistantExport>
  /** Whether this source accepts pictures with a message. */
  pictures?(): boolean
  /** Takes one queued message back before it is sent: Stop naming that queued turn. */
  unqueue?(request: W.ProjectAssistantStopRequest, signal: AbortSignal): Promise<W.ProjectAssistantReceiptResponse>
  canUnqueue?(): boolean
  /** Where the console reads a stored picture, or null when it cannot. */
  attachmentURL?(key: W.ProjectAssistantBotKey, id: string): string | null
}

export function failure(code: W.ProjectAssistantErrorCode): W.ProjectAssistantErrorResponse {
  return { contract_version: "project-assistants.v1", error: { code, field: "", retry_after_seconds: 0 } }
}

export function errorCode(error: unknown): W.ProjectAssistantErrorCode | "unavailable" {
  const value = error as Partial<W.ProjectAssistantErrorResponse> | null
  return value?.error?.code ?? "unavailable"
}

/** Validate metadata before publishing choices, including injected host clients. */
export function projectChoices(answer: W.ProjectAssistantProjectsResponse, request: W.ProjectAssistantProjectsRequest): W.ProjectAssistantProjectsResponse {
  if (!answer || answer.contract_version !== "project-assistants.v1" || !answer.source ||
    typeof answer.revision !== "string" || !answer.revision || !Array.isArray(answer.items) ||
    answer.items.length > request.page_size || !Array.isArray(answer.agents) ||
    answer.agents.some(a => a !== "claude" && a !== "codex") || new Set(answer.agents).size !== answer.agents.length ||
    typeof answer.has_more !== "boolean" || typeof answer.next_cursor !== "string" ||
    answer.has_more !== !!answer.next_cursor || (answer.has_more && (!answer.items.length || answer.next_cursor === request.cursor))) throw failure("pa_invalid")
  if (answer.source.machine_id !== request.machine_id) throw failure("pa_scope_denied")
  if (answer.source.state === "revoked") throw failure("pa_revoked")
  if (!answer.source.fresh || answer.source.state !== "online") throw failure("pa_offline")
  if (!answer.source.readable || answer.source.access === "hidden") throw failure("pa_scope_denied")
  const ids = new Set<string>()
  for (const item of answer.items) {
    if (!item || !item.target || typeof item.target.project_id !== "string" || !item.target.project_id || typeof item.label !== "string") throw failure("pa_invalid")
    if (item.target.machine_id !== request.machine_id) throw failure("pa_scope_denied")
    if (ids.has(item.target.project_id)) throw failure("pa_invalid")
    ids.add(item.target.project_id)
  }
  return answer
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
