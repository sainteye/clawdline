import {useCallback,useState} from "react"
import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
import {OutputViewer} from "./OutputViewer"
import type {Words} from "./words"

export function InputCard({message,file,source,w}:{message:W.ProjectAssistantMessage;file:W.ProjectAssistantInputFile;source:SourceClient;w:Words}) {
  const [open,setOpen]=useState(false)
  const close=useCallback(()=>setOpen(false),[])
  return <div className="pa-output-card pa-input-card"><button type="button" disabled={!source.input||!source.canInputs?.()} onClick={()=>setOpen(true)}><strong>▤ {file.name}</strong><small>{w.attachedFile} · {file.bytes.toLocaleString()} B</small></button>{open&&<OutputViewer item={{kind:"input",key:file.id,message,file}} source={source} w={w} onClose={close}/>}</div>
}
