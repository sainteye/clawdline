import type * as W from "@clawdline/contract"
import type { SourceClient } from "../project-assistants/client.js"
// @ts-expect-error -- Node's strip-types runner loads the source extension.
import { failure, projectChoices } from "../project-assistants/client.ts"
// @ts-expect-error -- Node's strip-types runner loads the source extension.
import { readViewRequest, readView } from "../project-assistants/read-views.ts"
// @ts-expect-error -- Node's strip-types runner loads the source extension.
import { createViewState, canRead } from "../project-assistants/view-state.ts"
import type { CloudClientHandle, CloudMachine } from "./copied.js"
import type { WorkV2Item } from "../pages/work/api.js"

export const PROJECT_ASSISTANTS_API_LEVEL = 58
export const PROJECT_ASSISTANTS_VIEW_API_LEVEL = 59
const base = "/v1/project-assistants"
export type AssistantFetch = (input: string, init?: RequestInit) => Promise<Response>
export interface AssistantWorkIntent {token:string;machineID:string;item:WorkV2Item;current:()=>boolean}
export interface AssistantGrant {
  accountKey: string
  readerID: string
  machineID: string
  authorizationRevision: string
  apiLevel: number | undefined
  paired: boolean
  online: boolean
  writable: boolean
}

/** Preserve source refusals verbatim; transport failures retain their original evidence. */
export class AssistantTransportFailure extends Error {
  readonly contract_version = "project-assistants.v1"
  readonly error: W.ProjectAssistantFailure
  readonly response: unknown
  constructor(code: W.ProjectAssistantErrorCode, response: unknown) {
    super(code)
    this.error = failure(code).error
    this.response = response
  }
}
function sourceFailure(value: unknown): value is W.ProjectAssistantErrorResponse {
  return typeof value === "object" && value !== null && "contract_version" in value &&
    "error" in value && typeof value.error === "object" && value.error !== null && "code" in value.error
}
function endpoint(key: W.ProjectAssistantBotKey, suffix = "") { return `${base}/${encodeURIComponent(key.bot_id)}${suffix}` }
function query(fields: Record<string, string | number>) { return "?" + new URLSearchParams(Object.entries(fields).map(([k,v]) => [k,String(v)])) }

