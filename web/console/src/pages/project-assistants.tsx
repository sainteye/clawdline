import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react"
import type * as W from "@clawdline/contract"
import { onStatusRefresh } from "../status-refresh.js"
import { Bots } from "../project-assistants/Bots.js"
import { browserBotVoiceAvailable } from "../project-assistants/BotVoicePanel"
import { botCallAudioAvailable } from "../project-assistants/bot-call-audio.js"
import { BotOrb } from "../project-assistants/BotOrb.js"
import { canRead } from "../project-assistants/view-state.js"
import { ProjectAssistantRegistry, assistantBadge, PROJECT_ASSISTANTS_API_LEVEL } from "../cloud/project-assistants.js"
import type { PageModule } from "./types.js"
import { assistantHistoryBackSteps, assistantWorkTarget } from "../page-route.js"
import { openWorkItem } from "./work/new-item.js"
import { requestPage } from "../overlays/events.js"
import { toast } from "../overlays/toast.js"
import { words } from "../project-assistants/words.js"
import { errorCode, botFromFragment } from "../project-assistants/client.js"
import {createCompletionTracker,startBotCompletionSound} from "../project-assistants/completion-sound.js"
import { followLiveRoster } from "../project-assistants/live-roster.js"

const BADGE_REFRESH_MS = 15_000

// The status bar opens the window; each source controls access to its own Bots.
export const page: PageModule = {id:"project-assistants",drawer:false,Component:() => null}

