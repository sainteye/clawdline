import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react"
import type * as W from "@clawdline/contract"
import { Bots } from "../project-assistants/Bots.js"
import { BotOrb } from "../project-assistants/BotOrb.js"
import { canRead } from "../project-assistants/view-state.js"
import { ProjectAssistantRegistry, assistantBadge, PROJECT_ASSISTANTS_API_LEVEL } from "../cloud/project-assistants.js"
import type { PageModule } from "./types.js"
import { assistantHistoryBackSteps, assistantWorkTarget } from "../page-route.js"
import { openWorkItem } from "./work/new-item.js"
import { requestPage } from "../overlays/events.js"
import { toast } from "../overlays/toast.js"
import { words } from "../project-assistants/words.js"
import { errorCode } from "../project-assistants/client.js"

// Entry belongs to Sessions. A typed URL must not enable a default-off feature.
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
  const [opened,setOpened] = useState(false), [entry,setEntry] = useState(0)
  const openedRef = useRef(false), opener = useRef<HTMLElement | null>(null)
  const pendingWork = useRef<W.ProjectAssistantWorkLink | null>(null)
  const ownedHistory = useRef("")
  const workHandler = useRef(onOpenWork)
  workHandler.current = onOpenWork
  const windowOrigin = useRef("100% 100%")
  const origin = useRef<{hash:string;href:string;state:unknown;scrolls:[HTMLElement,number,number][];x:number;y:number} | null>(null)
  useEffect(() => {if (opened && typeof history.state?.paView === "string") ownedHistory.current=history.state.paView},[opened,entry])
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
    const control = new AbortController()
    for (const source of sources) void (async () => {
      try {
        const status = await source.status(control.signal)
        const readable = status.sources.some(canRead)
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
        if (!control.signal.aborted) setMetadata(old => ({...old,[source.machineID]:{revision:source.authorizationRevision,readable,items}}))
      } catch { /* Keep the last observed badge on an offline source; revocation is filtered below. */ }
    })()
    return () => control.abort()
  },[sources,opened])
  const current = Object.entries(metadata).filter(([id,data]) => sources.some(source => source.machineID === id && source.authorizationRevision === data.revision))
  const admitted = current.some(([,data]) => data.readable)
  const badge = assistantBadge(current.flatMap(([,data]) => data.items),sources)
  // Bots folds back into the orb before it closes; a second close (the popstate
  // that history.go below triggers) finishes at once.
  const [closing,setClosing] = useState(false)
  const animating = useRef(false), folded = useRef(false)
  const motion = () => !matchMedia("(prefers-reduced-motion: reduce)").matches
  const finish = useCallback(() => {
    // Bots owns both nested entries. The host never pushes a second history stack.
    const steps = assistantHistoryBackSteps(history.state,ownedHistory.current)
    if (steps) { history.go(-steps); return }
    openedRef.current = false; setOpened(false)
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
  useEffect(() => { if (opened && !admitted) close() },[opened,admitted,close])
  const [ox,oy] = windowOrigin.current.split(" ")
  const content = opened ? <section data-page-view="project-assistants" aria-label="Bots" className="pa-window" data-closing={closing || undefined}
    style={{position:"fixed",inset:0,zIndex:1000,height:"100dvh",["--pa-ox" as string]:ox,["--pa-oy" as string]:oy}}>
    <Bots key={entry} sources={sources} language={language} accountKey={registry.accountKey} readerID={registry.readerID}
      viewState={registry.viewState} returnFocus={opener.current} manageHistory onClose={close}
      onOpenWork={work => {if (assistantWorkTarget(work)?.kind !== "board") return;pendingWork.current=work;close()}}/>
  </section> : null
  return {opened,openedRef,content,entry:admitted ? <BotOrb language={language} {...badge} onOpen={open}/> : null,
    dock:admitted ? <BotOrb variant="dock" language={language} {...badge} onOpen={open}/> : null}
}
