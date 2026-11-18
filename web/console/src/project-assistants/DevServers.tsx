import {useCallback,useEffect,useRef,useState} from "react"
import {createPortal} from "react-dom"
import type * as W from "@clawdline/contract"
import {showModalWithoutChoice} from "../modal-focus"
import {errorCode,type SourceClient} from "./client"
import {MessageMenu} from "./MessageMenu"
import type {Words} from "./words"
import "./dev-servers.css"

type Server=W.ProjectAssistantDevServer
type Action="start"|"restart"|"stop"|"remove"
export function serverState(s:Server,w:Words):string {
  switch(s.state){case "accepted":return w.serverStarting;case "running":return w.serverRunning;case "stopping":return w.serverStopping;case "stopped":return w.serverStopped;case "failed":return w.serverFailed;case "":return w.serverStopped;default:return w.serverUnknown}
}
export function serverActions(s:Server):Action[]{
  switch(s.state){case "running":return ["restart","stop"];case "accepted":return ["stop"];case "stopped":case "failed":case "":return ["start","remove"];default:return []}
}
export function serverControlRequest(key:W.ProjectAssistantBotKey,server:Server,idempotencyKey:string):W.ProjectAssistantDevServerControlRequest {
  return {key,id:server.id,fingerprint:server.fingerprint,attempt:server.attempt,revision:server.revision,idempotency_key:idempotencyKey}
}

/** This source-bound details view never accepts or constructs an executable command. */
export function DevServers({source,bot,enabled,active=true,frontier,w}:{source:SourceClient;bot:W.ProjectAssistantBot;enabled:boolean;active?:boolean;frontier?:number;w:Words}){
  const [rows,setRows]=useState<Server[]|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState(""),[inspect,setInspect]=useState(false),[selected,setSelected]=useState<Server|null>(null)
  const pending=useRef<AbortController|null>(null),mounted=useRef(true),writable=useRef(enabled),readAgain=useRef(false),visible=useRef(active)
  writable.current=enabled;visible.current=active
  const supported=source.canDevServers?.()===true&&!!source.devServers&&!!source.devServerLogs
  useEffect(()=>{mounted.current=true;return()=>{mounted.current=false;pending.current?.abort()}},[])
  useEffect(()=>{if(!enabled&&pending.current){pending.current.abort();setInspect(true)}},[enabled])
  useEffect(()=>{if(!active)setSelected(null)},[active])
  const read=useCallback(async():Promise<void>=>{
    if(pending.current){readAgain.current=true;return}
    if(!source.devServers)return
    readAgain.current=false
    const control=new AbortController();pending.current=control;setBusy(true)
    try{
      const result=await source.devServers(bot.key,control.signal)
      if(mounted.current&&!control.signal.aborted){setRows(result.servers);setInspect(false);setError("")}
    }catch(e){if(mounted.current)setError(errorCode(e))}
    finally{
      if(pending.current===control)pending.current=null
      if(mounted.current){setBusy(false);if(readAgain.current&&visible.current)void read()}
    }
  },[source,bot.key.machine_id,bot.key.bot_id])
  useEffect(()=>{if(active&&supported)void read()},[active,supported,frontier,read])
  const control=async(action:Action,server:Server)=>{
    if(pending.current||inspect||!writable.current||!source.controlDevServer||!active||source.current?.()!==true)return
    const request=serverControlRequest(bot.key,server,crypto.randomUUID())
    const abort=new AbortController();pending.current=abort;setBusy(true);setError("");setInspect(true)
    try{
      await source.controlDevServer(action,request,abort.signal)
      if(mounted.current&&!abort.signal.aborted){pending.current=null;await read()}
    }catch(e){if(mounted.current){setError(errorCode(e));setInspect(true)}}
    finally{if(pending.current===abort)pending.current=null;if(mounted.current)setBusy(false)}
  }
  if(!supported||source.current?.()===false||!error&&(rows===null||rows.length===0))return null
  return <section className="pa-dev-servers" aria-label={w.serversTitle}>
    <header><h3>{w.serversTitle}</h3><button type="button" disabled={busy} onClick={()=>void read()}>{w.serverRefresh}</button></header>
    {busy&&<p role="status">{w.loading}</p>}
    {error&&<p role="alert">{inspect?w.serverInspect:w.unavailable} ({error})</p>}
    {rows&&<ul>{rows.map(server=><ServerRow key={server.id} server={server} w={w} active={active} disabled={busy||inspect||!enabled||!active} onLogs={()=>setSelected(server)} onControl={action=>void control(action,server)}/>)}</ul>}
    {selected&&active&&<ServerLogs source={source} bot={bot} server={selected} w={w} onClose={()=>setSelected(null)}/>}
  </section>
}

