import { useEffect, useRef, useState } from "react"
import type { SessionInfoReply, SessionMenu, SessionRow } from "@clawdline/contract"
import { nextWord } from "../next-strings.js"
import * as L from "../legacy/bridge.js"
import { postPress } from "./press-holds.js"
import { pressKey } from "../press-key.js"
import "./remote-permissions.css"

/** Native RC offers do not have a terminal screen. Refresh asks the live worker;
 * answering names its immutable fingerprint, never browser-supplied tool input. */
export function RemotePermissions({ row, write }: {row: SessionRow; write: boolean}) {
  const [offer,setOffer]=useState<{menu:SessionMenu;expect:string}|null>(null)
  const [busy,setBusy]=useState(false)
  const [note,setNote]=useState("")
  const current=useRef("")
  const inFlight=useRef(false)
  const identity=JSON.stringify([row.id,row.execution_generation])
  current.current=identity
  useEffect(()=>{setOffer(null);setNote("");setBusy(false)},[identity])
  const inspect=async()=>{
    if(inFlight.current)return
    inFlight.current=true
    const held=identity;setBusy(true);setOffer(null);setNote("")
    try{
      const response=await fetch("/v1/sessions/"+encodeURIComponent(row.id)+"/info?parts=remote-permission",{credentials:"same-origin"})
      if(!response.ok)throw new Error(nextWord("claudeRemotePermissionUnknown"))
      const {info}=await response.json() as SessionInfoReply
      if(current.current!==held)return
      if(!info?.remotePermissionKnown)throw new Error(nextWord("claudeRemotePermissionUnknown"))
      if(info.remotePermission && info.remotePermissionExpect)setOffer({menu:info.remotePermission,expect:info.remotePermissionExpect})
      else setNote(nextWord("claudeRemotePermissionNone"))
    }catch(error){if(current.current===held)setNote(L.failureSentence(error,nextWord("claudeRemotePermissionUnknown")))}
    finally{inFlight.current=false;if(current.current===held)setBusy(false)}
  }
  const answer=async(n:number)=>{
    if(inFlight.current || !offer || !write)return
    inFlight.current=true
    const held=identity;const selected=offer;setBusy(true)
    // Clear on every attempted answer. A transport failure may have delivered it;
    // only an explicit fresh inspection can offer the request again.
    setOffer(null)
    try{
      const result=await postPress(globalThis.fetch.bind(globalThis),row.id,String(n),selected.expect,pressKey("rc-permission"))
      if(current.current!==held)return
      if(!result.ok)throw new Error(nextWord("claudeRemotePermissionUnknown"))
      setNote(nextWord("claudeRemotePermissionSubmitted"))
    }catch(error){if(current.current===held)setNote(L.failureSentence(error,nextWord("claudeRemotePermissionUnknown")))}
    finally{inFlight.current=false;if(current.current===held)setBusy(false)}
  }
  return <div className="rc-permissions">
    <button type="button" disabled={busy} onClick={()=>void inspect()}>{busy?nextWord("claudeRemotePermissionReading"):nextWord("claudeRemotePermissionRead")}</button>
    {note && <p role="status">{note}</p>}
    {offer && <div className="rc-permission-offer">
      <pre>{offer.menu.question}</pre>
      {offer.menu.options.map(option=><button key={option.n} type="button" disabled={!write||busy||!option.can} onClick={()=>void answer(option.n)}>{option.n===1?nextWord("claudeRemotePermissionAllow"):nextWord("claudeRemotePermissionDeny")}</button>)}
    </div>}
  </div>
}
