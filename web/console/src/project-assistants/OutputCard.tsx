import {useCallback,useState} from "react"
import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
import {OutputViewer} from "./OutputViewer"
import type {Words} from "./words"

export function OutputCard({message,source,w}:{message:W.ProjectAssistantMessage;source:SourceClient;w:Words}) {
  const o=message.output!,[open,setOpen]=useState(false)
  const close=useCallback(()=>setOpen(false),[])
  return <div className="pa-output-card"><button type="button" disabled={!source.output||source.canOutputs?.()===false} onClick={()=>setOpen(true)}><strong>{o.name}</strong><small>{o.artifact_id?w.artifact:w.sharedFile} · {o.bytes.toLocaleString()} B</small></button>{open&&<OutputViewer item={{kind:"output",key:o.artifact_id||o.id,message}} source={source} w={w} onClose={close}/>}</div>
}
