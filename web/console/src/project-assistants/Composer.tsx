import { useEffect, useRef, useState, type ClipboardEvent, type KeyboardEvent } from "react"
import { createPortal } from "react-dom"
import * as V from "../legacy/voice-bridge.js"
import * as L from "../legacy/bridge.js"
import { toast } from "../overlays/toast.js"
import { voiceSource } from "../session/voice-source.js"
import { ImageMarkup, type MarkupPicture } from "../session/ImageMarkup.js"
import { encodeMarkedPicture } from "../legacy/shots-bridge.js"
import "./bot-mentions.css"
import { useBotMentions, type BotMentionCandidate } from "./BotMentionField"
import type { Words } from "./words"

export function Composer({plain=false,text,onText,onSend,onStop,disabled,stopDisabled=disabled,stopBusy=false,busy,quote,onClearQuote,w,images=[],onAttach,acceptFiles=false,onRemoveImage,onReplaceImage,files=[],onRemoveFile,uploading,onCancelUpload,sendBlocked=false,callActive=false,draftEditable=false,sendTextDuringRecovery=false,mentions,marks,call}: {plain?:boolean;text:string;onText:(s:string)=>void;onSend:()=>void;onStop?:()=>void;disabled:boolean;stopDisabled?:boolean;stopBusy?:boolean;busy:boolean;quote?:string;onClearQuote:()=>void;w:Words;images?:readonly MarkupPicture[];onAttach?:(files:readonly File[])=>void;acceptFiles?:boolean;onRemoveImage?:(id:MarkupPicture["id"])=>void;onReplaceImage?:(id:MarkupPicture["id"],url:string)=>void;files?:readonly {id:string;file:File}[];onRemoveFile?:(id:string)=>void;uploading?:string;onCancelUpload?:()=>void;sendBlocked?:boolean;callActive?:boolean;draftEditable?:boolean;sendTextDuringRecovery?:boolean;mentions?:readonly BotMentionCandidate[];marks?:(display:string)=>readonly {start:number;end:number}[];call?:{label:string;disabled:boolean;start:()=>void}}) {
  const composing=useRef(false),picker=useRef<HTMLInputElement>(null)
  const voiceRow=useRef<HTMLDivElement>(null),editor=useRef<HTMLTextAreaElement>(null)
  const mine=useRef(false),gone=useRef(false),latest=useRef({text,onText})
  latest.current={text,onText}
  const mention=useBotMentions({text,onText,candidates:mentions,editor,w})
  const [markingID,setMarkingID]=useState<MarkupPicture["id"]|null>(null)
  const marking=images.find(picture=>picture.id===markingID)
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
    V.press({host,composer:null,source:voiceSource,pickEngine:true,say:toast,
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
  // Trailing button: stop > call > send. An empty box offers the call, as Codync does.
  const showCall=!showStop&&!!call&&empty&&!callActive
  const placeCall=()=>{
    if(!call||call.disabled)return
    if(mine.current&&V.busy())V.cancel()
    call.start()
  }
  const ready=!marking&&(!disabled||sendTextDuringRecovery)&&!busy&&!sendBlocked&&!callActive&&!voiceBusy&&!empty
  // Enter sends where there is a mouse and therefore probably a keyboard (`L.keyboard()`, the same
  // test the Session composer asks); on a touch screen Enter is a new line and the button sends.
  // Read on every key press, so a tablet that gains a trackpad changes without a reload.
  const keyDown=(event:KeyboardEvent<HTMLTextAreaElement>)=>{
    // Not while an input method is mid-word: Enter there accepts the candidate.
    if(composing.current||event.nativeEvent.isComposing||event.keyCode===229)return
    if(mention.keyDown(event)||event.key!=="Enter"||event.shiftKey||event.altKey)return
    if(!event.metaKey&&!event.ctrlKey&&!L.keyboard())return
    event.preventDefault()
    if(ready&&!event.repeat)onSend()
  }
  const paste=(e:ClipboardEvent<HTMLTextAreaElement>)=>{
    if(e.clipboardData.files.length){e.preventDefault();onAttach?.([...e.clipboardData.files])}
  }
  return <div className="pa-composer-wrap">{quote&&<div className="pa-reply"><span>{quote}</span><button onClick={onClearQuote} aria-label={w.clearQuote}>×</button></div>}
    {images.length>0&&<div className="pa-pictures">{images.map(picture=><span className="pa-picture" key={picture.id}>
      <button type="button" className="pa-picture-preview" aria-label={L.strings.webImagePreview}
        disabled={disabled||busy} onClick={()=>setMarkingID(picture.id)}><img src={picture.url} alt=""/></button>
      <button type="button" className="pa-picture-remove" aria-label={w.removePicture} disabled={busy} onClick={()=>onRemoveImage?.(picture.id)}>×</button>
    </span>)}</div>}
    {marking&&createPortal(
      <ImageMarkup shot={marking} onCancel={()=>setMarkingID(null)} onSave={(canvas,hasMarks)=>{
        if(!hasMarks)return true
        if(disabled||busy||!onReplaceImage)return w.pictureUnavailable
        try{
          const url=encodeMarkedPicture(canvas)
          if(!url.startsWith("data:image/"))return w.pictureUnavailable
          onReplaceImage(marking.id,url)
          return true
        }catch(error){
          return L.failureSentence(error,w.pictureUnavailable)
        }
      }}/>,document.body,
    )}
    {files.length>0&&<div className="pa-input-chips">{files.map(f=><span key={f.id}><span>{f.file.name} · {f.file.size.toLocaleString()} B</span><button type="button" aria-label={w.removeFile+" "+f.file.name} disabled={busy} onClick={()=>onRemoveFile?.(f.id)}>×</button></span>)}</div>}
    {uploading&&<p role="status">{w.uploadingFiles} {uploading} <button type="button" onClick={onCancelUpload}>{w.cancel}</button></p>}
    <div className="voice pa-voice" role="status" hidden ref={voiceRow}/>
    {mention.picker}
    <form className="pa-composer" onSubmit={e=>{e.preventDefault();if(ready&&!composing.current)onSend()}}>
      {onAttach&&<><button type="button" className="pa-attach" aria-label={acceptFiles?w.attachFiles:w.attachPicture} title={acceptFiles?w.attachFiles:w.attachPicture} disabled={disabled||busy} onMouseDown={e=>e.preventDefault()} onClick={()=>picker.current?.click()}><svg viewBox="0 0 24 24" aria-hidden="true" focusable="false"><path d="M12 5v14M5 12h14"/></svg></button>
        <input ref={picker} type="file" accept={acceptFiles?undefined:"image/png,image/jpeg,image/gif,image/webp"} multiple hidden onChange={e=>{onAttach([...(e.target.files??[])]);e.target.value=""}}/></>}
      {!plain&&<button type="button" className="pa-mic" aria-label={recording?L.strings.webVoiceStop:L.strings.webVoiceStart} title={recording?L.strings.webVoiceStop:L.strings.webVoiceStart} aria-pressed={recording} disabled={recording?false:disabled||busy||voiceBusy||callActive} onMouseDown={e=>e.preventDefault()} onClick={pressVoice}>
        <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">{recording?<rect x="7" y="7" width="10" height="10" rx="2" fill="currentColor" stroke="none"/>:<><rect x="9" y="3" width="6" height="11" rx="3"/><path d="M5.75 11.75v.5a6.25 6.25 0 0 0 12.5 0v-.5M12 18.5V21"/></>}</svg>
      </button>}
      <div className="pa-editor"><div className={mentions?.length?"pa-editor-mirror pa-mention-mirror":"pa-editor-mirror"} aria-hidden="true">{marks?markedRuns(mention.display,marks(mention.display)):mentions?.length?mention.marks:text+"​"}</div><textarea ref={editor} rows={1} enterKeyHint={L.keyboard()?"send":"enter"} aria-label={w.compose} placeholder={w.compose+"…"} {...mention.textareaProps} disabled={disabled&&!draftEditable}
        onCompositionStart={()=>{composing.current=true}} onCompositionEnd={()=>{composing.current=false}} onPaste={onAttach?paste:undefined}
        onKeyDown={keyDown}/></div>
      {showCall&&call?<button type="button" className="pa-send" data-call aria-label={call.label} title={call.label}
        disabled={call.disabled||!!marking} onMouseDown={e=>e.preventDefault()} onClick={placeCall}>
        <svg viewBox="0 0 16 16" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" aria-hidden="true" focusable="false"><path d="M2 7v2m3-4v6m3-9v12m3-9v6m3-4v2"/></svg>
      </button>:<button type={showStop?"button":"submit"} className="pa-send" data-stop={showStop||undefined} data-stop-busy={showStop&&stopBusy||undefined}
        aria-label={showStop?w.stop:w.send} title={showStop?w.stop:w.send} aria-busy={showStop&&stopBusy||undefined}
        disabled={showStop?stopDisabled||busy:!ready} onClick={showStop?onStop:undefined}>{showStop?(stopBusy?<span className="pa-send-spinner" aria-hidden="true"/>:null):"↑"}</button>}
    </form>
  </div>
}

/** The draft with the given runs highlighted, for a room that reads @ by its own rule. */
function markedRuns(display:string,runs:readonly {start:number;end:number}[]) {
  const out:(string|ReturnType<typeof mark>)[]=[]
  let at=0
  for(const r of runs){if(r.start<at)continue;if(r.start>at)out.push(display.slice(at,r.start));out.push(mark(display.slice(r.start,r.end),r.start));at=r.end}
  out.push(display.slice(at)+"\u200b")
  return out
}
const mark=(text:string,key:number)=><mark key={key} className="pa-mention-mark">{text}</mark>
