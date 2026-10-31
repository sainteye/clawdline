import { useEffect, useRef, useState } from "react"
import type { NativeTerminalFrame, SessionRow } from "@clawdline/contract"
import { client } from "../client.js"
import { catalogWord } from "../catalog.js"
import { pressKey } from "../press-key.js"
import { pendingSends } from "./send.js"
import { nativeTerminalCommand } from "./native-terminal-command.js"
import { FOLLOW_MS } from "./transcript-follow.js"
import "./native-terminal.css"

const word = (key: string) => catalogWord("nativeTerminal",key)
const keys = [["up","↑"],["down","↓"],["left","←"],["right","→"],["enter","Enter"],["escape","Esc"],["tab","Tab"],["shift+tab","Shift+Tab"],["backspace","⌫"],["delete","Delete"],["home","Home"],["end","End"],["pageup","PgUp"],["pagedown","PgDn"]] as const
const hardware:Record<string,string> = {ArrowUp:"up",ArrowDown:"down",ArrowLeft:"left",ArrowRight:"right",Enter:"enter",Escape:"escape",Tab:"tab",Backspace:"backspace",Delete:"delete",Home:"home",End:"end",PageUp:"pageup",PageDown:"pagedown"}

/** The existing foreground CLI; no new shell, replayed input or implicit Enter. */
export function NativeTerminalPanel({row,open,onClose}:{row:SessionRow|null;open:boolean;onClose:()=>void}) {
 const [frame,setFrame] = useState<NativeTerminalFrame|null>(null)
 const [draft,setDraft] = useState("")
 const [busy,setBusy] = useState(false)
 const [blocked,setBlocked] = useState(false)
 const [failure,setFailure] = useState("")
 const epoch = useRef(0)
 const working = useRef(false)
 const reading = useRef(false)
 const held = useRef<NativeTerminalFrame|null>(null)
 const region = useRef<HTMLPreElement>(null)
 const id=row?.id||""
 const destination = useRef(id); destination.current=id

 const read = async (acknowledge=false) => {
  if (!open || !id || reading.current || working.current) return
  const mine=epoch.current;reading.current=true
  try {
   const next=await client.nativeTerminal(id)
   if(mine!==epoch.current || destination.current!==id) return
   if(typeof next.expect!=="string" || !/^[a-f0-9]{64}$/.test(next.expect) || typeof next.text!=="string") throw Error("invalid_frame")
   held.current=next;setFrame(next)
   // A delivered slash command has a visible native continuation now; it need
   // not invent a user transcript turn. Uncertain sends retain their receipt.
   for (const card of pendingSends.of(id)) {
    if (card.state === "accepted" && nativeTerminalCommand(card.text,row?.assistant,row?.backend,card.pictures.length)) pendingSends.dismiss(card.token)
   }
   if(acknowledge){setBlocked(false);setFailure("")}
  } catch(error) {
   if(mine!==epoch.current)return
   held.current=null;setFrame(null);setBlocked(true)
   setFailure(word("unavailable")+" ("+String((error as {code?:string}).code||"unanswered")+")")
  } finally {if(mine===epoch.current)reading.current=false}
 }
 useEffect(()=>{
  epoch.current++;reading.current=false;working.current=false;held.current=null
  setFrame(null);setDraft("");setBusy(false);setBlocked(false);setFailure("")
  if(!open || !id)return
  void read()
  const timer=setInterval(()=>{if(document.visibilityState!=="hidden")void read()},FOLLOW_MS)
  region.current?.focus({preventScroll:true})
  return()=>{epoch.current++;clearInterval(timer)}
 },[open,id,row?.execution_generation])

 const input=async (fields:{key:string}|{text:string})=>{
  const before=held.current
  if(!open||!id||!before||working.current||blocked)return
  const mine=++epoch.current;reading.current=false;working.current=true;setBusy(true)
  try {
   await client.nativeTerminalInput(id,{expect:before.expect,...fields},pressKey("native-terminal"))
   if(mine!==epoch.current)return
   if("text" in fields)setDraft("")
  } catch(error) {
   if(mine!==epoch.current)return
   setBlocked(true)
   const code=String((error as {code?:string}).code||"unanswered")
   setFailure(word(code==="input_moved"?"changed":"uncertain")+" ("+code+")")
  } finally {
   if(mine===epoch.current){working.current=false;setBusy(false);void read()}
  }
 }
 const disabled=!frame||busy||blocked
 return <section className="panel-view native-terminal" id="native-terminal" hidden={!open} aria-labelledby="native-terminal-title">
  <header className="panel-view-head">
   <h2 id="native-terminal-title">{word("title")}</h2>
   <span>{row?.assistant==="codex"?"Codex":"Claude Code"}</span>
   <button type="button" className="chip" onClick={onClose}>{word("back")}</button>
  </header>
  <pre ref={region} className="native-terminal-grid" tabIndex={0} aria-label={word("screen")} onKeyDown={event=>{
   if(event.nativeEvent.isComposing||event.altKey||event.ctrlKey||event.metaKey)return
   const key=event.key==="Tab"&&event.shiftKey?"shift+tab":hardware[event.key]
   if(!key)return
   event.preventDefault();event.stopPropagation();void input({key})
  }}>{frame?.text||word("loading")}</pre>
  <div className="native-terminal-controls">
   <p>{word("hint")}</p>
   {failure&&<p role="alert">{failure}</p>}
   <button type="button" className="chip" disabled={busy||reading.current} onClick={()=>void read(true)}>{word("refresh")}</button>
   <div className="native-terminal-keys" aria-label={word("keys")}>
    {keys.map(([key,label])=><button key={key} type="button" className="chip" data-native-key={key} disabled={disabled} onClick={()=>void input({key})}>{label}</button>)}
   </div>
   <form onSubmit={event=>{event.preventDefault();if(draft)void input({text:draft})}}>
    <input aria-label={word("text")} value={draft} onChange={event=>setDraft(event.target.value)} disabled={busy||blocked} />
    <button type="submit" className="chip" disabled={disabled||!draft}>{busy?word("sending"):word("type")}</button>
   </form>
  </div>
 </section>
}
