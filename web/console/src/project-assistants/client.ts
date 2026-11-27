import type { BotReadMode, BotReadObservation } from "./read-observation"
import type {CapabilityGap} from "./source-errors"
import type { WorkV2Item } from "../pages/work/api"
import type * as W from "@clawdline/contract"
// @ts-expect-error -- Node's strip-types runner loads the generated source extension.
import {ProjectAssistantAgentValues} from "../../../contract/src/generated.ts"

/** Notification routing selects a chat; the live Bot still supplies browser authority. */
export function botFromFragment(hash: string): {bot:string;conversation:string;machine:string} | null {
  const fields=new URLSearchParams(hash.replace(/^#/,""))
  if(fields.getAll("bot").length!==1||fields.getAll("conversation").length!==1||fields.getAll("machine").length>1||fields.has("session"))return null
  const bot=fields.get("bot"),conversation=fields.get("conversation")
  return bot&&conversation?{bot,conversation,machine:fields.get("machine")??""}:null
}

/** One machine's Bots shared settings (dispatch rules and shared memory), API 116. */
export type SharedSettingsDocument={version:number;rules:unknown[];memory:unknown[];memory_opt_out?:string[];project_overrides?:unknown;peer_dispatch_cards?:boolean}
export type SharedSettingsAnswer<S=SharedSettingsDocument>={contract_version:"project-assistants.v1";machine_id:string;settings:S;agents:{agent:string;efforts:string[]}[];limits:Record<string,number>}
export type SharedSettingsWrite<S=SharedSettingsDocument>={machine_id:string;idempotency_key:string;expected_version:number;settings:S}

export type MemoryReadDetail={source_epoch:string;layer:string;entry_id:string;revision:string}
export type ProjectMemoryPage={page:number;page_size:number}

/** Source authority is captured by the host, never read from the active-machine singleton. */
/** A charter write's body: always the machine, Project and idempotency key. */
export type CharterRequestBody = W.ProjectAssistantCharterSaveRequest | W.ProjectAssistantCharterRestoreRequest | W.ProjectAssistantCharterRemoveRequest | W.ProjectAssistantCharterApplyRequest | W.ProjectAssistantCharterDismissRequest | W.ProjectAssistantCharterChangeRequest | W.ProjectAssistantCharterConflictResolveRequest | W.ProjectAssistantCharterPlaybookSaveRequest | W.ProjectAssistantCharterPlaybookRemoveRequest | W.ProjectAssistantCharterWorkflowSaveRequest

export interface SourceClient {
  beginReadObservation?(mode: BotReadMode, start: number, interactionID?: string): BotReadObservation | undefined
  canFirstRun?(): boolean
  firstRun?(signal: AbortSignal): Promise<W.ProjectAssistantFirstRun>
 canReactions?():boolean
 react?(request:W.ProjectAssistantReactRequest,signal:AbortSignal):Promise<W.ProjectAssistantReceiptResponse>
 /** The machine's Bots tower floor placements (API 97). */
 canTowerFloors?():boolean
 /** The machine's Bots shared settings, read and saved whole with the version read. */
 sharedSettings?(signal:AbortSignal):Promise<SharedSettingsAnswer>
 saveSharedSettings?(request:SharedSettingsWrite,signal:AbortSignal):Promise<SharedSettingsAnswer>
 /** The daemon's one model catalog (API 120): every agent, or the one named. */
 canModelCatalog?():boolean
 modelCatalog?(agent:string|undefined,signal:AbortSignal):Promise<W.ModelCatalogAnswer>
 towerFloors?(signal:AbortSignal):Promise<W.ProjectAssistantTowerFloorsResponse>
 /** Team charters (API 122); charter-api.ts shapes the path, query and body. */
 canCharters?():boolean
 charterRequest?<T>(path:string,method:"GET"|"PUT"|"POST",body:CharterRequestBody|undefined,signal:AbortSignal):Promise<T>
 /** Removing a charter (API 123). */
 canRemoveCharter?():boolean
 /** The Bots' charter conflict reports and their resolve route (API 124). */
 canCharterConflicts?():boolean
 /** Charter playbooks, their writes and who heard each (API 127). */
 canCharterPlaybooks?():boolean
 /** Charter workflows, workflow proposals and the change-request target (API 128). */
 canCharterWorkflow?():boolean
 saveTowerFloor?(request:W.ProjectAssistantTowerFloorRequest,signal:AbortSignal):Promise<W.ProjectAssistantTowerFloorsResponse>
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
 canGrok?():boolean
 canGemini?():boolean
 loginProfiles?(key:W.ProjectAssistantBotKey,conversation:string,agent:W.ProjectAssistantAgent,signal:AbortSignal):Promise<W.ProjectAssistantLoginProfilesResponse>
 loginProfileAction?(request:W.ProjectAssistantLoginProfileAction,signal:AbortSignal):Promise<W.ProjectAssistantLoginProfileResponse>
  extensions?(key:W.ProjectAssistantBotKey,conversation:string,signal:AbortSignal):Promise<W.ProjectAssistantExtensionsResponse>
  canTopicMemory?():boolean
  canMemorySettings?():boolean
  canProjectMemoryPages?():boolean
  memoryView?(key:W.ProjectAssistantBotKey,conversation:string,signal:AbortSignal,detail?:MemoryReadDetail,page?:ProjectMemoryPage):Promise<W.ProjectAssistantMemoryViewResponse>
  configuration?(key:W.ProjectAssistantBotKey,conversation:string,agent:W.ProjectAssistantAgent,model:string,signal:AbortSignal,loginProfile?:string):Promise<W.ProjectAssistantConfigurationResponse>
  canRenameArtifact?(): boolean
  renameArtifact?(request:W.ProjectAssistantRenameArtifactRequest,signal:AbortSignal):Promise<W.ProjectAssistantReceiptResponse>
  /** Account-scoped identity for persisting uncertain metadata commands. */
  readonly intentAccount?: string
  canGroups?(): boolean
  /** Why a capability gated by the source's API level is missing (source-errors.ts). */
  capabilityGap?(): CapabilityGap
  canTeamCards?(): boolean
  canDeferTraceCount?(): boolean
  /** Chat pages can carry exchange excerpts; the exchange panel still reads the full text (API 125). */
  canExchangeExcerpts?(): boolean
  canReadProgress?(): boolean
  /** The source rebases a settings save on its base and names conflicting fields (API 117). */
  canSettingsBase?(): boolean
  canReplyInput?(): boolean
  canLeadTeamDefault?(): boolean
  canGroupContext?(): boolean
  canGroupDescription?(): boolean
  canPersonalGroups?(): boolean
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
  workItem?(work: W.ProjectAssistantWorkLink, signal: AbortSignal): Promise<WorkV2Item>
  readonly machineID: string
  /** The machine's own name, when known; the roster says "this machine" otherwise. */
  readonly machineName?: string
  readonly authorizationRevision: string
  status(signal: AbortSignal): Promise<W.ProjectAssistantSourcesResponse>
  projects(request: W.ProjectAssistantProjectsRequest, signal: AbortSignal): Promise<W.ProjectAssistantProjectsResponse>
  list(request: W.ProjectAssistantListRequest, signal: AbortSignal, onFailure?: (failure: RosterFailure) => void): Promise<W.ProjectAssistantListResponse>
  create(request: W.ProjectAssistantCreateRequest, signal: AbortSignal): Promise<W.ProjectAssistantBotResponse>
  get(request: W.ProjectAssistantBotKey, signal: AbortSignal): Promise<W.ProjectAssistantGetResponse>
  edit(request: W.ProjectAssistantEditRequest, signal: AbortSignal): Promise<W.ProjectAssistantBotResponse>
  history(request: W.ProjectAssistantHistoryRequest, signal: AbortSignal): Promise<W.ProjectAssistantHistoryResponse>
  view(request: W.ProjectAssistantReadViewRequest, signal: AbortSignal, observation?: BotReadObservation): Promise<W.ProjectAssistantReadViewResponse>
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
  sendReceipt?(request: W.ProjectAssistantSendRequest, signal: AbortSignal): Promise<W.ProjectAssistantReceiptResponse>
  canRecoverSend?(): boolean
  export(request: W.ProjectAssistantBotKey, signal: AbortSignal): Promise<W.ProjectAssistantExport>
  /** Whether this source accepts pictures with a message. */
  pictures?(): boolean
  /** Takes one queued message back before it is sent: Stop naming that queued turn. */
  unqueue?(request: W.ProjectAssistantStopRequest, signal: AbortSignal): Promise<W.ProjectAssistantReceiptResponse>
  canUnqueue?(): boolean
  /** Sends one queued message into the running turn (Codync's steer). */
  canEditQueue?(): boolean
  editQueue?(request: W.ProjectAssistantQueueEditRequest, signal: AbortSignal): Promise<W.ProjectAssistantReceiptResponse>
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
  canRosterActions?(): boolean
  canRoutineEvents?(): boolean
  routineHook?(request: W.ProjectAssistantRoutineHookRequest, signal: AbortSignal): Promise<W.ProjectAssistantRoutineHookResponse>
  canRoutines?(): boolean
  canRoutinePreview?(): boolean
  routinePreview?(request: W.ProjectAssistantRoutinePreviewRequest, signal: AbortSignal): Promise<W.ProjectAssistantRoutinePreviewResponse>
  routines?(request: W.ProjectAssistantRoutineListRequest, signal: AbortSignal): Promise<W.ProjectAssistantRoutineListResponse>
  saveRoutine?(request: W.ProjectAssistantRoutineSaveRequest, signal: AbortSignal): Promise<W.ProjectAssistantReceiptResponse>
  enableRoutine?(request: W.ProjectAssistantRoutineEnabledRequest, signal: AbortSignal): Promise<W.ProjectAssistantReceiptResponse>
  deleteRoutine?(request: W.ProjectAssistantRoutineActionRequest, signal: AbortSignal): Promise<W.ProjectAssistantReceiptResponse>
  testRoutine?(request: W.ProjectAssistantRoutineActionRequest, signal: AbortSignal): Promise<W.ProjectAssistantReceiptResponse>
  /** Where the console reads a stored picture, or null when it cannot. */
  attachment?(key: W.ProjectAssistantBotKey, id: string, signal: AbortSignal): Promise<Blob>
  attachmentURL?(key: W.ProjectAssistantBotKey, id: string): string | null
}

export interface RosterFailure {
  machine: string
  code: string
  stage: "admission" | "request" | "answer"
  transport_code: string | null
  status: number | null
  has_ref: boolean
  request_seq: number | null
  api_level: number | null
}

export interface RosterScreenEvidence {
  machine: string | null
  source_count: number
  sources: string[]
  searching: boolean
  order: string
  online: boolean
}

export function failure(code: W.ProjectAssistantErrorCode): W.ProjectAssistantErrorResponse {
  return { contract_version: "project-assistants.v1", error: { code, field: "", retry_after_seconds: 0 } }
}

export function errorCode(error: unknown): W.ProjectAssistantErrorCode | "unavailable" {
  const value = error as Partial<W.ProjectAssistantErrorResponse> | null
  return value?.error?.code ?? "unavailable"
}

export function sendBlockedByInterruptedTurn(error: unknown): boolean {
  const refusal = (error as Partial<W.ProjectAssistantErrorResponse> | null)?.error
  return refusal?.field === "inspect_interrupted_turn" &&
    (refusal.code === "pa_stale_version" || refusal.code === "pa_outcome_unknown")
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
    answer.agents.some(a => !ProjectAssistantAgentValues.includes(a)) || new Set(answer.agents).size !== answer.agents.length ||
    (answer.personal_workspace !== undefined && typeof answer.personal_workspace !== "boolean") ||
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

/** The refusal's field, or "" for an error without an envelope. */
export function errorField(error: unknown): string {
  return (error as Partial<W.ProjectAssistantErrorResponse> | null)?.error?.field ?? ""
}

/**
 * Whether a write that reached its transport may have taken effect. A typed
 * refusal from the source wrote nothing; only an envelope that says the
 * outcome is unknown, a transport failure without one, an abort after the
 * request left, or this client's own authority recheck after the answer
 * came back (a fieldless `pa_revoked`) leave the effect uncertain.
 */
export function uncertainAfterSend(error: unknown): boolean {
  if (provablyNotSent(error) || sendBlockedByInterruptedTurn(error)) return false
  if ((error as Error | null)?.name === "AbortError") return true
  const code = errorCode(error)
  if (code === "pa_outcome_unknown" || code === "execution_effect_unknown" || code === "unavailable") return true
  return code === "pa_revoked" && errorField(error) === ""
}

/** A picture's long edge after downscaling, and the size worth re-encoding above. */
export const PICTURE_LONG_EDGE = 2048
export const PICTURE_KEEP_BYTES = 1_500_000
export const PICTURE_QUALITY = 0.85

/**
 * The size a picture is sent at: null keeps the original bytes. Phone photos
 * are often 4000px or more and several megabytes; sent raw they approach the
 * daemon's send bound and slow every hop. Animated GIFs and SVG keep theirs.
 */
export function pictureTarget(width: number, height: number, bytes: number, type: string): { width: number, height: number, type: "image/jpeg" | "image/png" } | null {
  if (type === "image/gif" || type === "image/svg+xml" || !(width > 0 && height > 0)) return null
  const scale = Math.min(1, PICTURE_LONG_EDGE / Math.max(width, height))
  if (scale === 1 && bytes <= PICTURE_KEEP_BYTES) return null
  return { width: Math.max(1, Math.round(width * scale)), height: Math.max(1, Math.round(height * scale)), type: type === "image/png" && bytes <= PICTURE_KEEP_BYTES ? "image/png" : "image/jpeg" }
}

function readDataURL(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result))
    reader.onerror = () => reject(reader.error)
    reader.readAsDataURL(blob)
  })
}

