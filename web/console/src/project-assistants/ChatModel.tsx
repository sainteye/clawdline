import { useLayoutEffect, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import { bindNativeModalBackdrop } from "../modal-backdrop.js"
import { errorCode, type SourceClient } from "./client"
import { AgentModelSelect } from "./AgentModelPicker"
import { settingsSaveFailure } from "./BotSettings"
import { SelectChevron } from "./PaSelect"
import { nativeDefault, resolvedDefault } from "./agent-model-summary"
import { configurationError, useModelCatalog } from "./RuntimeSettings"
import { retryableSourceError } from "./source-errors"
import type { Words } from "./words"

type SaveModel = (bot: W.ProjectAssistantBot, model: string) => Promise<void>

export function ChatModel({bot, source, enabled, activeTurn, reported, save, w}: {
  bot: W.ProjectAssistantBot; source: SourceClient; enabled: boolean; activeTurn: boolean;
  reported?: string; save: SaveModel; w: Words
}) {
  const [opener, setOpener] = useState<HTMLButtonElement|null>(null)
  const label = activeTurn ? reported || w.modelNotReported : bot.runtime_settings?.model || w.nativeDefault
  const editable = enabled && source.canMemorySettings?.() === true && !!source.configuration
  return <>
    {editable ? <button className="pa-chat-model" type="button" title={label}
      aria-label={`${w.changeModel} · ${label}`} aria-haspopup="dialog" aria-expanded={!!opener} onClick={event=>setOpener(event.currentTarget)}>
      <bdi>{w.model} · {label}</bdi><SelectChevron size={12}/>
    </button> : <small className="pa-chat-model-name" title={label}><bdi>{w.model} · {label}</bdi></small>}
    {opener && <ModelDialog bot={bot} source={source} enabled={editable} opener={opener} w={w} save={save} onClose={()=>setOpener(null)}/>}
  </>
}

function ModelDialog({bot, source, opener, enabled, w, save, onClose}: {
  bot: W.ProjectAssistantBot; source: SourceClient; opener: HTMLButtonElement|null; enabled: boolean;
  w: Words; save: SaveModel; onClose: ()=>void
}) {
  const baseline = useRef(bot).current
  const original = baseline.runtime_settings?.model ?? ""
  const [choice, setChoice] = useState(original)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<{code:string;field?:string}>()
  const busy = useRef(false)
  const dialog = useRef<HTMLDialogElement>(null)
  // Omitting a draft login profile makes discovery use the Bot's stored profile.
  const {catalog, pending, error:catalogError, field, retry} = useModelCatalog(source, baseline, baseline.agent, "", undefined, "", enabled && !saved)
  const model = catalog?.options.find(option=>option.category==="model")
  const defaultName = model && resolvedDefault(model)
  const changed = choice!==original && (choice!=="" || !model || nativeDefault(model)?.value!==original)
  const stale = baseline.version !== bot.version
  const blocked = stale || error?.code==="pa_outcome_unknown" || error?.code==="pa_stale_version" || error?.code==="pa_stale_generation"
  const saveError = stale&&error?.code!=="pa_outcome_unknown" ? {code:"pa_stale_version"} : error
  useLayoutEffect(()=>{
    const node = dialog.current!
    bindNativeModalBackdrop(node)
    node.showModal()
    return ()=>{
      node.close()
      if(opener?.isConnected)opener.focus({preventScroll:true})
    }
  },[opener])
  const saveChoice = async()=>{
    if(!enabled || busy.current || saved || blocked || !model || !changed) return
    busy.current = true
    setSaving(true)
    setError(undefined)
    try {
      await save(baseline, choice)
      setSaved(true)
    }catch(e){
      setError({code:errorCode(e),field:(e as Partial<W.ProjectAssistantErrorResponse>|null)?.error?.field})
    }finally{
      busy.current = false
      setSaving(false)
    }
  }
  return <dialog ref={dialog} className="pa-roster-confirm pa-chat-model-dialog" aria-label={w.changeModel}
    data-modal-dismiss={String(!saving)} onCancel={event=>{event.preventDefault();if(!busy.current)onClose()}}>
    <h2>{w.changeModel}</h2>
    {saved ? <p role="status">{w.chatModelSaved}</p> : <>
      {!enabled ? <p role="status">{w.cannotEdit}</p> :
        pending ? <p role="status">{w.nativeLoading}</p> :
        catalogError ? <div><p role="status">{configurationError(catalogError,w,field)}</p>
          {retryableSourceError(catalogError)&&<button type="button" onClick={retry}>{w.nativeRetry}</button>}
        </div> : model ? <AgentModelSelect model={model} value={choice} onChange={setChoice} w={w} disabled={saving||blocked}
          defaultLabel={defaultName?w.nativeDefaultCurrent.replace("{name}",defaultName):w.nativeDefault}/>
          : <p role="status">{w.nativeNoModels}</p>}
      <p className="pa-approval-detail">{w.nativeNextTurn}</p>
      {saveError&&<p role="alert">{settingsSaveFailure(saveError,w)}</p>}
    </>}
    <div className="pa-actions">
      <button type="button" disabled={saving} onClick={onClose}>{saved?w.close:w.cancel}</button>
      {!saved&&<button type="button" disabled={!enabled||saving||blocked||!model||!changed}
        aria-busy={saving||undefined} onClick={()=>void saveChoice()}>{saving?w.loading:w.save}</button>}
    </div>
  </dialog>
}
