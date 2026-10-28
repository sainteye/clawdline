/** Owner-local request aggregates. This adapter never uses an operator endpoint. */
export const LOCAL_PERFORMANCE_LIMITS = { bodyBytes: 2097152, points: 168, routes: 129, refreshMs: 30000, timeoutMs: 6000 } as const
export type Histogram = { bins: number[]; count: number; sum_ms: number; max_ms: number }
export type LocalTotals = { completed: number; status_5xx: number; cancelled: number; stream_opened: number; stream_completed: number; finite_latency_ms: Histogram; stream_lifetime_ms: Histogram }
export type LocalPoint = { start_ms: number; end_ms: number; state: "missing" | "partial" | "no_traffic" | "ok"; covered_seconds: number; totals: LocalTotals; source: { boot_id?: string; build?: string; mixed: boolean } }
export type LocalPerformance = { schema: 1; source: "owner_local"; boot_id: string; build: string; at_ms: number; stale: boolean; from_ms: number; until_ms: number; resolution_seconds: number; points: LocalPoint[]; recent_minutes: LocalPoint[]; totals: LocalTotals; slow_routes: { route: string; totals: LocalTotals }[]; recent_slow_routes: { route: string; totals: LocalTotals }[];
  active: number; open_streams: number; oldest_active_age_ms: number | null; active_coverage_complete: boolean; storage_state: string; persisted_at_ms: number; unpersisted_seconds: number; dropped: number; queued_events: number; loss_scope_known: boolean; route_overflow: number; histogram_upper_bounds_ms: number[] }
const fail = (): never => { throw new Error("api_performance_contract") }
const obj = (v: unknown): Record<string, unknown> => v && typeof v === "object" && !Array.isArray(v) ? v as Record<string, unknown> : fail()
const num = (v: unknown): number => typeof v === "number" && Number.isFinite(v) && v >= 0 ? v : fail()
const str = (v: unknown): string => typeof v === "string" && v.length <= 256 ? v : fail()
const bool = (v: unknown): boolean => typeof v === "boolean" ? v : fail()
const list = <T>(v: unknown, cap: number, parse: (v: unknown) => T): T[] => Array.isArray(v) && v.length <= cap ? v.map(parse) : fail()
function histogram(v: unknown): Histogram { const h = obj(v), bins = list(h.bins,16,num); if (bins.length !== 16 || bins.reduce((a,b)=>a+b,0) !== num(h.count)) fail(); return { bins, count:num(h.count),sum_ms:num(h.sum_ms),max_ms:num(h.max_ms) } }
function totals(v: unknown): LocalTotals { const t=obj(v); return { completed:num(t.completed),status_5xx:num(t.status_5xx),cancelled:num(t.cancelled),stream_opened:num(t.stream_opened),stream_completed:num(t.stream_completed),finite_latency_ms:histogram(t.finite_latency_ms),stream_lifetime_ms:histogram(t.stream_lifetime_ms) } }
function point(v: unknown): LocalPoint { const p=obj(v),s=obj(p.source); if (!["missing","partial","no_traffic","ok"].includes(String(p.state))) fail(); return { start_ms:num(p.start_ms),end_ms:num(p.end_ms),state:p.state as LocalPoint["state"],covered_seconds:num(p.covered_seconds),totals:totals(p.totals),source:{ mixed:bool(s.mixed),...(s.boot_id===undefined?{}:{boot_id:str(s.boot_id)}),...(s.build===undefined?{}:{build:str(s.build)}) } } }
export function parseLocalPerformance(v: unknown): LocalPerformance {
  const o=obj(v)
  if (o.schema !== 1 || o.source !== "owner_local") fail()
  const routes=(v:unknown)=>list(v,LOCAL_PERFORMANCE_LIMITS.routes,v=>{const r=obj(v);return {route:str(r.route),totals:totals(r.totals)}})
  const bounds=list(o.histogram_upper_bounds_ms,15,num)
  if (bounds.length!==15 || bounds.some((v,i)=>i>0 && v<=bounds[i-1])) fail()
  return { schema:1,source:"owner_local",boot_id:str(o.boot_id),build:str(o.build),at_ms:num(o.at_ms),stale:bool(o.stale),from_ms:num(o.from_ms),until_ms:num(o.until_ms),resolution_seconds:num(o.resolution_seconds),points:list(o.points,LOCAL_PERFORMANCE_LIMITS.points,point),recent_minutes:list(o.recent_minutes,6,point),totals:totals(o.totals),slow_routes:routes(o.slow_routes),recent_slow_routes:routes(o.recent_slow_routes),active:num(o.active),open_streams:num(o.open_streams),oldest_active_age_ms:o.oldest_active_age_ms===null?null:num(o.oldest_active_age_ms),active_coverage_complete:bool(o.active_coverage_complete),storage_state:str(o.storage_state),persisted_at_ms:num(o.persisted_at_ms),unpersisted_seconds:num(o.unpersisted_seconds),dropped:num(o.dropped),queued_events:num(o.queued_events),loss_scope_known:bool(o.loss_scope_known),route_overflow:num(o.route_overflow),histogram_upper_bounds_ms:bounds }
}
/** Returns null for empty or overflow; callers distinguish >last-bound from unavailable. */
export function localQuantile(h: Histogram, bounds: number[], p: number): number | null {
  if (!h.count) return null
  const rank=Math.ceil(h.count*p/100)
  let n=0
  for(let i=0;i<h.bins.length;i++){n+=h.bins[i];if(n>=rank)return i<bounds.length?bounds[i]:null}
  return null
}
export async function readLocalPerformance(get:typeof fetch, url:string, signal:AbortSignal):Promise<LocalPerformance> {
  const ctl=new AbortController(),abort=()=>ctl.abort()
  signal.addEventListener("abort",abort,{once:true});if(signal.aborted)ctl.abort()
  const timer=setTimeout(abort,LOCAL_PERFORMANCE_LIMITS.timeoutMs)
  try {
    const r=await get(url,{credentials:"same-origin",cache:"no-store",signal:ctl.signal})
    if (!r.ok) throw new Error(r.status===401||r.status===403?"api_performance_refused":r.status===404||r.status===501?"api_performance_unsupported":"api_performance_unavailable")
    const chunks:Uint8Array[]=[];let bytes=0
    const reader=r.body?.getReader();if(!reader)throw new Error("api_performance_contract")
    while(true){const {done,value}=await reader.read();if(done)break;bytes+=value.byteLength;if(bytes>LOCAL_PERFORMANCE_LIMITS.bodyBytes){await reader.cancel();throw new Error("api_performance_bound")}chunks.push(value)}
    const data=new Uint8Array(bytes);let offset=0;for(const c of chunks){data.set(c,offset);offset+=c.byteLength}
    return parseLocalPerformance(JSON.parse(new TextDecoder().decode(data)))
  }finally{clearTimeout(timer);signal.removeEventListener("abort",abort)}
}
