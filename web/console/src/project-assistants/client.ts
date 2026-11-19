import type * as W from "@clawdline/contract"

/** Notification routing selects a chat; the live Bot still supplies browser authority. */
export function botFromFragment(hash: string): {bot:string;conversation:string;machine:string} | null {
  const fields=new URLSearchParams(hash.replace(/^#/,""))
  if(fields.getAll("bot").length!==1||fields.getAll("conversation").length!==1||fields.getAll("machine").length>1||fields.has("session"))return null
  const bot=fields.get("bot"),conversation=fields.get("conversation")
  return bot&&conversation?{bot,conversation,machine:fields.get("machine")??""}:null
}

/** Source authority is captured by the host, never read from the active-machine singleton. */
export interface SourceClient {
 canReactions?():boolean
 react?(request:W.ProjectAssistantReactRequest,signal:AbortSignal):Promise<W.ProjectAssistantReceiptResponse>
 canNetworkSettings?():boolean
 networkSettings?(key:W.ProjectAssistantBotKey,conversation:string,signal:AbortSignal):Promise<W.ProjectAssistantNetworkSettingsResponse>
 saveNetworkSettings?(request:W.ProjectAssistantNetworkSettingsRequest,signal:AbortSignal):Promise<W.ProjectAssistantNetworkSettingsResponse>
 canConnectorCredentials?():boolean
 connectorCredential?(request:W.ProjectAssistantConnectorCredentialAction,signal:AbortSignal):Promise<W.ProjectAssistantConnectorCredentialResponse>
 connectorSecret?(request:W.ProjectAssistantConnectorCredentialAction,value:string,signal:AbortSignal):Promise<W.ProjectAssistantConnectorCredentialResponse>
 canKeyProfiles?():boolean
 canFallbackProfiles?():boolean
 loginSecret?(request:W.ProjectAssistantLoginProfileAction,value:string,signal:AbortSignal):Promise<W.ProjectAssistantLoginProfileResponse>
 canLoginProfiles?():boolean
 loginProfiles?(key:W.ProjectAssistantBotKey,conversation:string,agent:W.ProjectAssistantAgent,signal:AbortSignal):Promise<W.ProjectAssistantLoginProfilesResponse>
 loginProfileAction?(request:W.ProjectAssistantLoginProfileAction,signal:AbortSignal):Promise<W.ProjectAssistantLoginProfileResponse>
  extensions?(key:W.ProjectAssistantBotKey,conversation:string,signal:AbortSignal):Promise<W.ProjectAssistantExtensionsResponse>
  canTopicMemory?():boolean
  canMemorySettings?():boolean
  memoryView?(key:W.ProjectAssistantBotKey,conversation:string,signal:AbortSignal,detail?:{source_epoch:string;layer:string;entry_id:string;revision:string}):Promise<W.ProjectAssistantMemoryViewResponse>
  configuration?(key:W.ProjectAssistantBotKey,conversation:string,agent:W.ProjectAssistantAgent,model:string,signal:AbortSignal,loginProfile?:string):Promise<W.ProjectAssistantConfigurationResponse>
  canRenameArtifact?(): boolean
  renameArtifact?(request:W.ProjectAssistantRenameArtifactRequest,signal:AbortSignal):Promise<W.ProjectAssistantReceiptResponse>
  /** Account-scoped identity for persisting uncertain metadata commands. */
  readonly intentAccount?: string
  canGroups?(): boolean
  canGroupContext?(): boolean
  canGroupReactions?(): boolean
  groupReact?(request:W.ProjectAssistantGroupReactRequest,signal:AbortSignal):Promise<W.ProjectAssistantReceiptResponse>
  groups?(request:W.ProjectAssistantGroupListRequest,signal:AbortSignal):Promise<W.ProjectAssistantGroupListResponse>
  groupHistory?(request:W.ProjectAssistantGroupHistoryRequest,signal:AbortSignal):Promise<W.ProjectAssistantGroupHistoryResponse>
  createGroup?(request:W.ProjectAssistantGroupCreateRequest,signal:AbortSignal):Promise<W.ProjectAssistantGroupResponse>
  editGroup?(request:W.ProjectAssistantGroupEditRequest,signal:AbortSignal):Promise<W.ProjectAssistantGroupResponse>
  deleteGroup?(request:W.ProjectAssistantGroupDeleteRequest,signal:AbortSignal):Promise<W.ProjectAssistantReceiptResponse>
  stopGroup?(request:W.ProjectAssistantGroupStopRequest,signal:AbortSignal):Promise<W.ProjectAssistantGroupStopResponse>
  sendGroup?(request:W.ProjectAssistantGroupSendRequest,signal:AbortSignal):Promise<W.ProjectAssistantGroupSendResponse>
  teamPlans?(request:W.ProjectAssistantTeamPlansRequest,signal:AbortSignal):Promise<W.ProjectAssistantTeamPlansResponse>
  answerTeamPlan?(request:W.ProjectAssistantTeamAnswerRequest,signal:AbortSignal):Promise<W.ProjectAssistantTeamAnswerResponse>
  /** Bot browser calls use this captured source, never the selected-machine fetch. */
  browser?<T>(key: W.ProjectAssistantBotKey, conversation: string, method: "GET" | "POST", path: string, body: unknown, signal: AbortSignal): Promise<T>
  browserUnavailable?(): string
  readonly browserRemote?: boolean
  /** API72 configuration support, independent of native execution qualification. */
  canFallbacks?(): boolean
  canRoster?(): boolean
  subscribeRoster?(request: W.ProjectAssistantRosterRequest, receive: (frame: W.ProjectAssistantRosterFrame) => void, signal: AbortSignal): Promise<void>
	canInputs?(): boolean
  upload?(request:W.ProjectAssistantUploadRequest,signal:AbortSignal):Promise<W.ProjectAssistantUploadResponse>
  input?(request:W.ProjectAssistantInputRequest,signal:AbortSignal):Promise<W.ProjectAssistantInputResponse>
  current?(): boolean
	canOutputs?(): boolean
	output?(request:W.ProjectAssistantOutputRequest,signal:AbortSignal):Promise<W.ProjectAssistantOutputResponse>
  readonly machineID: string
  /** The machine's own name, when known; the roster says "this machine" otherwise. */
  readonly machineName?: string
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
  canHandoff?(): boolean
  handoff?(key: W.ProjectAssistantBotKey, conversation: string, signal: AbortSignal): Promise<W.ProjectAssistantHandoffResponse>
  writeHandoff?(request: W.ProjectAssistantHandoffRequest, signal: AbortSignal): Promise<W.ProjectAssistantHandoffResponse>
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
  /** Sends one queued message into the running turn (Codync's steer). */
  sendNow?(request: W.ProjectAssistantSendNowRequest, signal: AbortSignal): Promise<W.ProjectAssistantReceiptResponse>
  canSendNow?(): boolean
  /** A Claude Bot's standing rules for its Project (Always allow / Never), and revoking one. */
  rules?(key: W.ProjectAssistantBotKey, signal: AbortSignal): Promise<W.ProjectAssistantRulesResponse>
  removeRule?(request: W.ProjectAssistantRuleRemoveRequest, signal: AbortSignal): Promise<W.ProjectAssistantRulesResponse>
  canRules?(): boolean
  /** Exact source-owned server rules require the complete server capability. */
  canSourceRules?(): boolean
  canDevServers?(): boolean
  devServers?(key:W.ProjectAssistantBotKey,signal:AbortSignal):Promise<W.ProjectAssistantDevServersResponse>
  devServerLogs?(key:W.ProjectAssistantBotKey,id:string,signal:AbortSignal):Promise<W.ProjectAssistantDevServersResponse>
  controlDevServer?(action:"start"|"restart"|"stop"|"remove",request:W.ProjectAssistantDevServerControlRequest,signal:AbortSignal):Promise<W.ProjectAssistantDevServersResponse>
  /** Hide a Bot from the list or bring it back, and delete one (Codync's Hide from list and Delete). */
  setHidden?(request: W.ProjectAssistantVisibilityRequest, signal: AbortSignal): Promise<W.ProjectAssistantBotResponse>
  deleteBot?(request: W.ProjectAssistantDeleteRequest, signal: AbortSignal): Promise<unknown>
  canManage?(): boolean
  canRoutineEvents?(): boolean
  routineHook?(request: W.ProjectAssistantRoutineHookRequest, signal: AbortSignal): Promise<W.ProjectAssistantRoutineHookResponse>
  canRoutines?(): boolean
  routines?(request: W.ProjectAssistantRoutineListRequest, signal: AbortSignal): Promise<W.ProjectAssistantRoutineListResponse>
  saveRoutine?(request: W.ProjectAssistantRoutineSaveRequest, signal: AbortSignal): Promise<W.ProjectAssistantReceiptResponse>
  enableRoutine?(request: W.ProjectAssistantRoutineEnabledRequest, signal: AbortSignal): Promise<W.ProjectAssistantReceiptResponse>
  deleteRoutine?(request: W.ProjectAssistantRoutineActionRequest, signal: AbortSignal): Promise<W.ProjectAssistantReceiptResponse>
  testRoutine?(request: W.ProjectAssistantRoutineActionRequest, signal: AbortSignal): Promise<W.ProjectAssistantReceiptResponse>
  /** Where the console reads a stored picture, or null when it cannot. */
  attachment?(key: W.ProjectAssistantBotKey, id: string, signal: AbortSignal): Promise<Blob>
  attachmentURL?(key: W.ProjectAssistantBotKey, id: string): string | null
}

export function failure(code: W.ProjectAssistantErrorCode): W.ProjectAssistantErrorResponse {
  return { contract_version: "project-assistants.v1", error: { code, field: "", retry_after_seconds: 0 } }
}

export function errorCode(error: unknown): W.ProjectAssistantErrorCode | "unavailable" {
  const value = error as Partial<W.ProjectAssistantErrorResponse> | null
  return value?.error?.code ?? "unavailable"
}

/**
 * A write failure that provably never left this client: the request was
 * refused before any byte reached a transport. This is the generic transport
 * proof. A source's explicit pre-admission domain refusal is a different fact;
 * other write failures may have crossed and remain uncertain.
 * The mark is not enumerable, so the error's own envelope is unchanged.
 */
export function notSent<T>(error: T): T {
  if (typeof error === "object" && error !== null && Object.isExtensible(error)) Object.defineProperty(error, "clawdline_delivery", { value: "not_sent", configurable: true })
  return error
}
export function provablyNotSent(error: unknown): boolean {
  return typeof error === "object" && error !== null && (error as { clawdline_delivery?: unknown }).clawdline_delivery === "not_sent"
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
  try { check() } catch (error) { throw notSent(error) }
  const result = await run(signal)
  check()
  return result
}

export function command(key: W.ProjectAssistantBotKey, operation: W.ProjectAssistantOperation, id: string = crypto.randomUUID()): W.ProjectAssistantCommand {
  // The source recomputes and validates the content hash, per the frozen contract.
  return { key, operation, idempotency_key: id, content_hash: "" }
}