/** One immutable authority, independent of the console's selected machine. */
export class ProjectAssistantSourceClient implements SourceClient {
  readonly machineID: string
  readonly authorizationRevision: string
  readonly grant: Readonly<AssistantGrant>
  private readonly transport: AssistantFetch
  private readonly currentGrant: () => AssistantGrant | null
  private readonly lifetime = new AbortController()
  private access: W.ProjectAssistantAccess = "hidden"
  private lastStatus: W.ProjectAssistantSourceStatus | null = null
  constructor(grant: AssistantGrant, transport: AssistantFetch, currentGrant: () => AssistantGrant | null) {
    this.grant = { ...grant }; this.machineID = grant.machineID; this.authorizationRevision = grant.authorizationRevision
    this.transport = transport; this.currentGrant = currentGrant
  }
  revoke() { this.lifetime.abort() }
  current(): boolean { try {this.check(new AbortController().signal,false,false);return true} catch {return false} }
  private check(signal: AbortSignal, write: boolean, content: boolean, minimum = PROJECT_ASSISTANTS_API_LEVEL) {
    const live = this.currentGrant()
    if (!live || this.lifetime.signal.aborted || live.accountKey !== this.grant.accountKey ||
      live.readerID !== this.grant.readerID || live.machineID !== this.machineID || live.authorizationRevision !== this.authorizationRevision) throw failure("pa_revoked")
    if (signal.aborted) throw new DOMException("Aborted", "AbortError")
    if (!live.paired) throw failure("pa_unpaired")
    if (live.apiLevel === undefined || live.apiLevel < minimum) throw failure("pa_unsupported")
    if (!live.online) throw failure("pa_offline")
    if (write && content && (!live.writable || this.access !== "interactive" || !this.lastStatus?.writable || !this.lastStatus.fresh)) throw failure("pa_read_only")
    if (write && !content) {
      if (!this.lastStatus?.fresh || this.lastStatus.state !== "online") throw failure("pa_offline")
      if (!this.lastStatus.readable) throw failure("pa_scope_denied")
    }
  }
  private key(key: W.ProjectAssistantBotKey) { if (key.machine_id !== this.machineID) throw failure("pa_scope_denied") }
  private async request<T>(path: string, signal: AbortSignal, body?: { command: W.ProjectAssistantCommand }, method = "GET", content = true, minimum = PROJECT_ASSISTANTS_API_LEVEL): Promise<T> {
    const write = method !== "GET"
    this.check(signal,write,content,minimum)
    if (body) this.key(body.command.key)
    const control = new AbortController(), abort = () => control.abort()
    signal.addEventListener("abort",abort,{once:true}); this.lifetime.signal.addEventListener("abort",abort,{once:true})
    try {
      const response = await this.transport(path,{method,signal:control.signal,cache:"no-store",
        ...(body ? {body:JSON.stringify(body),headers:{"Content-Type":"application/json","Idempotency-Key":body.command.idempotency_key}} : {})})
      this.check(signal,write,content,minimum)
      const answer: unknown = await response.json()
      this.check(signal,write,content,minimum)
      if (sourceFailure(answer)) throw answer
      if (!response.ok) throw new AssistantTransportFailure(response.status === 501 ? "pa_unsupported" : write ? "pa_outcome_unknown" : "pa_offline",answer)
      return answer as T
    } catch (error) {
      this.check(signal,write,content,minimum)
      if (sourceFailure(error)) throw error
      throw new AssistantTransportFailure(write ? "pa_outcome_unknown" : "pa_offline",error)
    } finally { signal.removeEventListener("abort",abort); this.lifetime.signal.removeEventListener("abort",abort) }
  }
  async status(s: AbortSignal): Promise<W.ProjectAssistantSourcesResponse> {
    // An unavailable source is a typed status, never an authoritative empty roster.
    const grant = this.currentGrant()
    if (!grant || this.lifetime.signal.aborted) throw failure("pa_revoked")
    const state: W.ProjectAssistantSourceState | null = !grant.paired ? "unpaired" : grant.apiLevel === undefined || grant.apiLevel < PROJECT_ASSISTANTS_API_LEVEL ? "unsupported" : !grant.online ? "offline" : null
    if (s.aborted) throw new DOMException("Aborted","AbortError")
    if (state) return {contract_version:"project-assistants.v1",sources:[(state === "offline" || state === "unsupported") && this.lastStatus
      ? {...this.lastStatus,state,writable:false,fresh:false}
      : {machine_id:this.machineID,state,access:"hidden",readable:false,writable:false,fresh:false,observed_at:"",capabilities:[]}]}
    try {
      const answer = await this.request<W.ProjectAssistantSourcesResponse>(base+"/status",s)
      const own = answer.sources.find(source => source.machine_id === this.machineID)
      if (!own || answer.contract_version !== "project-assistants.v1") throw new AssistantTransportFailure("pa_invalid",answer)
      const effective = {...own,writable:own.writable && this.currentGrant()?.writable === true}
      this.access = effective.access;this.lastStatus = effective
      return {...answer,sources:[effective]}
    } catch (error) {
      this.access="hidden"
      if (this.lastStatus) this.lastStatus={...this.lastStatus,fresh:false,writable:false}
      throw error
    }
  }
  async projects(r: W.ProjectAssistantProjectsRequest,s: AbortSignal) {
    this.key({machine_id:r.machine_id,bot_id:""})
    if(typeof r.cursor!=="string"||!Number.isInteger(r.page_size)||r.page_size<1||r.page_size>100)throw failure("pa_invalid")
    const answer = projectChoices(await this.request<W.ProjectAssistantProjectsResponse>("/v1/project-assistant-targets"+query({...r}),s),r)
    this.check(s,false,false)
    const source={...answer.source,writable:answer.source.writable && this.currentGrant()?.writable === true}
    this.access=source.access;this.lastStatus=source
    return {...answer,source}
  }
  list(r: W.ProjectAssistantListRequest,s: AbortSignal) { this.key({machine_id:r.machine_id,bot_id:""}); return this.request<W.ProjectAssistantListResponse>(base+query({...r}),s) }
  create(r: W.ProjectAssistantCreateRequest,s: AbortSignal) { return this.request<W.ProjectAssistantBotResponse>(base,s,r,"POST") }
  async get(r: W.ProjectAssistantBotKey,s: AbortSignal) {
    this.key(r)
    const answer = await this.request<W.ProjectAssistantGetResponse>(endpoint(r)+query({machine_id:this.machineID}),s)
    this.check(s,false,false)
    if (answer.source) {
      if (answer.source.machine_id !== this.machineID || answer.bot.key.machine_id !== this.machineID || answer.bot.key.bot_id !== r.bot_id) throw failure("pa_scope_denied")
      answer.source={...answer.source,writable:answer.source.writable && this.currentGrant()?.writable === true}
      this.access=answer.source.access;this.lastStatus=answer.source
    }
    return answer
  }
  edit(r: W.ProjectAssistantEditRequest,s: AbortSignal) { return this.request<W.ProjectAssistantBotResponse>(endpoint(r.command.key),s,r,"PATCH") }
  history(r: W.ProjectAssistantHistoryRequest,s: AbortSignal) { this.key(r.key); return this.request<W.ProjectAssistantHistoryResponse>(endpoint(r.key,"/messages")+query({machine_id:this.machineID,conversation_id:r.conversation_id,before_seq:r.before_seq,page_size:r.page_size}),s) }
  async view(r: W.ProjectAssistantReadViewRequest,s: AbortSignal) {
    this.key(r.key);readViewRequest(r)
    const answer=await this.request<W.ProjectAssistantReadViewResponse>(endpoint(r.key,"/view")+query({machine_id:this.machineID,conversation_id:r.conversation_id,mode:r.mode,source_epoch:r.source_epoch,cursor:r.cursor,after_seq:r.after_seq,page_size:r.page_size}),s,undefined,"GET",true,PROJECT_ASSISTANTS_VIEW_API_LEVEL)
    this.check(s,false,false,PROJECT_ASSISTANTS_VIEW_API_LEVEL)
    if(!answer?.head)throw failure("pa_invalid")
    // The path owns Bot identity. Chat independently checks the captured Project scope.
    readView(answer,r,{key:r.key,project:answer.head.project,conversation_id:r.conversation_id})
    const source=answer.source
    answer.source={...source,writable:source.writable&&this.currentGrant()?.writable===true}
    this.access=answer.source.access;this.lastStatus=answer.source
    return answer
  }
  send(r: W.ProjectAssistantSendRequest,s: AbortSignal) { return this.request<W.ProjectAssistantReceiptResponse>(endpoint(r.command.key,"/messages"),s,r,"POST") }
  stop(r: W.ProjectAssistantStopRequest,s: AbortSignal) { return this.request<W.ProjectAssistantReceiptResponse>(endpoint(r.command.key,"/stop"),s,r,"POST") }
  answer(r: W.ProjectAssistantPermissionAnswerRequest,s: AbortSignal) { return this.request<W.ProjectAssistantReceiptResponse>(endpoint(r.command.key,`/permissions/${encodeURIComponent(r.offer_id)}/answer`),s,r,"POST") }
  markRead(r: W.ProjectAssistantReadRequest,s: AbortSignal) { const request: W.ProjectAssistantReadRequest = {...r,cursor:{...r.cursor,reader_id:this.grant.readerID}}; return this.request<W.ProjectAssistantReadResponse>(endpoint(r.command.key,"/read"),s,request,"POST",false) }
  observe(r: W.ProjectAssistantObserveRequest,s: AbortSignal) { return this.request<W.ProjectAssistantReceiptResponse>(endpoint(r.command.key,"/observed"),s,r,"POST",false) }
  events(r: W.ProjectAssistantEventsRequest,s: AbortSignal) { this.key(r.key); return this.request<W.ProjectAssistantEventsResponse>(endpoint(r.key,"/events")+query({machine_id:this.machineID,after_seq:r.after_seq,page_size:r.page_size}),s) }
  forget(r: W.ProjectAssistantForgetRequest,s: AbortSignal) { return this.request<W.ProjectAssistantMemoryResponse>(endpoint(r.command.key,"/memory/forget"),s,r,"POST") }
  saveMemory(r: W.ProjectAssistantMemoryWriteRequest,s: AbortSignal) { return this.request<W.ProjectAssistantMemoryResponse>(endpoint(r.command.key,"/memory"),s,r,"POST") }
  export(r: W.ProjectAssistantBotKey,s: AbortSignal) { this.key(r); return this.request<W.ProjectAssistantExport>(endpoint(r,"/export")+query({machine_id:this.machineID}),s) }
  /** Board remains its existing owner; this only reads its full item on the captured source. */
  async workItem(work: W.ProjectAssistantWorkLink,s: AbortSignal): Promise<WorkV2Item> {
    this.key(work.fence.key)
    if (!work.board_item_id || work.fence.project.machine_id !== this.machineID) throw failure("pa_scope_denied")
    const answer = await this.request<{item:WorkV2Item}>("/v1/work/v2/items/"+encodeURIComponent(work.board_item_id),s)
    if (answer.item.id !== work.board_item_id || answer.item.project.id !== work.fence.project.project_id) throw failure("pa_scope_denied")
    return answer.item
  }
}

