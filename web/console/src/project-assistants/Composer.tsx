import { useRef } from "react"
import { composerEnter } from "./composer-keyboard"
import type { Words } from "./words"
export function Composer({text,onText,onSend,disabled,busy,quote,onClearQuote,w}: {text:string;onText:(s:string)=>void;onSend:()=>void;disabled:boolean;busy:boolean;quote?:string;onClearQuote:()=>void;w:Words}) {
  const composing=useRef(false)
  return <div className="pa-composer-wrap">{quote&&<div className="pa-reply"><span>{quote}</span><button onClick={onClearQuote} aria-label={w.clearQuote}>×</button></div>}
    <form className="pa-composer" onSubmit={e=>{e.preventDefault();if(!disabled&&!busy&&text.trim()&&!composing.current)onSend()}}>
      <div className="pa-editor"><div className="pa-editor-mirror" aria-hidden="true">{text+"\u200b"}</div><textarea rows={1} aria-label={w.compose} placeholder={w.compose+"…"} value={text} disabled={disabled}
        onCompositionStart={()=>{composing.current=true}} onCompositionEnd={()=>{composing.current=false}}
        onChange={e=>onText(e.target.value)} onKeyDown={e=>{const decision=composerEnter({...e.nativeEvent,key:e.key,shiftKey:e.shiftKey,altKey:e.altKey},composing.current);if(decision==="send"){e.preventDefault();if(!disabled&&!busy&&text.trim())onSend()}}}/></div>
      <button type="submit" className="pa-send" aria-label={w.send} disabled={disabled||busy||!text.trim()}>↑</button>
    </form>
  </div>
}