function ServerRow({server,w,active,disabled,onLogs,onControl}:{server:Server;w:Words;active:boolean;disabled:boolean;onLogs:()=>void;onControl:(action:Action)=>void}){
  const anchor=useRef<HTMLButtonElement>(null),[open,setOpen]=useState(false)
  const close=(focus:boolean)=>{setOpen(false);if(focus)anchor.current?.focus()}
  useEffect(()=>{if(!active)setOpen(false)},[active])
  const labels:Record<Action,string>={start:w.serverStart,restart:w.serverRestart,stop:w.serverStop,remove:w.serverRemove}
  return <li className="pa-dev-server" data-failed={server.state==="failed"||undefined}>
    <button className="pa-server-name" type="button" onClick={onLogs}><strong>{server.name||server.command}</strong><small>{serverState(server,w)}</small></button>
    <button ref={anchor} className="pa-server-menu" type="button" aria-label={w.serverActions.replace("{name}",server.name||server.command)} aria-haspopup="menu" aria-expanded={open} onClick={()=>setOpen(!open)}>⋯</button>
    <MessageMenu open={open} anchor={anchor} label={w.serverActions.replace("{name}",server.name||server.command)} onClose={close}>
      <button type="button" role="menuitem" onClick={()=>{close(true);onLogs()}}>{w.serverLogs}</button>
      {serverActions(server).map(action=><button key={action} type="button" role="menuitem" disabled={disabled} onClick={()=>{close(true);onControl(action)}}>{labels[action]}</button>)}
    </MessageMenu>
  </li>
}

function ServerLogs({source,bot,server,w,onClose}:{source:SourceClient;bot:W.ProjectAssistantBot;server:Server;w:Words;onClose:()=>void}){
  const dialog=useRef<HTMLDialogElement>(null),[log,setLog]=useState<W.ProjectAssistantDevServerLog|null>(null),[error,setError]=useState(""),[tick,setTick]=useState(0)
  useEffect(()=>{const node=dialog.current;showModalWithoutChoice(node);return()=>node?.close()},[])
  useEffect(()=>{
    const control=new AbortController();setLog(null);setError("")
    void source.devServerLogs!(bot.key,server.id,control.signal).then(result=>{if(!control.signal.aborted){setLog(result.log??null)}}).catch(e=>{if(!control.signal.aborted)setError(errorCode(e))})
    return()=>control.abort()
  },[source,bot.key.machine_id,bot.key.bot_id,server.id,tick])
  return createPortal(<dialog ref={dialog} className="project-assistants pa-server-logs" data-modal-dismiss="true" aria-label={w.serverLogs+" · "+(server.name||server.command)} onCancel={event=>{event.preventDefault();onClose()}}>
    <header><h3>{server.name||server.command}</h3><button type="button" onClick={onClose}>{w.close}</button></header>
    <p className="pa-server-command"><code>{server.command}</code><br/>{server.folder}</p>
    <button type="button" onClick={()=>setTick(t=>t+1)}>{w.serverRefresh}</button>
    {error?<p role="alert">{w.unavailable} ({error})</p>:!log?<p role="status">{w.loading}</p>:<>{log.truncated&&<p role="status">{w.serverLogTail}</p>}<pre>{log.text||w.serverLogEmpty}</pre></>}
  </dialog>,document.body)
}