/** Route metadata used by the captured encrypted transport; never a second UI model. */
export function assistantRelayRequest(input: string, init?: RequestInit) {
  const url = new URL(input,"http://source.invalid"), method = init?.method ?? "GET"
  if (url.pathname === "/v1/projects" && method === "GET") return {word:"projects",kind:"read" as const,fields:{},id:""}
  if (url.pathname === "/v1/project-assistant-targets" && method === "GET") {
    const q=Object.fromEntries(url.searchParams)
    if (url.searchParams.size !== 3 || Object.keys(q).some(key=>!["machine_id","cursor","page_size"].includes(key)) ||
      typeof q.machine_id !== "string" || !q.machine_id || typeof q.cursor !== "string" || !/^[1-9][0-9]*$/.test(q.page_size) || Number(q.page_size)>100) throw failure("pa_invalid")
    return {word:"project-assistants.projects",kind:"read" as const,fields:{machine_id:q.machine_id,cursor:q.cursor,page_size:Number(q.page_size)},id:""}
  }
  const work = /^\/v1\/work\/v2\/items\/([^/]+)$/.exec(url.pathname)
  if (work && method === "GET") return {word:"work.v2.item",kind:"read" as const,fields:{id:decodeURIComponent(work[1])},id:""}
  const match = /^\/v1\/project-assistants(?:\/([^/]+))?(.*)$/.exec(url.pathname)
  if (!match) throw failure("pa_invalid")
  const bot = match[1] ? decodeURIComponent(match[1]) : "", tail = match[2]
  const q = Object.fromEntries(url.searchParams)
  if (method === "GET") {
    if (bot === "status" && !tail) return {word:"project-assistants.status",kind:"read" as const,fields:{},id:""}
    if (!bot) return {word:"project-assistants.list",kind:"read" as const,fields:{...q,page_size:Number(q.page_size)},id:""}
    const key = {machine_id:q.machine_id,bot_id:bot}
    if(tail==="/view") {
      const fields=["machine_id","conversation_id","mode","source_epoch","cursor","after_seq","page_size"]
      if(init?.body!=null||url.searchParams.size!==fields.length||fields.some(field=>!url.searchParams.has(field))||Object.keys(q).some(field=>!fields.includes(field))||! /^(0|[1-9][0-9]*)$/.test(q.after_seq)||! /^[1-9][0-9]*$/.test(q.page_size))throw failure("pa_invalid")
      const request:W.ProjectAssistantReadViewRequest={key,conversation_id:q.conversation_id,mode:q.mode as W.ProjectAssistantReadViewMode,source_epoch:q.source_epoch,cursor:q.cursor,after_seq:Number(q.after_seq),page_size:Number(q.page_size)}
      readViewRequest(request)
      return {word:"project-assistants.view",kind:"read" as const,fields:{...request},id:""}
    }
    const action = tail === "/messages" ? "history" : tail === "/events" ? "events" : tail === "/export" ? "export" : tail === "" ? "get" : null
    if (!action) throw failure("pa_invalid")
    const fields = action === "history" ? {key,conversation_id:q.conversation_id,before_seq:Number(q.before_seq),page_size:Number(q.page_size)} : action === "events" ? {key,after_seq:Number(q.after_seq),page_size:Number(q.page_size)} : key
    return {word:`project-assistants.${action}`,kind:"read" as const,fields,id:""}
  }
  if (typeof init?.body !== "string") throw failure("pa_invalid")
  const fields: Record<string,unknown> = JSON.parse(init.body)
  const command = fields.command
  if (!command || typeof command !== "object" || !("idempotency_key" in command) || typeof command.idempotency_key !== "string") throw failure("pa_invalid")
  if (new Headers(init.headers).get("Idempotency-Key") !== command.idempotency_key) throw failure("pa_invalid")
  const action = !bot && method === "POST" ? "create" : bot && !tail && method === "PATCH" ? "edit" : method !== "POST" ? null :
    tail === "/messages" ? "send" : tail === "/stop" ? "stop" : /^\/permissions\/[^/]+\/answer$/.test(tail) ? "answer" :
    tail === "/read" ? "read" : tail === "/observed" ? "observed" : tail === "/memory/forget" ? "forget" : tail === "/memory" ? "memory" : null
  if (!action) throw failure("pa_invalid")
  return {word:`project-assistants.${action}`,kind:"action" as const,fields,id:command.idempotency_key}
}

