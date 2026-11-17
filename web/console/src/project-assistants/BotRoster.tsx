import { botFailureText } from "./source-errors"
import type { ReactNode } from "react"
import type * as W from "@clawdline/contract"
import { BotRow } from "./BotRow"
import { botIdentity } from "./view-state"
import type { Words } from "./words"
import { PaSelect } from "./PaSelect"
import { ProjectMark } from "./ProjectMark"
import { groupByProject } from "./roster-groups"
import { LoadingSkeleton } from "../LoadingSkeleton"
import { useShowAfter } from "./PaLoading"
import type { SourceClient } from "./client"
import { Groups } from "./Groups"
export function BotRoster({items,machines,selected,query,onQuery,machine,onMachine,onSelect,onCreate,onClose,w,language,loading,errors,retrying={},onRetry,onMore,projectNames,projectIcons,status,machineNames,hidden,sources=[],groupsItems}: {
  items:W.ProjectAssistantBotSummary[];machines:readonly string[];selected:string|null;query:string;onQuery:(s:string)=>void;machine:string;onMachine:(s:string)=>void;
  onSelect:(i:W.ProjectAssistantBotSummary)=>void;onCreate?:()=>void;onClose:()=>void;w:Words;language:string;loading:boolean;
  retrying?:Readonly<Record<string,boolean>>;onRetry?:(id:string)=>void;errors:readonly [string,string,boolean][];onMore?:()=>void;projectNames:ReadonlyMap<string,string>;projectIcons?:ReadonlyMap<string,W.Icon>;status?:ReactNode;machineNames?:ReadonlyMap<string,string>;hidden?:ReactNode;sources?:readonly SourceClient[];groupsItems?:readonly W.ProjectAssistantBotSummary[]
}) {
  const shown=items.filter(i=>!machine||i.bot.key.machine_id===machine)
  // One machine needs no machine filter and no machine name on every row.
  const several=machines.length>1,named=(id:string)=>machineNames?.get(id)||w.thisMachine
  // The first read draws placeholder rows after 150ms; once Bots are listed, a later read only says so.
  const skeleton=useShowAfter(loading&&!items.length&&!errors.length)
  return <aside className="pa-roster" aria-label={w.allBots}>
    <header className="pa-toolbar"><button className="pa-circle pa-collapse" onClick={onClose} aria-label={w.collapse} title={w.collapse}><svg viewBox="0 0 16 16" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="M3.5 6 8 10.5 12.5 6"/></svg></button><h1>{w.allBots}</h1>{onCreate&&<button className="pa-circle" onClick={onCreate} aria-label={w.create}>+</button>}</header>
    <label className="pa-search"><span aria-hidden="true">⌕</span><input type="search" placeholder={w.search} aria-label={w.search} value={query} onChange={e=>onQuery(e.target.value)}/></label>
    {several&&<div className="pa-roster-filter"><span>{!items.length&&errors.length?(errors.some(([, ,retryable])=>retryable)?w.sourceConnecting:w.sourceError):<>{w.total} {loading&&!items.length?"…":shown.length}</>}</span><PaSelect compact ariaLabel={w.machine} value={machine} onChange={onMachine} options={[{value:"",label:w.allMachines},...machines.map(m=>({value:m,label:named(m)}))]}/></div>}
    <div className="pa-roster-scroll">{status}{errors.map(([id,code,retryable])=><section role="status" className="pa-source-error pa-connection-notice" key={id}>
      <div className="pa-connection-heading"><strong>{named(id)}</strong>{retryable&&onRetry&&<button type="button" onClick={()=>onRetry(id)} disabled={!!retrying[id]} aria-busy={!!retrying[id]}>{retrying[id]?w.sourceRetrying:w.retry}</button>}</div>
      <p>{
        // refusal-ok: The original typed code remains in the expandable Connection details below.
        code==="pa_offline"||code==="unavailable"?w.sourceOffline:code==="pa_revoked"||code==="pa_unpaired"||code==="pa_scope_denied"?w.sourcePairingRequired:botFailureText(code,w)}</p>
      {retryable&&<p className="pa-connection-hint">{w.sourceRetryHint}</p>}
      <details><summary>{w.sourceDetails}</summary><code>{code}</code></details>
    </section>)}{loading&&items.length>0&&<p role="status">{w.loading}</p>}{skeleton&&<LoadingSkeleton kind="rows" id="bots-roster" label={w.loading}/>}{!loading&&!items.length&&!errors.length&&<p className="pa-empty">{w.empty}</p>}
      <Groups sources={sources} items={groupsItems??items} projectNames={projectNames} query={query} machine={machine} selected={selected}/>
      {groupByProject(shown).map(group=><section className="pa-project-group" key={group.key} aria-label={projectNames.get(group.key)||w.unnamedProject}>
        <h2 className="pa-project-heading"><ProjectMark icon={projectIcons?.get(group.key)} size={12}/><span>{projectNames.get(group.key)||w.unnamedProject}</span>{several&&<small>{named(group.machineID)}</small>}</h2>
        {group.items.map(item=><BotRow key={botIdentity(item.bot.key)} item={item} grouped selected={selected===botIdentity(item.bot.key)} onSelect={()=>onSelect(item)} w={w} language={language} projectName={projectNames.get(group.key)} machineName={several?named(item.bot.key.machine_id):undefined}/>)}
      </section>)}
      {onMore&&<button className="pa-pill" onClick={onMore}>{w.more}</button>}{hidden}
    </div>
  </aside>
}
