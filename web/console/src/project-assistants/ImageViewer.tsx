import {useEffect,useRef,useState} from "react"
import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
import {errorCode} from "./client"
import {loadOutput} from "./outputs"
import {useModalBackdrop} from "../modal-backdrop.js"
import type {Words} from "./words"

/**
 * A Bot's shared picture, full size, as Codync's AttachmentViewer shows it:
 * the name and download beside close, and the picture itself. It opens on the
 * bytes the chat card already holds, so nothing is fetched again; only a tap
 * before the thumbnail arrived loads it, with a spinner rather than a file
 * transfer. The page disables pinch zoom, so the picture zooms itself with a
 * transform: pinch, double tap, ctrl/⌘ + wheel, trackpad pinch and + − 0 zoom,
 * and a drag moves it once it is zoomed in (see `useImageZoom`).
 */
export function ImageViewer({message,source,w,picture,onClose}:{message:W.ProjectAssistantMessage;source:SourceClient;w:Words;picture:Blob|null;onClose:()=>void}) {
  const output=message.output!
  const [blob,setBlob]=useState<Blob|null>(picture),[url,setURL]=useState(""),[error,setError]=useState(""),[attempt,setAttempt]=useState(0)
  const close=useRef<HTMLButtonElement>(null)
  const backdrop=useModalBackdrop(onClose)
  const stage=useImageZoom(onClose)
  useEffect(()=>{close.current?.focus()},[])
  useEffect(()=>{
    const escape=(e:KeyboardEvent)=>{if(e.key==="Escape"&&!e.defaultPrevented){e.stopImmediatePropagation();onClose()}}
    document.addEventListener("keydown",escape)
    return()=>document.removeEventListener("keydown",escape)
  },[onClose])
  useEffect(()=>{
    if(blob)return
    const control=new AbortController()
    setError("")
    const request={key:message.fence.key,conversation_id:message.fence.conversation_id,message_id:message.id,id:output.id,artifact_id:"",offset:0}
    void loadOutput(source,request,control.signal).then(result=>{
      if(!control.signal.aborted&&source.current?.()!==false)setBlob(result.blob)
    },e=>{if(!control.signal.aborted)setError(errorCode(e))})
    return()=>control.abort()
  },[blob,source,message,output.id,attempt])
  useEffect(()=>{
    if(!blob)return
    const made=URL.createObjectURL(blob)
    setURL(made)
    return()=>{URL.revokeObjectURL(made);setURL("")}
  },[blob])
  const save=()=>{
    if(!blob||source.current?.()===false)return
    const href=URL.createObjectURL(blob),a=document.createElement("a")
    a.href=href;a.download=output.name.replace(/[\/\\]/g,"_");a.click()
    setTimeout(()=>URL.revokeObjectURL(href),0)
  }
  return <div className="pa-output-scrim pa-image-viewer-scrim" {...backdrop}>
    <div className="pa-image-viewer" role="dialog" aria-modal="true" aria-label={output.name}>
      <header>
        <h3>{output.name}</h3>
        <button type="button" disabled={!blob} onClick={save}>{w.downloadFile}</button>
        <button type="button" ref={close} onClick={onClose}>{w.close}</button>
      </header>
      <div className="pa-image-viewer-stage" ref={stage}>
        {url?<img src={url} alt={output.name} draggable={false}/>:
          error?<p role="alert">{w.outputFailed} ({error}) <button type="button" onClick={()=>setAttempt(n=>n+1)}>{w.retry}</button></p>:
          <span className="pa-send-spinner pa-image-viewer-spinner" role="status" aria-label={w.openingOutput}/>}
      </div>
    </div>
  </div>
}

/** How big the picture is drawn relative to its fit size, and how far its center is from the box's center. */
type Zoom={scale:number;x:number;y:number}
type Point={x:number;y:number}
const FIT:Zoom={scale:1,x:0,y:0}
// Codync's viewer: up to 8× the fit size, and a double tap goes to 2.5× (apps/desktop/src/renderer/lib/image-zoom.ts:16-18).
// A wide picture fit to a phone is far below its own size, so it may also reach twice its own pixels.
const MAX_FIT_SCALE=8,OWN_SIZE_SCALE=2,DOUBLE_TAP_SCALE=2.5,KEY_STEP=1.25
// A pinch past either end gives way a little and springs back on release.
const STRETCH=0.3
const TAP_SLOP=10,DOUBLE_TAP_MS=300,DOUBLE_TAP_SLOP=40

const limit=(value:number,low:number,high:number)=>Math.min(Math.max(value,low),high)

/**
 * Pinch, pan and double tap for the stage's picture. The zoom lives in a ref and
 * is written straight to the picture's transform, so following a finger never
 * re-renders React. A tap on the stage outside the picture, at fit size, closes.
 */