/** A picture as a data URL, downscaled when it is large; the original when it cannot be decoded. */
export async function pictureDataURL(file: File): Promise<string> {
  let bitmap: ImageBitmap | null = null
  try {
    if (typeof createImageBitmap !== "function" || typeof document === "undefined") return await readDataURL(file)
    bitmap = await createImageBitmap(file, { imageOrientation: "from-image" })
    const target = pictureTarget(bitmap.width, bitmap.height, file.size, file.type)
    if (!target) return await readDataURL(file)
    const canvas = document.createElement("canvas")
    canvas.width = target.width; canvas.height = target.height
    const context = canvas.getContext("2d")
    if (!context) return await readDataURL(file)
    if (target.type === "image/jpeg") { context.fillStyle = "#fff"; context.fillRect(0, 0, target.width, target.height) }
    context.drawImage(bitmap, 0, 0, target.width, target.height)
    const blob = await new Promise<Blob | null>(resolve => canvas.toBlob(resolve, target.type, PICTURE_QUALITY))
    return blob && blob.size < file.size ? await readDataURL(blob) : await readDataURL(file)
  } catch {
    return await readDataURL(file)
  } finally { bitmap?.close() }
}

/** The source left the message queued because this agent cannot take it mid-turn. */
export function steeringRefused(error: unknown): boolean {
  return errorCode(error) === "pa_unsupported" && errorField(error) === "steering"
}
