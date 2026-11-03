import { useCallback, useEffect, useRef, useState } from "react"
import { requestKey, SupportFailure, SUPPORT_UI_LIMITS } from "../support/api.js"
export type CommunityCall = <T>(path:string,method?:string,body?:unknown,key?:string)=>Promise<T>
export function useCommunityRead<T>(path:string|null,call:CommunityCall) {
  const [state,setState]=useState<{path:string|null;data:T|null;error:string|null;loading:boolean}>({path:null,data:null,error:null,loading:false})
  const [revision,setRevision]=useState(0)
  const refresh=useCallback(()=>setRevision(n=>n+1),[])
  useEffect(()=>{
    if(!path)return
    let active=true
    setState(s=>s.path===path?{...s,loading:true,error:null}:{path,data:null,error:null,loading:true})
    void call<T>(path).then(data=>{if(active)setState({path,data,error:null,loading:false})},e=>{if(active)setState(s=>({path,data:s.path===path?s.data:null,error:e instanceof SupportFailure?e.code:"community_unavailable",loading:false}))})
    return()=>{active=false}
  },[path,call,revision])
  useEffect(()=>{
    const update=()=>{if(!document.hidden)refresh()}
    const timer=setInterval(update,SUPPORT_UI_LIMITS.refreshMs)
    window.addEventListener("focus",update)
    return()=>{clearInterval(timer);window.removeEventListener("focus",update)}
  },[path,refresh])
  return {data:state.path===path?state.data:null,error:state.path===path?state.error:null,loading:!!path&&(state.path!==path||state.loading),refresh,
    replace:(data:T)=>setState(s=>s.path===path?{...s,data,error:null,loading:false}:s)}
}
type Pending={path:string;body:unknown;key:string}
export function useCommunityMutation(call:CommunityCall,scope:string) {
  const storageKey="clawdline-community-pending:"+scope
  const [pending,setPending]=useState<Pending|null>(()=>{try{const p=JSON.parse(sessionStorage.getItem(storageKey)??"null");return p&&typeof p.path==="string"&&/^\/(su\/)?v1\/community\/topics(?:\/|$)/.test(p.path)&&typeof p.key==="string"?p:null}catch{return null}})
  const [busy,setBusy]=useState(false),[error,setError]=useState<string|null>(pending?"community_outcome_unknown":null)
  const lock=useRef(false),mounted=useRef(true)
  useEffect(()=>{mounted.current=true;return()=>{mounted.current=false}},[])
  useEffect(()=>{
    try{const p=JSON.parse(sessionStorage.getItem(storageKey)??"null");const value=p&&typeof p.path==="string"&&/^\/(su\/)?v1\/community\/topics(?:\/|$)/.test(p.path)&&typeof p.key==="string"?p:null;setPending(value);setError(value?"community_outcome_unknown":null)}catch{setPending(null)}
  },[storageKey])
  const save=(p:Pending|null)=>{setPending(p);try{if(p)sessionStorage.setItem(storageKey,JSON.stringify(p));else sessionStorage.removeItem(storageKey)}catch{}}
  const run=async<T,>(path:string,body:unknown,done:(value:T)=>void)=>{
    if(lock.current)return
    const input=pending??{path,body,key:requestKey()}
    lock.current=true;save(input);setBusy(true);setError(null)
    try{
      const value=await call<T>(input.path,"POST",input.body,input.key)
      // Clear a committed request even when navigation unmounted its form.
      try{sessionStorage.removeItem(storageKey)}catch{}
      if(mounted.current){setPending(null);done(value)}
    }catch(e){
      if(mounted.current){if(e instanceof SupportFailure&&e.definite)save(null);setError(e instanceof SupportFailure&&e.definite?e.code:"community_outcome_unknown")}
    }finally{lock.current=false;if(mounted.current)setBusy(false)}
  }
  return{run,busy,error,uncertain:!!pending,locked:busy||!!pending}
}
