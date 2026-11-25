import { useLayoutEffect, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import { bindNativeModalBackdrop } from "../modal-backdrop.js"
import { errorCode, type SourceClient } from "./client"
import { AgentModelSelect, CatalogModelSelect } from "./AgentModelPicker"
import { settingsSaveFailure } from "./BotSettings"
import { settingsBase, settingsConflicts } from "./settings-conflicts"
import { SelectChevron } from "./PaSelect"
import { catalogRuntime, choiceLabel, nativeDefault, resolvedDefault, splitChoice } from "./agent-model-summary"
import { configurationError, useBotConfiguration } from "./RuntimeSettings"
import { useModelCatalog } from "../model-catalog/useModelCatalog"
import { retryableSourceError } from "./source-errors"
import type { Words } from "./words"

/** A model change as a settings edit. With a base, Bot activity during the edit never refuses it; only a model changed elsewhere conflicts. */
export function modelEdit(original:W.ProjectAssistantBot, model:string, rebased:boolean, overwrite=false):W.ProjectAssistantEditSettings {
  return {expected_version:original.version, name:original.name, appearance:original.appearance, instructions:original.instructions.text,
    runtime_settings:{model,options:{}}, ...(rebased?{base:settingsBase(original),...(overwrite?{overwrite_conflicts:true}:{})}:{})}
}

type SaveModel = (bot: W.ProjectAssistantBot, model: string, overwrite?: boolean) => Promise<void>

export function ChatModel({bot, source, enabled, activeTurn, reported, save, w}: {
  bot: W.ProjectAssistantBot; source: SourceClient; enabled: boolean; activeTurn: boolean;
  reported?: string; save: SaveModel; w: Words
}) {
  const [opener, setOpener] = useState<HTMLButtonElement|null>(null)
  // A saved catalog choice or older ACP value reads as its catalog entry: "gpt-5.5/high" as gpt-5.5 · high, "default" as the Agent default.
  const label = activeTurn ? reported || w.modelNotReported : choiceLabel(bot.runtime_settings?.model, w.nativeDefault)
  // A catalog Agent's choice is checked by the daemon when it is saved; Gemini still needs its own ACP list.
  const editable = enabled && source.canMemorySettings?.() === true && (catalogRuntime(bot.agent) || !!source.configuration)
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
  // The Bot as the dialog opened, or as last loaded; other Bot activity does not replace it.
  const [baseline, setBaseline] = useState(bot)
  const original = baseline.runtime_settings?.model ?? ""
  const [choice, setChoice] = useState(original)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<{code:string;field?:string}>()
  const busy = useRef(false)
  const dialog = useRef<HTMLDialogElement>(null)
  // The machine's model catalog lists the models; Gemini keeps its own ACP list (catalogRuntime).
  const fromCatalog = catalogRuntime(baseline.agent)
  const catalogs = useModelCatalog(fromCatalog && enabled && !saved ? source : undefined)
  // Omitting a draft login profile makes discovery use the Bot's stored profile.
  const {catalog, pending, error:catalogError, field, retry} = useBotConfiguration(source, baseline, baseline.agent, "", undefined, "", !fromCatalog && enabled && !saved)
  const model = catalog?.options.find(option=>option.category==="model")
  const defaultName = model && resolvedDefault(model)
  const changed = fromCatalog ? choice!==original && !(splitChoice(choice).model==="" && splitChoice(original).model==="")
    : choice!==original && (choice!=="" || !model || nativeDefault(model)?.value!==original)
  const ready = fromCatalog || !!model
  // A turn, memory write or routine while the dialog is open does not block saving; the source decides by field.
  const conflict = error?.code==="pa_stale_version"
  const blocked = conflict || error?.code==="pa_outcome_unknown" || error?.code==="pa_stale_generation"
  const rebased = source.canSettingsBase?.()===true
  const conflictText = w.settingsSaveConflict.replace("{fields}", settingsConflicts(error).length ? w.model : w.settings)
  useLayoutEffect(()=>{
    const node = dialog.current!
    bindNativeModalBackdrop(node)
    node.showModal()
    return ()=>{
      node.close()
      if(opener?.isConnected)opener.focus({preventScroll:true})
    }
  },[opener])
  const saveChoice = async(overwrite=false)=>{
    if(!enabled || busy.current || saved || (blocked && !(overwrite && conflict)) || !ready || !changed) return
    busy.current = true
    setSaving(true)
    setError(undefined)
    try {
      await save(baseline, choice, overwrite)
      setSaved(true)
    }catch(e){
      setError({code:errorCode(e),field:(e as Partial<W.ProjectAssistantErrorResponse>|null)?.error?.field})
    }finally{
      busy.current = false
      setSaving(false)
    }
  }
  // The newest Bot this view holds becomes the base; the choice follows it.
  const loadLatest = ()=>{
    if(busy.current) return
    setBaseline(bot)
    setChoice(bot.runtime_settings?.model ?? "")
    setError(undefined)
  }
  return <dialog ref={dialog} className="pa-roster-confirm pa-chat-model-dialog" aria-label={w.changeModel}
    data-modal-dismiss={String(!saving)} onCancel={event=>{event.preventDefault();if(!busy.current)onClose()}}>
    <h2>{w.changeModel}</h2>
    {saved ? <p role="status">{w.chatModelSaved}</p> : <>
      {!enabled ? <p role="status">{w.cannotEdit}</p> :
        fromCatalog ? <CatalogModelSelect agent={baseline.agent} models={catalogs.get(baseline.agent)} retry={catalogs.retry} value={choice} onChange={setChoice} w={w} disabled={saving||blocked}/> :
        pending ? <p role="status">{w.nativeLoading}</p> :
        catalogError ? <div><p role="status">{configurationError(catalogError,w,field)}</p>
          {retryableSourceError(catalogError)&&<button type="button" onClick={retry}>{w.nativeRetry}</button>}
        </div> : model ? <AgentModelSelect model={model} value={choice} onChange={setChoice} w={w} disabled={saving||blocked}
          defaultLabel={defaultName?w.nativeDefaultCurrent.replace("{name}",defaultName):w.nativeDefault}/>
          : <p role="status">{w.nativeNoModels}</p>}
      <p className="pa-approval-detail">{w.nativeNextTurn}</p>
      {error&&<p role="alert">{settingsSaveFailure(error,w,conflictText)}</p>}
      {conflict&&<div className="pa-settings-conflict-actions">
        {rebased&&<button type="button" disabled={saving} onClick={()=>void saveChoice(true)}>{w.settingsConflictKeepMine}</button>}
        <button type="button" disabled={saving} onClick={loadLatest}>{w.settingsConflictLoadLatest}</button>
      </div>}
    </>}
    <div className="pa-actions">
      <button type="button" disabled={saving} onClick={onClose}>{saved?w.close:w.cancel}</button>
      {!saved&&<button type="button" disabled={!enabled||saving||blocked||!ready||!changed}
        aria-busy={saving||undefined} onClick={()=>void saveChoice()}>{saving?w.loading:w.save}</button>}
    </div>
  </dialog>
}
