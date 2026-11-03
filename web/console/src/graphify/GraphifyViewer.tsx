import { useEffect, useMemo, useRef, useState } from "react"
import { createPortal } from "react-dom"
import { RefusalError } from "@clawdline/core"
import type { GraphifyView } from "@clawdline/contract"
import { catalogFormat, currentCatalogTag } from "../catalog.js"
import { failureSentence } from "../legacy/bridge.js"
import { showModalWithoutChoice } from "../modal-focus.js"
import { GRAPHIFY_OPEN, GRAPHIFY_SETTING, graphWord as w, readGraphify, buildGraphify, buildFailureWord, type GraphifyTarget } from "./graphify.js"
import { graphSelection, layoutGraph } from "./layout.js"
import SigmaGraph, { GRAPH_COLORS } from "./SigmaGraph.js"
import { WorkingIndicator } from "../ui/WorkingIndicator.js"
import "./graphify.css"

export function GraphifyViewer() {
 const [target, setTarget] = useState<GraphifyTarget | null>(null)
 const [view, setView] = useState<GraphifyView | null>(null)
 const [error, setError] = useState("")
 const [starting,setStarting]=useState(false)
 const buildRequest=useRef<AbortController | null>(null)
 const [busy, setBusy] = useState(false)
 const [refresh, setRefresh] = useState(0)
 const [search, setSearch] = useState("")
 const [group, setGroup] = useState("*")
 const [selected, setSelected] = useState("")
 const dialog = useRef<HTMLDialogElement>(null)
 const opener = useRef<HTMLElement | null>(null)
 useEffect(() => {
  const open = (event: Event) => {
   const next = (event as CustomEvent<GraphifyTarget>).detail
   if (!next?.id || !["project", "session"].includes(next.kind)) return
   opener.current = document.activeElement as HTMLElement | null
   setView(null); setError(""); setSearch(""); setGroup("*"); setSelected(""); setTarget(next)
   setRefresh(value => value + 1)
   showModalWithoutChoice(dialog.current)
  }
  window.addEventListener(GRAPHIFY_OPEN, open)
  return () => window.removeEventListener(GRAPHIFY_OPEN, open)
 }, [])
 useEffect(() => {
  if (!target) return
  const controller = new AbortController()
  setBusy(true);setView(null);setError("")
  let timer:ReturnType<typeof setTimeout> | undefined
  const load=async()=>{
   try{
    const answer=await readGraphify(target,controller.signal)
    if (controller.signal.aborted)return
    setView(answer);setBusy(false)
    // Poll only an accepted, running operation. Reads never start or retry work.
    if(answer.build?.state==="running")timer=setTimeout(()=>void load(),1000)
   }catch(reason){if(!controller.signal.aborted){setError(failureSentence(reason,w("failed")));setBusy(false)}}
  }
  void load()
  const off = (event: Event) => { if ((event as CustomEvent<boolean>).detail !== true) {
   controller.abort();clearTimeout(timer);buildRequest.current?.abort();setStarting(false);setView(null);setBusy(false);setError(w("disabled"))
  } }
  window.addEventListener(GRAPHIFY_SETTING, off)
  return () => { controller.abort();clearTimeout(timer);buildRequest.current?.abort();window.removeEventListener(GRAPHIFY_SETTING, off) }
 }, [target, refresh])
 const positions = useMemo(() => layoutGraph(view?.nodes ?? []), [view])
 const filtered = useMemo(() => graphSelection(positions, view?.links ?? [], search, group), [positions, view, search, group])
 const visible = useMemo(() => new Set(filtered.nodes.map(node => node.id)), [filtered])
 const communities = [...new Set(positions.map(node => node.community))].sort()
 const node = positions.find(n => n.id === selected)
 const related = view?.links.filter(link => link.source === selected || link.target === selected) ?? []
 const build=async()=>{
  if(!target || starting)return
  const controller=new AbortController();buildRequest.current=controller;setStarting(true);setError("")
  try {await buildGraphify(target,controller.signal);if(!controller.signal.aborted)setRefresh(n=>n+1)}
  catch(reason){if(!controller.signal.aborted)setError(reason instanceof RefusalError && reason.code.startsWith("graphify_")?buildFailureWord(reason.code.slice(9)):failureSentence(reason,w("buildFailed")))}
  finally{if(!controller.signal.aborted)setStarting(false)}
 }
 const close = () => dialog.current?.close()
 return createPortal(<dialog ref={dialog} className="graphify-dialog" aria-labelledby="graphify-title" tabIndex={-1}
  onCancel={event => {event.preventDefault();close()}}
  onClose={() => { setTarget(null);setView(null);setError("");setStarting(false);
   const button = opener.current?.getClientRects().length ? opener.current : document.getElementById("detail-info")
   button?.focus({preventScroll:true})
  }}>
  <header className="graphify-header">
   <div className="graphify-heading"><span className="graphify-mark" aria-hidden="true"><svg viewBox="0 0 32 32" fill="none"><path d="M8 9L24 14L12 25Z" stroke="currentColor" strokeWidth="1.5"/><circle cx="8" cy="9" r="4" fill="currentColor"/><circle cx="24" cy="14" r="3" fill="currentColor"/><circle cx="12" cy="25" r="3" fill="currentColor"/></svg></span>
    <div><p className="graphify-eyebrow">{w("eyebrow")}</p><h2 id="graphify-title">{w("title")}</h2>{target?.label && <p className="graphify-project">{target.label}</p>}</div>
   </div>
   <button className="graphify-close" type="button" aria-label={w("close")} onClick={close}>×</button>
  </header>
  <div className="graphify-body" aria-busy={busy}>
   {busy && <div className="graphify-empty"><WorkingIndicator detail={w("loading")} /></div>}
   {error && <p className="graphify-error" role="alert">{error}</p>}
   {view && <>
    <div className="graphify-summary">
     <span className={`graphify-state is-${view.state}`}><i aria-hidden="true" />{w(view.build?.state === "running" ? "indexBuilding" : view.state === "source_matched" ? "sourceMatched" : view.state === "stale" ? "sourceStale" : view.state === "missing" ? "sourceMissing" : "sourceUnverified")}</span>
     {!!view.nodes.length && <span className="graphify-counts">{catalogFormat("graphifyView", "counts", [view.nodes.length,view.total_nodes,view.links.length,view.total_links])}</span>}
     <span className="graphify-meta">{w("checked")} {new Date(view.checked_at * 1000).toLocaleString(currentCatalogTag())}</span>
    </div>
    <p className={`graphify-status is-${view.state}`} role="status">{w(view.state === "source_matched" ? "matched" : view.state === "stale" ? "stale" : view.state === "missing" ? "missing" : "unverified")}</p>
    {!!view.nodes.length && <>
     <div className="graphify-controls">
      <label className="graphify-search"><span>{w("search")}</span><div><svg aria-hidden="true" viewBox="0 0 20 20" fill="none"><circle cx="8" cy="8" r="5" stroke="currentColor" strokeWidth="1.5"/><path d="m12 12 5 5" stroke="currentColor" strokeWidth="1.5"/></svg><input type="search" placeholder={w("searchPlaceholder")} value={search} onChange={event => {setSearch(event.target.value);setSelected("")}} /></div></label>
      <label className="graphify-community"><span>{w("community")}</span><select value={group} onChange={event => {setGroup(event.target.value);setSelected("")}}><option value="*">{w("all")}</option>{communities.map(g => <option key={g} value={g}>{g || w("ungrouped")}</option>)}</select></label>
     </div>
     <div className="graphify-workspace">
      <section className="graphify-map" aria-label={w("canvas")}>
       <div className="graphify-map-heading"><span>{w("overview")}</span><span>{catalogFormat("graphifyView","visibleCounts",[filtered.nodes.length,filtered.links.length])}</span></div>
       <SigmaGraph nodes={positions} links={view.links} visible={visible} selected={selected} onSelect={setSelected} />
       {!filtered.nodes.length && <p className="graphify-no-results" role="status">{w("noResults")}</p>}
       <div className="graphify-legend" aria-label={w("community")}>{communities.map((g,i) => <button key={g} type="button" aria-pressed={group===g} onClick={()=>{setGroup(group===g?"*":g);setSelected("")}}><i style={{background:GRAPH_COLORS[i%8]}} aria-hidden="true"/>{g || w("ungrouped")}</button>)}</div>
      </section>
      <aside className={`graphify-inspector${node?" has-selection":""}`} aria-label={w("inspector")}>
       {node ? <section className="graphify-node-detail">
        <div className="graphify-inspector-heading"><span>{w("selectedNode")}</span><button type="button" aria-label={w("clearSelection")} onClick={()=>setSelected("")}>×</button></div>
        <h3>{node.label}</h3><p className="graphify-file">{node.file || w("noSource")} {node.location}</p><span className="graphify-node-community"><i style={{background:GRAPH_COLORS[node.color]}} aria-hidden="true"/>{w("community")} {node.community || w("ungrouped")}</span>
        <h4>{w("relations")} <span>{related.length}</span></h4>{!related.length && <p className="graphify-hint">{w("noRelations")}</p>}
        <ul>{related.map((link,i) => {const other=positions.find(n => n.id === (link.source===node.id?link.target:link.source));return <li key={i}><span className="graphify-relation">{link.source===node.id?"→":"←"} {link.relation}</span><button type="button" onClick={() => setSelected(other?.id ?? "")}>{other?.label}</button><small>{link.confidence}</small></li>})}</ul>
       </section> : <div className="graphify-inspector-empty"><span aria-hidden="true">⌖</span><h3>{w("exploreTitle")}</h3><p>{w("exploreHint")}</p></div>}
       <details className="graphify-node-list"><summary>{w("nodeList")} <span>{filtered.nodes.length}</span></summary><ul>{filtered.nodes.map(n => <li key={n.id}><button type="button" aria-pressed={selected===n.id} onClick={() => setSelected(n.id)}><i style={{background:GRAPH_COLORS[n.color]}} aria-hidden="true"/><span>{n.label}<small>{n.file || w("noSource")}</small></span></button></li>)}</ul></details>
      </aside>
     </div>
     <div className="graphify-notes">{view.truncated && <p className="graphify-notice">{w("truncated")}</p>}{!!view.unverified_sources && <p>{catalogFormat("graphifyView","unverifiedSources",[view.unverified_sources])}</p>}<details><summary>{w("aboutGraph")}</summary><p>{w("evidence")}</p></details></div>
    </>}
    {view.state === "source_matched" && !view.nodes.length && <p className="graphify-empty">{w("empty")}</p>}
    {view.build?.state==="running" && <section className="graphify-build graphify-empty"><WorkingIndicator detail={w(`buildPhase_${view.build.phase}`)}><p>{catalogFormat("graphifyView","buildCounts",[view.build.files,(view.build.bytes/1048576).toFixed(1)])}</p></WorkingIndicator><p className="graphify-hint">{w("buildContinues")}</p></section>}
    {view.build?.state==="failed" && <p className="graphify-error" role="alert">{buildFailureWord(view.build.error)}</p>}
    {view.can_build && <section className="graphify-build graphify-empty"><h3>{w("buildTitle")}</h3><p>{w("buildScope")}</p><button className="graphify-primary" type="button" disabled={starting} onClick={()=>void build()}>{starting?w("buildStarting"):view.build?.state==="failed"?w("buildRetry"):w("build")}</button></section>}
    {view.state !== "source_matched" && !view.nodes.length && !view.can_build && view.build?.state!=="running" && <p className="graphify-empty">{w("noBuild")}</p>}
   </>}
   <footer><span>{w("localOnly")}</span><button className="chip" type="button" disabled={busy || starting} onClick={() => {setSelected("");setRefresh(n=>n+1)}}>{w("refresh")}</button></footer>
  </div>
 </dialog>,document.body)
}
