import { useRef, type ClipboardEvent, type DragEvent } from "react"
import { composerEnter } from "./composer-keyboard"
import type { Words } from "./words"

/** Pictures one message may carry; the daemon refuses more. */
export const MAX_PICTURES = 4

export function Composer({text,onText,onSend,disabled,busy,quote,onClearQuote,w,images=[],onImages,onRemoveImage}: {text:string;onText:(s:string)=>void;onSend:()=>void;disabled:boolean;busy:boolean;quote?:string;onClearQuote:()=>void;w:Words;images?:readonly string[];onImages?:(files:readonly File[])=>void;onRemoveImage?:(index:number)=>void}) {
  const composing=useRef(false),picker=useRef<HTMLInputElement>(null)
  const ready=!disabled&&!busy&&(!!text.trim()||images.length>0)
  const take=(files:readonly File[])=>{const pictures=files.filter(f=>f.type.startsWith("image/"));if(onImages&&pictures.length)onImages(pictures);return pictures.length>0}
  const paste=(e:ClipboardEvent<HTMLTextAreaElement>)=>{if(take([...e.clipboardData.files]))e.preventDefault()}
  const drop=(e:DragEvent<HTMLFormElement>)=>{if(!onImages)return;if(take([...e.dataTransfer.files]))e.preventDefault()}
  return <div className="pa-composer-wrap">{quote&&<div className="pa-reply"><span>{quote}</span><button onClick={onClearQuote} aria-label={w.clearQuote}>×</button></div>}
    {images.length>0&&<div className="pa-pictures">{images.map((src,i)=><span className="pa-picture" key={i}><img src={src} alt={w.picture}/><button type="button" aria-label={w.removePicture} onClick={()=>onRemoveImage?.(i)}>×</button></span>)}</div>}
    <form className="pa-composer" onSubmit={e=>{e.preventDefault();if(ready&&!composing.current)onSend()}} onDragOver={e=>{if(onImages)e.preventDefault()}} onDrop={drop}>
      {onImages&&<><button type="button" className="pa-attach" aria-label={w.attachPicture} title={w.attachPicture} disabled={disabled||images.length>=MAX_PICTURES} onClick={()=>picker.current?.click()}>+</button>
        <input ref={picker} type="file" accept="image/png,image/jpeg,image/gif,image/webp" multiple hidden onChange={e=>{take([...(e.target.files??[])]);e.target.value=""}}/></>}
      <div className="pa-editor"><div className="pa-editor-mirror" aria-hidden="true">{text+"​"}</div><textarea rows={1} aria-label={w.compose} placeholder={w.compose+"…"} value={text} disabled={disabled}
        onCompositionStart={()=>{composing.current=true}} onCompositionEnd={()=>{composing.current=false}} onPaste={onImages?paste:undefined}
        onChange={e=>onText(e.target.value)} onKeyDown={e=>{const decision=composerEnter({...e.nativeEvent,key:e.key,shiftKey:e.shiftKey,altKey:e.altKey},composing.current);if(decision==="send"){e.preventDefault();if(ready)onSend()}}}/></div>
      <button type="submit" className="pa-send" aria-label={w.send} disabled={!ready}>↑</button>
    </form>
  </div>
}
