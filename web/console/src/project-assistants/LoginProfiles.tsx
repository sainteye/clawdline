import {useEffect,useRef,useState} from "react"
import type * as W from "@clawdline/contract"
import {command,errorCode,type SourceClient} from "./client"
import {routineScopeIdentity} from "./routine-hook-intent"
import {validLoginIntent} from "./login-profile-intent"
import type {Words} from "./words"

/** Native sign-in belongs to the source. Every explicit replay keeps its exact
 * command; mounting or refreshing this component never starts a sign-in. */
export function LoginProfiles({source,bot,agent,value,onChange,onStatusChange,w}:{source?:SourceClient;bot:W.ProjectAssistantBot;agent:W.ProjectAssistantAgent;value:string;onChange:(id:string)=>void;onStatusChange?:(state:string)=>void;w:Words}){
 const [profiles,setProfiles]=useState<W.ProjectAssistantLoginProfile[]>([]),[name,setName]=useState(""),[error,setError]=useState(""),[busy,setBusy]=useState(false),[pending,setPending]=useState<W.ProjectAssistantLoginProfileAction|null>(null)
 const generation=useRef(0),scope=source?routineScopeIdentity(source,bot):"",journal="clawdline.bot-login-intent:"+scope
 const refresh=async(signal:AbortSignal)=>{if(!source?.loginProfiles)return;const n=generation.current,answer=await source.loginProfiles(bot.key,bot.conversation_id,agent,signal);if(n===generation.current&&!signal.aborted)setProfiles(answer.profiles)}
 useEffect(()=>{const c=new AbortController();generation.current++;setProfiles([]);setError("");setPending(null);try{const saved=localStorage.getItem(journal);if(saved){const request=JSON.parse(saved);if(validLoginIntent(request,bot))setPending(request);else{setError("pa_outcome_unknown");setBusy(true)}}}catch{setError("pa_outcome_unknown")};if(source?.canLoginProfiles?.())void refresh(c.signal).catch(e=>{if(!c.signal.aborted)setError(errorCode(e))});return()=>{generation.current++;c.abort()}},[source,scope,agent])
 const perform=async(request:W.ProjectAssistantLoginProfileAction)=>{
  if(!source?.loginProfileAction||busy||!validLoginIntent(request,bot))return
  const n=generation.current,c=new AbortController();setBusy(true);setError("")
  try{localStorage.setItem(journal,JSON.stringify(request));setPending(request);await source.loginProfileAction(request,c.signal);if(n!==generation.current)return;localStorage.removeItem(journal);setPending(null);await refresh(c.signal)}catch(e){if(n===generation.current)setError(errorCode(e))}finally{if(n===generation.current)setBusy(false)}
 }
 const action=async(kind:string,p?:W.ProjectAssistantLoginProfile)=>{
  if(!source)return
  const n=generation.current;setBusy(true)
  try{const current=await source.get(bot.key,new AbortController().signal);if(n!==generation.current)return;const c=current.conversation;setBusy(false);await perform({command:command(bot.key,"login-profile"),fence:{key:bot.key,project:bot.project,conversation_id:c.id,binding_id:c.binding_id,generation:c.generation},expected_version:current.bot.version,profile_id:p?.id??"",revision:p?.revision??0,agent,action:kind,name})}catch(e){setBusy(false);setError(errorCode(e))}
 }
 const selected=profiles.find(p=>p.id===value),available=source?.canLoginProfiles?.()===true
 useEffect(()=>{onStatusChange?.(selected?.state??"")},[value,selected?.state,onStatusChange])
 return <section className="pa-rules" aria-label={w.loginProfiles}>
  <h3>{w.loginProfiles}</h3><p>{w.loginHint}</p>
  {available?<>
   <label>{w.loginProfile}<select aria-label={w.loginProfile} disabled={busy||!!pending} value={value} onChange={e=>onChange(e.target.value)}><option value="">{w.loginDefault}</option>{profiles.map(p=><option key={p.id} value={p.id} disabled={p.state!=="signed_in"&&p.state!=="signed_out"}>{p.name}</option>)}{value&&!selected&&<option value={value} disabled>{w.loginMissing}</option>}</select></label>
   {selected&&<p role="status">{w.loginState}: {({signed_in:w.loginSignedIn,signed_out:w.loginSignedOut,accepted:w.loginAccepted,running:w.loginRunning,unknown:w.loginUnknown,removed:w.loginMissing} as Record<string,string>)[selected.state]??w.loginUnknown}</p>}
   <label>{w.loginName}<input aria-label={w.loginName} value={name} disabled={busy||!!pending} onChange={e=>setName(e.target.value)}/></label>
   <div className="pa-toolbar"><button type="button" disabled={busy||!!pending||!name.trim()} onClick={()=>void action("create")}>{w.loginCreate}</button><button type="button" disabled={busy} onClick={()=>void refresh(new AbortController().signal).catch(e=>setError(errorCode(e)))}>{w.refresh}</button></div>
   {selected&&<div className="pa-toolbar"><button type="button" disabled={busy||!!pending||selected.state==="running"||selected.state==="unknown"} onClick={()=>void action("start",selected)}>{w.loginStart}</button><button type="button" disabled={busy||!!pending||selected.state!=="running"} onClick={()=>void action("cancel",selected)}>{w.loginCancel}</button><button type="button" disabled={busy||!!pending} onClick={()=>void action("check",selected)}>{w.loginCheck}</button><button type="button" disabled={busy||!!pending||!name.trim()} onClick={()=>void action("rename",selected)}>{w.loginRename}</button><button type="button" disabled={busy||!!pending} onClick={()=>void action("remove",selected)}>{w.loginRemove}</button></div>}
   {pending&&<><p role="status">{w.loginReplayHint}</p><button type="button" disabled={busy} onClick={()=>void perform(pending)}>{w.loginReplay}</button></>}
  </>:<p>{w.loginUnavailable}</p>}
  {error&&<p role="alert">{w.unavailable} ({error})</p>}
 </section>
}