export function useProjectAssistantsHost({registry: supplied, apiLevel, cloud, language, onOpenWork}: {
  registry?: ProjectAssistantRegistry | null
  apiLevel?: number
  cloud: boolean
  language: string
  onOpenWork?: (work: W.ProjectAssistantWorkLink) => void
}) {
  const [local] = useState(() => new ProjectAssistantRegistry("local","local"))
  const registry = supplied ?? local
  const sources = useSyncExternalStore(registry.subscribe,registry.getSnapshot,registry.getSnapshot)
  const [opened,setOpened] = useState(false), [entry,setEntry] = useState(0),[voiceShortcut,setVoiceShortcut]=useState(0)
  const openedRef = useRef(false), opener = useRef<HTMLElement | null>(null)
  const pendingWork = useRef<W.ProjectAssistantWorkLink | null>(null)
  const ownedHistory = useRef("")
  const workHandler = useRef(onOpenWork)
  workHandler.current = onOpenWork
  const windowOrigin = useRef("100% 100%")
  const origin = useRef<{hash:string;href:string;state:unknown;scrolls:[HTMLElement,number,number][];x:number;y:number} | null>(null)
  useEffect(() => {if (opened && typeof history.state?.paView === "string") ownedHistory.current=history.state.paView},[opened,entry])
  // The orb's count follows new replies while Bots is closed, so it can ring.
  const completion=useRef(createCompletionTracker()),sound=useRef<ReturnType<typeof startBotCompletionSound>|null>(null)
  useEffect(()=>{sound.current=startBotCompletionSound();return()=>{sound.current?.stop();sound.current=null}},[])
  const completionScope=useCallback((source: typeof sources[number])=>JSON.stringify([registry.accountKey,source.machineID,source.authorizationRevision]),[registry.accountKey])
  useEffect(()=>completion.current.retain(sources.map(completionScope)),[sources,completionScope])
  const observeCompletions=useCallback((items:readonly W.ProjectAssistantBotSummary[],loadedSources:readonly string[])=>{
    let done=false
    for(const source of sources.filter(source=>loadedSources.includes(source.machineID)))done=completion.current.observe(completionScope(source),items.filter(item=>item.bot.key.machine_id===source.machineID))||done
    if(done)sound.current?.play()
  },[sources,completionScope])
  const observeSource=useCallback((source:typeof sources[number],items:readonly W.ProjectAssistantBotSummary[])=>{
    if(completion.current.observe(completionScope(source),items))sound.current?.play()
  },[completionScope])
  const [metadata,setMetadata] = useState<Record<string,{revision:string;readable:boolean;items:W.ProjectAssistantBotSummary[]}>>({})
  useEffect(() => {
    if (cloud || supplied || apiLevel === undefined || apiLevel < PROJECT_ASSISTANTS_API_LEVEL) return
    const control = new AbortController(), transport = globalThis.fetch.bind(globalThis)
    void transport("/v1/project-assistants/status",{signal:control.signal,cache:"no-store"}).then(async response => {
      if (!response.ok) return
      const answer: W.ProjectAssistantSourcesResponse = await response.json()
      if (control.signal.aborted || answer.contract_version !== "project-assistants.v1") return
      local.sync(answer.sources.map(source => ({accountKey:"local",readerID:"local",machineID:source.machine_id,
        authorizationRevision:"local",apiLevel,paired:true,online:true,writable:true})),() => transport)
    }).catch(() => {})
    return () => control.abort()
  },[cloud,supplied,apiLevel,local])
  useEffect(() => {
    const requests=new Map<typeof sources[number],{control:AbortController;statusPending:boolean}>()
    const refreshSource=async(source:typeof sources[number])=>{
      const prior=requests.get(source)
      // A refresh can replace a badge read, but must let the first authority response arrive.
      if(prior?.statusPending)return
      prior?.control.abort()
      const control=new AbortController(),request={control,statusPending:true}
      requests.set(source,request)
      try {
        const status = await source.status(control.signal)
        if(control.signal.aborted)return
        request.statusPending=false
        const readable = status.sources.some(canRead)
        // Voice shortcuts can use source authority before the paged badge read finishes.
        if (!control.signal.aborted) setMetadata(old => {
          const previous=old[source.machineID]
          return {...old,[source.machineID]:{revision:source.authorizationRevision,readable,
            items:readable&&previous?.revision===source.authorizationRevision?previous.items:[]}}
        })
        const items: W.ProjectAssistantBotSummary[] = []
        if (readable) {
          let cursor = ""
          const seen = new Set<string>()
          do {
            const answer = await source.list({machine_id:source.machineID,cursor,query:"",page_size:100},control.signal)
            items.push(...answer.items.filter(item => item.bot.key.machine_id === source.machineID && canRead(item.source)))
            cursor = answer.next_cursor
            if (cursor && seen.has(cursor)) throw new Error("Repeated source cursor")
            seen.add(cursor)
          } while (cursor && !control.signal.aborted)
        }
        if (!control.signal.aborted) {observeSource(source,items);setMetadata(old => ({...old,[source.machineID]:{revision:source.authorizationRevision,readable,items}}))}
      } catch (error) {
        // Keep counts through a transport failure. Explicit denials clear them.
        // Cached work stops animating after either failed read.
        const denied=["pa_revoked","pa_scope_denied","pa_unpaired","pa_disabled"].includes(errorCode(error))
        if(!control.signal.aborted)setMetadata(old=>{const previous=old[source.machineID];return previous?{...old,[source.machineID]:{...previous,readable:!denied&&previous.readable,items:denied?[]:previous.items.map(item=>({...item,source:{...item.source,fresh:false}}))}}:old})
      } finally {
        if(requests.get(source)===request)requests.delete(source)
      }
    }
    const refresh=()=>{
      for(const source of sources)if(opened||!source.canRoster?.()||!source.subscribeRoster)void refreshSource(source)
    }
    refresh()
    const unsubscribe=onStatusRefresh(refresh)
    const timer=!opened&&sources.some(source=>!source.canRoster?.()||!source.subscribeRoster)
      ?window.setInterval(()=>{if(document.visibilityState==="visible")refresh()},BADGE_REFRESH_MS):undefined
    return ()=>{
      unsubscribe();window.clearInterval(timer)
      for(const request of requests.values())request.control.abort()
    }
  },[sources,opened,observeSource])
  useEffect(()=>{
    if(opened)return
    const control=new AbortController()
    for(const source of sources){
      if(!source.canRoster?.()||!source.subscribeRoster)continue
      void followLiveRoster(source,()=>sources,control.signal,BADGE_REFRESH_MS,value=>{
        if(control.signal.aborted)return
        if(value.items)observeSource(source,value.items)
        setMetadata(old=>{
          const previous=old[source.machineID]?.revision===source.authorizationRevision?old[source.machineID]:undefined
          const items=value.items??(value.error?previous?.items.map(item=>({...item,source:{...item.source,fresh:false}})):previous?.items)??[]
          return {...old,[source.machineID]:{revision:source.authorizationRevision,readable:value.statuses?.some(canRead)??previous?.readable??false,items}}
        })
      })
    }
    return()=>control.abort()
  },[sources,opened,observeSource])
  const current = Object.entries(metadata).filter(([id,data]) => sources.some(source => source.machineID === id && source.authorizationRevision === data.revision))
  const hasReadableSource = current.some(([,data]) => data.readable)
  const badge = assistantBadge(current.flatMap(([,data]) => data.items),sources)
  // Bots folds back into the orb before it closes; a second close (the popstate
  // that history.go below triggers) finishes at once.
  const [closing,setClosing] = useState(false)
  const animating = useRef(false), folded = useRef(false)
  const motion = () => !matchMedia("(prefers-reduced-motion: reduce)").matches
  const finish = useCallback(() => {
    // Bots owns its roster and conversation entries. The host only closes the stack.
    const steps = assistantHistoryBackSteps(history.state,ownedHistory.current)
    if (steps) { history.go(-steps); return }
    openedRef.current = false; setOpened(false);setVoiceShortcut(0)
    const saved = origin.current
    if (saved && location.hash !== saved.hash) history.replaceState(saved.state,"",saved.href)
    requestAnimationFrame(() => {
      for (const [node,top,left] of saved?.scrolls ?? []) if (node.isConnected) {node.scrollTop=top;node.scrollLeft=left}
      if (saved) window.scrollTo(saved.x,saved.y)
      opener.current?.focus({preventScroll:true})
      const work = pendingWork.current; pendingWork.current = null
      if (!work) return
      if (workHandler.current) {workHandler.current(work);return}
      // Local App's Board owner stays mounted. Cloud supplies a machine-switch handshake instead.
      void registry.captureWorkItem(work,new AbortController().signal).then(captured => {
        if (!captured.current()) return
        requestPage({page:"work"});openWorkItem(captured.item)
      }).catch(error => toast(`${words(language).sourceError} (${errorCode(error)})`))
    })
  },[registry,language])
  const close = useCallback(() => {
    if (animating.current) return
    if (folded.current || !motion()) { finish(); return }
    animating.current = true; setClosing(true)
    window.setTimeout(() => { animating.current = false; folded.current = true; setClosing(false); finish() }, 200)
  },[finish])
  const open = (element?: HTMLElement) => {
    opener.current = element ?? null
    // Capture the real opener and scroll before hidden blurs the origin.
    origin.current = {hash:location.hash,href:location.href,state:history.state,x:window.scrollX,y:window.scrollY,scrolls:
      [...document.querySelectorAll<HTMLElement>('#app .scroller, #app .cloud-fleet-scroll')].map(node => [node,node.scrollTop,node.scrollLeft])}
    // The window grows out of the orb (or its docked icon).
    const box = element?.getBoundingClientRect()
    windowOrigin.current = box ? `${Math.round(box.left+box.width/2)}px ${Math.round(box.top+box.height/2)}px` : "100% 100%"
    folded.current = false
    openedRef.current = true; setEntry(value => value+1); setOpened(true)
  }
  // A notice may arrive before source metadata, or focus an already open app.
  const noticeOpened=useRef("")
  useEffect(()=>{
    const follow=()=>{
      const call=location.hash==="#call-bot"
      if(call ? !hasReadableSource : !botFromFragment(location.hash))return
      if(noticeOpened.current===location.hash)return
      noticeOpened.current=location.hash
      if(call&&(browserBotVoiceAvailable()||botCallAudioAvailable()))setVoiceShortcut(count=>count+1)
      if(!openedRef.current)open()
    }
    follow();window.addEventListener("hashchange",follow)
    return()=>window.removeEventListener("hashchange",follow)
  },[hasReadableSource])
  useEffect(()=>{
    const call=(event:KeyboardEvent)=>{
      if(!hasReadableSource||event.defaultPrevented||event.repeat||event.isComposing||!event.altKey||!event.shiftKey||event.ctrlKey||event.metaKey||event.code!=="KeyV"||!(browserBotVoiceAvailable()||botCallAudioAvailable()))return
      event.preventDefault()
      setVoiceShortcut(count=>count+1)
      if(!openedRef.current)open()
    }
    window.addEventListener("keydown",call)
    return()=>window.removeEventListener("keydown",call)
  },[hasReadableSource])
  const [ox,oy] = windowOrigin.current.split(" ")
  const content = opened ? <section data-page-view="project-assistants" aria-label="Bots" className="pa-window" data-closing={closing || undefined}
    style={{position:"fixed",inset:0,zIndex:1000,height:"100dvh",["--pa-ox" as string]:ox,["--pa-oy" as string]:oy}}>
    <Bots key={entry} onRosterObserved={observeCompletions} sources={sources} language={language} accountKey={registry.accountKey} readerID={registry.readerID}
      viewState={registry.viewState} voiceShortcut={voiceShortcut} returnFocus={opener.current} manageHistory onClose={close}
      onOpenWork={work => {if (assistantWorkTarget(work)?.kind !== "board") return;pendingWork.current=work;close()}}/>
  </section> : null
  return {opened,openedRef,content,dock:<BotOrb language={language} {...badge} onOpen={open}/>}
}
