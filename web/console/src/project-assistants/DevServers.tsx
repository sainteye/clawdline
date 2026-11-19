import {useCallback,useEffect,useRef,useState} from "react"
import {createPortal} from "react-dom"
import type * as W from "@clawdline/contract"
import {showModalWithoutChoice} from "../modal-focus"
import {parenthesized} from "../punctuation"
import {errorCode,type SourceClient} from "./client"
import {MessageMenu} from "./MessageMenu"
import type {Words} from "./words"
import "./dev-servers.css"
import "./message-reactions.css"

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

function listenerPort(port:number):boolean{return Number.isInteger(port)&&port>0&&port<=65535}
export function localServerSource(source:Pick<SourceClient,"browserRemote">,origin:string):boolean {
  if(source.browserRemote!==false)return false
  try{const url=new URL(origin);return ["http:","https:"].includes(url.protocol)&&(url.hostname==="localhost"||url.hostname.endsWith(".localhost")||url.hostname==="[::1]"||/^127\.\d+\.\d+\.\d+$/.test(url.hostname))}catch{return false}
}
/** Use only literal HTTP candidates returned by this source, never log text. */
export function serverAddresses(server:Server,localSource=false):string[]{
  if(server.state!=="running"||server.ports_known!==true||!Array.isArray(server.ports))return []
  const result=new Set<string>()
  for(const port of server.ports){
    if(!port||typeof port!=="object"||!listenerPort(port.port))continue
    const candidates=localSource?[port.local,...(Array.isArray(port.remote)?port.remote:[])]:Array.isArray(port.remote)?port.remote:[]
    for(const candidate of candidates){
      if(typeof candidate!=="string"||candidate!==candidate.trim())continue
      try{
        const url=new URL(candidate),host=url.hostname
        if(url.protocol!=="http:"||url.username||url.password||url.pathname!=="/"||url.search||url.hash||Number(url.port||80)!==port.port)continue
        const ipv4=/^\d+\.\d+\.\d+\.\d+$/.test(host),ipv6=host.startsWith("[")&&host.endsWith("]")&&host.includes(":")
        if(!ipv4&&!ipv6)continue
        if(!localSource){
          if(ipv4){const parts=host.split(".").map(Number);if(parts[0]===0||parts[0]===127||parts[0]>=224||parts[0]===169&&parts[1]===254)continue}
          else if(host==="[::]"||host==="[::1]"||host.startsWith("[::ffff:")||/^\[fe[89ab]/i.test(host)||host.startsWith("[ff"))continue
        }
        result.add(candidate)
      }catch{/* Malformed source data never becomes a navigation target. */}
    }
  }
  return [...result]
}
export function serverPortSummary(server:Server):string {
  if(server.state!=="running"||server.ports_known!==true||!Array.isArray(server.ports))return ""
  const ports=[...new Set(server.ports.flatMap(port=>port&&typeof port==="object"&&listenerPort(port.port)?[port.port]:[]))].sort((a,b)=>a-b)
  return ports.length?":"+ports[0]+(ports.length>1?" +"+(ports.length-1):""):""
}
export function serverCopyFailure(error:unknown,w:Words):string {
  // Clipboard refusals use DOMException names; an adapter may supply a typed
  // code. Preserve that category, without inventing a source-offline failure
  // or exposing arbitrary exception messages in the interface.
  const detail=error&&typeof error==="object"?error as {code?:unknown;name?:unknown}:null
  const category=typeof detail?.code==="string"?detail.code:typeof detail?.name==="string"?detail.name:""
  return w.serverCopyFailed+(/^[A-Za-z][A-Za-z0-9_]*$/.test(category)?parenthesized(category):"")
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
    {rows&&<ul>{rows.map(server=><ServerRow key={server.id} server={server} w={w} active={active} localSource={localServerSource(source,location.href)} canRead={()=>source.current?.()===true} addressesAvailable={!busy&&!inspect&&active} disabled={busy||inspect||!enabled||!active} onLogs={()=>setSelected(server)} onControl={action=>void control(action,server)}/>)}</ul>}
    {selected&&active&&<ServerLogs source={source} bot={bot} server={selected} w={w} onClose={()=>setSelected(null)}/>}
  </section>
}

export function ServerRow({server,w,active,localSource,canRead,addressesAvailable,disabled,onLogs,onControl}:{server:Server;w:Words;active:boolean;localSource:boolean;canRead:()=>boolean;addressesAvailable:boolean;disabled:boolean;onLogs:()=>void;onControl:(action:Action)=>void}){
  const anchor=useRef<HTMLButtonElement>(null),[open,setOpen]=useState(false),[feedback,setFeedback]=useState("")
  const close=(focus:boolean)=>{setOpen(false);if(focus)anchor.current?.focus()}
  useEffect(()=>{if(!active)setOpen(false)},[active])
  useEffect(()=>{setFeedback("");setOpen(false)},[server.attempt,server.revision,server.ports_known,server.ports])
  const addresses=serverAddresses(server,localSource),summary=serverPortSummary(server),readable=active&&addressesAvailable
  const openAddress=(address:string)=>{if(readable&&canRead())window.open(address,"_blank","noopener,noreferrer")}
  const copyAddress=async(address:string)=>{
    if(!readable||!canRead())return
    try{if(!navigator.clipboard)throw new DOMException("Clipboard unavailable","NotSupportedError");await navigator.clipboard.writeText(address);setFeedback(w.serverAddressCopied)}catch(error){setFeedback(serverCopyFailure(error,w))}
  }
  const labels:Record<Action,string>={start:w.serverStart,restart:w.serverRestart,stop:w.serverStop,remove:w.serverRemove}
  return <li className="pa-dev-server" data-failed={server.state==="failed"||undefined}>
    <button className="pa-server-name" type="button" title={addresses.length&&readable?w.serverOpen:w.serverLogs} onClick={()=>{if(addresses.length&&readable)openAddress(addresses[0]);else onLogs()}}><strong>{server.name||server.command}</strong><small>{serverState(server,w)}{summary&&" · "+summary}</small></button>
    <button ref={anchor} className="pa-server-menu" type="button" aria-label={w.serverActions.replace("{name}",server.name||server.command)} aria-haspopup="menu" aria-expanded={open} onClick={()=>setOpen(!open)}>⋯</button>
    <MessageMenu open={open} anchor={anchor} label={w.serverActions.replace("{name}",server.name||server.command)} onClose={close}>
      {addresses.map(address=><div key={address} className="pa-server-address-choice" role="group" aria-label={address}>
        <button type="button" role="menuitem" disabled={!readable} onClick={()=>{close(true);openAddress(address)}}><span>{w.serverOpen}</span><small>{address}</small></button>
        <button type="button" role="menuitem" disabled={!readable} aria-label={w.serverCopyAddress+" · "+address} onClick={()=>{close(true);void copyAddress(address)}}>{w.serverCopyAddress}</button>
      </div>)}
      {server.state==="running"&&!addresses.length&&<p className="pa-server-address-note">{server.ports_known!==true?w.serverPortsUnknown:!localSource&&serverAddresses(server,true).length?w.serverLocalOnly:w.serverNoPorts}</p>}
      <button type="button" role="menuitem" onClick={()=>{close(true);onLogs()}}>{w.serverLogs}</button>
      {serverActions(server).map(action=><button key={action} type="button" role="menuitem" disabled={disabled} onClick={()=>{close(true);onControl(action)}}>{labels[action]}</button>)}
    </MessageMenu>
    {feedback&&<span className="pa-server-copy-status" role="status">{feedback}</span>}
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
