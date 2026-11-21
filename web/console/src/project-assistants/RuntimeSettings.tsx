import {useEffect,useMemo,useRef,useState} from "react"
import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
import {errorCode} from "./client"
import type {Words} from "./words"
import {botFailureText,providerSetupFailure} from "./source-errors"

export function useModelCatalog(source:SourceClient|undefined,bot:W.ProjectAssistantBot,agent:W.ProjectAssistantAgent,model:string,loginProfile:string,loginState:string,active:boolean){
  const {machine_id,bot_id}=bot.key,conversation=bot.conversation_id
  const [revision,setRevision]=useState(0)
  const request=useMemo(()=>({source,machine_id,bot_id,conversation,agent,model,loginProfile,loginState,active,revision}),
    [source,machine_id,bot_id,conversation,agent,model,loginProfile,loginState,active,revision])
  const [result,setResult]=useState<{request:typeof request;catalog?:W.ProjectAssistantConfigurationCatalog;error?:string;field?:string}>()
  const previous=useRef<Promise<void>>(Promise.resolve())
  const controller=useRef<AbortController|null>(null)
  const unavailable=!active?"":!source?.configuration?"pa_unsupported":
    loginProfile&&loginState!=="signed_in"?"pa_auth_rejected":""
  useEffect(()=>()=>controller.current?.abort(),[])
  useEffect(()=>{
    if(!active||unavailable||!source?.configuration)return
    let current=true
    // Wait for the previous source reply, which includes probe cleanup.
    // Cancelling it on a draft change could race the source's single discovery slot.
    previous.current=previous.current.then(async()=>{
      if(!current)return
      const c=new AbortController()
      controller.current=c
      try{
        const answer=await source.configuration!({machine_id,bot_id},conversation,agent,model,c.signal,source.canLoginProfiles?.()?loginProfile:undefined)
        if(current&&!c.signal.aborted)setResult(answer.catalog.agent===agent?{request,catalog:answer.catalog}:{request,error:"pa_scope_denied"})
      }catch(e){
        const field=(e as Partial<W.ProjectAssistantErrorResponse>|null)?.error?.field
        if(current&&!c.signal.aborted)setResult({request,error:errorCode(e),field})
      }finally{
        if(controller.current===c)controller.current=null
      }
    })
    return()=>{current=false}
  },[request,source,active,unavailable,machine_id,bot_id,conversation,agent,model,loginProfile])
  const visible=result?.request===request?result:undefined
  return {
    catalog:visible?.catalog,
    error:unavailable||visible?.error||"",
    field:visible?.field,
    pending:active&&!unavailable&&!visible,
    retry:()=>setRevision(n=>n+1),
  }
}

export function configurationError(code:string,w:Words,field?:string):string{
  const setup=providerSetupFailure(field,w)
  if(setup)return setup
  if(code==="pa_capacity_full")return field&&field!=="configuration_discovery"?w.failFull:w.nativeBusy
  if(code==="pa_auth_rejected")return w.loginUnavailable
  if(code==="pa_unsupported"||code==="pa_invalid")return w.nativeUnavailable
  return botFailureText(code,w)
}