/** Uses the passed machine and authenticated connection, never install.ts's active reader. */
export type AssistantRelayConnection = Pick<CloudClientHandle,"_machineRequest" | "_machineRequestAs" | "machineDescriptor">
export function capturedAssistantRelayFetch(machineID: string, current: () => AssistantRelayConnection | null, carried: readonly string[]): AssistantFetch {
  return async (input,init) => {
    const request = assistantRelayRequest(input,init), client = current()
    if (!carried.includes(request.word)) return Response.json({error:{code:"cloud_not_carried"}},{status:501})
    if (!client) throw failure("pa_offline")
    if (init?.signal?.aborted) throw new DOMException("Aborted","AbortError")
    if (!client.machineDescriptor?.(machineID)?.machine?.commands?.includes(request.word)) throw failure("pa_unsupported")
    // The connection's authenticated principal is authoritative; body reader_id is only a hint.
    const answer = request.kind === "read" ? await client._machineRequest?.(machineID,request.word,request.fields,"read",undefined,{signal:init?.signal ?? undefined}) :
      await client._machineRequestAs?.(request.id,machineID,request.word,request.fields,"action")
    if (answer === undefined) throw failure("pa_unsupported")
    if (init?.signal?.aborted) throw new DOMException("Aborted","AbortError")
    return Response.json(answer)
  }
}

