import { useEffect, useState } from "react"
import type { HealthSay } from "./words"
import { PERFORMANCE_LIMITS } from "./performance"

/** Separate consent for an existing revocable Admin notification device. */
export function MonitoringNotifications({ get, origin, say }: { get: typeof fetch; origin: string; say: HealthSay }) {
  const [value,setValue]=useState<{available:boolean;enabled:boolean}|null>(null),[failed,setFailed]=useState<string|null>(null),[busy,setBusy]=useState(false)
  useEffect(()=>{
    let alive=true
    const ctl=new AbortController(),timer=setTimeout(()=>ctl.abort(),PERFORMANCE_LIMITS.timeoutMs)
    setValue(null);setFailed(null)
    void get(origin+"/su/v1/health/performance/notifications",{credentials:"include",cache:"no-store",signal:ctl.signal}).then(async r=>{
      if(!r.ok)throw new Error(`monitor_http_${r.status}`);const data=await r.json() as {available?:unknown;enabled?:unknown}
      if(typeof data.available!=="boolean"||typeof data.enabled!=="boolean")throw new Error("monitor_contract")
      if(alive&&!ctl.signal.aborted)setValue({available:data.available,enabled:data.enabled})
    }).catch(error=>{if(alive)setFailed(error instanceof Error ? error.message : "monitor_unavailable")}).finally(()=>clearTimeout(timer))
    return()=>{alive=false;clearTimeout(timer);ctl.abort()}
  },[get,origin])
  async function toggle(){
    if(!value||busy)return
    setBusy(true);setFailed(null)
    const ctl=new AbortController(),timer=setTimeout(()=>ctl.abort(),PERFORMANCE_LIMITS.timeoutMs)
    try{
      const r=await get(origin+"/su/v1/health/performance/notifications",{method:"PUT",credentials:"include",cache:"no-store",headers:{"content-type":"application/json"},body:JSON.stringify({enabled:!value.enabled}),signal:ctl.signal})
      if(r.status===401||r.status===403||r.status===404){setValue(null);throw new Error(`monitor_http_${r.status}`)}
      if(!r.ok)throw new Error(`monitor_http_${r.status}`);const data=await r.json() as {available:boolean;enabled:boolean}
      if(typeof data.available!=="boolean"||typeof data.enabled!=="boolean")throw new Error("monitor_contract");setValue(data)
    }catch(error){setFailed(error instanceof Error ? error.message : "monitor_unavailable")}finally{clearTimeout(timer);setBusy(false)}
  }
  return <section className="adm-panel"><h2>{say("pf_notifyTitle")}</h2><p className="adm-fine">{say("pf_notifyCopy")}</p>{value?.available?<button type="button" className="adm-btn adm-btn-small" disabled={busy} onClick={()=>void toggle()}>{say(value.enabled?"pf_notifyDisable":"pf_notifyEnable")}</button>:value?<a className="adh-link" href="/help/admin">{say("pf_notifySetup")}</a>:null}{failed?<p role="status">{say("pf_notifyUnavailable")} <code>{failed}</code></p>:null}<p className="adm-fine">{say("pf_notifyDelivery")}</p></section>
}
