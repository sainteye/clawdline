import { useEffect, useState } from "react"
import { client, followsRelay } from "../client"
import { nextWord } from "../next-strings"
import { LOCAL_PERFORMANCE_LIMITS, localQuantile, readLocalPerformance, type Histogram, type LocalPerformance, type LocalPoint } from "./api-performance"
import "./api-performance.css"

const fmt=(v:number|null|undefined)=>v==null?"—":v.toLocaleString(undefined,{maximumFractionDigits:1})
const at=(v:number)=>new Date(v).toLocaleString(undefined,{hour12:false})
export function LocalAPIPerformance() {
  const [hours,setHours]=useState<1|6|24|168>(1),[until,setUntil]=useState<number|null>(null),[refresh,setRefresh]=useState(0)
  const [held,setHeld]=useState<{key:string;data:LocalPerformance|null;error:string|null;loading:boolean}>({key:"",data:null,error:null,loading:true})
  const key=`${hours}|${until??"now"}`,remote=followsRelay()
  useEffect(()=>{
    if(remote)return
    let alive=true,ctl:AbortController|null=null
    const run=()=>{
      if(document.hidden||ctl)return
      const mine=new AbortController();ctl=mine
      setHeld(v=>({key,data:v.key===key?v.data:null,error:v.key===key?v.error:null,loading:true}))
      const q=new URLSearchParams({hours:String(hours)});if(until!==null)q.set("until",String(until))
      void readLocalPerformance(globalThis.fetch,client.url(`/v1/api-performance?${q}`),mine.signal).then(data=>{if(alive&&!mine.signal.aborted)setHeld({key,data,error:null,loading:false})}).catch(e=>{
        if(alive&&!mine.signal.aborted){const error=e instanceof Error?e.message:"api_performance_unavailable";setHeld(v=>({key,data:error==="api_performance_refused"?null:v.key===key?v.data:null,error,loading:false}))}
      }).finally(()=>{if(ctl===mine)ctl=null})
    }
    const visible=()=>{if(document.hidden){ctl?.abort();ctl=null;setHeld({key,data:null,error:null,loading:true})}else run()}
    const revalidate=()=>{ctl?.abort();ctl=null;setHeld({key,data:null,error:null,loading:true});run()}
    run();const timer=until===null?setInterval(run,LOCAL_PERFORMANCE_LIMITS.refreshMs):null
    document.addEventListener("visibilitychange",visible)
    window.addEventListener("focus",revalidate);window.addEventListener("storage",revalidate)
    return()=>{alive=false;ctl?.abort();if(timer)clearInterval(timer);document.removeEventListener("visibilitychange",visible);window.removeEventListener("focus",revalidate);window.removeEventListener("storage",revalidate)}
  },[key,hours,until,refresh,remote])
  if(remote)return <p className="machine-note" role="status">{nextWord("apiPerformanceDirect")}</p>
  const state=held.key===key?held:{key,data:null,error:null,loading:true},data=state.data
  const q=(h:Histogram,p=95)=>{if(!data)return"—";const value=localQuantile(h,data.histogram_upper_bounds_ms,p);return value===null?(h.count?">60000 ms":"—"):`${fmt(value)} ms`}
  const stateWord=(p:LocalPoint)=>p.state==="missing"?nextWord("apiPerformanceMissing"):p.state==="partial"?nextWord("apiPerformancePartial"):p.state==="no_traffic"?nextWord("apiPerformanceNoTraffic"):""
  const routes=(rows:LocalPerformance["slow_routes"])=>rows.length?<div className="lap-routes">{rows.map(r=><article key={r.route}><code>{r.route}</code><dl><dt>{nextWord("apiPerformanceCount")}</dt><dd>{fmt(r.totals.completed)}</dd><dt>P95 / P99</dt><dd>{q(r.totals.finite_latency_ms)} / {q(r.totals.finite_latency_ms,99)}</dd><dt>5xx / 499</dt><dd>{fmt(r.totals.status_5xx)} / {fmt(r.totals.cancelled)}</dd><dt>Σ / max</dt><dd>{fmt(r.totals.finite_latency_ms.sum_ms)} / {fmt(r.totals.finite_latency_ms.max_ms)} ms</dd></dl></article>)}</div>:<p className="machine-note">{nextWord("apiPerformanceEmpty")}</p>
  return <div className="lap">
    <p className="machine-note">{nextWord("apiPerformanceLocal")}</p>
    <div className="lap-controls" role="group" aria-label={nextWord("apiPerformanceWindow")}>{([1,6,24,168] as const).map(h=><button type="button" key={h} aria-pressed={hours===h} onClick={()=>setHours(h)}>{nextWord(`apiPerformance${h}`)}</button>)}<button type="button" disabled={state.loading} onClick={()=>setRefresh(v=>v+1)}>{nextWord("apiPerformanceRefresh")}</button></div>
    <div className="lap-controls"><button type="button" onClick={()=>setUntil((until??Date.now())-hours*3600000)}>{nextWord("apiPerformancePrevious")}</button><button type="button" disabled={until===null} onClick={()=>setUntil(until!+hours*3600000>=Date.now()?null:until!+hours*3600000)}>{nextWord("apiPerformanceNext")}</button>{until!==null?<button type="button" onClick={()=>setUntil(null)}>{nextWord("apiPerformanceNow")}</button>:null}</div>
    {state.error?<p className="lap-warning" role="alert">{nextWord(state.error==="api_performance_refused"?"apiPerformanceRefused":state.error==="api_performance_unsupported"?"apiPerformanceUnsupported":"apiPerformanceUnavailable")}</p>:null}
    {!data&&state.loading?<p role="status">{nextWord("apiPerformanceLoading")}</p>:null}
    {data?<>
      <p className={data.stale||state.error?"lap-warning":"machine-note"} role="status">{nextWord(data.stale||state.error?"apiPerformanceStale":"apiPerformanceLive")} · {at(data.at_ms)}</p>
      <h3>{nextWord("apiPerformanceWindow")}</h3><p className="machine-note">{at(data.from_ms)} – {at(data.until_ms)}</p>
      <div className="lap-cards">{[[nextWord("apiPerformanceCount"),fmt(data.totals.completed)],["P95",q(data.totals.finite_latency_ms)],[nextWord("apiPerformanceErrors"),`${fmt(data.totals.status_5xx)} / ${fmt(data.totals.cancelled)}`],[nextWord("apiPerformanceActive"),`${fmt(data.active)} / ${fmt(data.open_streams)}`]].map(([label,value])=><div key={label}><span>{label}</span><strong>{value}</strong></div>)}</div>
      <LocalHistory points={data.points} bounds={data.histogram_upper_bounds_ms}/>
      <section><h3>{nextWord("apiPerformanceRecent")}</h3><ul className="lap-values">{data.recent_minutes.slice(0,5).map(p=><li key={p.start_ms}><time>{at(p.start_ms)}</time><span>{fmt(p.totals.completed)} · P95 {q(p.totals.finite_latency_ms)}</span><small>{fmt(p.covered_seconds)} / 60 s · {stateWord(p)}</small></li>)}</ul></section>
      <section><h3>{nextWord("apiPerformanceRecentRoutes")}</h3>{routes(data.recent_slow_routes)}</section>
      <details><summary>{nextWord("apiPerformanceRoutes")}</summary>{routes(data.slow_routes)}</details>
      <details><summary>{nextWord("apiPerformanceTable")}</summary><ul className="lap-values">{data.points.map(p=><li key={p.start_ms}><time>{at(p.start_ms)}</time><span>{fmt(p.totals.completed)} · P95 {q(p.totals.finite_latency_ms)} · P99 {q(p.totals.finite_latency_ms,99)}</span><small>{fmt(p.covered_seconds)} / {data.resolution_seconds} s · {stateWord(p)} {p.source.mixed?nextWord("apiPerformanceMixed"):null}</small></li>)}</ul></details>
      <details><summary>{nextWord("apiPerformanceQuality")}</summary><p className="machine-note">{nextWord("apiPerformanceMethod")}</p><dl><dt>{nextWord("apiPerformanceBuild")}</dt><dd><code>{data.build}</code></dd><dt>{nextWord("apiPerformanceStorage")}</dt><dd>{data.storage_state}</dd><dt>{nextWord("apiPerformanceStored")}</dt><dd>{data.persisted_at_ms?at(data.persisted_at_ms):"—"}</dd><dt>{nextWord("apiPerformanceUnpersisted")}</dt><dd>{fmt(data.unpersisted_seconds)}</dd><dt>{nextWord("apiPerformanceDropped")}</dt><dd>{fmt(data.dropped)} / {fmt(data.queued_events)}</dd></dl>{data.storage_state!=="ok"?<p className="lap-warning">{nextWord("apiPerformanceUnavailableStore")}</p>:null}</details>
    </>:null}
  </div>
}

