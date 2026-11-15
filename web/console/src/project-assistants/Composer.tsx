import { useRef, type ClipboardEvent, type DragEvent } from "react"
import { composerEnter } from "./composer-keyboard"
import type { Words } from "./words"

/** Pictures one message may carry; the daemon refuses more. */
export const MAX_PICTURES = 4

export function Composer({text,onText,onSend,disabled,busy,quote,onClearQuote,w,images=[],onImages,onRemoveImage,files=[],onFiles,onRemoveFile,uploading,onCancelUpload,sendBlocked=false,onFileError}: {text:string;onText:(s:string)=>void;onSend:()=>void;disabled:boolean;busy:boolean;quote?:string;onClearQuote:()=>void;w:Words;images?:readonly string[];onImages?:(files:readonly File[])=>void;onRemoveImage?:(index:number)=>void;files?:readonly {id:string;file:File}[];onFiles?:(files:readonly File[])=>boolean|void;onRemoveFile?:(id:string)=>void;uploading?:string;onCancelUpload?:()=>void;sendBlocked?:boolean;onFileError?:(error:string)=>void}) {
  const composing=useRef(false),picker=useRef<HTMLInputElement>(null)
  const ready=!disabled&&!busy&&!sendBlocked&&(!!text.trim()||images.length>0||files.length>0)
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
    <form className="pa-composer" onSubmit={e=>{e.preventDefault();if(ready&&!composing.current)onSend()}} onDragOver={e=>{if(onImages||onFiles)e.preventDefault()}} onDrop={drop}>
      {(onImages||onFiles)&&<><button type="button" className="pa-attach" aria-label={onFiles?w.attachFiles:w.attachPicture} title={onFiles?w.attachFiles:w.attachPicture} disabled={disabled||busy} onClick={()=>picker.current?.click()}>+</button>
        <input ref={picker} type="file" accept={onFiles?undefined:"image/png,image/jpeg,image/gif,image/webp"} multiple hidden onChange={e=>{take([...(e.target.files??[])]);e.target.value=""}}/></>}
      <div className="pa-editor"><div className="pa-editor-mirror" aria-hidden="true">{text+"​"}</div><textarea rows={1} aria-label={w.compose} placeholder={w.compose+"…"} value={text} disabled={disabled}
        onCompositionStart={()=>{composing.current=true}} onCompositionEnd={()=>{composing.current=false}} onPaste={onImages||onFiles?paste:undefined}
        onChange={e=>onText(e.target.value)} onKeyDown={e=>{const decision=composerEnter({...e.nativeEvent,key:e.key,shiftKey:e.shiftKey,altKey:e.altKey},composing.current);if(decision==="send"){e.preventDefault();if(ready)onSend()}}}/></div>
      <button type="submit" className="pa-send" aria-label={w.send} disabled={!ready}>↑</button>
    </form>
  </div>
}
