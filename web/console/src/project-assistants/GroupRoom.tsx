import { Fragment, useEffect, useId, useLayoutEffect, useRef, useState } from "react"
import { createPortal } from "react-dom"
import type * as W from "@clawdline/contract"
import { authorized, command, errorCode, type SourceClient } from "./client"
import { canRead, canWrite } from "./view-state"
import { GroupEditor, TeamApprovals, groupWord, sameGroupProject } from "./Groups"
import { PaMenu } from "./PaSelect"
import { BotAvatar } from "./BotAvatar"
import { GroupActionIcon, GroupAvatar } from "./GroupAvatar"
import { words } from "./words"
import { currentCatalogTag } from "../catalog"
import { EventRefreshMs } from "./Chat"
import { followSnapshotRoster, serialSnapshotRefresh } from "./snapshot-roster"
import { draftGroupMentions } from "./group-mentions"
import { Composer } from "./Composer"
import { botMentionToken, decodeBotMentions } from "./bot-mentions"
import type { BotMentionCandidate } from "./BotMentionField"
import { GroupMessageText } from "./GroupMessageText"
import { GroupMemberOutcome, showGroupMemberOutcome } from "./GroupMemberOutcome"
import { GroupMessageRow } from "./GroupMessageRow"
import { useMessageArrivals } from "./message-arrivals"
import { chatTimeBreak, chatTimeFormat } from "./chat-time"

const identity=(k:W.ProjectAssistantBotKey)=>JSON.stringify([k.machine_id,k.bot_id])
const merge=(previous:W.ProjectAssistantRoomEntry[],incoming:W.ProjectAssistantRoomEntry[])=>[...new Map([...previous,...incoming].map(line=>[line.seq,line])).values()].sort((a,b)=>a.seq-b.seq)
type Position={top:number;following:boolean;seq?:string;offset?:number}