/** Account-owned state survives App's active-machine key and token renewal. */
export class ProjectAssistantRegistry {
  readonly accountKey: string
  readonly readerID: string
  readonly viewState = createViewState()
  private grants = new Map<string,AssistantGrant>()
  private revisions = new Map<string,string>()
  private clients = new Map<string,ProjectAssistantSourceClient>()
  private listeners = new Set<() => void>()
  private snapshot: readonly SourceClient[] = []
  constructor(accountKey: string, readerID: string) { this.accountKey = accountKey; this.readerID = readerID }
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => {this.listeners.delete(listener)} }
  getSnapshot = () => this.snapshot
  async captureWorkItem(work: W.ProjectAssistantWorkLink,signal: AbortSignal) {
    const source = this.clients.get(work.fence.key.machine_id)
    if (!source) throw failure("pa_revoked")
    const item = await source.workItem(work,signal)
    return {item,machineID:source.machineID,current:() => this.clients.get(source.machineID) === source && source.current()}
  }
  sync(grants: readonly AssistantGrant[], transport: (grant: AssistantGrant) => AssistantFetch) {
    if (grants.some(grant => grant.accountKey !== this.accountKey || grant.readerID !== this.readerID)) throw failure("pa_revoked")
    const next = new Map(grants.map(grant => [grant.machineID,grant]))
    let changed = this.grants.size !== next.size || grants.some(grant => {
      const old = this.grants.get(grant.machineID)
      return !old || JSON.stringify({...old,authorizationRevision:grant.authorizationRevision}) !== JSON.stringify(grant)
    })
    for (const [id,client] of this.clients) {
      if (next.get(id)?.authorizationRevision !== this.revisions.get(id)) {client.revoke(); this.clients.delete(id);this.revisions.delete(id); changed = true}
    }
    const effective = new Map<string,AssistantGrant>()
    for (const grant of grants) {
      const revision = this.clients.get(grant.machineID)?.authorizationRevision ?? `${grant.authorizationRevision}:${crypto.randomUUID()}`
      const held = {...grant,authorizationRevision:revision}
      effective.set(grant.machineID,held)
      if (!this.clients.has(grant.machineID)) {
        this.clients.set(grant.machineID,new ProjectAssistantSourceClient(held,transport(held),() => this.grants.get(grant.machineID) ?? null))
        this.revisions.set(grant.machineID,grant.authorizationRevision);changed = true
      }
    }
    this.grants = effective
    if (changed) {this.snapshot = [...this.clients.values()]; for (const listener of this.listeners) listener()}
  }
  revoke(machineID?: string) {
    for (const [id,client] of this.clients) if (!machineID || id === machineID) {client.revoke(); this.clients.delete(id); this.grants.delete(id);this.revisions.delete(id)}
    this.snapshot = [...this.clients.values()]; for (const listener of this.listeners) listener()
  }
}
export function cloudAssistantGrants(account: string, machines: readonly CloudMachine[], apiLevel: (id: string) => number | undefined, revisions: (id: string) => string): AssistantGrant[] {
  return machines.map(machine => ({accountKey:account,readerID:account,machineID:machine.id,
    authorizationRevision:revisions(machine.id),apiLevel:apiLevel(machine.id),paired:machine.pairing === "paired",
    online:machine.freshness === "current",writable:machine.operationAllowed === true}))
}

export function assistantBadge(items: readonly W.ProjectAssistantBotSummary[], authorized: readonly SourceClient[]) {
  return items.reduce((count,item) => authorized.some(source => source.machineID === item.bot.key.machine_id) && canRead(item.source)
    ? {unread:count.unread+item.unread_count,pendingPermissions:count.pendingPermissions+item.pending_permissions} : count,{unread:0,pendingPermissions:0})
}
