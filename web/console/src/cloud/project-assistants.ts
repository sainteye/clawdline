import { base64Bytes } from "../legacy/js/net/cloud-crypto.js"
import { RELAY_CTL_MAX_BYTES } from "../legacy/js/net/cloud-failure.js"
import type * as W from "@clawdline/contract"
import type { SourceClient } from "../project-assistants/client.js"
// @ts-expect-error -- Node's strip-types runner loads the source extension.
import { failure, projectChoices, notSent, provablyNotSent } from "../project-assistants/client.ts"
// @ts-expect-error -- Node's strip-types runner loads the source extension.
import { readViewRequest, readView } from "../project-assistants/read-views.ts"
// @ts-expect-error -- Node's strip-types runner loads the source extension.
import { RosterSSEDecoder, validateRosterRequest, RosterStreamFailure } from "../project-assistants/roster-stream.ts"
// @ts-expect-error -- Node's strip-types runner loads the source extension.
import { createViewState, canRead } from "../project-assistants/view-state.ts"
// @ts-expect-error -- Node's strip-types runner loads the source extension.
import { rosterMood, type BotMood } from "../project-assistants/working.ts"
import type { CloudClientHandle, CloudMachine } from "./copied.js"
import type { WorkV2Item } from "../pages/work/api.js"

