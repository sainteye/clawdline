import { useState } from "react"
import { BRAND_IMAGE } from "../brand-mark.js"
import { githubAvatarSource, type Message } from "./model.js"
import humanAvatar from "./assets/support-human-v1.png"

export function ConversationAvatar({kind,avatar}:{kind:Message["kind"];avatar?:string|null}) {
  const source=kind==="operator"?humanAvatar:kind==="member"?githubAvatarSource(avatar):null
  const [failed,setFailed]=useState<string|null>(null)
  if(source&&source!==failed)return <span className={"help-avatar help-avatar-"+kind} aria-hidden="true"><img key={source} src={source} alt="" width="32" height="32" loading="lazy" decoding="async" referrerPolicy="no-referrer" crossOrigin="anonymous" onError={()=>setFailed(source)}/></span>
  return <span className={"help-avatar help-avatar-"+kind} aria-hidden="true">{kind==="agent"?<svg viewBox="0 0 208 143" focusable="false"><image href={BRAND_IMAGE} width="208" height="143" /></svg>:<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" focusable="false"><circle cx="12" cy="8" r="3.5"/><path d="M5 21v-2a7 7 0 0 1 14 0v2"/>{kind==="operator"?<path d="M4 10V8a8 8 0 0 1 16 0v2m-16 0v4m16-4v4"/>:null}</svg>}</span>
}
