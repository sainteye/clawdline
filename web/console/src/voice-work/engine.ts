/** Durable state belongs to the selected machine, never localStorage. */
export interface VoiceTask {
  id: string; text: string; tool: string; args: Record<string, unknown>; sources: string[]; depends_on: string[]
  priority: number; version: number; approved_version?: number; status: string; result?: string; error?: string
  runner?: string; action_key?: string; lease_until?: number
}
export interface VoiceInput { id: string; text: string; call?: string; planned: boolean; at: number }
export interface VoiceState { id: string; revision: number; plan_version: number; inputs: VoiceInput[]; tasks: VoiceTask[]; reply?: string; planner_error?: string }
export interface VoiceAnswer { state: VoiceState; claimed?: VoiceTask | null; planning?: boolean }
export const WORK_REQUEST_MS = 10000
export const WORK_POLL_MS = 1000
export const WORK_HEARTBEAT_MS = 30000
export const WORK_TEXT_RUNES = 4000
export const WORK_EXECUTION_MS = 120000
export const shortResult = (s: string) => Array.from(s).slice(0, WORK_TEXT_RUNES).join('')
export class WorkRefusal extends Error {
  readonly code:string;readonly uncertain:boolean
  constructor(code: string, uncertain: boolean) { super(code);this.code=code;this.uncertain=uncertain }
}

export async function workRequest(fetcher: typeof fetch, change?: Record<string, unknown>, signal?: AbortSignal): Promise<VoiceAnswer> {
  const control=new AbortController()
  const aborted=()=>control.abort(signal?.reason)
  if(signal?.aborted)aborted();else signal?.addEventListener('abort',aborted,{once:true})
  const timer=setTimeout(()=>control.abort(new Error('voice_work_timeout')),WORK_REQUEST_MS)
  try {
    const res = await fetcher('/v1/voice/copilot-work', change ? {
      method:'POST',headers:{'Content-Type':'application/json','Idempotency-Key':crypto.randomUUID()},body:JSON.stringify(change),signal:control.signal,
    } : {signal:control.signal})
    const body = await res.json().catch(() => null)
    if (!res.ok || !body?.state) throw new WorkRefusal(String(body?.error?.code ?? body?.error ?? 'voice_work_unavailable'),res.status >= 500)
    return body as VoiceAnswer
  } catch(error) {
    if(error instanceof WorkRefusal)throw error
    throw new WorkRefusal(control.signal.aborted?'voice_work_timeout':'voice_work_transport_unknown',!!change)
  } finally {clearTimeout(timer);signal?.removeEventListener('abort',aborted)}
}

export interface WorkRuntimeDeps {
  request: (change?: Record<string, unknown>, signal?: AbortSignal) => Promise<VoiceAnswer>
  execute: (task: VoiceTask, signal: AbortSignal, progress: (value: string) => Promise<void>) => Promise<{result: string; error?: string; unknown?: boolean}>
}

/** One page owner, fenced by both machine generation and daemon claim receipts.
 * Claims are shared across devices. Lost writes are never automatically retried.
 */
export class VoiceWorkRuntime {
  state: VoiceState | null = null
  planning = false
  failure = ''
  private owners = 0
  private generation = 0
  private polling = false
  private reading?:AbortController
  private timer: ReturnType<typeof setTimeout> | undefined
  private active = new Map<string, AbortController>()
  private runner = crypto.randomUUID()
  private listeners = new Set<() => void>()
  onPrepared?: (task: VoiceTask, value: string, final: boolean) => void
  private deps:WorkRuntimeDeps
  constructor(deps: WorkRuntimeDeps) {this.deps=deps}
  subscribe = (f: () => void) => { this.listeners.add(f); return () => {this.listeners.delete(f)} }
  private publish() { for (const f of this.listeners) f() }
  private accept(answer: VoiceAnswer) {
    if (!this.state || answer.state.revision >= this.state.revision) this.state = answer.state
    if (answer.planning !== undefined) this.planning = answer.planning
    this.failure = ''; this.publish()
  }
  activate() {
    this.owners++; void this.refresh()
    return () => { this.owners--; if (!this.owners && this.timer) clearTimeout(this.timer) }
  }
  /** Forget content before even issuing a request on a new machine. */
  switchMachine() {
    this.reading?.abort();this.polling=false
    this.generation++; this.runner=crypto.randomUUID()
    for (const control of this.active.values()) control.abort()
    this.active.clear(); this.state=null;this.planning=false;this.failure='';this.publish()
    if(this.owners) void this.refresh()
  }
  async change(change: Record<string, unknown>): Promise<VoiceAnswer> {
    const generation=this.generation
    const answer=await this.deps.request(change)
    if(generation!==this.generation) throw new WorkRefusal('voice_work_machine_changed',true)
    this.accept(answer);void this.refresh();return answer
  }
  async refresh() {
    if(this.polling) return
    this.polling=true;const generation=this.generation
    const reading=new AbortController();this.reading=reading
    try {
      const answer=await this.deps.request(undefined,reading.signal)
      if(generation!==this.generation)return
      this.accept(answer)
      if(this.owners && answer.state.tasks.some(t=>t.status==='queued'||((t.status==='running'||t.status==='cancel_requested')&&(t.lease_until??0)<Date.now()/1000))) {
        const claim=await this.deps.request({action:'claim',runner:this.runner},reading.signal)
        if(generation!==this.generation)return
        this.accept(claim)
        if(claim.claimed) this.run(claim.claimed,generation,this.runner)
      }
    } catch(error) { if(generation===this.generation){this.failure=error instanceof Error?error.message:String(error);this.publish()} }
    finally {
      if(generation!==this.generation)return
      this.polling=false
      if(this.reading===reading)this.reading=undefined
      if(this.timer)clearTimeout(this.timer)
      if(this.owners) this.timer=setTimeout(()=>{void this.refresh()},WORK_POLL_MS)
    }
  }
  private run(task: VoiceTask,generation: number,runner: string) {
    const control=new AbortController();this.active.set(task.id,control)
    const current=()=>generation===this.generation
    const deadline=setTimeout(()=>control.abort(new Error('voice_work_execution_timeout')),WORK_EXECUTION_MS)
    const heartbeat=setInterval(()=>{
      if(current()) void this.deps.request({action:'heartbeat',runner}).catch(()=>undefined)
    },WORK_HEARTBEAT_MS)
    void (async()=>{
      let result:{result:string;error?:string;unknown?:boolean}
      try {
        result=await Promise.race([this.deps.execute(task,control.signal,async(value)=>{
          if(!current())return
          const answer=await this.deps.request({action:'progress',id:task.id,runner,key:task.action_key,result:shortResult(value)})
          if(current()){this.accept(answer);this.onPrepared?.(task,value,false)}
        }),new Promise<never>((_,reject)=>control.signal.addEventListener('abort',()=>reject(control.signal.reason??new Error('voice_work_execution_stopped')),{once:true}))])
      } catch(error) {result={result:'',error:shortResult(error instanceof Error?error.message:String(error)),unknown:true} }
      if(!current())return
      const answer=await this.deps.request({action:'finish',id:task.id,runner,key:task.action_key,...result,result:shortResult(result.result)})
      if(current()){this.accept(answer);this.onPrepared?.(task,result.result,true)}
    })().catch(error=>{if(current()){this.failure=String(error);this.publish()}}).finally(()=>{
      clearInterval(heartbeat);clearTimeout(deadline);if(this.active.get(task.id)===control)this.active.delete(task.id)
      if(current()&&this.owners)void this.refresh()
    })
  }
}
