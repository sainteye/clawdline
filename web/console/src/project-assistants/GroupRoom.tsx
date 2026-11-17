import { useEffect, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import { authorized, command, errorCode, type SourceClient } from "./client"
import { canWrite } from "./view-state"
import { GroupEditor, TeamApprovals, groupWord } from "./Groups"
import {PaMenu} from "./PaSelect"
import {composerEnter} from "./composer-keyboard"
import {BotAvatar} from "./BotAvatar"
import {words} from "./words"
import {currentCatalogTag} from "../catalog"
import { EventRefreshMs } from "./Chat"

const identity=(k:W.ProjectAssistantBotKey)=>JSON.stringify([k.machine_id,k.bot_id])
export function GroupRoom({source,current,group:initial,bots,projectNames,onClose,onChanged}:{source:SourceClient;current:()=>readonly SourceClient[];group:W.ProjectAssistantGroup;bots:readonly W.ProjectAssistantBot[];projectNames:ReadonlyMap<string,string>;onClose:()=>void;onChanged:(group:W.ProjectAssistantGroup)=>void}) {
  const [group,setGroup]=useState(initial),[page,setPage]=useState<W.ProjectAssistantGroupHistoryResponse|null>(null),[lines,setLines]=useState<W.ProjectAssistantRoomEntry[]>([]),[text,setText]=useState(""),[reply,setReply]=useState<W.ProjectAssistantBotKey|undefined>(),[error,setError]=useState(""),[busy,setBusy]=useState(false),[unknown,setUnknown]=useState(false),[editing,setEditing]=useState(false),[deleting,setDeleting]=useState(false)
  const [membersOpen,setMembersOpen]=useState(false)
  const composing=useRef(false),composer=useRef<HTMLTextAreaElement>(null),memberToggle=useRef<HTMLButtonElement>(null),membersPanel=useRef<HTMLElement>(null),w=words(currentCatalogTag())
  const botFor=(key?:W.ProjectAssistantBotKey)=>key?bots.find(b=>identity(b.key)===identity(key)):undefined
  const target=(key?:W.ProjectAssistantBotKey)=>{setReply(key);pending.current=null;requestAnimationFrame(()=>composer.current?.focus())}
  useEffect(()=>{
    if(!membersOpen)return
    membersPanel.current?.querySelector<HTMLElement>("button")?.focus({preventScroll:true})
    const escape=(event:KeyboardEvent)=>{if(event.key==="Escape"&&!event.isComposing&&!event.defaultPrevented&&event.target===document.body){setMembersOpen(false);requestAnimationFrame(()=>memberToggle.current?.focus())}}
    document.addEventListener("keydown",escape)
    return()=>document.removeEventListener("keydown",escape)
  },[membersOpen])
  const pending=useRef<W.ProjectAssistantGroupSendRequest|null>(null),control=useRef(new AbortController()),refresh=useRef<()=>Promise<void>>(async()=>{})
  const currentRef=useRef(current);currentRef.current=current
  const scroll=useRef<HTMLDivElement>(null),[more,setMore]=useState<{before:number;hasMore:boolean}|null>(null)
  const linesRef=useRef(lines),moreRef=useRef(more)
  const rememberMore=(value:{before:number;hasMore:boolean})=>{moreRef.current=value;setMore(value)}
  const merge=(previous:W.ProjectAssistantRoomEntry[],incoming:W.ProjectAssistantRoomEntry[])=>[...new Map([...previous,...incoming].map(l=>[l.seq,l])).values()].sort((a,b)=>a.seq-b.seq)
  async function load(before=0){
    try {
      const answer=await authorized(source,currentRef.current,signal=>source.groupHistory!({key:initial.key,before_seq:before,page_size:100},signal),control.current.signal)
      if(control.current.signal.aborted)return
      const previous=linesRef.current,last=previous.at(-1)?.seq??0
      const gap=!before&&previous.length>0&&answer.lines.length>0&&answer.lines[0].seq>last+1
      const next=gap?answer.lines:merge(previous,answer.lines);linesRef.current=next
      setPage(answer);setGroup(old=>answer.group.version>=old.version?answer.group:old);setLines(next);if(before||!moreRef.current||gap)rememberMore({before:answer.next_before_seq,hasMore:answer.has_more});setError("")
      if(answer.group.deleted)onChanged(answer.group)
    }catch(e){if(!control.current.signal.aborted){const code=errorCode(e);setError(code);setPage(null);if(["pa_revoked","pa_scope_denied","pa_unpaired"].includes(code)){linesRef.current=[];setLines([])}}}
  }
  refresh.current=()=>load()
  useEffect(()=>{
    control.current=new AbortController();void refresh.current();const timer=setInterval(()=>void refresh.current(),EventRefreshMs)
    return()=>{control.current.abort();clearInterval(timer)}
  },[source,initial.key.machine_id,initial.key.bot_id])
  const writable=!!page&&canWrite(page.source)&&source.current?.()!==false&&!group.deleted
  const author=(key?:W.ProjectAssistantBotKey)=>key?(bots.find(b=>identity(b.key)===identity(key))?.name??key.bot_id):groupWord("groupPerson")
  async function effect(run:(signal:AbortSignal)=>Promise<unknown>){
    if(!writable||busy||unknown)return
    setBusy(true);setError("")
    try {await authorized(source,currentRef.current,run,control.current.signal);await load()}
    catch(e){const code=errorCode(e);setError(code);if(code==="pa_outcome_unknown")setUnknown(true)}finally{setBusy(false)}
  }
  async function send(){
    if(!text.trim()||!writable||busy||unknown)return
    const request=pending.current??{command:command(group.key,"send"),text,...(reply?{reply_to_author:reply}:{})}
    pending.current=request
    await effect(async signal=>{const answer=await source.sendGroup!(request,signal);pending.current=null;setText("");setReply(undefined);const next=merge(linesRef.current,[answer.entry]);linesRef.current=next;setLines(next);requestAnimationFrame(()=>scroll.current?.scrollTo({top:scroll.current.scrollHeight}));return answer})
  }
  return <section className={`pa-form-panel pa-room-layout${membersOpen?" pa-members-open":""}`} role="dialog" aria-modal="true" aria-label={group.name} style={{maxWidth:"100%",padding:0,zIndex:8}} onKeyDown={event=>{if(membersOpen&&!editing&&event.key==="Escape"&&!event.nativeEvent.isComposing&&!event.defaultPrevented){event.stopPropagation();setMembersOpen(false);requestAnimationFrame(()=>memberToggle.current?.focus())}}}>
    <div className="pa-chat"><header className="pa-chat-header"><button type="button" onClick={onClose}>{groupWord("groupBack")}</button><div><h2>{group.name}</h2><small>{group.members.map(m=>author(m.key)).join(" · ")}</small></div><button ref={memberToggle} type="button" aria-expanded={membersOpen} onClick={()=>setMembersOpen(open=>!open)}>{groupWord("groupMembers")} ({group.members.length})</button><PaMenu label={w.moreActions} items={[
      {id:"edit",label:groupWord("groupEdit"),disabled:!writable||busy||unknown,onSelect:()=>setEditing(true)},
      {id:"stop",label:groupWord("groupStop"),disabled:!writable||busy||unknown,onSelect:()=>void effect(signal=>source.stopGroup!({command:command(group.key,"stop")},signal))},
      {id:"delete",label:groupWord("groupDelete"),disabled:!writable||busy||unknown,onSelect:()=>setDeleting(true)}
    ]}/></header>

    <div className="pa-chat-scroll" ref={scroll}><div className="pa-chat-content">
      <TeamApprovals sources={[source]} bots={page?.room.running?bots.filter(bot=>group.members.some(m=>identity(m.key)===identity(bot.key))):[]}/>
      {more?.hasMore&&<button type="button" disabled={busy} onClick={()=>void load(more.before)}>{groupWord("groupOlder")}</button>}
      {lines.map(line=>{const member=botFor(line.author),person=line.kind==="person";return <article key={line.seq} className={`pa-room-message${person?" pa-room-person":""}`} data-room-seq={line.seq}>
        {!person&&<div className="pa-room-author">{member&&<BotAvatar appearance={member.appearance} size={28}/>}<strong style={member?{color:member.appearance.color}:undefined}>{author(line.author)}</strong></div>}
        <div className="pa-room-bubble">{line.reply_to_author&&<div className="pa-room-context">↳ {groupWord("groupReplyTo")} {author(line.reply_to_author)}</div>}<p>{line.text}</p></div>
        {line.kind==="member"&&line.author&&<button className="pa-room-reply" type="button" disabled={!writable||busy||unknown} onClick={()=>target(line.author)}>{groupWord("groupReply")} {author(line.author)}</button>}
      </article>})}
      {!lines.length&&<p>{groupWord("groupEmpty")}</p>}
    </div></div>
    <footer className="pa-chat-footer">
      {!!page?.room.running&&<p role="status">{groupWord("groupRunning")}</p>}
      <form className="pa-composer pa-room-composer" onSubmit={event=>{event.preventDefault();if(!composing.current)void send()}}>
        <div className="pa-room-target"><select aria-label={w.roomTarget} disabled={!writable||busy||unknown} value={reply?identity(reply):""} onChange={e=>target(group.members.find(member=>identity(member.key)===e.target.value)?.key)}><option value="">@{w.roomEveryone}</option>{group.members.map(member=><option key={identity(member.key)} value={identity(member.key)}>@{author(member.key)}</option>)}</select></div>
        <div className="pa-editor"><div className="pa-editor-mirror" aria-hidden="true">{text+"​"}</div><textarea ref={composer} rows={1} aria-label={groupWord("groupMessage")} placeholder={groupWord("groupMessage")} value={text} disabled={!writable||busy||unknown}
          onCompositionStart={()=>{composing.current=true}} onCompositionEnd={()=>{composing.current=false}} onChange={event=>{setText(event.target.value);pending.current=null}} onKeyDown={event=>{if(composerEnter(event.nativeEvent,composing.current)==="send"){event.preventDefault();void send()}}}/></div>
        <button className="pa-send" type="submit" aria-label={groupWord(busy?"groupWorking":"groupSend")} disabled={!writable||busy||unknown||!text.trim()}>↑</button>
      </form>
      {deleting&&<div role="alert"><p>{groupWord("groupDeleteHint")}</p><button type="button" disabled={busy||unknown} onClick={()=>setDeleting(false)}>{groupWord("groupCancel")}</button><button type="button" disabled={busy||unknown} onClick={()=>void effect(async signal=>{const result=await source.deleteGroup!({command:command(group.key,"delete"),expected_version:group.version},signal);onChanged({...group,deleted:true});return result})}>{groupWord("groupDeleteConfirm")}</button></div>}
      {(error||unknown)&&<p role="alert">{groupWord(unknown?"groupUnknown":"groupUnavailable")} {error&&`(${error})`}</p>}
    </footer>
    </div><aside ref={membersPanel} hidden={!membersOpen} tabIndex={-1} className="pa-room-members" aria-label={groupWord("groupMembers")}><header className="pa-toolbar"><h3>{groupWord("groupMembers")}</h3><button type="button" aria-label={w.closeInformation} onClick={()=>{setMembersOpen(false);requestAnimationFrame(()=>memberToggle.current?.focus())}}>×</button></header><h2>{group.name}</h2><p>{group.members.length} {groupWord("groupMembers")}</p>{group.members.map(member=>{const bot=botFor(member.key);return <button className="pa-room-member" key={identity(member.key)} type="button" disabled={!writable||busy||unknown} onClick={()=>{target(member.key);if(matchMedia("(max-width: 700px)").matches)setMembersOpen(false)}}>{bot&&<BotAvatar appearance={bot.appearance} size={36}/>}<span><strong>{author(member.key)}</strong><small>{w.roomTarget}</small></span></button>})}<button type="button" disabled={!writable||busy||unknown} onClick={()=>setEditing(true)}>{groupWord("groupEdit")}</button></aside>
    {editing&&page&&<GroupEditor sources={[{source,groups:[group],status:page.source}]} bots={bots} current={current} projectNames={projectNames} group={group} onClose={()=>setEditing(false)} onSaved={(_,changed)=>{setGroup(changed);setEditing(false);onChanged(changed);void load()}}/>}
  </section>
}
