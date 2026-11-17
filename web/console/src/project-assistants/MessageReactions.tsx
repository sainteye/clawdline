import {useEffect,useRef,useState} from "react"
import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
import {command,errorCode,provablyNotSent} from "./client"
import {sameConversation} from "./view-state"
import {catalogWord} from "../catalog"
import "./message-reactions.css"
import {BotAvatar} from "./BotAvatar"

const tr=(key:string)=>catalogWord("projectAssistants",key)
const refused=new Set(["pa_invalid","pa_scope_denied","pa_stale_version","pa_stale_generation","pa_read_only","pa_disabled","pa_unpaired","pa_unsupported","pa_not_found","pa_idempotency_conflict","pa_capacity_full"])
const palette=["👍","❤️","😂","🎉","👀","✅"]

/** Durable desired state, with explicit same-ID recovery after an unknown reply. */
export function MessageReactions({message,source}:{message:W.ProjectAssistantMessage;source?:SourceClient}) {
 const [open,setOpen]=useState(false),[busy,setBusy]=useState(false),[held,setHeld]=useState<W.ProjectAssistantReactRequest|null>(null),[error,setError]=useState(""),[blocked,setBlocked]=useState(false)
 const control=useRef<AbortController|null>(null),epoch=useRef(0),inflight=useRef(false)
 const scope=JSON.stringify([source?.intentAccount??"local",source?.machineID,source?.authorizationRevision,message.fence.key,message.fence.project,message.fence.conversation_id,message.id])
 const storageKey="clawdline.bot-reaction."+JSON.stringify([source?.intentAccount??"local",message.fence.key,message.fence.project,message.fence.conversation_id,message.id])
 useEffect(()=>{
  const current=++epoch.current;inflight.current=false;setHeld(null);setBusy(false);setError("");setBlocked(false);setOpen(false)
  try{const raw=localStorage.getItem(storageKey);if(raw){const q=JSON.parse(raw) as W.ProjectAssistantReactRequest
   if(q.command?.operation!=="react"||q.command.key?.machine_id!==message.fence.key.machine_id||q.command.key?.bot_id!==message.fence.key.bot_id||typeof q.command.idempotency_key!=="string"||!q.command.idempotency_key||q.message_id!==message.id||!sameConversation(q.fence,message.fence)||typeof q.present!=="boolean"||typeof q.emoji!=="string"||!Number.isSafeInteger(q.expected_revision)||q.expected_revision<0)throw Error("intent")
   setHeld(q);setError("pa_outcome_unknown")
  }}catch{setBlocked(true);setError("pa_outcome_unknown")}
  return()=>{if(epoch.current===current)epoch.current++;control.current?.abort()}
 },[scope,storageKey])
 const own=message.reactions?.find(a=>a.own),enabled=source?.canReactions?.()===true&&source.current?.()!==false
 async function send(emoji?:string){
  if(!source?.react||!enabled||inflight.current||busy||blocked)return
  inflight.current=true
  const current=epoch.current,c=new AbortController();control.current=c;setBusy(true);setError("")
  let q=held,sent=false
  try{
   if(!q){
    // Another mounted view may already own an unresolved request. Do not replace it.
    const existing=localStorage.getItem(storageKey)
    if(existing){let saved:W.ProjectAssistantReactRequest;try{saved=JSON.parse(existing) as W.ProjectAssistantReactRequest}catch{setBlocked(true);setError("pa_outcome_unknown");return};if(saved.command?.operation!=="react"||saved.command.key?.machine_id!==message.fence.key.machine_id||saved.command.key?.bot_id!==message.fence.key.bot_id||typeof saved.command.idempotency_key!=="string"||!saved.command.idempotency_key||saved.message_id!==message.id||!sameConversation(saved.fence,message.fence)||typeof saved.present!=="boolean"||typeof saved.emoji!=="string"||!Number.isSafeInteger(saved.expected_revision)||saved.expected_revision<0){setBlocked(true);setError("pa_outcome_unknown");return};setHeld(saved);setError("pa_outcome_unknown");return}
    if(!emoji)return;q={command:command(message.fence.key,"react"),fence:message.fence,message_id:message.id,emoji,present:!own?.emojis.includes(emoji),expected_revision:own?.revision??0};localStorage.setItem(storageKey,JSON.stringify(q));setHeld(q)}
   sent=true;const r=await source.react(q,c.signal)
   if(r.contract_version!=="project-assistants.v1"||r.receipt.command.idempotency_key!==q.command.idempotency_key||r.receipt.command.operation!=="react"||r.receipt.command.key.machine_id!==q.command.key.machine_id||r.receipt.command.key.bot_id!==q.command.key.bot_id||r.receipt.resource_id!==message.id||r.receipt.state!=="delivered")throw {error:{code:"pa_outcome_unknown"}}
   if(JSON.parse(localStorage.getItem(storageKey)??"null")?.command?.idempotency_key===q.command.idempotency_key)localStorage.removeItem(storageKey)
   if(epoch.current===current&&!c.signal.aborted){setHeld(null);setOpen(false)}
  }catch(e){const code=errorCode(e);if(!sent||provablyNotSent(e)||refused.has(code)&&source.canReactions?.()===true){try{if(q&&JSON.parse(localStorage.getItem(storageKey)??"null")?.command?.idempotency_key===q.command.idempotency_key)localStorage.removeItem(storageKey)}catch{};if(epoch.current===current)setHeld(null)}if(epoch.current===current)setError(code)}
  finally{if(epoch.current===current){inflight.current=false;setBusy(false)}}
 }
 if(message.kind!=="user"&&message.kind!=="assistant")return null
 return <div className="pa-reactions" aria-label={tr("reactions")}>
  {message.reactions?.flatMap(a=>a.emojis.map(emoji=>a.own?<button type="button" className="pa-reaction pa-reaction-own" key={a.id+emoji} aria-label={tr("removeReaction")+" "+emoji} title={tr("yourReaction")} disabled={!enabled||busy||!!held||blocked} onClick={()=>void send(emoji)}>{emoji}<span>{tr("youReaction")}</span></button>:<span className="pa-reaction" key={a.id+emoji} title={a.kind==="bot"?a.name:tr("otherReaction")}>{a.kind==="bot"&&a.appearance&&<BotAvatar appearance={a.appearance} size={18}/>} {emoji}<span>{a.kind==="bot"?a.name:tr("otherReaction")}</span></span>))}
  {source?.react&&<button className="pa-reaction-add" type="button" aria-expanded={open} title={enabled?tr("addReaction"):tr("reactionsUnavailable")} disabled={!enabled||busy||!!held||blocked} onClick={()=>setOpen(v=>!v)}>{tr("addReaction")}</button>}
  {open&&<div className="pa-reaction-palette" role="group" aria-label={tr("chooseReaction")} onKeyDown={e=>{if(e.key==="Escape"){e.stopPropagation();setOpen(false)}}}>{palette.map(emoji=><button type="button" key={emoji} aria-label={tr("reactWith")+" "+emoji} aria-pressed={own?.emojis.includes(emoji)??false} disabled={busy||!!held||blocked} onClick={()=>void send(emoji)}>{emoji}</button>)}</div>}
  {error&&<p role="status">{held||blocked?tr("reactionUnknown"):tr("reactionFailed")} ({error}) {held&&<button type="button" disabled={!enabled||busy} onClick={()=>void send()}>{tr("retryReaction")}</button>}</p>}
 </div>
}