// Bot routes were built at levels 58/59 on a branch while main used 57-59
// for other routes; they first reached main together at 61. No released
// daemon has 60, so base routes and the view route stay distinct tiers.
export const PROJECT_ASSISTANTS_BROWSER_API_LEVEL = 74
export const PROJECT_ASSISTANTS_ROUTINES_API_LEVEL = 73
export const PROJECT_ASSISTANTS_FALLBACKS_API_LEVEL = 72
export const PROJECT_ASSISTANTS_INPUTS_API_LEVEL = 70
export const PROJECT_ASSISTANTS_ROSTER_API_LEVEL = 71
export const PROJECT_ASSISTANTS_OUTPUTS_API_LEVEL = 69
export const PROJECT_ASSISTANTS_API_LEVEL = 60
export const PROJECT_ASSISTANTS_VIEW_API_LEVEL = 61
// Pictures with a message and their read route arrived at 62.
export const PROJECT_ASSISTANTS_PICTURES_API_LEVEL = 62
export const PROJECT_ASSISTANTS_CLOUD_PICTURES_API_LEVEL = 68
// Stopping a queued turn removes that one message, and turns carry started_at, from 63.
export const PROJECT_ASSISTANTS_UNQUEUE_API_LEVEL = 63
// Send now (Codync's steer) arrived at 64; this machine's own console only, until the Cloud relay carries it.
export const PROJECT_ASSISTANTS_SEND_NOW_API_LEVEL = 64
// Listing and revoking a Claude Bot's standing rules arrived at 66; this machine's own console only.
export const PROJECT_ASSISTANTS_RULES_API_LEVEL = 66
// Hiding and deleting a Bot arrived at 67; this machine's own console only, until the Cloud relay carries them.
export const PROJECT_ASSISTANTS_MANAGE_API_LEVEL = 67
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
  /** The machine's own name, when Cloud knows it. */
  name?: string
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
  readonly machineName?: string
  private readonly transport: AssistantFetch
  private readonly currentGrant: () => AssistantGrant | null
  private readonly lifetime = new AbortController()
  private access: W.ProjectAssistantAccess = "hidden"
  private lastStatus: W.ProjectAssistantSourceStatus | null = null
  constructor(grant: AssistantGrant, transport: AssistantFetch, currentGrant: () => AssistantGrant | null) {
    this.grant = { ...grant }; this.machineID = grant.machineID; this.machineName = grant.name; this.authorizationRevision = grant.authorizationRevision
    this.transport = transport; this.currentGrant = currentGrant
  }
  revoke() { this.lifetime.abort() }
  get browserRemote() { return this.grant.accountKey !== "local" }
  browserUnavailable(): string {
    try { this.check(new AbortController().signal,false,true,PROJECT_ASSISTANTS_BROWSER_API_LEVEL); return "" }
    catch (error) { return sourceFailure(error) ? error.error.code : "pa_offline" }
  }
  async browser<T>(key: W.ProjectAssistantBotKey, conversation: string, method: "GET" | "POST", path: string, body: unknown, signal: AbortSignal): Promise<T> {
    const minimum = PROJECT_ASSISTANTS_BROWSER_API_LEVEL, write = method === "POST"
    const url = new URL(path,"http://source.invalid")
    let fields: Record<string,unknown>
    try {
      this.key(key)
      if (!key.bot_id || !conversation || url.origin !== "http://source.invalid") throw failure("pa_invalid")
      this.check(signal,write,true,minimum)
      fields = write ? {...body as Record<string,unknown>} : Object.fromEntries(url.searchParams)
      // Target fields are owned here, and never accepted from panel arguments.
      if (["bot_id","owner","machine_id"].some(field => field in fields)) throw failure("pa_invalid")
      fields = {...fields,bot_id:key.bot_id,conversation}
      if (!write) { url.search=""; for(const [k,v] of Object.entries(fields))url.searchParams.set(k,String(v)) }
      browserRelayRequest(url,method,write?fields:undefined,write?"validation":"")
    } catch(error) { throw notSent(error) }
    const control=new AbortController(),abort=()=>control.abort()
    signal.addEventListener("abort",abort,{once:true});this.lifetime.signal.addEventListener("abort",abort,{once:true})
    try {
      const response=await this.transport(url.pathname+url.search,{method,signal:control.signal,cache:"no-store",...(write?{headers:{"Content-Type":"application/json","Idempotency-Key":crypto.randomUUID()},body:JSON.stringify(fields)}:{})})
      this.recheck(signal,write,true,minimum,response)
      const answer=await response.json()
      this.recheck(signal,write,true,minimum,answer)
      const raw=answer?.error, code=typeof raw==="string"?raw:raw?.code
      if(!response.ok||code) {
        const error=Object.assign(new Error(typeof answer?.detail==="string"?answer.detail:code??"browser_unavailable"),{code:code??"browser_unavailable",response:answer})
        throw error
      }
      return answer as T
    } finally { signal.removeEventListener("abort",abort);this.lifetime.signal.removeEventListener("abort",abort) }
  }
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
    // Refusals here precede the transport, so they prove the write never left.
    try { this.check(signal,write,content,minimum); if (body) this.key(body.command.key) } catch (error) { throw notSent(error) }
    const control = new AbortController(), abort = () => control.abort()
    signal.addEventListener("abort",abort,{once:true}); this.lifetime.signal.addEventListener("abort",abort,{once:true})
    let answered = false
    try {
      const response = await this.transport(path,{method,signal:control.signal,cache:"no-store",
        ...(body ? {body:JSON.stringify(body),headers:{"Content-Type":"application/json","Idempotency-Key":body.command.idempotency_key}} : {})})
      this.recheck(signal,write,content,minimum,response)
      const answer: unknown = await response.json()
      this.recheck(signal,write,content,minimum,answer)
      answered = true
      if (sourceFailure(answer)) throw answer
      if (!response.ok) throw new AssistantTransportFailure(response.status === 501 ? "pa_unsupported" : write ? "pa_outcome_unknown" : "pa_offline",answer)
      return answer as T
    } catch (error) {
      if (answered || error instanceof AssistantTransportFailure) throw error
      // A transport that refused before sending says so; nothing crossed.
      if (provablyNotSent(error)) {
        try { this.check(signal,write,content,minimum) } catch (current) { throw notSent(current) }
        throw sourceFailure(error) ? error : notSent(new AssistantTransportFailure("pa_offline",error))
      }
      this.recheck(signal,write,content,minimum,error)
      if (sourceFailure(error)) throw error
      throw new AssistantTransportFailure(write ? "pa_outcome_unknown" : "pa_offline",error)
    } finally { signal.removeEventListener("abort",abort); this.lifetime.signal.removeEventListener("abort",abort) }
  }
  /**
   * After a write reached the transport, a changed grant or an abort cannot
   * prove that nothing happened. Reads keep their typed refusal; writes become
   * outcome unknown and retain what the transport returned as evidence.
   */
  private recheck(signal: AbortSignal, write: boolean, content: boolean, minimum: number, evidence: unknown) {
    try { this.check(signal,write,content,minimum) }
    catch (error) { if (!write) throw error; throw new AssistantTransportFailure("pa_outcome_unknown",{evidence,authority:error}) }
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
  canRoster() {
    if (this.grant.accountKey !== "local" || (this.grant.apiLevel ?? 0) < PROJECT_ASSISTANTS_ROSTER_API_LEVEL) return false
    try { this.check(this.lifetime.signal,false,false,PROJECT_ASSISTANTS_ROSTER_API_LEVEL); return true } catch { return false }
  }
  /** One local connection. The caller alone owns application, cursors and retries. */
  async subscribeRoster(r: W.ProjectAssistantRosterRequest, receive: (frame: W.ProjectAssistantRosterFrame) => void, s: AbortSignal): Promise<void> {
    this.check(s,false,false,PROJECT_ASSISTANTS_ROSTER_API_LEVEL)
    if (this.grant.accountKey !== "local" || (this.grant.apiLevel ?? 0) < PROJECT_ASSISTANTS_ROSTER_API_LEVEL) throw failure("pa_unsupported")
    validateRosterRequest(r); this.key({machine_id:r.machine_id,bot_id:""})
    // Capture input too: caller mutation must not replace the frame's scope.
    const request = { ...r }, decoder = new RosterSSEDecoder(request), control = new AbortController()
    let reader: ReadableStreamDefaultReader<Uint8Array> | undefined
    let body: ReadableStream<Uint8Array> | null = null
    const abort = () => { control.abort(); if (reader) void reader.cancel().catch(() => undefined) }
    s.addEventListener("abort",abort,{once:true}); this.lifetime.signal.addEventListener("abort",abort,{once:true})
    try {
      const response = await this.transport(base+"/roster-events"+query({machine_id:request.machine_id,...(request.project_id !== undefined ? {project_id:request.project_id} : {}),...(request.cursor !== undefined ? {cursor:request.cursor} : {})}),{method:"GET",signal:control.signal,cache:"no-store",headers:{Accept:"text/event-stream"}})
      body = response.body
      this.check(s,false,false,PROJECT_ASSISTANTS_ROSTER_API_LEVEL)
      if (!response.ok) {
        const answer: unknown = await response.json()
        this.check(s,false,false,PROJECT_ASSISTANTS_ROSTER_API_LEVEL)
        if (sourceFailure(answer)) throw answer
        throw new AssistantTransportFailure(response.status === 501 ? "pa_unsupported" : "pa_offline",answer)
      }
      if (response.headers.get("Content-Type")?.split(";",1)[0].trim().toLowerCase() !== "text/event-stream" || !response.body) throw new RosterStreamFailure("invalid")
      reader = response.body.getReader()
      for (;;) {
        const chunk = await reader.read()
        this.check(s,false,false,PROJECT_ASSISTANTS_ROSTER_API_LEVEL)
        if (chunk.done) return decoder.end()
        decoder.push(chunk.value, frame => {
          this.check(s,false,false,PROJECT_ASSISTANTS_ROSTER_API_LEVEL)
          // Wire read identities are source-issued (including the local owner
          // digest), not the host's logical "local"/account reader label.
          receive({...frame,changes:frame.changes?.map(row => row.summary ? {...row,summary:{...row.summary,source:{...row.summary.source,writable:row.summary.source.writable && this.currentGrant()?.writable === true}}} : row)})
        })
      }
    } catch (error) {
      this.check(s,false,false,PROJECT_ASSISTANTS_ROSTER_API_LEVEL)
      if (sourceFailure(error)) throw error
      throw new AssistantTransportFailure("pa_offline",error)
    } finally {
      s.removeEventListener("abort",abort); this.lifetime.signal.removeEventListener("abort",abort)
      control.abort()
      if (reader) { void reader.cancel().catch(() => undefined); reader.releaseLock() }
      else if (body) void body.cancel().catch(() => undefined)
    }
  }
  canRoutines() { return (this.grant.apiLevel??0)>=PROJECT_ASSISTANTS_ROUTINES_API_LEVEL }
  async routines(r: W.ProjectAssistantRoutineListRequest,s: AbortSignal) {
    this.key(r.key)
    if(r.project.machine_id!==this.machineID || !Number.isInteger(r.page_size) || r.page_size<1 || r.page_size>100 || !r.conversation_id || r.cursor.length>4096)throw failure("pa_invalid")
    const suffix="/routines"+(r.routine_id?"/"+encodeURIComponent(r.routine_id)+"/runs":"")
    const answer=await this.request<W.ProjectAssistantRoutineListResponse>(endpoint(r.key,suffix)+query({machine_id:this.machineID,project_id:r.project.project_id,canonical_id:r.project.canonical_id,kind:r.project.kind,conversation_id:r.conversation_id,cursor:r.cursor,page_size:r.page_size}),s,undefined,"GET",true,PROJECT_ASSISTANTS_ROUTINES_API_LEVEL)
    const same=(a:W.ProjectAssistantBotKey,b:W.ProjectAssistantBotKey)=>a?.machine_id===b.machine_id&&a.bot_id===b.bot_id
    const scope=(a:W.ProjectAssistantProjectScope)=>a?.machine_id===r.project.machine_id&&a.project_id===r.project.project_id&&a.canonical_id===r.project.canonical_id&&a.kind===r.project.kind
    if(answer.contract_version!=="project-assistants.v1" || answer.source?.machine_id!==this.machineID || !answer.fence || !same(answer.fence.key,r.key) || !scope(answer.fence.project) || answer.fence.conversation_id!==r.conversation_id || !Array.isArray(answer.definitions) || !Array.isArray(answer.runs) || answer.definitions.length+answer.runs.length>r.page_size || typeof answer.next_cursor!=="string")throw failure("pa_scope_denied")
    for(const d of answer.definitions)if(!same(d.key,r.key)||!scope(d.project)||d.conversation_id!==r.conversation_id)throw failure("pa_scope_denied")
    for(const run of answer.runs)if(run.routine_id!==r.routine_id)throw failure("pa_scope_denied")
    answer.source={...answer.source,writable:answer.source.writable&&this.currentGrant()?.writable===true};this.access=answer.source.access;this.lastStatus=answer.source
    return answer
  }
  private async routineWrite(path:string,r:{command:W.ProjectAssistantCommand},s:AbortSignal,method="POST") {
    const answer=await this.request<W.ProjectAssistantReceiptResponse>(path,s,r,method,true,PROJECT_ASSISTANTS_ROUTINES_API_LEVEL)
    const receipt=answer?.receipt,c=receipt?.command,key=r.command.key
    if(answer?.contract_version!=="project-assistants.v1"||!receipt?.id||!receipt.resource_id||!c||c.key?.machine_id!==key.machine_id||c.key.bot_id!==key.bot_id||c.operation!=="edit"||c.idempotency_key!==r.command.idempotency_key||!['accepted','delivered'].includes(receipt.state))throw new AssistantTransportFailure("pa_outcome_unknown",answer)
    return answer
  }
  saveRoutine(r:W.ProjectAssistantRoutineSaveRequest,s:AbortSignal) { return this.routineWrite(endpoint(r.command.key,"/routines"),r,s) }
  enableRoutine(r:W.ProjectAssistantRoutineEnabledRequest,s:AbortSignal) { return this.routineWrite(endpoint(r.command.key,"/routines/"+encodeURIComponent(r.id)+"/enabled"),r,s) }
  deleteRoutine(r:W.ProjectAssistantRoutineActionRequest,s:AbortSignal) { return this.routineWrite(endpoint(r.command.key,"/routines/"+encodeURIComponent(r.id)),r,s,"DELETE") }
  testRoutine(r:W.ProjectAssistantRoutineActionRequest,s:AbortSignal) { return this.routineWrite(endpoint(r.command.key,"/routines/"+encodeURIComponent(r.id)+"/runs"),r,s) }
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
  canFallbacks() { try { this.check(this.lifetime.signal,false,false,PROJECT_ASSISTANTS_FALLBACKS_API_LEVEL); return true } catch { return false } }
  edit(r: W.ProjectAssistantEditRequest,s: AbortSignal) { return this.request<W.ProjectAssistantBotResponse>(endpoint(r.command.key),s,r,"PATCH",true,r.settings?.fallback_agents !== undefined ? PROJECT_ASSISTANTS_FALLBACKS_API_LEVEL : PROJECT_ASSISTANTS_API_LEVEL) }
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
  send(r: W.ProjectAssistantSendRequest,s: AbortSignal) { return this.request<W.ProjectAssistantReceiptResponse>(endpoint(r.command.key,"/messages"),s,r,"POST",true,r.upload_ids?.length?PROJECT_ASSISTANTS_INPUTS_API_LEVEL:r.images?.length?(this.grant.accountKey==="local"?PROJECT_ASSISTANTS_PICTURES_API_LEVEL:PROJECT_ASSISTANTS_CLOUD_PICTURES_API_LEVEL):PROJECT_ASSISTANTS_API_LEVEL) }
  canInputs() { return (this.grant.apiLevel??0)>=PROJECT_ASSISTANTS_INPUTS_API_LEVEL }
  upload(r:W.ProjectAssistantUploadRequest,s:AbortSignal) { return this.request<W.ProjectAssistantUploadResponse>(endpoint(r.command.key,"/upload"),s,r,"POST",true,PROJECT_ASSISTANTS_INPUTS_API_LEVEL) }
  input(r:W.ProjectAssistantInputRequest,s:AbortSignal) { this.key(r.key);const {key,...fields}=r;return this.request<W.ProjectAssistantInputResponse>(endpoint(key,"/input")+query({machine_id:key.machine_id,...fields}),s,undefined,"GET",true,PROJECT_ASSISTANTS_INPUTS_API_LEVEL) }
  canOutputs() { return (this.grant.apiLevel??0)>=PROJECT_ASSISTANTS_OUTPUTS_API_LEVEL }
  output(r:W.ProjectAssistantOutputRequest,s:AbortSignal):Promise<W.ProjectAssistantOutputResponse> {
    this.key(r.key)
    const {key,...fields}=r
    return this.request<W.ProjectAssistantOutputResponse>(endpoint(key,"/output")+query({machine_id:key.machine_id,...fields}),s,undefined,"GET",true,PROJECT_ASSISTANTS_OUTPUTS_API_LEVEL)
  }
  pictures() { return (this.grant.apiLevel ?? 0) >= (this.grant.accountKey==="local"?PROJECT_ASSISTANTS_PICTURES_API_LEVEL:PROJECT_ASSISTANTS_CLOUD_PICTURES_API_LEVEL) }
  attachmentURL(key: W.ProjectAssistantBotKey, id: string) { return this.grant.accountKey==="local" && this.pictures() && key.machine_id === this.machineID ? endpoint(key,`/attachments/${encodeURIComponent(id)}`)+query({machine_id:this.machineID}) : null }
  async attachment(key: W.ProjectAssistantBotKey,id:string,s:AbortSignal):Promise<Blob> {
    this.key(key)
    if(!/^[0-9a-f]{64}$/.test(id))throw failure("pa_invalid")
    const minimum=this.grant.accountKey==="local"?PROJECT_ASSISTANTS_PICTURES_API_LEVEL:PROJECT_ASSISTANTS_CLOUD_PICTURES_API_LEVEL
    const answer=await this.request<{contract_version:string;id:string;media_type:string;byte_count:number;data:string}>(endpoint(key,`/attachments/${id}`)+query({machine_id:this.machineID}),s,undefined,"GET",true,minimum)
    if(answer.contract_version!=="project-assistants.v1"||answer.id!==id||!["image/png","image/jpeg","image/gif","image/webp"].includes(answer.media_type)||!Number.isSafeInteger(answer.byte_count)||answer.byte_count<=0)throw failure("pa_invalid")
    let bytes:Uint8Array
    try{bytes=base64Bytes(answer.data,"attachment")}catch{throw failure("pa_invalid")}
    if(bytes.length!==answer.byte_count)throw failure("pa_invalid")
    const digest=Array.from(new Uint8Array(await crypto.subtle.digest("SHA-256",new Uint8Array(bytes)))).map(b=>b.toString(16).padStart(2,"0")).join("")
    this.check(s,false,false,minimum)
    if(digest!==id)throw failure("pa_invalid")
    return new Blob([new Uint8Array(bytes)],{type:answer.media_type})
  }
  stop(r: W.ProjectAssistantStopRequest,s: AbortSignal) { return this.request<W.ProjectAssistantReceiptResponse>(endpoint(r.command.key,"/stop"),s,r,"POST") }
  unqueue(r: W.ProjectAssistantStopRequest,s: AbortSignal) { return this.request<W.ProjectAssistantReceiptResponse>(endpoint(r.command.key,"/stop"),s,r,"POST",true,PROJECT_ASSISTANTS_UNQUEUE_API_LEVEL) }
  canUnqueue() { return (this.grant.apiLevel ?? 0) >= PROJECT_ASSISTANTS_UNQUEUE_API_LEVEL }
  sendNow(r: W.ProjectAssistantSendNowRequest,s: AbortSignal) { return this.request<W.ProjectAssistantReceiptResponse>(endpoint(r.command.key,"/send-now"),s,r,"POST",true,PROJECT_ASSISTANTS_SEND_NOW_API_LEVEL) }
  rules(key: W.ProjectAssistantBotKey,s: AbortSignal) { return this.request<W.ProjectAssistantRulesResponse>(endpoint(key,"/rules")+query({machine_id:this.machineID}),s,undefined,"GET",true,PROJECT_ASSISTANTS_RULES_API_LEVEL) }
  removeRule(r: W.ProjectAssistantRuleRemoveRequest,s: AbortSignal) { return this.request<W.ProjectAssistantRulesResponse>(endpoint(r.command.key,"/rules/remove"),s,r,"POST",true,PROJECT_ASSISTANTS_RULES_API_LEVEL) }
  setHidden(r: W.ProjectAssistantVisibilityRequest,s: AbortSignal) { return this.request<W.ProjectAssistantBotResponse>(endpoint(r.command.key,"/visibility"),s,r,"POST",true,PROJECT_ASSISTANTS_MANAGE_API_LEVEL) }
  deleteBot(r: W.ProjectAssistantDeleteRequest,s: AbortSignal) { return this.request<W.ProjectAssistantRulesResponse>(endpoint(r.command.key,"/delete"),s,r,"POST",true,PROJECT_ASSISTANTS_MANAGE_API_LEVEL) }
  // Hide and delete are carried over Cloud too, so a phone can remove a Bot.
  canManage() { return (this.grant.apiLevel ?? 0) >= PROJECT_ASSISTANTS_MANAGE_API_LEVEL }
  canRules() { return (this.grant.apiLevel ?? 0) >= PROJECT_ASSISTANTS_RULES_API_LEVEL }
  canSendNow() { return (this.grant.apiLevel ?? 0) >= PROJECT_ASSISTANTS_SEND_NOW_API_LEVEL }
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

/** Exact browser vocabulary; neither client authority nor an owner crosses this boundary. */
function browserRelayRequest(url: URL, method: string, body?: Record<string,unknown>, id = "") {
  const routes: Record<string,{method:string;word:string;fields:string[];required:string[]}> = {
    status:{method:"GET",word:"chrome-tab-status",fields:[],required:[]},
    activity:{method:"GET",word:"chrome-tab-activity",fields:[],required:[]},
    audit:{method:"GET",word:"chrome-audit",fields:["limit","site","since"],required:["limit"]},
    screenshot:{method:"GET",word:"chrome-tab-screenshot",fields:["tab","browser_mode","quality"],required:["tab"]},
    control:{method:"POST",word:"chrome-tab-control",fields:["tab","browser_mode","person"],required:["tab","person"]},
    person:{method:"POST",word:"chrome-tab-input",fields:["op","tab","browser_mode","args"],required:["op","tab","args"]},
    open:{method:"POST",word:"chrome-tab-open",fields:["url","layout"],required:["url"]},
    mute:{method:"POST",word:"chrome-tab-mute",fields:["muted"],required:["muted"]},
  }
  const route=routes[url.pathname.replace(/^\/v1\/browser\//,"")]
  const fields:Record<string,unknown>=body?{...body}:Object.fromEntries(url.searchParams)
  if(!route||route.method!==method||!fields.bot_id||typeof fields.bot_id!=="string"||!fields.conversation||typeof fields.conversation!=="string"||
    route.required.some(k=>!(k in fields))||Object.keys(fields).some(k=>!["bot_id","conversation",...route.fields].includes(k))||(!body&&url.searchParams.size!==Object.keys(fields).length)||
    (method==="POST"&&(!body||!id||url.search))||(method==="GET"&&body))throw failure("pa_invalid")
  // Screenshot query fields stay strings in Cloud's existing vocabulary.
  for(const key of (route.word==="chrome-tab-screenshot"?[]:["tab","limit","since"]))if(key in fields) {
    const value=Number(fields[key]);if(!Number.isSafeInteger(value)||value<0)throw failure("pa_invalid");fields[key]=value
  }
  return {word:route.word,kind:method==="GET"?"read" as const:"action" as const,fields,id:method==="GET"?"":id}
}

/** Route metadata used by the captured encrypted transport; never a second UI model. */
export function assistantRelayRequest(input: string, init?: RequestInit) {
  const url = new URL(input,"http://source.invalid"), method = init?.method ?? "GET"
  if(url.pathname.startsWith("/v1/browser/"))return browserRelayRequest(url,method,init?.body?JSON.parse(String(init.body)):undefined,new Headers(init?.headers).get("Idempotency-Key")??"")
  if (url.pathname === base+"/roster-events") throw failure("pa_unsupported")
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
    const routines=/^\/routines(?:\/([^/]+)\/runs)?$/.exec(tail)
    if(routines) {
      const fields=["machine_id","project_id","canonical_id","kind","conversation_id","cursor","page_size"]
      if(init?.body!=null||url.searchParams.size!==fields.length||fields.some(f=>!url.searchParams.has(f))||!/^[1-9][0-9]*$/.test(q.page_size))throw failure("pa_invalid")
      const page_size=Number(q.page_size);if(!Number.isSafeInteger(page_size)||page_size>100)throw failure("pa_invalid")
      return {word:"project-assistants."+(routines[1]?"routine-runs":"routines"),kind:"read" as const,fields:{key,project:{machine_id:q.machine_id,project_id:q.project_id,canonical_id:q.canonical_id,kind:q.kind},conversation_id:q.conversation_id,cursor:q.cursor,page_size,...(routines[1]?{routine_id:decodeURIComponent(routines[1])}:{})},id:""}
    }
    if(tail==="/input") {
      const fields=["machine_id","conversation_id","message_id","id","offset"]
      if(init?.body!=null||url.searchParams.size!==fields.length||fields.some(f=>!url.searchParams.has(f))||!/^(0|[1-9][0-9]*)$/.test(q.offset))throw failure("pa_invalid")
      const offset=Number(q.offset);if(!Number.isSafeInteger(offset)||offset>100*1024*1024)throw failure("pa_invalid")
      return {word:"project-assistants.input",kind:"read" as const,fields:{...key,conversation_id:q.conversation_id,message_id:q.message_id,id:q.id,offset},id:""}
    }
    if(tail==="/output") {
      const fields=["machine_id","conversation_id","message_id","id","artifact_id","offset"]
      if(init?.body!=null||url.searchParams.size!==fields.length||fields.some(f=>!url.searchParams.has(f))||! /^(0|[1-9][0-9]*)$/.test(q.offset))throw failure("pa_invalid")
      const offset=Number(q.offset);if(!Number.isSafeInteger(offset)||offset>100*1024*1024)throw failure("pa_invalid")
      return {word:"project-assistants.output",kind:"read" as const,fields:{...key,conversation_id:q.conversation_id,message_id:q.message_id,id:q.id,artifact_id:q.artifact_id,offset},id:""}
    }
    const picture=/^\/attachments\/([0-9a-f]{64})$/.exec(tail)
    if(picture){if(url.searchParams.size!==1||!q.machine_id)throw failure("pa_invalid");return {word:"project-assistants.attachment",kind:"read" as const,fields:{...key,id:picture[1]},id:""}}
    const action = tail === "/messages" ? "history" : tail === "/events" ? "events" : tail === "/export" ? "export" : tail === "/rules" ? "rules" : tail === "" ? "get" : null
    if (!action) throw failure("pa_invalid")
    const fields = action === "history" ? {key,conversation_id:q.conversation_id,before_seq:Number(q.before_seq),page_size:Number(q.page_size)} : action === "events" ? {key,after_seq:Number(q.after_seq),page_size:Number(q.page_size)} : key
    return {word:`project-assistants.${action}`,kind:"read" as const,fields,id:""}
  }
  if (typeof init?.body !== "string") throw failure("pa_invalid")
  const fields: Record<string,unknown> = JSON.parse(init.body)
  const command = fields.command
  if (!command || typeof command !== "object" || !("idempotency_key" in command) || typeof command.idempotency_key !== "string") throw failure("pa_invalid")
  if (new Headers(init.headers).get("Idempotency-Key") !== command.idempotency_key) throw failure("pa_invalid")
  const routine=/^\/routines(?:\/([^/]+)(?:\/(enabled|runs|hook))?)?$/.exec(tail)
  if(routine) {
    const action=method==="DELETE"&&routine[1]&&!routine[2]?"routine-delete":method==="POST"&&!routine[1]?"routine-save":method==="POST"&&routine[2]==="enabled"?"routine-enabled":method==="POST"&&routine[2]==="runs"?"routine-test":method==="POST"&&routine[2]==="hook"?"routine-hook":null
    if(!action||(routine[1]&&decodeURIComponent(routine[1])!==fields.id))throw failure("pa_invalid")
    return {word:"project-assistants."+action,kind:"action" as const,fields,id:command.idempotency_key}
  }
  const action = !bot && method === "POST" ? "create" : bot && !tail && method === "PATCH" ? "edit" : method !== "POST" ? null :
    tail === "/messages" ? "send" : tail === "/upload" ? "upload" : tail === "/stop" ? "stop" : /^\/permissions\/[^/]+\/answer$/.test(tail) ? "answer" :
    tail === "/read" ? "read" : tail === "/observed" ? "observed" : tail === "/memory/forget" ? "forget" : tail === "/memory" ? "memory" :
    tail === "/visibility" ? "visibility" : tail === "/delete" ? "delete" :
    tail === "/send-now" ? "send-now" : tail === "/rules/remove" ? "rule-remove" : null
  if (!action) throw failure("pa_invalid")
  return {word:`project-assistants.${action==="send"&&Array.isArray(fields.upload_ids)&&fields.upload_ids.length?"send-files":action==="send"&&Array.isArray(fields.images)&&fields.images.length?"send-pictures":action}`,kind:"action" as const,fields,id:command.idempotency_key}
}

/** Uses the passed machine and authenticated connection, never install.ts's active reader. */
export type AssistantRelayConnection = Pick<CloudClientHandle,"_machineRequest" | "_machineRequestAs" | "machineDescriptor">
export function capturedAssistantRelayFetch(machineID: string, current: () => AssistantRelayConnection | null, carried: readonly string[]): AssistantFetch {
  return async (input,init) => {
    let request: ReturnType<typeof assistantRelayRequest>, client: AssistantRelayConnection | null
    // Everything before the encrypted request is a refusal that sent nothing.
    try {
      request = assistantRelayRequest(input,init); client = current()
      if (!carried.includes(request.word)) return Response.json({error:{code:"cloud_not_carried"}},{status:501})
      if (!client) throw failure("pa_offline")
      if (init?.signal?.aborted) throw new DOMException("Aborted","AbortError")
      if (!client.machineDescriptor?.(machineID)?.machine?.commands?.includes(request.word)) throw failure("pa_unsupported")
      if(request.word==="project-assistants.send-pictures"){
        // AES-GCM adds its 16-byte authentication tag to the complete plaintext.
        const bytes=new TextEncoder().encode(JSON.stringify({type:request.word,session:"__clawdline_machine__",request:request.id,...request.fields})).byteLength
        if(bytes+16>RELAY_CTL_MAX_BYTES)throw {...failure("pa_capacity_full"),error:{...failure("pa_capacity_full").error,field:"cloud_command_bytes"}}
      }
    } catch (error) { throw notSent(error) }
    // The connection's authenticated principal is authoritative; body reader_id is only a hint.
    const connection = client!, read = request.kind === "read"
    if (!(read ? connection._machineRequest : connection._machineRequestAs)) throw notSent(failure("pa_unsupported"))
    const answer = read ? await connection._machineRequest?.(machineID,request.word,request.fields,"read",undefined,{signal:init?.signal ?? undefined}) :
      await connection._machineRequestAs?.(request.id,machineID,request.word,request.fields,"action")
    if (answer === undefined) throw failure("pa_unsupported")
    if (init?.signal?.aborted) throw new DOMException("Aborted","AbortError")
    return Response.json(answer)
  }
}

function browserStorage(): Storage | undefined {
  try { return typeof window === "undefined" ? undefined : window.localStorage } catch { return undefined }
}

/** Account-owned state survives App's active-machine key and token renewal. */
export class ProjectAssistantRegistry {
  readonly accountKey: string
  readonly readerID: string
  // Drafts are keyed by account and Bot; without browser storage every chat
  // showed "Draft could not be saved on this device".
  readonly viewState = createViewState(browserStorage())
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
      // The first roster can precede the signed API descriptor. Capability
      // controls belong to that descriptor, so replace its captured client.
      if (next.get(id)?.authorizationRevision !== this.revisions.get(id) || next.get(id)?.apiLevel !== this.grants.get(id)?.apiLevel) {client.revoke(); this.clients.delete(id);this.revisions.delete(id); changed = true}
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
    // Every paired device may act (the person, 2026-11-14). Only a machine
    // that turned Cloud operation off is read-only here; an operation flag not
    // yet received is not a refusal, and the daemon still checks each write.
    online:machine.freshness === "current",writable:machine.operationAllowed !== false,name:machine.name||undefined}))
}

export function assistantBadge(items: readonly W.ProjectAssistantBotSummary[], authorized: readonly SourceClient[]) {
  return items.reduce((count,item) => {
    const source=authorized.find(source=>source.machineID===item.bot.key.machine_id)
    if(item.bot.hidden||!source||!canRead(item.source))return count
    // current() also becomes false while offline. Preserve the existing unread
    // badge in that case, but never animate the cached activity.
    const mood=source.current?.()===false?"idle":rosterMood(item)
    return {unread:count.unread+item.unread_count,pendingPermissions:count.pendingPermissions+item.pending_permissions,
      mood:count.mood==="needsInput"||mood==="needsInput"?"needsInput" as const:count.mood==="working"||mood==="working"?"working" as const:"idle" as const}
  },{unread:0,pendingPermissions:0,mood:"idle" as BotMood})
}
