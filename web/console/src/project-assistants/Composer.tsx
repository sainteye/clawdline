import { useEffect, useRef, useState, type ClipboardEvent, type DragEvent } from "react"
import * as V from "../legacy/voice-bridge.js"
import * as L from "../legacy/bridge.js"
import { toast } from "../overlays/toast.js"
import { voiceSource } from "../session/voice-source.js"
import { composerEnter } from "./composer-keyboard"
import type { Words } from "./words"

/** Pictures one message may carry; the daemon refuses more. */
export const MAX_PICTURES = 4

export function Composer({plain=false,text,onText,onSend,onStop,disabled,stopDisabled=disabled,stopBusy=false,busy,quote,onClearQuote,w,images=[],onImages,onRemoveImage,files=[],onFiles,onRemoveFile,uploading,onCancelUpload,sendBlocked=false,callActive=false,draftEditable=false,onFileError}: {plain?:boolean;text:string;onText:(s:string)=>void;onSend:()=>void;onStop?:()=>void;disabled:boolean;stopDisabled?:boolean;stopBusy?:boolean;busy:boolean;quote?:string;onClearQuote:()=>void;w:Words;images?:readonly string[];onImages?:(files:readonly File[])=>void;onRemoveImage?:(index:number)=>void;files?:readonly {id:string;file:File}[];onFiles?:(files:readonly File[])=>boolean|void;onRemoveFile?:(id:string)=>void;uploading?:string;onCancelUpload?:()=>void;sendBlocked?:boolean;callActive?:boolean;draftEditable?:boolean;onFileError?:(error:string)=>void}) {
  const composing=useRef(false),picker=useRef<HTMLInputElement>(null)
  const voiceRow=useRef<HTMLDivElement>(null),editor=useRef<HTMLTextAreaElement>(null)
  const mine=useRef(false),gone=useRef(false),latest=useRef({text,onText})
  latest.current={text,onText}
  const [voice,setVoice]=useState<V.VoiceState>("off")
  const recording=voice==="recording",voiceBusy=voice!=="off"
  useEffect(()=>{
    gone.current=false
    return ()=>{gone.current=true;if(mine.current&&V.busy())V.cancel()}
  },[])
  const pressVoice=()=>{
    const host=voiceRow.current
    if(!host)return
    if(V.busy()&&!mine.current){toast(L.strings.webVoiceInUse,true);return}
    mine.current=true
    V.press({host,composer:null,source:voiceSource,say:toast,
      guard:()=>gone.current||!host.isConnected,
      changed:next=>{mine.current=next!=="off";if(!gone.current)setVoice(next)},
      sink:said=>{
        if(gone.current||!said.trim())return
        latest.current.onText(V.appendedText(latest.current.text,said.trim()))
        editor.current?.focus({preventScroll:true})
      },
    })
    if(!V.busy())mine.current=false
  }
  const empty=!text.trim()&&!images.length&&!files.length
  const showStop=!!onStop&&empty&&!voiceBusy
  const ready=!disabled&&!busy&&!sendBlocked&&!callActive&&!voiceBusy&&!empty
  const take=(selected:readonly File[])=>{
    if(disabled||busy)return false
    const pictures=selected.filter(f=>f.type.startsWith("image/")),regular=selected.filter(f=>!f.type.startsWith("image/"))
    if(regular.length&&!onFiles){onFileError?.(w.filesUnavailable);return false}
    if(regular.some(f=>!!f.webkitRelativePath)){onFileError?.(w.foldersUnsupported);return false}
    if(onFiles&&regular.length&&onFiles(regular)===false)return false
    if(onImages&&pictures.length)onImages(pictures)
    return !!(onImages&&pictures.length||onFiles&&regular.length)
  }
  const paste=(e:ClipboardEvent<HTMLTextAreaElement>)=>{if(e.clipboardData.files.length){e.preventDefault();take([...e.clipboardData.files])}}
  const drop=(e:DragEvent<HTMLFormElement>)=>{
    if(!onImages&&!onFiles)return
    e.preventDefault()
    if([...e.dataTransfer.items].some(item=>item.webkitGetAsEntry?.()?.isDirectory)){onFileError?.(w.foldersUnsupported);return}
    take([...e.dataTransfer.files])
  }
  return <div className="pa-composer-wrap">{quote&&<div className="pa-reply"><span>{quote}</span><button onClick={onClearQuote} aria-label={w.clearQuote}>×</button></div>}
    {images.length>0&&<div className="pa-pictures">{images.map((src,i)=><span className="pa-picture" key={i}><img src={src} alt={w.picture}/><button type="button" aria-label={w.removePicture} disabled={busy} onClick={()=>onRemoveImage?.(i)}>×</button></span>)}</div>}
    {files.length>0&&<div className="pa-input-chips">{files.map(f=><span key={f.id}><span>{f.file.name} · {f.file.size.toLocaleString()} B</span><button type="button" aria-label={w.removeFile+" "+f.file.name} disabled={busy} onClick={()=>onRemoveFile?.(f.id)}>×</button></span>)}</div>}
    {uploading&&<p role="status">{w.uploadingFiles} {uploading} <button type="button" onClick={onCancelUpload}>{w.cancel}</button></p>}
    <div className="voice pa-voice" role="status" hidden ref={voiceRow}/>
    <form className="pa-composer" onSubmit={e=>{e.preventDefault();if(ready&&!composing.current)onSend()}} onDragOver={e=>{if(onImages||onFiles)e.preventDefault()}} onDrop={drop}>
      {(onImages||onFiles)&&<><button type="button" className="pa-attach" aria-label={onFiles?w.attachFiles:w.attachPicture} title={onFiles?w.attachFiles:w.attachPicture} disabled={disabled||busy} onMouseDown={e=>e.preventDefault()} onClick={()=>picker.current?.click()}><svg viewBox="0 0 24 24" aria-hidden="true" focusable="false"><path d="M12 5v14M5 12h14"/></svg></button>
        <input ref={picker} type="file" accept={onFiles?undefined:"image/png,image/jpeg,image/gif,image/webp"} multiple hidden onChange={e=>{take([...(e.target.files??[])]);e.target.value=""}}/></>}
      {!plain&&<button type="button" className="pa-mic" aria-label={recording?L.strings.webVoiceStop:L.strings.webVoiceStart} title={recording?L.strings.webVoiceStop:L.strings.webVoiceStart} aria-pressed={recording} disabled={recording?false:disabled||busy||voiceBusy||callActive} onMouseDown={e=>e.preventDefault()} onClick={pressVoice}>
        <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">{recording?<rect x="7" y="7" width="10" height="10" rx="2" fill="currentColor" stroke="none"/>:<><rect x="9" y="3" width="6" height="11" rx="3"/><path d="M5.75 11.75v.5a6.25 6.25 0 0 0 12.5 0v-.5M12 18.5V21"/></>}</svg>
      </button>}
      <div className="pa-editor"><div className="pa-editor-mirror" aria-hidden="true">{text+"​"}</div><textarea ref={editor} rows={1} aria-label={w.compose} placeholder={w.compose+"…"} value={text} disabled={disabled&&!draftEditable}
        onCompositionStart={()=>{composing.current=true}} onCompositionEnd={()=>{composing.current=false}} onPaste={onImages||onFiles?paste:undefined}
        onChange={e=>onText(e.target.value)} onKeyDown={e=>{const decision=composerEnter({...e.nativeEvent,key:e.key,shiftKey:e.shiftKey,altKey:e.altKey},composing.current);if(decision==="send"){e.preventDefault();if(ready)onSend()}}}/></div>
      <button type={showStop?"button":"submit"} className="pa-send" data-stop={showStop||undefined} data-stop-busy={showStop&&stopBusy||undefined}
        aria-label={showStop?w.stop:w.send} title={showStop?w.stop:w.send} aria-busy={showStop&&stopBusy||undefined}
        disabled={showStop?stopDisabled||busy:!ready} onClick={showStop?onStop:undefined}>{showStop?(stopBusy?<span className="pa-send-spinner" aria-hidden="true"/>:null):"↑"}</button>
    </form>
  </div>
}