export function GroupRoom({source,current,group:initial,bots,projectNames,onClose,onChanged,active=true,onOpenMember}:{source:SourceClient;current:()=>readonly SourceClient[];group:W.ProjectAssistantGroup;bots:readonly W.ProjectAssistantBot[];projectNames:ReadonlyMap<string,string>;onClose:()=>void;onChanged:(group:W.ProjectAssistantGroup)=>void;active?:boolean;onOpenMember?:(key:W.ProjectAssistantBotKey)=>void}) {
  const [group,setGroup]=useState(initial),[page,setPage]=useState<W.ProjectAssistantGroupHistoryResponse|null>(null),[lines,setLines]=useState<W.ProjectAssistantRoomEntry[]>([]),[text,setText]=useState(""),[quote,setQuote]=useState<W.ProjectAssistantRoomEntry|null>(null),[error,setError]=useState(""),[busy,setBusy]=useState(false),[unknown,setUnknown]=useState(false),[editing,setEditing]=useState<"edit"|"add"|null>(null),[deleting,setDeleting]=useState(false)
  const [membersOpen,setMembersOpen]=useState(false),[paging,setPaging]=useState(false),[following,setFollowing]=useState(true),[host,setHost]=useState<HTMLElement|null>(null)
  const [sending,setSending]=useState<W.ProjectAssistantGroupSendRequest|null>(null)
  const anchor=useRef<HTMLSpanElement>(null),room=useRef<HTMLElement>(null),memberToggle=useRef<HTMLButtonElement>(null),membersPanel=useRef<HTMLElement>(null),lastFocus=useRef<HTMLElement|null>(null),membersID=useId()
  const language=currentCatalogTag(),w=words(language),timeFormat=chatTimeFormat(language)
  const membersFocus=useRef<"panel"|"header"|null>(null)
  const composerFocusFrame=useRef(0)
  const pending=useRef<W.ProjectAssistantGroupSendRequest|null>(null),control=useRef(new AbortController()),refresh=useRef<(earlier?:boolean)=>Promise<void>>(async()=>{})
  const observing=useRef(false)
  const currentRef=useRef(current),changedRef=useRef(onChanged),activeRef=useRef(active)
  currentRef.current=current;changedRef.current=onChanged;activeRef.current=active
  const scroll=useRef<HTMLDivElement>(null),[more,setMore]=useState<{before:number;hasMore:boolean}|null>(null)
  const linesRef=useRef(lines),moreRef=useRef(more),position=useRef<Position>({top:0,following:true}),restore=useRef<Position|null>(null)
  const sentAfter=useRef(0),localEntry=useRef(0)
  const rememberMore=(value:{before:number;hasMore:boolean})=>{moreRef.current=value;setMore(value)}
  const botFor=(key?:W.ProjectAssistantBotKey)=>key?bots.find(bot=>identity(bot.key)===identity(key)):undefined
  const author=(key?:W.ProjectAssistantBotKey)=>key?(botFor(key)?.name??key.bot_id):groupWord("groupPerson")
  const members=group.members.flatMap(member=>{const bot=botFor(member.key);return bot?[bot]:[]})
  const memberKeys=group.members.map(member=>member.key)
  const memberCount=groupWord(memberKeys.length===1?"groupBotsOne":"groupBots").replace("{count}",String(memberKeys.length))
  const candidates:BotMentionCandidate[]=members.map(bot=>({id:bot.key.bot_id,name:bot.name,appearance:bot.appearance,agent:bot.agent,
    project:bot.project.kind==="personal"?w.personalWorkspace:projectNames.get(JSON.stringify([bot.key.machine_id,bot.project.project_id]))||w.unnamedProject}))
  const markDraft=(display:string)=>draftGroupMentions(display,candidates)
  // The draft keeps a picked Bot as @[Name](bot:ID) for the shared @ list; the
  // room routes on the @Name the person reads, so that is what is sent.
  const plain=decodeBotMentions(text).display
  const composerField=()=>room.current?.querySelector<HTMLTextAreaElement>(".pa-chat-footer textarea")??null
  const writable=active&&!!page&&canWrite(page.source)&&source.current?.()!==false&&!group.deleted
  const readable=active&&!!page&&canRead(page.source)&&source.current?.()!==false&&!group.deleted
  const canAdd=writable&&members.length===memberKeys.length&&bots.some(bot=>!bot.hidden&&sameGroupProject(bot.project,group.project)&&!memberKeys.some(key=>identity(key)===identity(bot.key)))
  const canQuote=source.canGroupContext?.()===true
  const motion=useMessageArrivals({items:lines,ready:active&&!!page,following,seq:line=>line.seq,id:line=>String(line.seq),
    skip:line=>line.seq===localEntry.current||line.kind!=="person"&&line.kind!=="member"})

  useLayoutEffect(()=>{setHost(anchor.current?.closest<HTMLElement>(".pa-shell")??null)},[])
  useLayoutEffect(()=>{
    if(!active||!host)return
    const viewport=window.visualViewport,header=room.current?.querySelector<HTMLElement>(".pa-chat-header"),footer=room.current?.querySelector<HTMLElement>(".pa-chat-footer")
    const measure=()=>{
      // DOM rectangles and hit testing use client coordinates. iOS can scroll
      // the document when a focused field opens its keyboard while offsetTop
      // still includes that pan. Convert the viewport's document position to
      // the same client origin before intersecting it with the host.
      const visibleTop=viewport?viewport.pageTop-window.scrollY:0
      const box=host.getBoundingClientRect(),top=Math.max(box.top,visibleTop),bottom=Math.min(box.bottom,visibleTop+(viewport?.height??innerHeight))
      host.style.setProperty("--pa-room-visible-top",`${Math.max(0,top-box.top)}px`)
      host.style.setProperty("--pa-room-visible-bottom",`${Math.max(0,box.bottom-bottom)}px`)
      host.style.setProperty("--pa-room-visible-height",`${Math.max(0,bottom-top)}px`)
      if(position.current.following&&scroll.current)scroll.current.scrollTop=scroll.current.scrollHeight
    }
    const sizes=new ResizeObserver(measure)
    sizes.observe(host)
    if(header)sizes.observe(header)
    if(footer)sizes.observe(footer)
    if(scroll.current)sizes.observe(scroll.current)
    viewport?.addEventListener("resize",measure);viewport?.addEventListener("scroll",measure);window.addEventListener("resize",measure);host.parentElement?.addEventListener("animationend",measure)
    measure()
    return()=>{
      sizes.disconnect();viewport?.removeEventListener("resize",measure);viewport?.removeEventListener("scroll",measure);window.removeEventListener("resize",measure);host.parentElement?.removeEventListener("animationend",measure)
      for(const name of ["--pa-room-visible-top","--pa-room-visible-bottom","--pa-room-visible-height"])host.style.removeProperty(name)
    }
  },[active,host])
  useLayoutEffect(()=>{
    if(!active||!host)return
    const savedFocus=lastFocus.current
    const frame=requestAnimationFrame(()=>{
      const panelOpen=membersPanel.current?.hidden===false
      const focused=document.activeElement
      if(focused===document.body||!room.current?.contains(focused)){
        const usable=savedFocus?.isConnected&&!savedFocus.matches(":disabled")&&!savedFocus.closest("[hidden]")&&
          getComputedStyle(savedFocus).visibility!=="hidden"&&(!panelOpen||membersPanel.current?.contains(savedFocus))
        const target=usable?savedFocus:panelOpen?membersPanel.current?.querySelector<HTMLElement>("button"):composerField()
        target?.focus({preventScroll:true})
      }
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
      observing.current=!!answer.room.running||!!answer.activity?.length||
        !!answer.member_outcomes?.some(outcome=>outcome.work?.some(work=>work.state==="accepted"||work.state==="waiting_for_person"||work.state==="executing"))
      const previous=linesRef.current,last=previous.at(-1)?.seq??0
      const gap=!before&&previous.length>0&&answer.lines.length>0&&answer.lines[0].seq>last+1
      const next=gap?answer.lines:merge(previous,answer.lines)
      restore.current=capturePosition();linesRef.current=next
      setPage(old=>old&&old.room.revision>answer.room.revision?old:answer);setGroup(old=>answer.group.version>=old.version?answer.group:old);setLines(next)
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
  const focusComposer=(at?:number)=>{
    cancelAnimationFrame(composerFocusFrame.current)
    composerFocusFrame.current=requestAnimationFrame(()=>{
      if(!activeRef.current||membersPanel.current?.hidden===false)return
      const field=composerField()
      field?.focus({preventScroll:true})
      if(at!==undefined)field?.setSelectionRange(at,at)
    })
  }
  const showMembers=(open:boolean,focus=true)=>{
    if(open)cancelAnimationFrame(composerFocusFrame.current)
    restore.current=capturePosition()
    membersFocus.current=focus?(open?"panel":"header"):null
    setMembersOpen(open)
  }
  useLayoutEffect(()=>{
    const destination=membersFocus.current
    membersFocus.current=null
    if(!active||!destination)return
    const target=destination==="panel"?membersPanel.current?.querySelector("button"):memberToggle.current
    target?.focus({preventScroll:true})
  },[active,membersOpen])
  useEffect(()=>{
    if(!active||!membersOpen||editing)return
    const escape=(event:KeyboardEvent)=>{
      if(event.key==="Escape"&&!event.isComposing&&!event.defaultPrevented&&event.target===document.body)showMembers(false)
    }
    document.addEventListener("keydown",escape)
    return()=>document.removeEventListener("keydown",escape)
  },[active,membersOpen,editing])
  const replyTo=(line:W.ProjectAssistantRoomEntry)=>{setQuote(line);pending.current=null;focusComposer()}
  const mention=(bot:W.ProjectAssistantBot)=>{
    const next=text+(text&&!/\s$/u.test(text)?" ":"")+botMentionToken({id:bot.key.bot_id,name:bot.name})+" "
    setText(next);pending.current=null
    focusComposer(decodeBotMentions(next).display.length)
  }
  async function effect(run:(signal:AbortSignal)=>Promise<unknown>){
    if(!writable||busy||unknown)return
    setBusy(true);setError("")
    try {await authorized(source,currentRef.current,run,control.current.signal);await refresh.current()}
    catch(e){const code=errorCode(e);setError(code);if(code==="pa_outcome_unknown")setUnknown(true)}finally{setBusy(false)}
  }
  async function send(){
    if(!plain.trim()||!writable||busy||unknown)return
    const draft=text
    const request=pending.current??{command:command(group.key,"send"),text:plain,...(quote?{quote:{seq:quote.seq}}:{})}
    pending.current=request
    sentAfter.current=linesRef.current.at(-1)?.seq??0
    position.current.following=true;setFollowing(true)
    setSending(request);setText("")
    if(scroll.current)scroll.current.scrollTop=scroll.current.scrollHeight
    await effect(async signal=>{
      const answer=await source.sendGroup!(request,signal)
      observing.current=true
      localEntry.current=answer.entry.seq
      pending.current=null;setText("");setQuote(null)
      position.current.following=true;restore.current={top:0,following:true}
      const next=merge(linesRef.current,[answer.entry]);linesRef.current=next;setLines(next)
      setSending(null)
      setPage(old=>old&&answer.room.revision>=old.room.revision?{...old,room:answer.room,activity:[],member_outcomes:[]}:old)
      return answer
    })
    if(pending.current===request)setText(draft)
    setSending(null)
  }
  const activity=page?.activity??[]
  const working=!!page?.room.running||activity.length>0
  const stopRoom=()=>void effect(signal=>source.stopGroup!({command:command(group.key,"stop")},signal))
  const outcomes=readable&&page.group.version>=group.version?page.member_outcomes?.filter(showGroupMemberOutcome)??[]:[]
  const unanswered=!!page&&!working&&!busy&&!error&&!unknown&&outcomes.length===0&&lines.at(-1)?.kind==="person"
  const openEditor=(opener:HTMLElement|null,mode:"edit"|"add"="edit")=>{opener?.focus({preventScroll:true});setEditing(mode)}
  const body=<section ref={room} hidden={!active} className={`pa-room-layout${membersOpen?" pa-members-open":""}`} role="dialog" aria-label={group.name}
    onFocusCapture={event=>{if(event.target instanceof HTMLElement)lastFocus.current=event.target}}
    onKeyDown={event=>{if(membersOpen&&!editing&&event.key==="Escape"&&!event.nativeEvent.isComposing&&!event.defaultPrevented){event.stopPropagation();showMembers(false)}}}>
    <div className="pa-chat"><header className="pa-chat-header"><button type="button" onClick={onClose}>{groupWord("groupBack")}</button>
    <h2 className="pa-room-heading"><button ref={memberToggle} type="button" className="pa-room-identity" aria-expanded={membersOpen} aria-controls={membersID} onClick={()=>showMembers(!membersOpen)}>
      <GroupAvatar keys={memberKeys} bots={bots} size={34}/><span className="pa-room-identity-text"><strong>{group.name}</strong><small>{memberCount}</small></span>
      <svg className="pa-room-chevron" viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="m6 4 4 4-4 4"/></svg>
    </button></h2><PaMenu label={w.moreActions} items={[
      {id:"edit",label:groupWord("groupEdit"),disabled:!writable||busy||unknown,onSelect:()=>openEditor(room.current?.querySelector<HTMLElement>(".pa-chat-header .pa-menu>button")??null)},
      {id:"stop",label:groupWord("groupStop"),disabled:!writable||busy||unknown,onSelect:stopRoom},
      {id:"delete",label:groupWord("groupDelete"),disabled:!writable||busy||unknown,onSelect:()=>setDeleting(true)}
    ]}/></header>
    <div className="pa-chat-scroll" ref={scroll} onScroll={()=>{const el=scroll.current;if(el&&activeRef.current){const end=el.scrollHeight-el.scrollTop-el.clientHeight<=1;position.current={top:el.scrollTop,following:end};setFollowing(end)}}}><div className="pa-chat-content">
      <TeamApprovals sources={[source]} bots={source.canGroupContext?.()?members.filter(bot=>activity.some(turn=>identity(turn.member)===identity(bot.key))):page?.room.running?members:[]}/>
      {more?.hasMore&&<button type="button" disabled={paging||busy} onClick={()=>void refresh.current(true)}>{paging?w.earlierLoading:groupWord("groupOlder")}</button>}
      {lines.map((line,index)=>{
        const previous=lines[index-1],member=botFor(line.author),person=line.kind==="person"
        const separate=chatTimeBreak(line.at,previous?.at)
        const continued=!separate&&previous?.kind===line.kind&&(person||!!line.author&&!!previous.author&&identity(previous.author)===identity(line.author))
        const shownAuthor=author(line.author),canReply=canQuote&&writable&&!busy&&!unknown
        return <Fragment key={line.seq}>
          {separate&&line.at&&<div className="pa-separator"><time dateTime={line.at}>{timeFormat.format(Date.parse(line.at))}</time></div>}
          <GroupMessageRow line={line} group={group} source={source} author={shownAuthor}
            continued={continued} arriving={motion.arrivals.has(String(line.seq))} onArrivalEnd={()=>motion.finish(String(line.seq))}
            readable={readable} writable={writable} dismissKey={JSON.stringify([active,readable,membersOpen,editing,deleting])}
            replyLabel={groupWord("groupReply")} onReply={canReply?()=>replyTo(line):undefined} onReactionChanged={()=>void refresh.current()}
            header={!person&&!continued&&<div className="pa-room-author">{member&&<BotAvatar appearance={member.appearance} size={28}/>}<strong style={member?{color:member.appearance.color}:undefined}>{shownAuthor}</strong></div>}>
            {line.quote?<blockquote data-quote-seq={line.quote.seq}><strong>{author(line.quote.author)}</strong><p>{line.quote.text}{line.quote.truncated?"…":""}</p></blockquote>:line.reply_to_author&&<div className="pa-room-context">{groupWord("groupReplyTo")} {author(line.reply_to_author)}</div>}
            <GroupMessageText line={line} members={candidates} w={w}/>
          </GroupMessageRow>
        </Fragment>
      })}
      {outcomes.map(outcome=><GroupMemberOutcome key={outcome.turn_id} outcome={outcome} w={w}/>)}
      {!lines.length&&!sending&&<p role="status">{page?groupWord("groupEmpty"):error?groupWord("groupUnavailable"):w.chatLoading}</p>}
      {sending&&<article key={sending.command.idempotency_key} data-local-entrance className="pa-room-message pa-room-person pa-sending" aria-live="polite">
        {!lines.some(line=>line.kind==="person"&&line.seq>sentAfter.current&&line.text===sending.text)&&<div className="pa-room-bubble">{sending.text}</div>}
        <small><span className="pa-send-spinner" aria-hidden="true"/>{groupWord("groupWorking")}</small>
      </article>}
    </div></div>
    {!following&&lines.length>0&&<button type="button" className="pa-room-jump" onClick={()=>{position.current.following=true;setFollowing(true);if(scroll.current)scroll.current.scrollTop=scroll.current.scrollHeight}}>{w.jump} ↓</button>}
    <footer className="pa-chat-footer">
      {unanswered&&<p className="pa-room-unanswered" role="status">{groupWord("groupNoReply")}</p>}
      {working&&<div className="pa-room-working" role="status">{activity.length?activity.map(turn=>{const bot=botFor(turn.member);return <span key={turn.turn_id} data-working-member={identity(turn.member)} data-working-state={turn.state}>{bot&&<BotAvatar appearance={bot.appearance} size={20} mood={turn.state==="waiting_permission"?"needsInput":turn.state==="running"?"working":"idle"}/>}<span>{author(turn.member)} · {turn.state==="waiting_permission"?w.waiting_permission:turn.state==="running"?w.running:w.queued}</span></span>}):<span>{groupWord("groupRunning")}</span>}</div>}
      {quote&&<div className="pa-room-draft-quote"><div><strong>{groupWord("groupReplyTo")} {author(quote.author)}</strong><p>{quote.text}</p></div><button type="button" aria-label={w.clearQuote} disabled={busy||unknown} onClick={()=>{setQuote(null);pending.current=null;focusComposer()}}>×</button></div>}
      <Composer text={text} onText={next=>{setText(next);pending.current=null}} onSend={()=>void send()} onStop={working&&source.stopGroup?stopRoom:undefined}
        disabled={!writable||unknown} stopDisabled={!writable||unknown} busy={busy} onClearQuote={()=>{}} w={w} mentions={candidates} marks={markDraft}/>
      {deleting&&<div role="alert"><p>{groupWord("groupDeleteHint")}</p><button type="button" disabled={busy||unknown} onClick={()=>setDeleting(false)}>{groupWord("groupCancel")}</button><button type="button" disabled={busy||unknown} onClick={()=>void effect(async signal=>{const result=await source.deleteGroup!({command:command(group.key,"delete"),expected_version:group.version},signal);changedRef.current({...group,deleted:true});return result})}>{groupWord("groupDeleteConfirm")}</button></div>}
      {(error||unknown)&&<p role="alert">{groupWord(unknown?"groupUnknown":"groupUnavailable")} {error&&`(${error})`}</p>}
    </footer>
    </div>
    <aside ref={membersPanel} id={membersID} hidden={!membersOpen} inert={!membersOpen} tabIndex={-1} className="pa-room-members pa-side-inspector" aria-label={groupWord("groupMembers")}>
      <header className="pa-room-members-identity">
        <button className="pa-room-members-close pa-circle" type="button" aria-label={w.closeInformation} onClick={()=>showMembers(false)}>×</button>
        <GroupAvatar keys={memberKeys} bots={bots} size={64}/><h2>{group.name}</h2>{group.description&&<p className="pa-room-members-about">{group.description}</p>}<small>{memberCount}</small>
      </header>
      <ul className="pa-room-member-list">
        {group.members.map(member=>{
          const bot=botFor(member.key)
          return <li className="pa-room-member" key={identity(member.key)}>
            <button type="button" disabled={!readable||busy||!bot||!onOpenMember} onClick={event=>{lastFocus.current=event.currentTarget;onOpenMember?.(member.key)}}>{bot&&<BotAvatar appearance={bot.appearance} size={36}/>}<strong>{author(member.key)}</strong></button>
            <button type="button" className="pa-room-mention" aria-label={`@${author(member.key)}`} title={`@${author(member.key)}`} disabled={!writable||busy||unknown||!bot} onClick={()=>{if(bot)mention(bot);showMembers(false,false)}}>@</button>
          </li>
        })}
      </ul>
      <div className="pa-room-member-actions">
        <button className="pa-room-add-member" type="button" disabled={!canAdd||busy||unknown} onClick={event=>openEditor(event.currentTarget,"add")}><span className="pa-room-row-icon" aria-hidden="true"><GroupActionIcon/></span>{groupWord("groupAddMember")}</button>
        <button className="pa-room-member-edit" type="button" disabled={!writable||busy||unknown} onClick={event=>openEditor(event.currentTarget)}><span className="pa-room-row-icon" aria-hidden="true"><svg viewBox="0 0 16 16" width="15" height="15" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"><path d="M10.5 2.5l3 3L6 13H3v-3z"/></svg></span>{groupWord("groupEdit")}</button>
      </div>
    </aside>
    {editing&&page&&<GroupEditor sources={[{source,groups:[group],status:page.source}]} bots={bots} current={current} projectNames={projectNames} group={group} addMembers={editing==="add"} onDelete={()=>{setEditing(null);showMembers(false,false);setDeleting(true)}} onClose={()=>setEditing(null)} onSaved={(_,changed)=>{setGroup(changed);setEditing(null);changedRef.current(changed);void refresh.current()}}/>}
  </section>
  return <><span ref={anchor} hidden/>{host&&createPortal(body,host)}</>
}
