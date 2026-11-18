import { useEffect, useId, useLayoutEffect, useRef, useState } from "react"
import { createPortal } from "react-dom"
import type * as W from "@clawdline/contract"
import { authorized, command, errorCode, type SourceClient } from "./client"
import { canRead, canWrite } from "./view-state"
import { GroupEditor, TeamApprovals, groupWord } from "./Groups"
import { PaMenu } from "./PaSelect"
import { composerEnter } from "./composer-keyboard"
import { BotAvatar } from "./BotAvatar"
import { words } from "./words"
import { currentCatalogTag } from "../catalog"
import { EventRefreshMs } from "./Chat"
import { followSnapshotRoster, serialSnapshotRefresh } from "./snapshot-roster"
import { groupMentionOptions, withGroupMention } from "./group-mentions"

const identity=(k:W.ProjectAssistantBotKey)=>JSON.stringify([k.machine_id,k.bot_id])
const merge=(previous:W.ProjectAssistantRoomEntry[],incoming:W.ProjectAssistantRoomEntry[])=>[...new Map([...previous,...incoming].map(line=>[line.seq,line])).values()].sort((a,b)=>a.seq-b.seq)
type Position={top:number;following:boolean;seq?:string;offset?:number}

export function GroupRoom({source,current,group:initial,bots,projectNames,onClose,onChanged,active=true,onOpenMember}:{source:SourceClient;current:()=>readonly SourceClient[];group:W.ProjectAssistantGroup;bots:readonly W.ProjectAssistantBot[];projectNames:ReadonlyMap<string,string>;onClose:()=>void;onChanged:(group:W.ProjectAssistantGroup)=>void;active?:boolean;onOpenMember?:(key:W.ProjectAssistantBotKey)=>void}) {
  const [group,setGroup]=useState(initial),[page,setPage]=useState<W.ProjectAssistantGroupHistoryResponse|null>(null),[lines,setLines]=useState<W.ProjectAssistantRoomEntry[]>([]),[text,setText]=useState(""),[quote,setQuote]=useState<W.ProjectAssistantRoomEntry|null>(null),[error,setError]=useState(""),[busy,setBusy]=useState(false),[unknown,setUnknown]=useState(false),[editing,setEditing]=useState(false),[deleting,setDeleting]=useState(false)
  const [membersOpen,setMembersOpen]=useState(false),[paging,setPaging]=useState(false),[following,setFollowing]=useState(true),[host,setHost]=useState<HTMLElement|null>(null)
  const [caret,setCaret]=useState(0),[highlight,setHighlight]=useState(0),[dismissed,setDismissed]=useState<string|null>(null)
  const anchor=useRef<HTMLSpanElement>(null),room=useRef<HTMLElement>(null),composer=useRef<HTMLTextAreaElement>(null),memberToggle=useRef<HTMLButtonElement>(null),membersPanel=useRef<HTMLElement>(null),lastFocus=useRef<HTMLElement|null>(null),composing=useRef(false),w=words(currentCatalogTag()),mentionID=useId()
  const pending=useRef<W.ProjectAssistantGroupSendRequest|null>(null),control=useRef(new AbortController()),refresh=useRef<(earlier?:boolean)=>Promise<void>>(async()=>{})
  const observing=useRef(false)
  const currentRef=useRef(current),changedRef=useRef(onChanged),activeRef=useRef(active)
  currentRef.current=current;changedRef.current=onChanged;activeRef.current=active
  const scroll=useRef<HTMLDivElement>(null),[more,setMore]=useState<{before:number;hasMore:boolean}|null>(null)
  const linesRef=useRef(lines),moreRef=useRef(more),position=useRef<Position>({top:0,following:true}),restore=useRef<Position|null>(null)
  const rememberMore=(value:{before:number;hasMore:boolean})=>{moreRef.current=value;setMore(value)}
  const botFor=(key?:W.ProjectAssistantBotKey)=>key?bots.find(bot=>identity(bot.key)===identity(key)):undefined
  const author=(key?:W.ProjectAssistantBotKey)=>key?(botFor(key)?.name??key.bot_id):groupWord("groupPerson")
  const members=group.members.flatMap(member=>{const bot=botFor(member.key);return bot?[bot]:[]})
  const options=dismissed===text?[]:groupMentionOptions(text,caret,members)
  const picked=Math.min(highlight,Math.max(0,options.length-1))
  const writable=active&&!!page&&canWrite(page.source)&&source.current?.()!==false&&!group.deleted
  const readable=active&&!!page&&canRead(page.source)&&source.current?.()!==false&&!group.deleted
  const canQuote=source.canGroupContext?.()===true

  useLayoutEffect(()=>{setHost(anchor.current?.closest<HTMLElement>(".pa-shell")??null)},[])
  useLayoutEffect(()=>{
    if(!active||!host)return
    const savedFocus=lastFocus.current
    const frame=requestAnimationFrame(()=>{
      const usable=savedFocus?.isConnected&&!savedFocus.matches(":disabled")&&!savedFocus.closest("[hidden]")&&getComputedStyle(savedFocus).visibility!=="hidden"
      const target=usable?savedFocus:membersOpen?membersPanel.current?.querySelector<HTMLElement>("button"):composer.current
      target?.focus({preventScroll:true})
      if(scroll.current)scroll.current.scrollTop=position.current.following?scroll.current.scrollHeight:position.current.top
    })
    return()=>cancelAnimationFrame(frame)
  },[active,host,!!page])
  const capturePosition=():Position=>{
    const el=scroll.current
    if(!el)return position.current
    const top=el.getBoundingClientRect().top
    const first=[...el.querySelectorAll<HTMLElement>("[data-room-seq]")].find(row=>row.getBoundingClientRect().bottom>top)
    return {...position.current,top:el.scrollTop,seq:first?.dataset.roomSeq,offset:first?first.getBoundingClientRect().top-top:undefined}
  }
  useLayoutEffect(()=>{
    const el=scroll.current,saved=restore.current
    if(!active||!el||!saved)return
    restore.current=null
    const row=saved.seq?[...el.querySelectorAll<HTMLElement>("[data-room-seq]")].find(row=>row.dataset.roomSeq===saved.seq):undefined
    if(saved.following)el.scrollTop=el.scrollHeight
    else if(row&&saved.offset!==undefined)el.scrollTop+=row.getBoundingClientRect().top-el.getBoundingClientRect().top-saved.offset
    else el.scrollTop=saved.top
    position.current={top:el.scrollTop,following:saved.following};setFollowing(saved.following)
  },[lines,active,membersOpen])
  async function load(before=0,signal=control.current.signal){
    if(signal.aborted||!activeRef.current)return
    if(before)setPaging(true)
    try {
      const answer=await authorized(source,currentRef.current,readSignal=>source.groupHistory!({key:initial.key,before_seq:before,page_size:100},readSignal),signal)
      if(signal.aborted||!activeRef.current)return
      observing.current=!!answer.room.running||!!answer.activity?.length
      const previous=linesRef.current,last=previous.at(-1)?.seq??0
      const gap=!before&&previous.length>0&&answer.lines.length>0&&answer.lines[0].seq>last+1
      const next=gap?answer.lines:merge(previous,answer.lines)
      restore.current=capturePosition();linesRef.current=next
      setPage(answer);setGroup(old=>answer.group.version>=old.version?answer.group:old);setLines(next)
      if(before||!moreRef.current||gap)rememberMore({before:answer.next_before_seq,hasMore:answer.has_more})
      setError("")
      if(answer.group.deleted)changedRef.current(answer.group)
    }catch(e){
      if(!signal.aborted){
        observing.current=false
        const code=errorCode(e);setError(code);setPage(null)
        if(["pa_revoked","pa_scope_denied","pa_unpaired"].includes(code)){control.current.abort();linesRef.current=[];setLines([])}
      }
    }finally{if(before)setPaging(false)}
  }
  useEffect(()=>{
    if(!active)return
    const controller=new AbortController();control.current=controller
    const queue=serialSnapshotRefresh(earlier=>load(earlier?(moreRef.current?.before??0):0,controller.signal),controller.signal)
    const follow=followSnapshotRoster(()=>queue(),controller.signal,EventRefreshMs,document,window,{readOnEntry:true,keepReading:()=>observing.current&&activeRef.current&&source.current?.()!==false})
    refresh.current=earlier=>earlier?queue(true):follow?.()??Promise.resolve()
    return()=>{observing.current=false;controller.abort()}
  },[source,initial.key.machine_id,initial.key.bot_id,active])
  const focusComposer=()=>requestAnimationFrame(()=>composer.current?.focus({preventScroll:true}))
  const showMembers=(open:boolean)=>{
    restore.current=capturePosition();setMembersOpen(open)
    if(open)requestAnimationFrame(()=>{if(activeRef.current&&membersPanel.current&&!membersPanel.current.hidden)membersPanel.current.querySelector<HTMLElement>("button")?.focus({preventScroll:true})})
  }
  const replyTo=(line:W.ProjectAssistantRoomEntry)=>{setQuote(line);pending.current=null;focusComposer()}
  const mention=(bot:W.ProjectAssistantBot,replace=true)=>{
    const next=replace?withGroupMention(text,bot.name):text+(text&&!/\s$/u.test(text)?" ":"")+"@"+bot.name+" "
    setText(next);setCaret(next.length);setHighlight(0);setDismissed(null);pending.current=null
    requestAnimationFrame(()=>{composer.current?.focus({preventScroll:true});composer.current?.setSelectionRange(next.length,next.length)})
  }
  async function effect(run:(signal:AbortSignal)=>Promise<unknown>){
    if(!writable||busy||unknown)return
    setBusy(true);setError("")
    try {await authorized(source,currentRef.current,run,control.current.signal);await refresh.current()}
    catch(e){const code=errorCode(e);setError(code);if(code==="pa_outcome_unknown")setUnknown(true)}finally{setBusy(false)}
  }
  async function send(){
    if(!text.trim()||!writable||busy||unknown)return
    const request=pending.current??{command:command(group.key,"send"),text,...(quote?{quote:{seq:quote.seq}}:{})}
    pending.current=request
    await effect(async signal=>{
      const answer=await source.sendGroup!(request,signal)
      observing.current=true
      pending.current=null;setText("");setCaret(0);setQuote(null);setDismissed(null)
      position.current.following=true;restore.current={top:0,following:true}
      const next=merge(linesRef.current,[answer.entry]);linesRef.current=next;setLines(next)
      return answer
    })
  }
  const activity=page?.activity??[]
  const working=!!page?.room.running||activity.length>0
  const openEditor=(opener:HTMLElement|null)=>{opener?.focus({preventScroll:true});setEditing(true)}
  const body=<section ref={room} hidden={!active} className={`pa-room-layout${membersOpen?" pa-members-open":""}`} role="dialog" aria-modal="true" aria-label={group.name}
    onFocusCapture={event=>{if(event.target instanceof HTMLElement)lastFocus.current=event.target}}
    onKeyDown={event=>{if(membersOpen&&!editing&&event.key==="Escape"&&!event.nativeEvent.isComposing&&!event.defaultPrevented){event.stopPropagation();showMembers(false);requestAnimationFrame(()=>memberToggle.current?.focus({preventScroll:true}))}}}>
    <div className="pa-chat"><header className="pa-chat-header"><button type="button" onClick={onClose}>{groupWord("groupBack")}</button><div><h2>{group.name}</h2><small>{group.members.map(member=>author(member.key)).join(" · ")}</small></div><button ref={memberToggle} type="button" aria-expanded={membersOpen} onClick={()=>showMembers(!membersOpen)}>{groupWord("groupMembers")} ({group.members.length})</button><PaMenu label={w.moreActions} items={[
      {id:"edit",label:groupWord("groupEdit"),disabled:!writable||busy||unknown,onSelect:()=>openEditor(room.current?.querySelector<HTMLElement>(".pa-chat-header .pa-menu>button")??null)},
      {id:"stop",label:groupWord("groupStop"),disabled:!writable||busy||unknown,onSelect:()=>void effect(signal=>source.stopGroup!({command:command(group.key,"stop")},signal))},
      {id:"delete",label:groupWord("groupDelete"),disabled:!writable||busy||unknown,onSelect:()=>setDeleting(true)}
    ]}/></header>
    <div className="pa-chat-scroll" ref={scroll} onScroll={()=>{const el=scroll.current;if(el&&activeRef.current){const end=el.scrollHeight-el.scrollTop-el.clientHeight<=1;position.current={top:el.scrollTop,following:end};setFollowing(end)}}}><div className="pa-chat-content">
      <TeamApprovals sources={[source]} bots={source.canGroupContext?.()?members.filter(bot=>activity.some(turn=>identity(turn.member)===identity(bot.key))):page?.room.running?members:[]}/>
      {more?.hasMore&&<button type="button" disabled={paging||busy} onClick={()=>void refresh.current(true)}>{paging?w.earlierLoading:groupWord("groupOlder")}</button>}
      {lines.map(line=>{const member=botFor(line.author),person=line.kind==="person";return <article key={line.seq} className={`pa-room-message${person?" pa-room-person":""}`} data-room-seq={line.seq}>
        {!person&&<div className="pa-room-author">{member&&<BotAvatar appearance={member.appearance} size={28}/>}<strong style={member?{color:member.appearance.color}:undefined}>{author(line.author)}</strong></div>}
        <div className="pa-room-bubble">{line.quote?<blockquote data-quote-seq={line.quote.seq}><strong>{author(line.quote.author)}</strong><p>{line.quote.text}{line.quote.truncated?"…":""}</p></blockquote>:line.reply_to_author&&<div className="pa-room-context">↳ {groupWord("groupReplyTo")} {author(line.reply_to_author)}</div>}<p>{line.text}</p></div>
        {canQuote&&(line.kind==="member"||person)&&<button className="pa-room-reply" type="button" aria-label={`${groupWord("groupReply")} · ${author(line.author)}`} disabled={!writable||busy||unknown} onClick={()=>replyTo(line)}>↩ {groupWord("groupReply")}</button>}
      </article>})}
      {!lines.length&&<p role="status">{page?groupWord("groupEmpty"):error?groupWord("groupUnavailable"):w.chatLoading}</p>}
    </div></div>
    {!following&&lines.length>0&&<button type="button" className="pa-room-jump" onClick={()=>{position.current.following=true;setFollowing(true);if(scroll.current)scroll.current.scrollTop=scroll.current.scrollHeight}}>{w.jump} ↓</button>}
    <footer className="pa-chat-footer">
      {working&&<div className="pa-room-working" role="status">{activity.length?activity.map(turn=>{const bot=botFor(turn.member);return <span key={turn.turn_id} data-working-member={identity(turn.member)} data-working-state={turn.state}>{bot&&<BotAvatar appearance={bot.appearance} size={20} mood={turn.state==="waiting_permission"?"needsInput":turn.state==="running"?"working":"idle"}/>}<span>{author(turn.member)} · {turn.state==="waiting_permission"?w.waiting_permission:turn.state==="running"?w.running:w.queued}</span></span>}):<span>{groupWord("groupRunning")}</span>}</div>}
      {quote&&<div className="pa-room-draft-quote"><div><strong>{groupWord("groupReplyTo")} {author(quote.author)}</strong><p>{quote.text}</p></div><button type="button" aria-label={w.clearQuote} disabled={busy||unknown} onClick={()=>{setQuote(null);pending.current=null;focusComposer()}}>×</button></div>}
      <form className="pa-composer pa-room-composer" onSubmit={event=>{event.preventDefault();if(!composing.current)void send()}}>
        {options.length>0&&<div id={mentionID} className="pa-room-mentions" role="listbox" aria-label={groupWord("groupMembers")}>{options.map((bot,index)=><button id={`${mentionID}-${index}`} key={identity(bot.key)} type="button" role="option" aria-selected={index===picked} onMouseDown={event=>event.preventDefault()} onClick={()=>mention(bot)}><BotAvatar appearance={bot.appearance} size={28}/><span>{bot.name}</span></button>)}</div>}
        <div className="pa-editor"><div className="pa-editor-mirror" aria-hidden="true">{text+"​"}</div><textarea ref={composer} rows={1} aria-label={groupWord("groupMessage")} placeholder={groupWord("groupMessage")} value={text} disabled={!writable||busy||unknown}
          aria-controls={options.length?mentionID:undefined} aria-expanded={options.length>0} aria-autocomplete="list" aria-activedescendant={options.length?`${mentionID}-${picked}`:undefined}
          onCompositionStart={()=>{composing.current=true}} onCompositionEnd={()=>{composing.current=false}}
          onSelect={event=>setCaret(event.currentTarget.selectionStart)}
          onChange={event=>{setText(event.target.value);setCaret(event.target.selectionStart);setHighlight(0);setDismissed(null);pending.current=null}}
          onKeyDown={event=>{
            if(event.nativeEvent.isComposing||composing.current||event.keyCode===229)return
            if(options.length&&!event.shiftKey&&!event.altKey&&!event.ctrlKey&&!event.metaKey){
              if(event.key==="ArrowDown"||event.key==="ArrowUp"){event.preventDefault();setHighlight((picked+(event.key==="ArrowDown"?1:-1)+options.length)%options.length);return}
              if(event.key==="Escape"){event.preventDefault();event.stopPropagation();setDismissed(text);return}
              if(event.key==="Tab"||(event.key==="Enter"&&!event.repeat)){event.preventDefault();mention(options[picked]);return}
            }
            if(composerEnter(event.nativeEvent,composing.current)==="send"){event.preventDefault();void send()}
          }}/></div>
        <button className="pa-send" type="submit" aria-label={groupWord(busy?"groupWorking":"groupSend")} disabled={!writable||busy||unknown||!text.trim()}>↑</button>
      </form>
      {deleting&&<div role="alert"><p>{groupWord("groupDeleteHint")}</p><button type="button" disabled={busy||unknown} onClick={()=>setDeleting(false)}>{groupWord("groupCancel")}</button><button type="button" disabled={busy||unknown} onClick={()=>void effect(async signal=>{const result=await source.deleteGroup!({command:command(group.key,"delete"),expected_version:group.version},signal);changedRef.current({...group,deleted:true});return result})}>{groupWord("groupDeleteConfirm")}</button></div>}
      {(error||unknown)&&<p role="alert">{groupWord(unknown?"groupUnknown":"groupUnavailable")} {error&&`(${error})`}</p>}
    </footer>
    </div><aside ref={membersPanel} hidden={!membersOpen} tabIndex={-1} className="pa-room-members" aria-label={groupWord("groupMembers")}><header className="pa-toolbar"><h3>{groupWord("groupMembers")}</h3><button type="button" aria-label={w.closeInformation} onClick={()=>{showMembers(false);requestAnimationFrame(()=>memberToggle.current?.focus({preventScroll:true}))}}>×</button></header><h2>{group.name}</h2>{group.members.map(member=>{const bot=botFor(member.key);return <div className="pa-room-member" key={identity(member.key)}><button type="button" disabled={!readable||busy||!bot||!onOpenMember} onClick={event=>{lastFocus.current=event.currentTarget;onOpenMember?.(member.key)}}>{bot&&<BotAvatar appearance={bot.appearance} size={36}/>}<span><strong>{author(member.key)}</strong></span></button><button type="button" aria-label={`@${author(member.key)}`} disabled={!writable||busy||unknown||!bot} onClick={()=>{if(bot)mention(bot,false);showMembers(false)}}>@</button></div>})}<button type="button" disabled={!writable||busy||unknown} onClick={event=>openEditor(event.currentTarget)}>{groupWord("groupEdit")}</button></aside>
    {editing&&page&&<GroupEditor sources={[{source,groups:[group],status:page.source}]} bots={bots} current={current} projectNames={projectNames} group={group} onClose={()=>setEditing(false)} onSaved={(_,changed)=>{setGroup(changed);setEditing(false);changedRef.current(changed);void refresh.current()}}/>}
  </section>
  return <><span ref={anchor} hidden/>{host&&createPortal(body,host)}</>
}