function useImageZoom(onClose:()=>void) {
  const closeRef=useRef(onClose)
  closeRef.current=onClose
  const [box,setBox]=useState<HTMLDivElement|null>(null)
  useEffect(()=>{
    if(!box)return
    // What the finger has asked for (may be past the limits mid-pinch) and what is drawn.
    let wanted:Zoom=FIT,shown:Zoom=FIT
    const pointers=new Map<number,Point>()
    let press:{x:number;y:number;target:EventTarget|null}|null=null,multi=false,lastTap:{x:number;y:number;at:number}|null=null
    const picture=()=>box.querySelector("img")
    const measure=()=>{
      const img=picture(),rect=box.getBoundingClientRect()
      const size={width:img?.offsetWidth??0,height:img?.offsetHeight??0}
      const own=img&&size.width>0?img.naturalWidth/size.width:1
      return {
        box:{width:box.clientWidth,height:box.clientHeight},picture:size,
        max:Math.max(MAX_FIT_SCALE,own*OWN_SIZE_SCALE),
        center:(x:number,y:number):Point=>({x:x-rect.left-rect.width/2,y:y-rect.top-rect.height/2}),
      }
    }
    type M=ReturnType<typeof measure>
    const clamp=(z:Zoom,m:M,stretch=false):Zoom=>{
      let scale=z.scale
      if(stretch){
        if(scale<1)scale=1-(1-scale)*STRETCH
        else if(scale>m.max)scale=m.max+(scale-m.max)*STRETCH
      }else scale=limit(scale,1,m.max)
      const reach=(boxSide:number,side:number)=>Math.max(0,(side*scale-boxSide)/2)
      const rx=reach(m.box.width,m.picture.width),ry=reach(m.box.height,m.picture.height)
      return {scale,x:limit(z.x,-rx,rx)||0,y:limit(z.y,-ry,ry)||0}
    }
    const zoomBy=(z:Zoom,factor:number,at:Point,m:M,stretch=false):Zoom=>{
      const scale=stretch?z.scale*factor:limit(z.scale*factor,1,m.max),ratio=scale/z.scale
      return clamp({scale,x:at.x-(at.x-z.x)*ratio,y:at.y-(at.y-z.y)*ratio},m,stretch)
    }
    const draw=(next:Zoom,animate=false)=>{
      const img=picture()
      shown=next
      box.toggleAttribute("data-zoomed",next.scale>1.001)
      if(!img)return
      img.toggleAttribute("data-animate",animate)
      img.style.transform=next.scale===1&&next.x===0&&next.y===0?"":`translate(${next.x}px,${next.y}px) scale(${next.scale})`
      img.dataset.scale=next.scale.toFixed(3)
    }
    const set=(next:Zoom,animate=false)=>{wanted=next;draw(next,animate)}
    // Released: whatever stretched past a limit springs back inside it.
    const settle=()=>{
      const m=measure(),back=clamp(shown,m)
      if(back.scale!==shown.scale||back.x!==shown.x||back.y!==shown.y)set(back,true)
      else wanted=shown
    }
    const reset=()=>{wanted=FIT;draw(FIT)}
    const toggle=(at:Point)=>{
      const m=measure()
      set(shown.scale>1.001?FIT:zoomBy(shown,DOUBLE_TAP_SCALE,m.center(at.x,at.y),m),true)
    }

    const down=(e:PointerEvent)=>{
      if(e.pointerType==="mouse"&&e.button!==0)return
      if(!picture())return
      try{box.setPointerCapture(e.pointerId)}catch{/* a synthetic pointer cannot be captured */}
      pointers.set(e.pointerId,{x:e.clientX,y:e.clientY})
      if(pointers.size===1){press={x:e.clientX,y:e.clientY,target:e.target};multi=false;wanted=shown;draw(shown)}
      else multi=true
    }
    const move=(e:PointerEvent)=>{
      if(!pointers.has(e.pointerId))return
      if(e.pointerType==="mouse"&&e.buttons===0){pointers.delete(e.pointerId);return}
      const before=[...pointers.values()]
      pointers.set(e.pointerId,{x:e.clientX,y:e.clientY})
      const after=[...pointers.values()],m=measure()
      if(press&&Math.hypot(e.clientX-press.x,e.clientY-press.y)>TAP_SLOP)press=null
      if(after.length>=2){
        const [a0,b0]=before,[a1,b1]=after
        const from=Math.hypot(a0.x-b0.x,a0.y-b0.y),to=Math.hypot(a1.x-b1.x,a1.y-b1.y)
        const o0={x:(a0.x+b0.x)/2,y:(a0.y+b0.y)/2},o1={x:(a1.x+b1.x)/2,y:(a1.y+b1.y)/2}
        const zoomed=zoomBy(wanted,from>0?to/from:1,m.center(o0.x,o0.y),m,true)
        wanted={...zoomed,x:zoomed.x+o1.x-o0.x,y:zoomed.y+o1.y-o0.y}
        draw(clamp(wanted,m,true))
      }else if(after.length===1&&!multi&&shown.scale>1.001){
        set(clamp({...shown,x:shown.x+after[0].x-before[0].x,y:shown.y+after[0].y-before[0].y},m))
      }
    }
    const up=(e:PointerEvent)=>{
      if(!pointers.delete(e.pointerId))return
      if(pointers.size>0)return
      settle()
      const tap=e.type==="pointerup"&&press&&!multi?press:null
      press=null
      if(!tap)return
      const now=performance.now()
      if(lastTap&&now-lastTap.at<DOUBLE_TAP_MS&&Math.hypot(tap.x-lastTap.x,tap.y-lastTap.y)<DOUBLE_TAP_SLOP){
        lastTap=null
        if(tap.target instanceof HTMLImageElement||shown.scale>1.001)toggle(tap)
        return
      }
      lastTap={x:tap.x,y:tap.y,at:now}
      // Only a tap on the empty stage at fit size closes; zoomed in, a stray tap never does.
      if(!(tap.target instanceof HTMLImageElement)&&shown.scale<=1.001)closeRef.current()
    }
    // Not a React handler: a trackpad pinch arrives as ctrl + wheel, and the page must not zoom with it.
    const wheel=(e:WheelEvent)=>{
      if(!picture())return
      e.preventDefault()
      const m=measure()
      if(e.ctrlKey||e.metaKey){
        const pixels=e.deltaMode===1?e.deltaY*16:e.deltaMode===2?e.deltaY*box.clientHeight:e.deltaY
        set(zoomBy(shown,Math.exp(-limit(pixels,-50,50)*0.01),m.center(e.clientX,e.clientY),m))
      }else if(shown.scale>1.001)set(clamp({...shown,x:shown.x-e.deltaX,y:shown.y-e.deltaY},m))
    }
    // Safari's own pinch events: a Mac trackpad sends only these. On a phone the pointers already handle two fingers.
    let gestureStart:Zoom|null=null
    type Gesture=Event&{scale:number;clientX:number;clientY:number}
    const gesture=(e:Event)=>{
      e.preventDefault()
      if(pointers.size>0||!picture())return
      const g=e as Gesture,m=measure()
      if(e.type==="gesturestart")gestureStart=shown
      else if(e.type==="gesturechange"&&gestureStart)set(zoomBy(gestureStart,g.scale,m.center(g.clientX,g.clientY),m))
      else gestureStart=null
    }
    const key=(e:KeyboardEvent)=>{
      if(e.defaultPrevented||e.metaKey||e.ctrlKey||e.altKey||!picture())return
      const target=e.target
      if(target instanceof HTMLElement&&(target.isContentEditable||/^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName)))return
      const factor=e.key==="+"||e.key==="="?KEY_STEP:e.key==="-"?1/KEY_STEP:null
      if(factor===null&&e.key!=="0")return
      e.preventDefault()
      set(factor===null?FIT:zoomBy(shown,factor,{x:0,y:0},measure()),true)
    }
    const loaded=(e:Event)=>{if(e.target instanceof HTMLImageElement)reset()}
    const done=(e:TransitionEvent)=>{if(e.target instanceof HTMLImageElement)e.target.removeAttribute("data-animate")}
    const resized=new ResizeObserver(()=>{if(shown!==FIT)set(clamp(shown,measure()))})
    resized.observe(box)
    box.addEventListener("pointerdown",down)
    box.addEventListener("pointermove",move)
    box.addEventListener("pointerup",up)
    box.addEventListener("pointercancel",up)
    box.addEventListener("wheel",wheel,{passive:false})
    for(const type of ["gesturestart","gesturechange","gestureend"])box.addEventListener(type,gesture)
    box.addEventListener("load",loaded,true)
    box.addEventListener("transitionend",done)
    document.addEventListener("keydown",key)
    return()=>{
      resized.disconnect()
      box.removeEventListener("pointerdown",down)
      box.removeEventListener("pointermove",move)
      box.removeEventListener("pointerup",up)
      box.removeEventListener("pointercancel",up)
      box.removeEventListener("wheel",wheel)
      for(const type of ["gesturestart","gesturechange","gestureend"])box.removeEventListener(type,gesture)
      box.removeEventListener("load",loaded,true)
      box.removeEventListener("transitionend",done)
      document.removeEventListener("keydown",key)
    }
  },[box])
  return setBox
}
