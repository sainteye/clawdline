import {useEffect,useRef,useState} from "react"
import type * as W from "@clawdline/contract"
import {command,errorCode,provablyNotSent,type SourceClient} from "./client"
import {routineScopeIdentity} from "./routine-hook-intent"
import {validLoginIntent} from "./login-profile-intent"
import type {Words} from "./words"

/** Native sign-in belongs to the source. Every explicit replay keeps its exact
 * command; mounting or refreshing this component never starts a sign-in. */
export function LoginProfiles({source,bot,agent,value,onChange,onStatusChange,w}:{source?:SourceClient;bot:W.ProjectAssistantBot;agent:W.ProjectAssistantAgent;value:string;onChange:(id:string)=>void;onStatusChange?:(state:string)=>void;w:Words}){
 const [profiles,setProfiles]=useState<W.ProjectAssistantLoginProfile[]>([]),[name,setName]=useState(""),[error,setError]=useState(""),[busy,setBusy]=useState(false),[pending,setPending]=useState<W.ProjectAssistantLoginProfileAction|null>(null)
 const [handoff,setHandoff]=useState<W.ProjectAssistantLoginProfile|null>(null),secretField=useRef<HTMLTextAreaElement>(null)
 const generation=useRef(0),scope=source?routineScopeIdentity(source,bot):"",journal="clawdline.bot-login-intent:"+scope
 const refresh=async(signal:AbortSignal)=>{if(!source?.loginProfiles)return;const n=generation.current,answer=await source.loginProfiles(bot.key,bot.conversation_id,agent,signal);if(n===generation.current&&!signal.aborted)setProfiles(answer.profiles)}
 useEffect(()=>{const c=new AbortController();generation.current++;setProfiles([]);setHandoff(null);if(secretField.current)secretField.current.value="";setError("");setPending(null);try{const saved=localStorage.getItem(journal);if(saved){const request=JSON.parse(saved);if(validLoginIntent(request,bot))setPending(request);else{setError("pa_outcome_unknown");setBusy(true)}}}catch{setError("pa_outcome_unknown")};if(source?.canLoginProfiles?.())void refresh(c.signal).catch(e=>{if(!c.signal.aborted)setError(errorCode(e))});return()=>{generation.current++;c.abort();if(secretField.current)secretField.current.value=""}},[source,scope,agent])
 const perform=async(request:W.ProjectAssistantLoginProfileAction,secret="")=>{
  if(!source?.loginProfileAction||busy||!validLoginIntent(request,bot))return
  const n=generation.current,c=new AbortController();setBusy(true);setError("")
  try{localStorage.setItem(journal,JSON.stringify(request));setPending(request);const answer=request.action==="submit-key"?await source.loginSecret!(request,secret,c.signal):await source.loginProfileAction(request,c.signal);secret="";if(n!==generation.current)return;localStorage.removeItem(journal);setPending(null);if(answer.profile.handoff)setHandoff(answer.profile);else if(request.action==="submit-key")setHandoff(null);await refresh(c.signal)}catch(e){if(n===generation.current){setError(errorCode(e));const refusal=e as Partial<W.ProjectAssistantErrorResponse>;if(provablyNotSent(e)||refusal.error?.code==="pa_expired"&&refusal.error.field==="login_secret_target"||refusal.error?.code==="pa_scope_denied"&&refusal.error.field==="login_profile_in_use"){localStorage.removeItem(journal);setPending(null);setHandoff(null)}}}finally{secret="";if(n===generation.current)setBusy(false)}
 }
 const action=async(kind:string,p?:W.ProjectAssistantLoginProfile,keyProfile=false)=>{
  if(!source)return
  const n=generation.current;setBusy(true)
  let secret="";if(kind==="submit-key"){secret=secretField.current?.value??"";if(secretField.current)secretField.current.value=""}
  try{const current=await source.get(bot.key,new AbortController().signal);if(n!==generation.current)return;const c=current.conversation;setBusy(false);await perform({command:command(bot.key,"login-profile"),fence:{key:bot.key,project:bot.project,conversation_id:c.id,binding_id:c.binding_id,generation:c.generation},expected_version:current.bot.version,profile_id:p?.id??"",revision:p?.revision??0,agent,action:kind,name,...(keyProfile?{kind:"api_key"}:{}),...(kind==="submit-key"?{target:p?.handoff?.id}: {})},secret)}catch(e){setBusy(false);setError(errorCode(e))}finally{secret=""}
 }
 const selected=profiles.find(p=>p.id===value),available=source?.canLoginProfiles?.()===true
 useEffect(()=>{onStatusChange?.(selected?.state??"")},[value,selected?.state,onStatusChange])
 return <section className="pa-rules" aria-label={w.loginProfiles}>
  <h3>{w.loginProfiles}</h3><p>{w.loginHint}</p>
  {available?<>
   <label>{w.loginProfile}<select aria-label={w.loginProfile} disabled={busy||!!pending} value={value} onChange={e=>onChange(e.target.value)}><option value="">{w.loginDefault}</option>{profiles.map(p=><option key={p.id} value={p.id} disabled={p.state!=="signed_in"&&p.state!=="signed_out"}>{p.name}</option>)}{value&&!selected&&<option value={value} disabled>{w.loginMissing}</option>}</select></label>
   {selected&&<p role="status">{w.loginState}: {({signed_in:selected.kind==="api_key"?w.loginKeyStored:w.loginSignedIn,signed_out:w.loginSignedOut,accepted:w.loginAccepted,running:w.loginRunning,unknown:w.loginUnknown,removed:w.loginMissing} as Record<string,string>)[selected.state]??w.loginUnknown}</p>}
   <label>{w.loginName}<input aria-label={w.loginName} value={name} disabled={busy||!!pending} onChange={e=>setName(e.target.value)}/></label>
   <div className="pa-toolbar" style={{flexWrap:"wrap",justifyContent:"flex-start"}}><button type="button" disabled={busy||!!pending||!name.trim()} onClick={()=>void action("create")}>{w.loginCreate}</button>{source?.canKeyProfiles?.()&&<button type="button" disabled={busy||!!pending||!name.trim()} onClick={()=>void action("create",undefined,true)}>{w.loginCreateKey}</button>}<button type="button" disabled={busy} onClick={()=>void refresh(new AbortController().signal).catch(e=>setError(errorCode(e)))}>{w.refresh}</button></div>
   {selected&&<div className="pa-toolbar" style={{flexWrap:"wrap",justifyContent:"flex-start"}}>{selected.kind==="api_key"?<button type="button" disabled={busy||!!pending||selected.state==="unknown"} onClick={()=>void action("prepare-key",selected)}>{w.loginPrepareKey}</button>:<><button type="button" disabled={busy||!!pending||selected.state==="running"||selected.state==="unknown"} onClick={()=>void action("start",selected)}>{w.loginStart}</button><button type="button" disabled={busy||!!pending||selected.state!=="running"} onClick={()=>void action("cancel",selected)}>{w.loginCancel}</button></>}<button type="button" disabled={busy||!!pending} onClick={()=>void action("check",selected)}>{w.loginCheck}</button><button type="button" disabled={busy||!!pending||!name.trim()} onClick={()=>void action("rename",selected)}>{w.loginRename}</button><button type="button" disabled={busy||!!pending} onClick={()=>void action("remove",selected)}>{w.loginRemove}</button></div>}
   {handoff?.id===value&&handoff.handoff&&<form className="secret-handoff-form" onSubmit={e=>{e.preventDefault();void action("submit-key",handoff)}}><label htmlFor="pa-login-secret">{handoff.variables?.join(", ")}<textarea id="pa-login-secret" aria-label={w.loginKeyValue} ref={secretField} autoComplete="off" spellCheck={false} required disabled={busy||!!pending}/></label><p>{w.loginKeyHint}</p><button type="submit" disabled={busy||!!pending}>{w.loginSubmitKey}</button></form>}
   {pending&&<><p role="status">{w.loginReplayHint}</p><button type="button" disabled={busy} onClick={()=>void perform(pending)}>{w.loginReplay}</button></>}
  </>:<p>{w.loginUnavailable}</p>}
  {error&&<p role="alert">{w.unavailable} ({error})</p>}
 </section>
}