function LocalHistory({points,bounds}:{points:LocalPoint[];bounds:number[]}) {
  const latency=points.map(p=>localQuantile(p.totals.finite_latency_ms,bounds,95)),max=Math.max(1,...latency.map(v=>v??0))
  let path="",open=false
  points.forEach((p,i)=>{const v=latency[i];if(v===null||p.state==="missing"){open=false;return}path+=`${open?"L":"M"}${20+i/Math.max(1,points.length-1)*760},${140-v/max*120} `;open=true})
  return <div className="lap-chart"><svg viewBox="0 0 800 170" role="img" aria-label="P95"><title>P95</title><line x1="20" x2="780" y1="140" y2="140" stroke="currentColor" opacity=".2"/><path d={path} fill="none" stroke="#d39b75" strokeWidth="2" vectorEffect="non-scaling-stroke"/><text x="20" y="165">{points[0]?new Date(points[0].start_ms).toLocaleTimeString([],{hour:"2-digit",minute:"2-digit"}):""}</text><text x="780" y="165" textAnchor="end">{points.at(-1)?new Date(points.at(-1)!.end_ms).toLocaleTimeString([],{hour:"2-digit",minute:"2-digit"}):""}</text><text x="20" y="16">{fmt(max)} ms</text></svg></div>
}
