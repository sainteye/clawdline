import { useRef, useState, type KeyboardEvent } from "react"
import { botFailureText } from "./source-errors"
import type { ReactNode } from "react"
import type * as W from "@clawdline/contract"
import { BotRow } from "./BotRow"
import { botIdentity } from "./view-state"
import type { Words } from "./words"
import { PaSelect } from "./PaSelect"
import { ProjectMark } from "./ProjectMark"
import { groupByProject, projectKey, pinnedGroupKey } from "./roster-groups"
import { LoadingSkeleton } from "../LoadingSkeleton"
import { useShowAfter } from "./PaLoading"
import type { SourceClient } from "./client"
import { Groups } from "./Groups"
import { moveRosterBot, pinRosterBot, reorderKeys } from "./roster-state"
import { useRosterState } from "./useRosterState"
import { RosterHandle, type RosterDrag } from "./RosterHandle"
import "./roster-controls.css"
export function BotRoster({rosterScope,items,machines,selected,query,onQuery,machine,onMachine,onSelect,onCreate,onClose,w,language,loading,errors,retrying={},onRetry,onMore,projectNames,projectIcons,status,machineNames,hidden,sources=[],groupsItems}: {
  rosterScope:string;items:W.ProjectAssistantBotSummary[];machines:readonly string[];selected:string|null;query:string;onQuery:(s:string)=>void;machine:string;onMachine:(s:string)=>void;
  onSelect:(i:W.ProjectAssistantBotSummary)=>void;onCreate?:()=>void;onClose:()=>void;w:Words;language:string;loading:boolean;
  retrying?:Readonly<Record<string,boolean>>;onRetry?:(id:string)=>void;errors:readonly [string,string,boolean][];onMore?:()=>void;projectNames:ReadonlyMap<string,string>;projectIcons?:ReadonlyMap<string,W.Icon>;status?:ReactNode;machineNames?:ReadonlyMap<string,string>;hidden?:ReactNode;sources?:readonly SourceClient[];groupsItems?:readonly W.ProjectAssistantBotSummary[]
}) {
  const {state: rosterState, problem, update} = useRosterState(rosterScope)
  const dragging = useRef<RosterDrag | null>(null)
  const [active, setActive] = useState<RosterDrag | null>(null), [over, setOver] = useState<RosterDrag | null>(null)
  const shown = items.filter(i => !machine || i.bot.key.machine_id === machine)
  const groups = groupByProject(shown, rosterState.pinnedBotKeys, rosterState.projectSortOrder, rosterState.botSortOrder)
  const botKeys = items.map(item => botIdentity(item.bot.key))
  const startDrag = (source: RosterDrag) => {dragging.current = source; setActive(source)}
  const endDrag = () => {dragging.current = null; setActive(null); setOver(null)}
  const drop = (target: RosterDrag, source = dragging.current) => {
    if (!source || source.key === target.key) return
    if (source.kind === "project" && target.kind === "project" && target.key !== pinnedGroupKey) {
      const current = groupByProject(items, rosterState.pinnedBotKeys).filter(g => g.kind === "project").map(g => g.key)
      const full = [...rosterState.projectSortOrder, ...current.filter(key => !rosterState.projectSortOrder.includes(key))]
      update({...rosterState, projectSortOrder: reorderKeys(full, source.key, target.key)})
    } else if (source.kind === "bot") {
      const item = items.find(item => botIdentity(item.bot.key) === source.key)
      if (!item) return
      const group = target.kind === "bot" ? groups.find(g => g.items.some(i => botIdentity(i.bot.key) === target.key)) : groups.find(g => g.key === target.key)
      const pinned = target.key === pinnedGroupKey || group?.kind === "pinned"
      // Moving a row changes its presentation, never the Project it belongs to.
      if (!pinned && group?.key !== projectKey(item.bot.key.machine_id, item.bot.project.project_id)) return
      if (target.kind === "bot") update(moveRosterBot(rosterState, source.key, target.key, pinned, botKeys))
      else update(pinRosterBot(rosterState, source.key, pinned))
    }
  }
  const keyboard = (source: RosterDrag, keys: string[]) => (event: KeyboardEvent<HTMLButtonElement>) => {
    if (event.key === "Escape") {endDrag(); return}
    const index = keys.indexOf(source.key)
    const next = event.key === "ArrowUp" ? index - 1 : event.key === "ArrowDown" ? index + 1 : event.key === "Home" ? 0 : event.key === "End" ? keys.length - 1 : -1
    if (next < 0 || next >= keys.length || index < 0) return
    event.preventDefault(); drop({kind: source.kind, key: keys[next]}, source)
  }
  const targetProps = (target: RosterDrag) => ({
    "data-roster-drop-kind": target.kind, "data-roster-drop-key": target.key,
    "data-dragging": active?.kind === target.kind && active.key === target.key || undefined,
    "data-drop": over?.kind === target.kind && over.key === target.key || undefined
  })
  const handle = (source: RosterDrag, name: string, keys: string[]) => <RosterHandle source={source} label={`${w.rosterReorder}: ${name}`} hint={w.rosterReorderHint} onStart={startDrag} onEnd={endDrag} onDrop={drop} onHover={setOver} onKey={keyboard(source, keys)}/>
  // One machine needs no machine filter and no machine name on every row.
  const several=machines.length>1,named=(id:string)=>machineNames?.get(id)||w.thisMachine
  // The first read draws placeholder rows after 150ms; once Bots are listed, a later read only says so.
  const skeleton=useShowAfter(loading&&!items.length&&!errors.length)
  return <aside className="pa-roster" aria-label={w.allBots}>
    <header className="pa-toolbar"><button className="pa-circle pa-collapse" onClick={onClose} aria-label={w.collapse} title={w.collapse}><svg viewBox="0 0 16 16" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="M3.5 6 8 10.5 12.5 6"/></svg></button><h1>{w.allBots}</h1>{onCreate&&<button className="pa-circle" onClick={onCreate} aria-label={w.create}>+</button>}</header>
    <label className="pa-search"><svg className="pa-search-icon" viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><circle cx="6.5" cy="6.5" r="4.5"/><path d="M10 10l3.5 3.5"/></svg><input type="search" placeholder={w.search} aria-label={w.search} value={query} onChange={e=>onQuery(e.target.value)}/>{query&&<button type="button" className="pa-search-clear" aria-label={w.clearQuote||"Clear"} onClick={()=>onQuery("")}><svg viewBox="0 0 16 16" width="12" height="12" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M4 4l8 8M12 4L4 12"/></svg></button>}</label>
    {several&&<div className="pa-roster-filter"><span>{!items.length&&errors.length?(errors.some(([, ,retryable])=>retryable)?w.sourceConnecting:w.sourceError):<>{w.total} {loading&&!items.length?"…":shown.length}</>}</span><PaSelect compact ariaLabel={w.machine} value={machine} onChange={onMachine} options={[{value:"",label:w.allMachines},...machines.map(m=>({value:m,label:named(m)}))]}/></div>}
    <div className="pa-roster-scroll">{errors.map(([id,code,retryable])=><section role="status" className="pa-source-error pa-connection-notice" key={id}>
      <div className="pa-connection-heading"><strong>{named(id)}</strong>{retryable&&onRetry&&<button type="button" onClick={()=>onRetry(id)} disabled={!!retrying[id]} aria-busy={!!retrying[id]}>{retrying[id]?w.sourceRetrying:w.retry}</button>}</div>
      <p>{
        // refusal-ok: The original typed code remains in the expandable Connection details below.
        code==="pa_offline"||code==="unavailable"?w.sourceOffline:code==="pa_revoked"||code==="pa_unpaired"||code==="pa_scope_denied"?w.sourcePairingRequired:botFailureText(code,w)}</p>
      {retryable&&<p className="pa-connection-hint">{w.sourceRetryHint}</p>}
      <details><summary>{w.sourceDetails}</summary><code>{code}</code></details>
    </section>)}{loading&&items.length>0&&<p role="status">{w.loading}</p>}{skeleton&&<LoadingSkeleton kind="rows" id="bots-roster" label={w.loading}/>}{!loading&&!items.length&&!errors.length&&<p className="pa-empty">{w.empty}</p>}
      <Groups sources={sources} items={groupsItems??items} projectNames={projectNames} projectIcons={projectIcons} query={query} machine={machine} selected={selected}/>
      {problem&&<p role="status" className="pa-roster-save-error">{w.rosterUnsaved}</p>}
      {!groups.some(g => g.kind === "pinned")&&<section className="pa-roster-pin-target" {...targetProps({kind:"project",key:pinnedGroupKey})}>{w.rosterDropPin}</section>}
      {groups.map(group => {
        const pinned = group.kind === "pinned", name = pinned ? w.pinned : projectNames.get(group.key)||w.unnamedProject
        const keys = group.items.map(item => botIdentity(item.bot.key))
        return <section className="pa-project-group" key={group.key} aria-label={name} {...targetProps({kind:"project", key:group.key})}>
          <h2 className="pa-project-heading">{pinned ? <span>{w.pinned}</span> : <><ProjectMark icon={projectIcons?.get(group.key)} size={12}/><span>{name}</span></>}{several&&!pinned&&<small>{named(group.machineID)}</small>}{!pinned&&handle({kind:"project",key:group.key},name,groups.filter(g=>g.kind==="project").map(g=>g.key))}</h2>
          {group.items.map(item => {
            const key = botIdentity(item.bot.key), project = projectKey(item.bot.key.machine_id,item.bot.project.project_id)
            return <div className="pa-roster-entry" key={key} {...targetProps({kind:"bot",key})}>
              <BotRow item={item} grouped={!pinned} selected={selected===key} onSelect={()=>onSelect(item)} w={w} language={language} projectName={projectNames.get(project)} projectIcon={projectIcons?.get(project)} machineName={several?named(item.bot.key.machine_id):undefined}/>
              <div className="pa-roster-actions"><button type="button" className="pa-roster-pin" aria-label={`${pinned?w.rosterUnpin:w.rosterPin}: ${item.bot.name}`} aria-pressed={pinned} title={pinned?w.rosterUnpin:w.rosterPin} onClick={()=>update(pinRosterBot(rosterState,key,!pinned))}><svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true" fill={pinned?"currentColor":"none"} stroke="currentColor" strokeWidth="1.3"><path d="m8 1.7 1.9 3.9 4.3.6-3.1 3 .7 4.3L8 11.4l-3.8 2.1.7-4.3-3.1-3 4.3-.6Z"/></svg></button>{handle({kind:"bot",key},item.bot.name,keys)}</div>
            </div>
          })}
        </section>
      })}
      {onMore&&<button className="pa-pill" onClick={onMore}>{w.more}</button>}{hidden}
    </div>
    {status&&<footer className="pa-status-bar">{status}</footer>}
  </aside>
}
