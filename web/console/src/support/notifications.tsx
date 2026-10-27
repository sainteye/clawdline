import { useEffect, useState } from "react"
import type { SupportLocale } from "./model.js"
import { supportWord as w } from "./words.js"
import { SUPPORT_UI_LIMITS, requestKey } from "./api.js"

type Call = <T>(path: string, method?: string, body?: unknown, key?: string) => Promise<T>
const scope = "/help/admin/"
const path = "/su/v1/support/notifications"
function supported() { return typeof Notification !== "undefined" && "serviceWorker" in navigator && "PushManager" in window }
function own(registration?: ServiceWorkerRegistration) { return registration?.scope === new URL(scope, location.origin).href ? registration : null }
function keyBytes(key: string) {
  return Uint8Array.from(atob(key.replace(/-/g,"+").replace(/_/g,"/")), c=>c.charCodeAt(0))
}
export function SupportNotifications({ call, locale }: { call: Call; locale: SupportLocale }) {
  const [loadVersion,setLoadVersion]=useState(0)
  const [enabled,setEnabled]=useState<boolean|null>(null),[busy,setBusy]=useState(false),[message,setMessage]=useState("")
  useEffect(()=>{let active=true;void call<{enabled:boolean}>(path).then(async row=>{
    const registration=supported()?own(await navigator.serviceWorker.getRegistration(scope)):null
    const sub=await registration?.pushManager.getSubscription()
    if(active)setEnabled(row.enabled&&!!sub)
  }).catch(()=>{if(active)setMessage(w("notifyFailed"))});return()=>{active=false}},[call,loadVersion])
  async function toggle() {
    if(busy)return
    setBusy(true);setMessage("")
    try {
      if(enabled){
        await call(path,"PUT",{enabled:false},requestKey())
        setEnabled(false)
        const registration=own(await navigator.serviceWorker.getRegistration(scope))
        await (await registration?.pushManager.getSubscription())?.unsubscribe()
      }else{
        if(!supported()) {setMessage(w("notifyUnsupported"));return}
        const permission=await Notification.requestPermission()
        if(permission!=="granted"){setMessage(w("notifyBlocked"));return}
        const {public_key}=await call<{public_key:string}>(path+"/key")
        const registration=await navigator.serviceWorker.register("/support-sw.js",{scope,updateViaCache:"none"})
        if (!registration.active) await new Promise<void>((resolve,reject)=>{
          const worker=registration.installing??registration.waiting
          if(!worker){reject(new Error("worker_unavailable"));return}
          const done=()=>{if(worker.state==="activated"){clearTimeout(timer);worker.removeEventListener("statechange",done);resolve()}else if(worker.state==="redundant"){clearTimeout(timer);worker.removeEventListener("statechange",done);reject(new Error("worker_redundant"))}}
          const timer=setTimeout(()=>{worker.removeEventListener("statechange",done);reject(new Error("worker_timeout"))},SUPPORT_UI_LIMITS.requestMs)
          worker.addEventListener("statechange",done);done()
        })
        // The activated worker owns only the Admin path; never use serviceWorker.ready,
        // which may return the encrypted Session worker at the root scope.
        const existing=await registration.pushManager.getSubscription()
        let sub=existing
        const held=existing?.options.applicationServerKey
        if(held&&Array.from(new Uint8Array(held)).join(",")!==Array.from(keyBytes(public_key)).join(",")){await existing!.unsubscribe();sub=null}
        if(!sub)sub=await registration.pushManager.subscribe({userVisibleOnly:true,applicationServerKey:keyBytes(public_key)})
        await call(path,"PUT",{enabled:true,subscription:sub.toJSON(),locale},requestKey())
        setEnabled(true)
      }
    }catch{setMessage(w("notifyFailed"))}finally{setBusy(false)}
  }
  async function test() {
    setBusy(true);setMessage("")
    try{const result=await call<{status:string}>(path+"/test","POST",{},requestKey());setMessage(w(result.status==="sent"?"notifyTestSent":"notifyFailed"))}catch{setMessage(w("notifyFailed"))}finally{setBusy(false)}
  }
  return <section className="help-notifications" aria-label={w("notifyTitle")}>
    <div><strong>{w("notifyTitle")}</strong><p>{w("notifyCopy")}</p></div>
    {supported()?<div className="help-notification-actions"><button className="help-btn" disabled={busy||enabled===null} onClick={()=>void toggle()}>{busy?w("busy"):w(enabled?"notifyDisable":"notifyEnable")}</button>{enabled?<><span role="status">{w("notifyOn")}</span><button className="help-resolve" disabled={busy} onClick={()=>void test()}>{w("notifyTest")}</button></>:null}</div>:<p>{w("notifyUnsupported")}</p>}
    {message?<p role="status">{message}{enabled===null?<button className="help-resolve" onClick={()=>{setMessage("");setLoadVersion(v=>v+1)}}>{w("retry")}</button>:null}</p>:null}
  </section>
}
