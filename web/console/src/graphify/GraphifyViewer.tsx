import { useEffect, useMemo, useRef, useState } from "react"
import { createPortal } from "react-dom"
import type { GraphifyView } from "@clawdline/contract"
import { catalogFormat, currentCatalogTag } from "../catalog.js"
import { failureSentence } from "../legacy/bridge.js"
import { showModalWithoutChoice } from "../modal-focus.js"
import { GRAPHIFY_OPEN, GRAPHIFY_SETTING, graphWord as w, readGraphify, type GraphifyTarget } from "./graphify.js"
import { graphSelection, layoutGraph } from "./layout.js"
import "./graphify.css"

export function GraphifyViewer() {
 const [target, setTarget] = useState<GraphifyTarget | null>(null)
 const [view, setView] = useState<GraphifyView | null>(null)
 const [error, setError] = useState("")
 const [busy, setBusy] = useState(false)
 const [refresh, setRefresh] = useState(0)
 const [search, setSearch] = useState("")
 const [group, setGroup] = useState("*")
 const [selected, setSelected] = useState("")
 const [zoom, setZoom] = useState(1)
 const dialog = useRef<HTMLDialogElement>(null)
 const opener = useRef<HTMLElement | null>(null)
 useEffect(() => {
  const open = (event: Event) => {
   const next = (event as CustomEvent<GraphifyTarget>).detail
   if (!next?.id || !["project", "session"].includes(next.kind)) return
   opener.current = document.activeElement as HTMLElement | null
   setView(null); setError(""); setSearch(""); setGroup("*"); setSelected(""); setZoom(1); setTarget(next)
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
  void readGraphify(target, controller.signal).then(answer => {
   if (!controller.signal.aborted) {setView(answer);setBusy(false)}
  }, reason => { if (!controller.signal.aborted) {setError(failureSentence(reason, w("failed")));setBusy(false)} })
  const off = (event: Event) => { if ((event as CustomEvent<boolean>).detail !== true) {
   controller.abort();setView(null);setBusy(false);setError(w("disabled"))
  } }
  window.addEventListener(GRAPHIFY_SETTING, off)
  return () => { controller.abort();window.removeEventListener(GRAPHIFY_SETTING, off) }
 }, [target, refresh])
 const positions = useMemo(() => layoutGraph(view?.nodes ?? []), [view])
 const filtered = useMemo(() => graphSelection(positions, view?.links ?? [], search, group), [positions, view, search, group])
 const points = new Map(filtered.nodes.map(node => [node.id, node]))
 const node = positions.find(n => n.id === selected)
 const related = view?.links.filter(link => link.source === selected || link.target === selected) ?? []
 const neighbors = new Set(related.flatMap(link => [link.source, link.target]))
 const close = () => dialog.current?.close()
 return createPortal(<dialog ref={dialog} className="graphify-dialog" aria-labelledby="graphify-title" tabIndex={-1}
  onCancel={event => {event.preventDefault();close()}}
  onClose={() => { setTarget(null);setView(null);setError("");
   const button = opener.current?.getClientRects().length ? opener.current : document.getElementById("session-info")
   button?.focus({preventScroll:true})
  }}>
  <header><div><h2 id="graphify-title">{w("title")}</h2>{target?.label && <p>{target.label}</p>}</div>
   <button className="chip" type="button" onClick={close}>{w("close")}</button></header>
  <div className="graphify-body" aria-busy={busy}>
   {busy && <p role="status">{w("loading")}</p>}
   {error && <p role="alert">{error}</p>}
   {view && <>
    <p className={`graphify-status is-${view.state}`} role="status">{w(view.state === "source_matched" ? "matched" : view.state === "stale" ? "stale" : view.state === "missing" ? "missing" : "unverified")}</p>
    <p className="graphify-meta">{w("checked")} {new Date(view.checked_at * 1000).toLocaleString(currentCatalogTag())}</p>
    {!!view.nodes.length && <>
     <p>{catalogFormat("graphifyView", "counts", [view.nodes.length,view.total_nodes,view.links.length,view.total_links])}</p>
     {view.truncated && <p className="graphify-notice">{w("truncated")}</p>}
     <div className="graphify-controls">
      <label>{w("search")}<input type="search" value={search} onChange={event => setSearch(event.target.value)} /></label>
      <label>{w("community")}<select value={group} onChange={event => setGroup(event.target.value)}><option value="*">{w("all")}</option>
       {[...new Set(positions.map(n => n.community))].sort().map(g => <option key={g} value={g}>{g || w("ungrouped")}</option>)}</select></label>
      <div className="graphify-zoom"><button className="chip" type="button" aria-label={w("zoomOut")} onClick={() => setZoom(n => Math.max(1,n-0.5))}>−</button>
       <button className="chip" type="button" onClick={() => setZoom(1)}>{w("fit")}</button>
       <button className="chip" type="button" aria-label={w("zoomIn")} onClick={() => setZoom(n => Math.min(4,n+0.5))}>+</button></div>
     </div>
     <p className="graphify-hint">{w("hint")}</p>
     <div className="graphify-canvas" tabIndex={0} aria-label={w("canvas")}>
      <svg viewBox="0 0 900 600" style={{width:`${zoom*100}%`}} role="group" aria-label={w("canvas")}>
       {filtered.links.map((link,i) => {const a=points.get(link.source)!,b=points.get(link.target)!;return <line key={i} x1={a.x} y1={a.y} x2={b.x} y2={b.y} className={selected && (link.source===selected || link.target===selected) ? "is-related" : ""} />})}
       {filtered.nodes.map(n => <g key={n.id} className={`graphify-node color-${n.color}${selected && !neighbors.has(n.id) ? " is-dim" : ""}`} role="button" tabIndex={0}
        aria-label={`${n.label} · ${n.file}`} aria-pressed={selected===n.id} onClick={() => setSelected(n.id)} onKeyDown={event => {if (event.key === "Enter" || event.key === " ") {event.preventDefault();setSelected(n.id)}}}>
        <title>{n.label} · {n.file}</title><circle cx={n.x} cy={n.y} r={selected===n.id?9:6} />
        <text x={n.x+10} y={n.y+4}>{n.label}</text>
       </g>)}
      </svg>
     </div>
     <details className="graphify-node-list"><summary>{w("nodeList")} ({filtered.nodes.length})</summary><ul>{filtered.nodes.map(n => <li key={n.id}>
      <button type="button" aria-pressed={selected===n.id} onClick={() => setSelected(n.id)}>{n.label}</button><span>{n.file}</span></li>)}</ul></details>
     {node && <section className="graphify-node-detail"><h3>{node.label}</h3><p>{node.file} {node.location}</p><p>{w("community")}: {node.community || w("ungrouped")}</p>
      <h4>{w("relations")}</h4>{!related.length && <p>{w("noRelations")}</p>}
      <ul>{related.map((link,i) => {const other=positions.find(n => n.id === (link.source===node.id?link.target:link.source));return <li key={i}>
       <span>{link.source===node.id?"→":"←"} {link.relation}</span><button type="button" onClick={() => setSelected(other?.id ?? "")}>{other?.label}</button><small>{link.confidence}</small></li>})}</ul></section>}
     <p className="graphify-hint">{w("evidence")}</p>
    </>}
    {view.state === "source_matched" && !view.nodes.length && <p>{w("empty")}</p>}
    {view.state !== "source_matched" && !view.nodes.length && <p>{w("noBuild")}</p>}
   </>}
   <footer><button className="chip" type="button" disabled={busy} onClick={() => {setSelected("");setRefresh(n=>n+1)}}>{w("refresh")}</button></footer>
  </div>
 </dialog>,document.body)
}
